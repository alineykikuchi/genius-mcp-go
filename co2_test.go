package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"genius-mcp-go/internal/cambio"
)

// Pedido no formato que o DataHub devolve (copiado de uma leitura real de Trunk,
// com valores trocados). Usado só para testar o encadeamento das tools.
const pedidoFake = `{"data":{"orders":{"items":[{
  "id":"29594155","clientCode":"4502531314","sentDate":"2026-03-10T08:22:49Z","currency":"BRL",
  "total":5614.30,"refusalStatus":0,"incoterms":"FCA","tenantId":1920633,"deliveryPlaceId":4,
  "supplier":{"id":"2315944","name":"FORNECEDOR X","companyName":"FORNECEDOR X LTDA","city":"Guarulhos","state":"SP","country":"BR"},
  "deliveryAddress":{"placeId":4,"name":"FABRICA","city":"OTACÍLIO COSTA","state":"SC"},
  "items":[
    {"item":10,"code":"166005","description":"ROTAMETRO NORMAL ROSCA NPTF 1/2","currency":null,"price":1871.43,"quantity":3,"unitOfMeasure":"CDA","total":5614.30,"canceled":false,"deliveryPlaceId":null},
    {"item":20,"code":"X","description":"ITEM CANCELADO","price":10,"quantity":1,"total":10,"canceled":true,"deliveryPlaceId":null}
  ]}]}}}`

const (
	clientIDFake     = "client-de-teste"
	clientSecretFake = "secret-de-teste"
	tokenServicoFake = "token-de-servico-fake"
)

// dataHubFake simula o endpoint de token e o GraphQL do DataHub, e guarda o que
// recebeu para os testes conferirem.
type dataHubFake struct {
	*httptest.Server
	mu          sync.Mutex
	tokens      int      // quantas vezes o token foi pedido
	consultas   []string // corpos das chamadas GraphQL
	formDoToken string
}

func novoDataHubFake(t *testing.T) *dataHubFake {
	t.Helper()
	f := &dataHubFake{}
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		f.tokens++
		f.formDoToken = r.Form.Encode()
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("client_id") != clientIDFake || r.Form.Get("client_secret") != clientSecretFake {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"` + tokenServicoFake + `","expires_in":300,"token_type":"Bearer"}`))
	})
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		// O DataHub só aceita o token de serviço, nunca o token do usuário.
		if r.Header.Get("Authorization") != "Bearer "+tokenServicoFake {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.consultas = append(f.consultas, string(body))
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		// A tool tenta por id (número) e depois por código do cliente (string).
		if strings.Contains(string(body), `"v":404`) || strings.Contains(string(body), `"v":"404"`) {
			_, _ = w.Write([]byte(`{"data":{"orders":{"items":[]}}}`))
			return
		}
		_, _ = w.Write([]byte(pedidoFake))
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)

	t.Setenv("DATAHUB_GRAPHQL_URL", f.URL+"/graphql")
	t.Setenv("DATAHUB_TOKEN_URL", f.URL+"/oauth/token")
	t.Setenv("CLIENT_ID", clientIDFake)
	t.Setenv("CLIENT_SECRET", clientSecretFake)
	t.Setenv("TENANT_CLAIM", "")
	t.Setenv("TENANT_FILTRO", "")
	return f
}

func fakePTAX(t *testing.T) {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"value":[{"cotacaoVenda":5.6143,"dataHoraCotacao":"2026-03-10 13:08:21.353"}]}`))
	}))
	t.Cleanup(ts.Close)
	old := cambio.BaseURL
	cambio.BaseURL = ts.URL
	t.Cleanup(func() { cambio.BaseURL = old })
}

// jwtDoUsuario simula o token que o Genius repassa, com as claims dadas.
func jwtDoUsuario(claims map[string]any) http.Header {
	claims["sub"] = "123"
	claims["exp"] = time.Now().Add(time.Hour).Unix()
	h := http.Header{}
	h.Set("Authorization", "Bearer "+fakeJWT(claims))
	return h
}

