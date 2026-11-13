package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// Resposta do flow do recomendador, no formato visto em Trunk (valores trocados).
// O pedido fake entrega em Otacílio Costa/SC: Lages/SC fica perto, Manaus/AM
// longe, e "Cidade Teste" não existe (cai no ponto de referência da UF).
const recomendacaoFake = `{"jaVenderam":[
  {"posicao":1,"nomeFornecedor":"LONGE LTDA","cnpj":"11111111000111","uf":"AM","municipio":"Manaus","score":80,"qtdPedidos":9,"ultimoPedido":"2026-10-01","prazoRealMedioDias":12.5,"pctNoPrazo":90,"jaVendeuAoComprador":1,"precoMediano":50,"exemploDescricao":"ROTAMETRO DE OUTRO CLIENTE"},
  {"posicao":2,"nomeFornecedor":"PERTO LTDA","cnpj":"22222222000122","uf":"SC","municipio":"Lages","score":70,"qtdPedidos":3,"ultimoPedido":"2026-09-01","prazoRealMedioDias":null,"pctNoPrazo":null,"jaVendeuAoComprador":0},
  {"posicao":3,"nomeFornecedor":"Fornecedor X Ltda","cnpj":"33333333000133","uf":"SP","municipio":"Cidade Teste","score":60,"qtdPedidos":1,"ultimoPedido":"2024-07-22","jaVendeuAoComprador":0}
],"potenciais":[],"potenciaisConsultados":false}`

func TestRecomendarFornecedoresReordenaPeloCO2(t *testing.T) {
	dh := novoDataHubFake(t)
	ts := newTestServer(t, "require")
	cs := connect(t, ts.URL, jwtDoUsuario(map[string]any{"tenantId": 1920633}))

	args := map[string]any{
		"numero": "29594155",
		"itens": []map[string]any{
			{"item": 10, "nucleo": [][]string{{"rotametro"}, {"medidor vazao"}}, "peso_kg_por_unidade": 2.0},
			{"item": 20, "nucleo": [][]string{{"CANCELADO"}}}, // item cancelado: fica fora da busca
		},
	}
	r := callJSON(t, cs, "esg_recomendar_fornecedores", args)
	alts := r["alternativas"].([]any)
	if len(alts) != 3 {
		t.Fatalf("esperava 3 alternativas: %v", alts)
	}
	a0, a1, a2 := alts[0].(map[string]any), alts[1].(map[string]any), alts[2].(map[string]any)

	// Peso de CO2 padrão (0,3): o fornecedor perto passa o de maior score.
	// PERTO: 0,7×70 + 0,3×100 = 79. LONGE: 0,7×80 + 0 = 56.
	if a0["fornecedor"] != "PERTO LTDA" || a0["posicao_sem_co2"].(float64) != 2 || a0["nota_co2"].(float64) != 1 || a0["score_final"].(float64) != 79 {
		t.Fatalf("primeiro deveria ser o fornecedor perto: %v", a0)
	}
	if a2["fornecedor"] != "LONGE LTDA" || a2["nota_co2"].(float64) != 0 || a2["score_final"].(float64) != 56 || a2["ja_vendeu_a_sua_empresa"] != true {
		t.Fatalf("último deveria ser o fornecedor longe: %v", a2)
	}
	// Município desconhecido: distância pela UF, sinalizada. O nome bate com o
	// fornecedor do pedido.
	if a1["distancia_por_uf"] != true || a1["e_o_fornecedor_atual"] != true {
		t.Fatalf("fornecedor com município desconhecido: %v", a1)
	}

	// Carga = 3 un × 2 kg; CO2 do frete = 0,006 t × km × 0,10.
	if r["carga_kg"].(float64) != 6 {
		t.Fatalf("carga: %v", r["carga_kg"])
	}
	km, frete := a0["distancia_km"].(float64), a0["co2_frete_kg"].(float64)
	if esperado := 0.006 * km * 0.10; frete < esperado-0.01 || frete > esperado+0.01 {
		t.Fatalf("co2 do frete: veio %v, esperava ~%v", frete, esperado)
	}
	atual := r["fornecedor_atual"].(map[string]any)
	if d := a0["delta_co2_frete_kg_vs_atual"].(float64); d >= 0 || atual["co2_frete_kg"].(float64) <= frete {
		t.Fatalf("o fornecedor perto deveria emitir menos que o atual (Guarulhos): delta %v, atual %v", d, atual)
	}

	// Preço e descrição de pedidos de outros clientes não saem na resposta.
	if b, _ := json.Marshal(r); strings.Contains(string(b), "precoMediano") || strings.Contains(string(b), "OUTRO CLIENTE") {
		t.Fatalf("a resposta não deveria trazer preço nem descrição de outros clientes: %s", b)
	}

	// Com peso de CO2 zero, vale a ordem do recomendador.
	args["peso_co2"] = 0.0
	r0 := callJSON(t, cs, "esg_recomendar_fornecedores", args)
	if p := r0["alternativas"].([]any)[0].(map[string]any); p["fornecedor"] != "LONGE LTDA" {
		t.Fatalf("com peso_co2 = 0 o primeiro é o de maior score: %v", p)
	}

	dh.mu.Lock()
	defer dh.mu.Unlock()
	// O flow recebe só o item válido, com palavras em maiúsculas (uma por
	// elemento), a quantidade do pedido e o comprador.
	var enviado struct {
		Itens []struct {
			Item       int        `json:"item"`
			Nucleo     [][]string `json:"nucleo"`
			Quantidade float64    `json:"quantidade"`
		} `json:"itens"`
		IDMainComprador   int  `json:"idMainComprador"`
		TopN              int  `json:"topN"`
		IncluirPotenciais bool `json:"incluirPotenciais"`
	}
	if err := json.Unmarshal([]byte(dh.flows[0]), &enviado); err != nil {
		t.Fatal(err)
	}
	if len(enviado.Itens) != 1 || enviado.Itens[0].Item != 10 || enviado.Itens[0].Quantidade != 3 ||
		len(enviado.Itens[0].Nucleo) != 2 || strings.Join(enviado.Itens[0].Nucleo[1], "+") != "MEDIDOR+VAZAO" {
		t.Fatalf("itens enviados ao flow: %s", dh.flows[0])
	}
	if enviado.IDMainComprador != 1920633 || enviado.TopN != 10 || enviado.IncluirPotenciais {
		t.Fatalf("parâmetros enviados ao flow: %s", dh.flows[0])
	}
}

