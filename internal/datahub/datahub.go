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

// Requisição e cotação: só os campos que as tools usam. A descrição do item
// fica no produto; na requisição, às vezes só no complemento.
const requestFields = `
      id clientCode createdDate tenantId statusFk localEntrega
      deliveryAddress { placeId name city state }
      items { item quantity estimatedPrice currency measure complement status local product { description } }`

const rfqFields = `
      id clientCode createdDate tenantId status currency localEntrega
      deliveryAddress { placeId name city state }
      items { itemNumber quantity estimatedPrice canceled placeId product { description } }`

// queryDocumento monta a consulta de um documento (orders, requests, rfqs).
// campo é "id" ou "clientCode"; com tenant, acrescenta o filtro pela empresa
// dona do documento (tenantId, nas três entidades).
func queryDocumento(root, fields, campo, tipo string, comTenant bool, limite int) string {
	vars, filtro := "$v: "+tipo, campo+": { eq: $v }"
	if comTenant {
		vars += ", $t: Int"
		filtro += ", tenantId: { eq: $t }"
	}
	return fmt.Sprintf(`query(%s) { %s(filter: { %s }, limit: %d, offset: 0) { items {%s } } }`, vars, root, filtro, limite, fields)
}

// buscar procura um documento pelo número do ME (id) ou, se não achar, pelo
// número no cliente (clientCode), e decodifica o encontrado em out.
func (c *Client) buscar(ctx context.Context, root, fields, nome string, tenant *int, numero string, out any) error {
	numero = strings.TrimSpace(numero)
	if numero == "" {
		return fmt.Errorf("informe o número %s %s", artigo(nome, "do", "da"), nome)
	}
	tentar := func(campo, tipo string, v any, limite int) ([]json.RawMessage, error) {
		vars := map[string]any{"v": v}
		if tenant != nil {
			vars["t"] = *tenant
		}
		var r map[string]struct {
			Items []json.RawMessage `json:"items"`
		}
		if err := c.do(ctx, queryDocumento(root, fields, campo, tipo, tenant != nil, limite), vars, &r); err != nil {
			return nil, err
		}
		return r[root].Items, nil
	}
	// O id é Int no GraphQL: só tenta por id se couber em 32 bits.
	if n, err := strconv.ParseInt(numero, 10, 32); err == nil && n > 0 {
		items, err := tentar("id", "Int", n, 2)
		if err != nil {
			return err
		}
		if len(items) > 0 {
			return json.Unmarshal(items[0], out)
		}
	}
	items, err := tentar("clientCode", "String", numero, 5)
	if err != nil {
		return err
	}
	switch len(items) {
	case 0:
		return ErrNaoEncontrado
	case 1:
		return json.Unmarshal(items[0], out)
	}
	ids := make([]string, 0, len(items))
	for _, raw := range items {
		var x struct {
			ID ID `json:"id"`
		}
		_ = json.Unmarshal(raw, &x)
		ids = append(ids, string(x.ID))
	}
	return fmt.Errorf("há mais de %s %s com o código %q; informe o número no ME (%s)", artigo(nome, "um", "uma"), nome, numero, strings.Join(ids, ", "))
}

func artigo(nome, masc, fem string) string {
	if nome == "pedido" {
		return masc
	}
	return fem
}

const queryLocais = `query($t: Int, $p: [Int!]) { addresses(filter: { id: { eq: $t }, placeId: { in: $p } }, limit: 100, offset: 0) { items { placeId name city state } } }`

// Order busca um pedido pelo número do ME (Pedidos.Pedido) ou, se não achar,
// pelo número do pedido no cliente (CodigoCliente).
//
// tenant é o IdMain da empresa do usuário e vira filtro da consulta. Passe nil
// só quando o filtro por tenant foi desligado de propósito.
func (c *Client) Order(ctx context.Context, tenant *int, numero string) (*Order, error) {
	var o Order
	if err := c.buscar(ctx, "orders", orderFields, "pedido", tenant, numero, &o); err != nil {
		return nil, err
	}
	return &o, nil
}