func erroDaTool(t *testing.T, cs *mcp.ClientSession, name string, args any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return err.Error()
	}
	if !res.IsError {
		t.Fatalf("%s deveria ter falhado: %+v", name, res.StructuredContent)
	}
	b, _ := json.Marshal(res.Content)
	return string(b)
}

func TestEstimativaCO2PontaAPonta(t *testing.T) {
	dh := novoDataHubFake(t)
	fakePTAX(t)
	ts := newTestServer(t, "require")
	cs := connect(t, ts.URL, jwtDoUsuario(map[string]any{"tenantId": 1920633}))

	// Passo 1: itens (o cancelado fica de fora).
	p1 := callJSON(t, cs, "esg_itens_do_pedido", map[string]any{"pedido": "29594155"})
	itens := p1["itens"].([]any)
	if len(itens) != 1 || p1["itens_cancelados_ignorados"].(float64) != 1 {
		t.Fatalf("passo 1: %v", p1)
	}

	// Passo 2: NAICS 334513 (instrumentos de medição industrial), 2 kg por unidade.
	p2 := callJSON(t, cs, "esg_estimar_co2_do_pedido", map[string]any{
		"pedido": "29594155",
		"itens":  []map[string]any{{"item": 10, "naics": "334513", "peso_kg_por_unidade": 2.0, "confianca": 0.8}},
	})
	it := p2["itens"].([]any)[0].(map[string]any)
	// R$ 5.614,30 / 5,6143 = US$ 1.000 → co2_produto = 1000 × fator do NAICS.
	prod := it["co2_produto_kg"].(float64)
	if prod <= 0 || prod > 2000 {
		t.Fatalf("co2_produto fora do esperado: %v", it)
	}
	// Guarulhos/SP → Otacílio Costa/SC: ~530 km em linha reta, ~690 com sinuosidade.
	km := it["distancia_km"].(float64)
	if km < 550 || km > 850 {
		t.Fatalf("distância fora do esperado: %v", km)
	}
	// 3 un × 2 kg = 0,006 t × km × 0,10.
	transp := it["co2_transporte_kg"].(float64)
	if esperado := 0.006 * km * 0.10; transp < esperado-0.01 || transp > esperado+0.01 {
		t.Fatalf("co2_transporte: veio %v, esperava ~%v", transp, esperado)
	}
	tot := p2["co2"].(map[string]any)
	if got := tot["total_kg"].(float64); got < prod+transp-0.02 || got > prod+transp+0.02 {
		t.Fatalf("total não fecha: %v", tot)
	}
	pesos := p2["pesos"].(map[string]any)
	if pesos["custo"].(float64) != 0.7 || pesos["co2"].(float64) != 0.3 {
		t.Fatalf("pesos padrão: %v", pesos)
	}
	if len(p2["premissas"].([]any)) < 5 {
		t.Fatalf("premissas faltando: %v", p2["premissas"])
	}

	// NAICS que não existe: não calcula o produto e devolve candidatos.
	p3 := callJSON(t, cs, "esg_estimar_co2_do_pedido", map[string]any{
		"pedido": "29594155",
		"itens":  []map[string]any{{"item": 10, "naics": "334599", "peso_kg_por_unidade": 2.0, "confianca": 0.4}},
	})
	it3 := p3["itens"].([]any)[0].(map[string]any)
	if it3["naics_invalido"] != true || it3["co2_produto_kg"] != nil || len(it3["naics_candidatos"].([]any)) == 0 {
		t.Fatalf("naics inválido: %v", it3)
	}
	if it3["baixa_confianca"] != true {
		t.Fatalf("confiança 0,4 deveria marcar baixa confiança: %v", it3)
	}

	dh.mu.Lock()
	defer dh.mu.Unlock()
	// Três chamadas de tool, um token só: o token de serviço fica em cache.
	if dh.tokens != 1 {
		t.Fatalf("esperava 1 pedido de token, foram %d", dh.tokens)
	}
	// Toda consulta de pedido leva o tenant do usuário como filtro.
	for _, c := range dh.consultas {
		if !strings.Contains(c, "tenantId: { eq: $t }") || !strings.Contains(c, `"t":1920633`) {
			t.Fatalf("consulta sem filtro de tenant: %s", c)
		}
	}
}