func TestRecomendarFornecedoresMostraOCarbonoEvitado(t *testing.T) {
	novoDataHubFake(t)
	ts := newTestServer(t, "require")
	cs := connect(t, ts.URL, jwtDoUsuario(map[string]any{"tenantId": 1920633}))
	recomendar := func(peso float64, extra map[string]any) map[string]any {
		item := map[string]any{"item": 10, "nucleo": [][]string{{"rotametro"}}}
		if peso > 0 {
			item["peso_kg_por_unidade"] = peso
		}
		args := map[string]any{"numero": "29594155", "itens": []map[string]any{item}}
		for k, v := range extra {
			args[k] = v
		}
		return callJSON(t, cs, "esg_recomendar_fornecedores", args)
	}
	// Cada alternativa com delta traz o carbono evitado = −delta / 1000 × preço.
	conferir := func(r map[string]any, preco float64) {
		t.Helper()
		comDelta := 0
		for _, v := range r["alternativas"].([]any) {
			a := v.(map[string]any)
			delta, tem := a["delta_co2_frete_kg_vs_atual"].(float64)
			if !tem {
				continue
			}
			comDelta++
			evitado, ok := a["carbono_frete_evitado_brl"].(float64)
			if esperado := -delta / 1000 * preco; !ok || evitado < esperado-0.006 || evitado > esperado+0.006 {
				t.Fatalf("carbono evitado: veio %v, esperava ~%v: %v", a["carbono_frete_evitado_brl"], esperado, a)
			}
			if a["carbono_frete_evitado_fmt"] == nil || a["co2_frete_fmt"] == nil || a["delta_co2_frete_fmt"] == nil {
				t.Fatalf("campos _fmt faltando: %v", a)
			}
		}
		if comDelta != 3 {
			t.Fatalf("esperava 3 alternativas com delta, vieram %d", comDelta)
		}
	}

	// Carga de 3 un × 2.000 kg, para o frete dar valores visíveis.
	r := recomendar(2000, nil)
	conferir(r, 500)
	if r["preco_carbono"].(map[string]any)["origem"] != "padrao" || !contemTexto(r["premissas"], "não é cobrado") {
		t.Fatalf("preço padrão: %v", r["preco_carbono"])
	}
	// O fornecedor perto emite menos que o atual: carbono evitado positivo.
	if a0 := r["alternativas"].([]any)[0].(map[string]any); a0["fornecedor"] != "PERTO LTDA" || a0["carbono_frete_evitado_brl"].(float64) <= 0 ||
		!strings.HasPrefix(a0["delta_co2_frete_fmt"].(string), "−") {
		t.Fatalf("fornecedor perto: %v", a0)
	}
	if s, _ := r["como_responder"].(string); !strings.Contains(s, "carbono_frete_evitado_fmt") || !strings.Contains(s, "esg_precificar_carbono") {
		t.Fatalf("como_responder: %q", s)
	}

	// Preço informado: valores recalculados.
	u := recomendar(2000, map[string]any{"preco_carbono_brl_t": 200})
	conferir(u, 200)
	if u["preco_carbono"].(map[string]any)["origem"] != "usuario" {
		t.Fatalf("preço informado: %v", u["preco_carbono"])
	}

	// Sem peso: sem carbono evitado, e como_responder avisa.
	s := recomendar(0, nil)
	for _, v := range s["alternativas"].([]any) {
		a := v.(map[string]any)
		for _, campo := range []string{"carbono_frete_evitado_brl", "carbono_frete_evitado_fmt", "co2_frete_fmt", "delta_co2_frete_fmt"} {
			if _, tem := a[campo]; tem {
				t.Fatalf("sem peso não devia trazer %s: %v", campo, a)
			}
		}
	}
	if txt, _ := s["como_responder"].(string); !strings.Contains(txt, "falta o peso") {
		t.Fatalf("como_responder sem a parte do peso ausente: %q", txt)
	}

	// Preço negativo: erro da tool.
	if msg := erroDaTool(t, cs, "esg_recomendar_fornecedores", map[string]any{"numero": "29594155", "preco_carbono_brl_t": -1}); !strings.Contains(msg, "reais por tonelada") {
		t.Fatalf("mensagem: %s", msg)
	}
}

