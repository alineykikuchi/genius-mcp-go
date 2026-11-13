// Package datahub lê pedidos no GraphQL do ME-DataHub (fonte "meweb").
//
// A chamada usa um token de serviço, obtido com client id e secret
// (client_credentials). Esse token NÃO tem tenant: o DataHub devolve dados de
// qualquer cliente. Por isso quem chama precisa informar o tenant do usuário, e
// este pacote o coloca como filtro em toda consulta.
//
// Variáveis de ambiente:
//
//	CLIENT_ID, CLIENT_SECRET  credenciais do client de serviço (obrigatórias)
//	DATAHUB_GRAPHQL_URL       GraphQL da fonte meweb (padrão: Trunk)
//	DATAHUB_FLOWS_URL         raiz dos flows REST (padrão: Trunk)
//	DATAHUB_TOKEN_URL         endpoint de token (padrão: Trunk)
package datahub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// URLPadrao é o GraphQL da fonte meweb em Trunk.
	URLPadrao = "https://trunk.datahub.me.com.br/_api/dp/meweb/graphql"
	// FlowsURLPadrao é a raiz dos flows REST do DataHub em Trunk.
	FlowsURLPadrao = "https://trunk.datahub.me.com.br/_api/flows"
	// TokenURLPadrao é o endpoint de token do DataHub em Trunk.
	TokenURLPadrao = "https://trunk.datahub.me.com.br/oauth/token"
)

// ErrNaoEncontrado: nenhum pedido com esse número para o tenant.
var ErrNaoEncontrado = errors.New("pedido não encontrado")

// ErrSemCredencial: CLIENT_ID ou CLIENT_SECRET não configurados.
var ErrSemCredencial = errors.New("CLIENT_ID e CLIENT_SECRET não estão configurados no servidor")

type Client struct {
	URL          string
	FlowsURL     string
	TokenURL     string
	ClientID     string
	ClientSecret string
	HTTP         *http.Client

	mu     sync.Mutex
	tok    string
	tokExp time.Time
}