// Request busca uma requisição de compra e a devolve no formato de pedido: sem
// fornecedor, com o preço estimado, a data de criação no lugar da data de envio
// e os itens cancelados (status 6) marcados. Cancelado diz se a requisição toda
// foi cancelada (status do workflow 3).
func (c *Client) Request(ctx context.Context, tenant *int, numero string) (o *Order, cancelado bool, err error) {
	var r struct {
		ID              ID       `json:"id"`
		ClientCode      string   `json:"clientCode"`
		CreatedDate     string   `json:"createdDate"`
		TenantID        *int     `json:"tenantId"`
		StatusFk        *int     `json:"statusFk"`
		LocalEntrega    *int     `json:"localEntrega"`
		DeliveryAddress *Address `json:"deliveryAddress"`
		Items           []struct {
			Item           int      `json:"item"`
			Quantity       *float64 `json:"quantity"`
			EstimatedPrice *float64 `json:"estimatedPrice"`
			Currency       string   `json:"currency"`
			Measure        string   `json:"measure"`
			Complement     string   `json:"complement"`
			Status         *int     `json:"status"`
			Local          *int     `json:"local"`
			Product        *struct {
				Description string `json:"description"`
			} `json:"product"`
		} `json:"items"`
	}
	if err := c.buscar(ctx, "requests", requestFields, "requisição", tenant, numero, &r); err != nil {
		return nil, false, err
	}
	o = &Order{ID: r.ID, ClientCode: r.ClientCode, SentDate: r.CreatedDate, TenantID: r.TenantID, DeliveryPlaceID: r.LocalEntrega, DeliveryAddress: r.DeliveryAddress}
	for _, it := range r.Items {
		desc := ""
		if it.Product != nil {
			desc = it.Product.Description
		}
		o.Items = append(o.Items, Item{
			Item: it.Item, Description: descricao(desc, it.Complement), Quantity: it.Quantity, Price: it.EstimatedPrice,
			Currency: it.Currency, UnitOfMeasure: it.Measure, DeliveryPlaceID: it.Local,
			Canceled: ptrBool(it.Status != nil && *it.Status == 6),
		})
	}
	return o, r.StatusFk != nil && *r.StatusFk == 3, nil
}

// RFQ busca uma cotação e a devolve no formato de pedido (sem fornecedor, com o
// preço estimado e a data de criação no lugar da data de envio). Cancelada diz
// se a cotação foi cancelada (status 3).
func (c *Client) RFQ(ctx context.Context, tenant *int, numero string) (o *Order, cancelada bool, err error) {
	var r struct {
		ID              ID       `json:"id"`
		ClientCode      string   `json:"clientCode"`
		CreatedDate     string   `json:"createdDate"`
		TenantID        *int     `json:"tenantId"`
		Status          *int     `json:"status"`
		Currency        string   `json:"currency"`
		LocalEntrega    *int     `json:"localEntrega"`
		DeliveryAddress *Address `json:"deliveryAddress"`
		Items           []struct {
			ItemNumber     int      `json:"itemNumber"`
			Quantity       *float64 `json:"quantity"`
			EstimatedPrice *float64 `json:"estimatedPrice"`
			Canceled       *bool    `json:"canceled"`
			PlaceID        *int     `json:"placeId"`
			Product        *struct {
				Description string `json:"description"`
			} `json:"product"`
		} `json:"items"`
	}
	if err := c.buscar(ctx, "rfqs", rfqFields, "cotação", tenant, numero, &r); err != nil {
		return nil, false, err
	}
	o = &Order{ID: r.ID, ClientCode: r.ClientCode, SentDate: r.CreatedDate, TenantID: r.TenantID, Currency: r.Currency, DeliveryPlaceID: r.LocalEntrega, DeliveryAddress: r.DeliveryAddress}
	for _, it := range r.Items {
		desc := ""
		if it.Product != nil {
			desc = it.Product.Description
		}
		o.Items = append(o.Items, Item{
			Item: it.ItemNumber, Description: descricao(desc, ""), Quantity: it.Quantity, Price: it.EstimatedPrice,
			DeliveryPlaceID: it.PlaceID, Canceled: it.Canceled,
		})
	}
	return o, r.Status != nil && *r.Status == 3, nil
}

// descricao usa a do produto; vazia ou "0" (visto em Trunk), cai no complemento,
// sem as quebras de linha HTML.
func descricao(produto, complemento string) string {
	if p := strings.TrimSpace(produto); p != "" && p != "0" {
		return p
	}
	c := strings.Join(strings.Fields(strings.NewReplacer("<br>", " ", "<br/>", " ", "<br />", " ").Replace(complemento)), " ")
	if r := []rune(c); len(r) > 300 {
		c = string(r[:300]) + "…"
	}
	return c
}

func ptrBool(b bool) *bool { return &b }

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
