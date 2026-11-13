// Package datahub lê pedidos no GraphQL do ME-DataHub (fonte "meweb").
//
// A chamada leva o token do usuário que o Genius repassou ao MCP. O DataHub
// filtra a leitura pelo tenant desse token, então a tool só enxerga pedidos da
// empresa do usuário: o tenant nunca é parâmetro.
package datahub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// URLPadrao é o GraphQL da fonte meweb em Trunk. Troque com DATAHUB_GRAPHQL_URL.
const URLPadrao = "https://trunk.datahub.me.com.br/_api/dp/meweb/graphql"

// ErrNaoEncontrado: nenhum pedido com esse número visível para o usuário.
var ErrNaoEncontrado = errors.New("pedido não encontrado")

type Client struct {
	URL  string
	HTTP *http.Client
}

// NewFromEnv monta o cliente a partir de DATAHUB_GRAPHQL_URL.
func NewFromEnv() *Client {
	url := os.Getenv("DATAHUB_GRAPHQL_URL")
	if url == "" {
		url = URLPadrao
	}
	return &Client{URL: url, HTTP: &http.Client{Timeout: 30 * time.Second}}
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

const queryPorID = `query($v: Int) { orders(filter: { id: { eq: $v } }, limit: 2, offset: 0) { items {` + orderFields + ` } } }`
const queryPorCodigo = `query($v: String) { orders(filter: { clientCode: { eq: $v } }, limit: 5, offset: 0) { items {` + orderFields + ` } } }`
const queryLocais = `query($t: Int, $p: [Int!]) { addresses(filter: { id: { eq: $t }, placeId: { in: $p } }, limit: 100, offset: 0) { items { placeId name city state } } }`

// Order busca um pedido pelo número do ME (Pedidos.Pedido) ou, se não achar,
// pelo número do pedido no cliente (CodigoCliente).
func (c *Client) Order(ctx context.Context, token, numero string) (*Order, error) {
	numero = strings.TrimSpace(numero)
	if numero == "" {
		return nil, fmt.Errorf("informe o número do pedido")
	}
	type resp struct {
		Orders struct {
			Items []Order `json:"items"`
		} `json:"orders"`
	}
	// O id é Int no GraphQL: só tenta por id se couber em 32 bits.
	if n, err := strconv.ParseInt(numero, 10, 32); err == nil && n > 0 {
		var r resp
		if err := c.do(ctx, token, queryPorID, map[string]any{"v": n}, &r); err != nil {
			return nil, err
		}
		if len(r.Orders.Items) > 0 {
			return &r.Orders.Items[0], nil
		}
	}
	var r resp
	if err := c.do(ctx, token, queryPorCodigo, map[string]any{"v": numero}, &r); err != nil {
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
func (c *Client) Addresses(ctx context.Context, token string, tenantID int, placeIDs []int) (map[int]Address, error) {
	out := map[int]Address{}
	if len(placeIDs) == 0 {
		return out, nil
	}
	var r struct {
		Addresses struct {
			Items []Address `json:"items"`
		} `json:"addresses"`
	}
	if err := c.do(ctx, token, queryLocais, map[string]any{"t": tenantID, "p": placeIDs}, &r); err != nil {
		return nil, err
	}
	for _, a := range r.Addresses.Items {
		if a.PlaceID != nil {
			out[*a.PlaceID] = a
		}
	}
	return out, nil
}

func (c *Client) do(ctx context.Context, token, query string, vars map[string]any, out any) error {
	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("DataHub indisponível: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("o DataHub recusou o token do usuário (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("o DataHub respondeu HTTP %d", resp.StatusCode)
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