// NewFromEnv monta o cliente a partir das variáveis de ambiente.
func NewFromEnv() *Client {
	return &Client{
		URL:          envOr("DATAHUB_GRAPHQL_URL", URLPadrao),
		FlowsURL:     envOr("DATAHUB_FLOWS_URL", FlowsURLPadrao),
		TokenURL:     envOr("DATAHUB_TOKEN_URL", TokenURLPadrao),
		ClientID:     os.Getenv("CLIENT_ID"),
		ClientSecret: os.Getenv("CLIENT_SECRET"),
		HTTP:         &http.Client{Timeout: 30 * time.Second},
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// ID aceita número ou string no JSON (o DataHub devolve chaves como string).
type ID string

func (i *ID) UnmarshalJSON(b []byte) error {
	*i = ID(strings.Trim(string(b), `"`))
	if *i == "null" {
		*i = ""
	}
	return nil
}

type Order struct {
	ID              ID       `json:"id"`
	ClientCode      string   `json:"clientCode"`
	SentDate        string   `json:"sentDate"`
	Currency        string   `json:"currency"`
	Total           *float64 `json:"total"`
	RefusalStatus   *int     `json:"refusalStatus"`
	Incoterms       string   `json:"incoterms"`
	TenantID        *int     `json:"tenantId"`
	DeliveryPlaceID *int     `json:"deliveryPlaceId"`
	Supplier        *Party   `json:"supplier"`
	DeliveryAddress *Address `json:"deliveryAddress"`
	Items           []Item   `json:"items"`
}

type Party struct {
	ID          ID     `json:"id"`
	Name        string `json:"name"`
	CompanyName string `json:"companyName"`
	City        string `json:"city"`
	State       string `json:"state"`
	Country     string `json:"country"`
}

type Address struct {
	PlaceID *int   `json:"placeId"`
	Name    string `json:"name"`
	City    string `json:"city"`
	State   string `json:"state"`
}

type Item struct {
	Item                  int      `json:"item"`
	Code                  string   `json:"code"`
	Description           string   `json:"description"`
	DescriptionComplement string   `json:"descriptionComplement"`
	Currency              string   `json:"currency"`
	Price                 *float64 `json:"price"`
	Quantity              *float64 `json:"quantity"`
	UnitOfMeasure         string   `json:"unitOfMeasure"`
	Total                 *float64 `json:"total"`
	Canceled              *bool    `json:"canceled"`
	PurchaseGroup         string   `json:"purchaseGroup"`
	DeliveryPlaceID       *int     `json:"deliveryPlaceId"`
}

const orderFields = `
      id clientCode sentDate currency total refusalStatus incoterms tenantId deliveryPlaceId
      supplier { id name companyName city state country }
      deliveryAddress { placeId name city state }
      items {
        item code description descriptionComplement currency price quantity
        unitOfMeasure total canceled purchaseGroup deliveryPlaceId
      }`

// queryPedidos monta a consulta de pedidos. campo é "id" ou "clientCode"; com
// tenant, acrescenta o filtro pelo comprador do pedido (tenantId).
func queryPedidos(campo, tipo string, comTenant bool, limite int) string {
	vars, filtro := "$v: "+tipo, campo+": { eq: $v }"
	if comTenant {
		vars += ", $t: Int"
		filtro += ", tenantId: { eq: $t }"
	}
	return fmt.Sprintf(`query(%s) { orders(filter: { %s }, limit: %d, offset: 0) { items {%s } } }`, vars, filtro, limite, orderFields)
}

const queryLocais = `query($t: Int, $p: [Int!]) { addresses(filter: { id: { eq: $t }, placeId: { in: $p } }, limit: 100, offset: 0) { items { placeId name city state } } }`

// Order busca um pedido pelo número do ME (Pedidos.Pedido) ou, se não achar,
// pelo número do pedido no cliente (CodigoCliente).
//
// tenant é o IdMain da empresa do usuário e vira filtro da consulta. Passe nil
// só quando o filtro por tenant foi desligado de propósito.
func (c *Client) Order(ctx context.Context, tenant *int, numero string) (*Order, error) {
	numero = strings.TrimSpace(numero)
	if numero == "" {
		return nil, fmt.Errorf("informe o número do pedido")
	}
	type resp struct {
		Orders struct {
			Items []Order `json:"items"`
		} `json:"orders"`
	}
	vars := func(v any) map[string]any {
		m := map[string]any{"v": v}
		if tenant != nil {
			m["t"] = *tenant
		}
		return m
	}
	// O id é Int no GraphQL: só tenta por id se couber em 32 bits.
	if n, err := strconv.ParseInt(numero, 10, 32); err == nil && n > 0 {
		var r resp
		if err := c.do(ctx, queryPedidos("id", "Int", tenant != nil, 2), vars(n), &r); err != nil {
			return nil, err
		}
		if len(r.Orders.Items) > 0 {
			return &r.Orders.Items[0], nil
		}
	}
	var r resp
	if err := c.do(ctx, queryPedidos("clientCode", "String", tenant != nil, 5), vars(numero), &r); err != nil {
		return nil, err
	}
	switch len(r.Orders.Items) {
	case 0:
		return nil, ErrNaoEncontrado
	case 1:
		return &r.Orders.Items[0], nil
	}
	ids := make([]string, 0, len(r.Orders.Items))
	for _, o := range r.Orders.Items {
		ids = append(ids, string(o.ID))
	}
	return nil, fmt.Errorf("há mais de um pedido com o código %q; informe o número do pedido no ME (%s)", numero, strings.Join(ids, ", "))
}

// Addresses busca locais do tenant pelo número do local. A chave de Locais é
// composta (ID do tenant + Local): o número sozinho não identifica o local.
func (c *Client) Addresses(ctx context.Context, tenantID int, placeIDs []int) (map[int]Address, error) {
	out := map[int]Address{}
	if len(placeIDs) == 0 {
		return out, nil
	}
	var r struct {
		Addresses struct {
			Items []Address `json:"items"`
		} `json:"addresses"`
	}
	if err := c.do(ctx, queryLocais, map[string]any{"t": tenantID, "p": placeIDs}, &r); err != nil {
		return nil, err
	}
	for _, a := range r.Addresses.Items {
		if a.PlaceID != nil {
			out[*a.PlaceID] = a
		}
	}
	return out, nil
}

// serviceToken devolve o token de serviço, renovando quando falta pouco para
// vencer. O token nunca é gravado em log.
func (c *Client) serviceToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tok != "" && time.Now().Before(c.tokExp) {
		return c.tok, nil
	}
	if c.ClientID == "" || c.ClientSecret == "" {
		return "", ErrSemCredencial
	}
	// As credenciais vão no corpo: o DataHub recusa client_credentials via Basic.
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.ClientID},
		"client_secret": {c.ClientSecret},
		"scope":         {"openid"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("não foi possível obter o token do DataHub: endpoint de token indisponível")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var body struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Error       string `json:"error"`
	}
	_ = json.Unmarshal(raw, &body)
	if resp.StatusCode != http.StatusOK || body.AccessToken == "" {
		if body.Error != "" {
			return "", fmt.Errorf("o DataHub recusou CLIENT_ID/CLIENT_SECRET (HTTP %d, %s)", resp.StatusCode, body.Error)
		}
		return "", fmt.Errorf("o DataHub não emitiu token (HTTP %d)", resp.StatusCode)
	}
	if body.ExpiresIn <= 0 {
		body.ExpiresIn = 60
	}
	c.tok = body.AccessToken
	// Renova 30 s antes de vencer.
	c.tokExp = time.Now().Add(time.Duration(body.ExpiresIn)*time.Second - 30*time.Second)
	return c.tok, nil
}

func (c *Client) esquecerToken() {
	c.mu.Lock()
	c.tok = ""
	c.mu.Unlock()
}

// post envia um JSON ao DataHub com o token de serviço e devolve o status e o
// corpo da resposta.
func (c *Client) post(ctx context.Context, endereco string, body []byte) (int, []byte, error) {
	var status int
	var raw []byte
	// Se o DataHub recusar o token em cache, pede um novo e tenta mais uma vez.
	for tentativa := 0; tentativa < 2; tentativa++ {
		tok, err := c.serviceToken(ctx)
		if err != nil {
			return 0, nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endereco, bytes.NewReader(body))
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Authorization", "Bearer "+tok)

		resp, err := c.HTTP.Do(req)
		if err != nil {
			return 0, nil, fmt.Errorf("DataHub indisponível: %w", err)
		}
		raw, _ = io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		status = resp.StatusCode
		if status != http.StatusUnauthorized {
			break
		}
		c.esquecerToken()
	}
	return status, raw, nil
}

func (c *Client) do(ctx context.Context, query string, vars map[string]any, out any) error {
	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})

	status, raw, err := c.post(ctx, c.URL, body)
	if err != nil {
		return err
	}

	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return fmt.Errorf("o DataHub recusou o token de serviço (HTTP %d)", status)
	}
	if status != http.StatusOK {
		return fmt.Errorf("o DataHub respondeu HTTP %d", status)
	}
	var env struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("o DataHub devolveu uma resposta que não é JSON (em manutenção?)")
	}
	if len(env.Errors) > 0 {
		return fmt.Errorf("erro do DataHub: %s", env.Errors[0].Message)
	}
	return json.Unmarshal(env.Data, out)
}
