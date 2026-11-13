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
	// pedidoEmDolar: número que faz o DataHub falso devolver o pedido em USD.
	pedidoEmDolar = "777001"
	// pedidoDoisItens: número que faz o DataHub falso devolver o pedido com o
	// segundo item ativo (dois itens no total).
	pedidoDoisItens = "777002"
)

// dataHubFake simula o endpoint de token e o GraphQL do DataHub, e guarda o que
// recebeu para os testes conferirem.
type dataHubFake struct {
	*httptest.Server
	mu          sync.Mutex
	tokens      int      // quantas vezes o token foi pedido
	consultas   []string // corpos das chamadas GraphQL
	flows       []string // corpos das chamadas ao flow do recomendador
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
		switch {
		case strings.Contains(string(body), `"v":`+pedidoEmDolar) || strings.Contains(string(body), `"v":"`+pedidoEmDolar+`"`):
			_, _ = w.Write([]byte(strings.Replace(pedidoFake, `"currency":"BRL"`, `"currency":"USD"`, 1)))
		case strings.Contains(string(body), `"v":`+pedidoDoisItens) || strings.Contains(string(body), `"v":"`+pedidoDoisItens+`"`):
			_, _ = w.Write([]byte(strings.Replace(pedidoFake, `"canceled":true`, `"canceled":false`, 1)))
		case strings.Contains(string(body), "requests("):
			_, _ = w.Write([]byte(requisicaoFake))
		case strings.Contains(string(body), "rfqs("):
			_, _ = w.Write([]byte(cotacaoFake))
		default:
			_, _ = w.Write([]byte(pedidoFake))
		}
	})
	mux.HandleFunc("/flows/recomendacao-fornecedores/recomendar", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+tokenServicoFake {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.flows = append(f.flows, string(body))
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(recomendacaoFake))
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)

	t.Setenv("DATAHUB_FLOWS_URL", f.URL+"/flows")
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
	p1 := callJSON(t, cs, "esg_itens_do_pedido", map[string]any{"numero": "29594155"})
	itens := p1["itens"].([]any)
	if len(itens) != 1 || p1["itens_cancelados_ignorados"].(float64) != 1 {
		t.Fatalf("passo 1: %v", p1)
	}

	// Passo 2: NAICS 334513 (instrumentos de medição industrial), 2 kg por unidade.
	p2 := callJSON(t, cs, "esg_estimar_co2_do_pedido", map[string]any{
		"numero": "29594155",
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
		"numero": "29594155",
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

func TestEstimativaTrazOValorDoCarbono(t *testing.T) {
	novoDataHubFake(t)
	fakePTAX(t)
	ts := newTestServer(t, "require")
	cs := connect(t, ts.URL, jwtDoUsuario(map[string]any{"tenantId": 1920633}))
	perto := func(a, b float64) bool { return a > b-0.011 && a < b+0.011 }
	estimar := func(numero string, confianca float64, extra map[string]any) map[string]any {
		args := map[string]any{
			"numero": numero,
			"itens":  []map[string]any{{"item": 10, "naics": "334513", "peso_kg_por_unidade": 2.0, "confianca": confianca}},
		}
		for k, v := range extra {
			args[k] = v
		}
		return callJSON(t, cs, "esg_estimar_co2_do_pedido", args)
	}

	// Preço padrão: carbono = total_kg / 1000 × 500, e o custo com carbono usa o
	// valor do documento, que está em reais.
	r := estimar("29594155", 0.8, nil)
	total := r["co2"].(map[string]any)["total_kg"].(float64)
	c := r["carbono"].(map[string]any)
	if !perto(c["custo_carbono_brl"].(float64), total/1000*500) || c["co2e_kg"].(float64) != total {
		t.Fatalf("custo do carbono: %v (total_kg %v)", c, total)
	}
	if !perto(c["custo_total_brl"].(float64), 5614.30+c["custo_carbono_brl"].(float64)) || c["valor_pedido_fmt"] != "R$ 5.614,30" {
		t.Fatalf("custo com carbono: %v", c)
	}
	if c["preco_carbono"].(map[string]any)["origem"] != "padrao" || c["custo_carbono_fmt"] == "" || c["peso_carbono_fmt"] == "" {
		t.Fatalf("bloco carbono: %v", c)
	}
	s, _ := r["como_responder"].(string)
	if !strings.Contains(s, "carbono.custo_carbono_fmt") || strings.Contains(s, "confiança baixa") {
		t.Fatalf("como_responder: %q", s)
	}
	if !contemTexto(r["premissas"], "não é cobrado") || !contemTexto(r["premissas"], "R$ 500/t") {
		t.Fatalf("premissas de preço: %v", r["premissas"])
	}

	// Preço informado pelo usuário.
	u := estimar("29594155", 0.8, map[string]any{"preco_carbono_brl_t": 200})
	cu := u["carbono"].(map[string]any)
	if cu["preco_carbono"].(map[string]any)["origem"] != "usuario" || !perto(cu["custo_carbono_brl"].(float64), total/1000*200) {
		t.Fatalf("preço informado: %v", cu)
	}

	// Documento em dólar: custo do carbono sem custo com carbono nem percentual.
	d := estimar(pedidoEmDolar, 0.8, nil)
	cd := d["carbono"].(map[string]any)
	if cd["custo_carbono_brl"].(float64) <= 0 {
		t.Fatalf("documento em dólar: %v", cd)
	}
	for _, campo := range []string{"custo_total_brl", "valor_pedido_brl", "peso_carbono_percentual"} {
		if _, tem := cd[campo]; tem {
			t.Fatalf("documento em dólar não devia trazer %s: %v", campo, cd)
		}
	}
	if !contemTexto(d["premissas"], "não está em reais") {
		t.Fatalf("premissa de moeda: %v", d["premissas"])
	}

	// Item com baixa confiança: como_responder manda dizer isso.
	b := estimar("29594155", 0.4, nil)
	if s, _ := b["como_responder"].(string); !strings.Contains(s, "confiança baixa") {
		t.Fatalf("como_responder sem a parte de baixa confiança: %q", s)
	}

	// Preço negativo: erro da tool.
	if msg := erroDaTool(t, cs, "esg_estimar_co2_do_pedido", map[string]any{
		"numero": "29594155", "itens": []map[string]any{}, "preco_carbono_brl_t": -10,
	}); !strings.Contains(msg, "reais por tonelada") {
		t.Fatalf("mensagem: %s", msg)
	}
}

func TestEstimativaDevolveOResumo(t *testing.T) {
	novoDataHubFake(t)
	fakePTAX(t)
	ts := newTestServer(t, "require")
	cs := connect(t, ts.URL, jwtDoUsuario(map[string]any{"tenantId": 1920633}))
	item := func(n int, confianca float64) map[string]any {
		return map[string]any{"item": n, "naics": "334513", "peso_kg_por_unidade": 2.0, "confianca": confianca}
	}
	estimar := func(args map[string]any) map[string]any {
		return callJSON(t, cs, "esg_estimar_co2_do_pedido", args)
	}
	resumoDe := func(r map[string]any) map[string]any { return r["resumo"].(map[string]any) }
	// Vale para qualquer estimativa: resposta sem premissas, mas com elas na saída.
	conferirPremissas := func(nome string, r map[string]any) {
		t.Helper()
		if s, _ := r["como_responder"].(string); !strings.Contains(s, "NÃO mostre premissas") {
			t.Fatalf("%s: como_responder devia mandar não mostrar premissas: %q", nome, s)
		}
		if len(r["premissas"].([]any)) < 5 {
			t.Fatalf("%s: premissas deviam continuar na saída: %v", nome, r["premissas"])
		}
	}

	// Um item: sem item_principal, sem tabela, faixas padrão.
	um := estimar(map[string]any{"numero": "29594155", "itens": []map[string]any{item(10, 0.9)}})
	conferirPremissas("um item", um)
	ru := resumoDe(um)
	if _, tem := ru["item_principal"]; tem {
		t.Fatalf("um item só não devia trazer item_principal: %v", ru)
	}
	if _, tem := ru["falta_transporte"]; tem {
		t.Fatalf("pedido com fornecedor não devia trazer falta_transporte: %v", ru)
	}
	if s, _ := ru["total_fmt"].(string); !strings.HasSuffix(s, " kg") {
		t.Fatalf("total_fmt: %v", ru)
	}
	if f := ru["faixas"].(map[string]any); f["baixo_ate_pct"].(float64) != 1 || f["alto_a_partir_pct"].(float64) != 5 || f["origem"] != "padrao" {
		t.Fatalf("faixas padrão: %v", f)
	}
	if s := um["como_responder"].(string); !strings.Contains(s, "Não use tabela") || strings.Contains(s, "falta o transporte") {
		t.Fatalf("como_responder de um item: %q", s)
	}
	iu := um["itens"].([]any)[0].(map[string]any)
	prod, transp := iu["co2_produto_kg"].(float64), iu["co2_transporte_kg"].(float64)
	if kg := iu["co2e_kg"].(float64); kg < prod+transp-0.011 || kg > prod+transp+0.011 {
		t.Fatalf("co2e_kg do item: %v", iu)
	}
	if iu["valor_fmt"] != "R$ 5.614,30" || iu["pct_fmt"] != "100%" || iu["co2e_fmt"] != ru["total_fmt"] {
		t.Fatalf("campos prontos do item: %v", iu)
	}

	// Dois itens: o principal é o de maior CO2e e as partes somam 100.
	dois := estimar(map[string]any{"numero": pedidoDoisItens, "itens": []map[string]any{item(10, 0.9), item(20, 0.9)}})
	conferirPremissas("dois itens", dois)
	if p, _ := resumoDe(dois)["item_principal"].(map[string]any); p == nil || p["item"].(float64) != 10 || p["pct_fmt"] == "" {
		t.Fatalf("item_principal: %v", resumoDe(dois))
	}
	soma := 0.0
	for _, i := range dois["itens"].([]any) {
		soma += i.(map[string]any)["pct_do_total"].(float64)
	}
	if soma < 99.9 || soma > 100.1 {
		t.Fatalf("pct_do_total devia somar 100, somou %v", soma)
	}
	if s := dois["como_responder"].(string); !strings.Contains(s, "UMA tabela") || !strings.Contains(s, "resumo.item_principal") {
		t.Fatalf("como_responder de dois itens: %q", s)
	}

	// Veredito pelo peso do carbono sobre o valor: o preço por tonelada é
	// escolhido para o peso dar 0,5%, 3% e 8% de R$ 5.614,30.
	total := um["co2"].(map[string]any)["total_kg"].(float64)
	for _, c := range []struct {
		pct      float64
		veredito string
	}{{0.5, "baixo"}, {3, "medio"}, {8, "alto"}} {
		r := estimar(map[string]any{
			"numero": "29594155", "itens": []map[string]any{item(10, 0.9)},
			"preco_carbono_brl_t": c.pct / 100 * 5614.30 / (total / 1000),
		})
		if rr := resumoDe(r); rr["veredito"] != c.veredito || !strings.Contains(rr["veredito_base"].(string), "do valor da compra") {
			t.Fatalf("peso de %v%%: esperava %s, veio %v", c.pct, c.veredito, rr)
		}
	}

	// Documento em dólar: sem custo com carbono, sem veredito.
	usd := estimar(map[string]any{"numero": pedidoEmDolar, "itens": []map[string]any{item(10, 0.9)}})
	conferirPremissas("dólar", usd)
	if _, tem := resumoDe(usd)["veredito"]; tem {
		t.Fatalf("documento em dólar não devia ter veredito: %v", resumoDe(usd))
	}
	if _, tem := usd["itens"].([]any)[0].(map[string]any)["valor_fmt"]; tem {
		t.Fatalf("item em dólar não devia ter valor_fmt: %v", usd["itens"])
	}

	// Requisição sem fornecedor: falta o transporte, com motivo.
	rq := estimar(map[string]any{"tipo": "requisicao", "numero": "10955121", "itens": []map[string]any{item(10, 0.9)}})
	conferirPremissas("requisição", rq)
	if f, _ := resumoDe(rq)["falta_transporte"].(map[string]any); f == nil || !strings.Contains(f["motivo"].(string), "fornecedor") {
		t.Fatalf("falta_transporte: %v", resumoDe(rq))
	}
	if s := rq["como_responder"].(string); !strings.Contains(s, "falta o transporte") {
		t.Fatalf("como_responder sem o aviso do transporte: %q", s)
	}

	// Confiança do resumo.
	for _, c := range []struct {
		confianca float64
		esperado  string
	}{{0.9, "alta"}, {0.6, "media"}, {0.3, "baixa"}} {
		r := estimar(map[string]any{"numero": "29594155", "itens": []map[string]any{item(10, c.confianca)}})
		if got := resumoDe(r)["confianca"]; got != c.esperado {
			t.Fatalf("confiança %v: esperava %s, veio %v", c.confianca, c.esperado, got)
		}
	}
}

func TestFaixasDoCarbonoPorVariavel(t *testing.T) {
	novoDataHubFake(t)
	fakePTAX(t)
	faixas := func() map[string]any {
		cs := connect(t, newTestServer(t, "require").URL, jwtDoUsuario(map[string]any{"tenantId": 1920633}))
		r := callJSON(t, cs, "esg_estimar_co2_do_pedido", map[string]any{
			"numero": "29594155",
			"itens":  []map[string]any{{"item": 10, "naics": "334513", "peso_kg_por_unidade": 2.0, "confianca": 0.8}},
		})
		return r["resumo"].(map[string]any)["faixas"].(map[string]any)
	}

	t.Setenv("FAIXA_CARBONO_BAIXO_PCT", "2")
	t.Setenv("FAIXA_CARBONO_ALTO_PCT", "10")
	if f := faixas(); f["baixo_ate_pct"].(float64) != 2 || f["alto_a_partir_pct"].(float64) != 10 || f["origem"] != "padrao" {
		t.Fatalf("faixas por variável: %v", f)
	}

	// Baixo maior ou igual ao alto, ou valor inválido: volta ao padrão.
	for _, c := range [][2]string{{"10", "2"}, {"abc", "5"}} {
		t.Setenv("FAIXA_CARBONO_BAIXO_PCT", c[0])
		t.Setenv("FAIXA_CARBONO_ALTO_PCT", c[1])
		if f := faixas(); f["baixo_ate_pct"].(float64) != 1 || f["alto_a_partir_pct"].(float64) != 5 {
			t.Fatalf("faixas %v deviam voltar ao padrão: %v", c, f)
		}
	}
}

func TestPedidoNaoEncontradoEPesosInvalidos(t *testing.T) {
	novoDataHubFake(t)
	fakePTAX(t)
	ts := newTestServer(t, "require")
	cs := connect(t, ts.URL, jwtDoUsuario(map[string]any{"tenantId": "1920633"})) // tenant como string também vale

	if msg := erroDaTool(t, cs, "esg_itens_do_pedido", map[string]any{"numero": "404"}); !strings.Contains(msg, "não encontrado") {
		t.Fatalf("mensagem: %s", msg)
	}
	if msg := erroDaTool(t, cs, "esg_estimar_co2_do_pedido", map[string]any{
		"numero": "29594155", "itens": []map[string]any{}, "peso_custo": 0.9, "peso_co2": 0.9,
	}); !strings.Contains(msg, "pesos inválidos") {
		t.Fatalf("mensagem: %s", msg)
	}
}

func TestSemTenantNoTokenNaoConsulta(t *testing.T) {
	dh := novoDataHubFake(t)
	ts := newTestServer(t, "require")
	cs := connect(t, ts.URL, jwtDoUsuario(map[string]any{"email": "x@exemplo.com"}))

	if msg := erroDaTool(t, cs, "esg_itens_do_pedido", map[string]any{"numero": "29594155"}); !strings.Contains(msg, "TENANT_CLAIM") {
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
	callJSON(t, cs, "esg_itens_do_pedido", map[string]any{"numero": "29594155"})
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
	callJSON(t, cs2, "esg_itens_do_pedido", map[string]any{"numero": "29594155"})
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

	msg := erroDaTool(t, cs, "esg_itens_do_pedido", map[string]any{"numero": "29594155"})
	if !strings.Contains(msg, "CLIENT_ID e CLIENT_SECRET") {
		t.Fatalf("mensagem: %s", msg)
	}
}