func TestRecomendarFornecedoresEntradaInvalida(t *testing.T) {
	dh := novoDataHubFake(t)
	ts := newTestServer(t, "require")
	cs := connect(t, ts.URL, jwtDoUsuario(map[string]any{"tenantId": 1920633}))

	// Sem núcleo: não chama o recomendador.
	if msg := erroDaTool(t, cs, "esg_recomendar_fornecedores", map[string]any{
		"numero": "29594155", "itens": []map[string]any{{"item": 10, "nucleo": [][]string{}}},
	}); !strings.Contains(msg, "nucleo") {
		t.Fatalf("mensagem: %s", msg)
	}
	// Mais alternativas do que o flow aceita.
	muitas := make([][]string, 13)
	for i := range muitas {
		muitas[i] = []string{"PALAVRA"}
	}
	if msg := erroDaTool(t, cs, "esg_recomendar_fornecedores", map[string]any{
		"numero": "29594155", "itens": []map[string]any{{"item": 10, "nucleo": muitas}},
	}); !strings.Contains(msg, "no máximo 12") {
		t.Fatalf("mensagem: %s", msg)
	}
	if msg := erroDaTool(t, cs, "esg_recomendar_fornecedores", map[string]any{
		"numero": "29594155", "itens": []map[string]any{{"item": 10, "nucleo": [][]string{{"X"}}}}, "peso_co2": 1.5,
	}); !strings.Contains(msg, "peso_co2") {
		t.Fatalf("mensagem: %s", msg)
	}
	dh.mu.Lock()
	defer dh.mu.Unlock()
	if len(dh.flows) != 0 {
		t.Fatalf("o recomendador não deveria ter sido chamado: %v", dh.flows)
	}
}

// Requisição e cotação no formato visto em Trunk (valores trocados). A
// requisição traz a descrição só no complemento e um item cancelado (status 6).
const requisicaoFake = `{"data":{"requests":{"items":[{
  "id":"10955121","clientCode":"10237755","tenantId":1920633,"statusFk":1,"localEntrega":4,
  "deliveryAddress":{"placeId":4,"name":"FABRICA","city":"OTACÍLIO COSTA","state":"SC"},
  "items":[
    {"item":10,"quantity":3,"estimatedPrice":100,"measure":"UN","complement":"ROTAMETRO<br>NPTF 1/2","status":null,"product":{"description":"0"}},
    {"item":20,"quantity":1,"estimatedPrice":10,"measure":"UN","complement":null,"status":6,"product":{"description":"ITEM CANCELADO"}}
  ]}]}}}`

const cotacaoFake = `{"data":{"rfqs":{"items":[{
  "id":"14505280","clientCode":null,"tenantId":1920633,"status":0,"currency":null,"localEntrega":4,
  "deliveryAddress":{"placeId":4,"name":"FABRICA","city":"OTACÍLIO COSTA","state":"SC"},
  "items":[{"itemNumber":1,"quantity":3,"estimatedPrice":2.3,"canceled":null,"product":{"description":"ROTAMETRO NORMAL"}}]
}]}}}`