func TestPedidoNaoEncontradoEPesosInvalidos(t *testing.T) {
	novoDataHubFake(t)
	fakePTAX(t)
	ts := newTestServer(t, "require")
	cs := connect(t, ts.URL, jwtDoUsuario(map[string]any{"tenantId": "1920633"})) // tenant como string também vale

	if msg := erroDaTool(t, cs, "esg_itens_do_pedido", map[string]any{"pedido": "404"}); !strings.Contains(msg, "não encontrado") {
		t.Fatalf("mensagem: %s", msg)
	}
	if msg := erroDaTool(t, cs, "esg_estimar_co2_do_pedido", map[string]any{
		"pedido": "29594155", "itens": []map[string]any{}, "peso_custo": 0.9, "peso_co2": 0.9,
	}); !strings.Contains(msg, "pesos inválidos") {
		t.Fatalf("mensagem: %s", msg)
	}
}

func TestSemTenantNoTokenNaoConsulta(t *testing.T) {
	dh := novoDataHubFake(t)
	ts := newTestServer(t, "require")
	cs := connect(t, ts.URL, jwtDoUsuario(map[string]any{"email": "x@exemplo.com"}))

	if msg := erroDaTool(t, cs, "esg_itens_do_pedido", map[string]any{"pedido": "29594155"}); !strings.Contains(msg, "TENANT_CLAIM") {
		t.Fatalf("mensagem: %s", msg)
	}
	dh.mu.Lock()
	defer dh.mu.Unlock()
	if len(dh.consultas) != 0 || dh.tokens != 0 {
		t.Fatalf("não deveria ter chamado o DataHub: %d consultas, %d tokens", len(dh.consultas), dh.tokens)
	}
}

func TestTenantClaimConfiguravelEFiltroDesligado(t *testing.T) {
	dh := novoDataHubFake(t)

	// Claim com outro nome, indicada por TENANT_CLAIM.
	t.Setenv("TENANT_CLAIM", "empresa")
	ts := newTestServer(t, "require")
	cs := connect(t, ts.URL, jwtDoUsuario(map[string]any{"empresa": 777}))
	callJSON(t, cs, "esg_itens_do_pedido", map[string]any{"pedido": "29594155"})
	dh.mu.Lock()
	if n := len(dh.consultas); n != 1 || !strings.Contains(dh.consultas[0], `"t":777`) {
		t.Fatalf("esperava filtro pelo tenant 777: %v", dh.consultas)
	}
	dh.consultas = nil
	dh.mu.Unlock()

	// Filtro desligado de propósito: consulta sem tenant.
	t.Setenv("TENANT_FILTRO", "off")
	ts2 := newTestServer(t, "require")
	cs2 := connect(t, ts2.URL, jwtDoUsuario(map[string]any{"email": "x@exemplo.com"}))
	callJSON(t, cs2, "esg_itens_do_pedido", map[string]any{"pedido": "29594155"})
	dh.mu.Lock()
	defer dh.mu.Unlock()
	if n := len(dh.consultas); n != 1 || strings.Contains(dh.consultas[0], "tenantId: { eq") {
		t.Fatalf("com o filtro desligado a consulta não leva tenant: %v", dh.consultas)
	}
}

func TestSemCredencialDeServico(t *testing.T) {
	novoDataHubFake(t)
	t.Setenv("CLIENT_SECRET", "")
	ts := newTestServer(t, "require")
	cs := connect(t, ts.URL, jwtDoUsuario(map[string]any{"tenantId": 1920633}))

	msg := erroDaTool(t, cs, "esg_itens_do_pedido", map[string]any{"pedido": "29594155"})
	if !strings.Contains(msg, "CLIENT_ID e CLIENT_SECRET") {
		t.Fatalf("mensagem: %s", msg)
	}
}
