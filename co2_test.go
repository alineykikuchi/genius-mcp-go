package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func fakeDataHub(t *testing.T, tokenEsperado string) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+tokenEsperado {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		// A tool tenta por id (número) e depois por código do cliente (string).
		if strings.Contains(string(body), `"v":404`) || strings.Contains(string(body), `"v":"404"`) {
			_, _ = w.Write([]byte(`{"data":{"orders":{"items":[]}}}`))
			return
		}
		_, _ = w.Write([]byte(pedidoFake))
	}))
	t.Cleanup(ts.Close)
	return ts
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

func TestEstimativaCO2PontaAPonta(t *testing.T) {
	const tok = "tokenopaco1234567890"
	dh := fakeDataHub(t, tok)
	fakePTAX(t)
	t.Setenv("DATAHUB_GRAPHQL_URL", dh.URL)

	ts := newTestServer(t, "require")
	h := http.Header{}
	h.Set("Authorization", "Bearer "+tok)
	cs := connect(t, ts.URL, h)

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
}

func TestPedidoNaoEncontradoEPesosInvalidos(t *testing.T) {
	const tok = "tokenopaco1234567890"
	dh := fakeDataHub(t, tok)
	fakePTAX(t)
	t.Setenv("DATAHUB_GRAPHQL_URL", dh.URL)

	ts := newTestServer(t, "require")
	h := http.Header{}
	h.Set("Authorization", "Bearer "+tok)
	cs := connect(t, ts.URL, h)

	erro := func(name string, args any) string {
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

	if msg := erro("esg_itens_do_pedido", map[string]any{"pedido": "404"}); !strings.Contains(msg, "não encontrado") {
		t.Fatalf("mensagem: %s", msg)
	}
	if msg := erro("esg_estimar_co2_do_pedido", map[string]any{
		"pedido": "29594155", "itens": []map[string]any{}, "peso_custo": 0.9, "peso_co2": 0.9,
	}); !strings.Contains(msg, "pesos inválidos") {
		t.Fatalf("mensagem: %s", msg)
	}
}