func TestRecomendarFornecedoresRequisicaoECotacao(t *testing.T) {
	dh := novoDataHubFake(t)
	ts := newTestServer(t, "require")
	cs := connect(t, ts.URL, jwtDoUsuario(map[string]any{"tenantId": 1920633}))

	// Sem itens: devolve os itens ativos do documento, com a descrição do
	// complemento quando o produto vem "0".
	r := callJSON(t, cs, "esg_recomendar_fornecedores", map[string]any{"tipo": "requisicao", "numero": "10955121"})
	itens := r["itens_do_documento"].([]any)
	if len(itens) != 1 || itens[0].(map[string]any)["descricao"] != "ROTAMETRO NPTF 1/2" || r["proximo_passo"] == "" {
		t.Fatalf("itens da requisição: %v", r)
	}
	dh.mu.Lock()
	n := len(dh.flows)
	dh.mu.Unlock()
	if n != 0 {
		t.Fatal("sem itens, o recomendador não deveria ser chamado")
	}

	// Com itens: recomenda; sem fornecedor atual, avisa nas premissas.
	for _, c := range []struct {
		tipo, numero string
		item         int
	}{{"requisicao", "10955121", 10}, {"cotacao", "14505280", 1}} {
		r := callJSON(t, cs, "esg_recomendar_fornecedores", map[string]any{
			"tipo": c.tipo, "numero": c.numero,
			"itens": []map[string]any{{"item": c.item, "nucleo": [][]string{{"rotametro"}}, "peso_kg_por_unidade": 2.0}},
		})
		if alts := r["alternativas"].([]any); len(alts) != 3 || alts[0].(map[string]any)["fornecedor"] != "PERTO LTDA" {
			t.Fatalf("%s: alternativas %v", c.tipo, alts)
		}
		if b, _ := json.Marshal(r["premissas"]); !strings.Contains(string(b), "ainda sem fornecedor") {
			t.Fatalf("%s: premissas %s", c.tipo, b)
		}
	}

	if msg := erroDaTool(t, cs, "esg_recomendar_fornecedores", map[string]any{"tipo": "contrato", "numero": "1"}); !strings.Contains(msg, "tipo") {
		t.Fatalf("mensagem: %s", msg)
	}

	dh.mu.Lock()
	defer dh.mu.Unlock()
	// A consulta da requisição e da cotação leva o filtro pelo tenant.
	for _, q := range dh.consultas {
		if (strings.Contains(q, "requests(") || strings.Contains(q, "rfqs(")) && !strings.Contains(q, `"t":1920633`) {
			t.Fatalf("consulta sem tenant: %s", q)
		}
	}
}

func TestCO2DeRequisicaoECotacao(t *testing.T) {
	novoDataHubFake(t)
	fakePTAX(t)
	ts := newTestServer(t, "require")
	cs := connect(t, ts.URL, jwtDoUsuario(map[string]any{"tenantId": 1920633}))

	for _, c := range []struct {
		tipo, numero string
		item         int
		valor        float64
	}{{"requisicao", "10955121", 10, 300}, {"cotacao", "14505280", 1, 6.9}} {
		p1 := callJSON(t, cs, "esg_itens_do_pedido", map[string]any{"tipo": c.tipo, "numero": c.numero})
		if itens := p1["itens"].([]any); len(itens) != 1 || p1["tipo"] != c.tipo {
			t.Fatalf("%s passo 1: %v", c.tipo, p1)
		}

		p2 := callJSON(t, cs, "esg_estimar_co2_do_pedido", map[string]any{
			"tipo": c.tipo, "numero": c.numero,
			"itens": []map[string]any{{"item": c.item, "naics": "334513", "peso_kg_por_unidade": 2.0, "confianca": 0.8}},
		})
		it := p2["itens"].([]any)[0].(map[string]any)
		// Valor = preço estimado × quantidade; sem fornecedor, sem transporte.
		if it["valor"].(float64) != c.valor || it["co2_produto_kg"] == nil || it["co2_transporte_kg"] != nil {
			t.Fatalf("%s item: %v", c.tipo, it)
		}
		if b, _ := json.Marshal(p2["premissas"]); !strings.Contains(string(b), "ainda sem fornecedor") || !strings.Contains(string(b), "preço estimado") {
			t.Fatalf("%s premissas: %s", c.tipo, b)
		}
	}
}
