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
		"pedido": "29594155",
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

func TestRecomendarFornecedoresEntradaInvalida(t *testing.T) {
	dh := novoDataHubFake(t)
	ts := newTestServer(t, "require")
	cs := connect(t, ts.URL, jwtDoUsuario(map[string]any{"tenantId": 1920633}))

	// Sem núcleo: não chama o recomendador.
	if msg := erroDaTool(t, cs, "esg_recomendar_fornecedores", map[string]any{
		"pedido": "29594155", "itens": []map[string]any{{"item": 10, "nucleo": [][]string{}}},
	}); !strings.Contains(msg, "nucleo") {
		t.Fatalf("mensagem: %s", msg)
	}
	// Mais alternativas do que o flow aceita.
	muitas := make([][]string, 13)
	for i := range muitas {
		muitas[i] = []string{"PALAVRA"}
	}
	if msg := erroDaTool(t, cs, "esg_recomendar_fornecedores", map[string]any{
		"pedido": "29594155", "itens": []map[string]any{{"item": 10, "nucleo": muitas}},
	}); !strings.Contains(msg, "no máximo 12") {
		t.Fatalf("mensagem: %s", msg)
	}
	if msg := erroDaTool(t, cs, "esg_recomendar_fornecedores", map[string]any{
		"pedido": "29594155", "itens": []map[string]any{{"item": 10, "nucleo": [][]string{{"X"}}}}, "peso_co2": 1.5,
	}); !strings.Contains(msg, "peso_co2") {
		t.Fatalf("mensagem: %s", msg)
	}
	dh.mu.Lock()
	defer dh.mu.Unlock()
	if len(dh.flows) != 0 {
		t.Fatalf("o recomendador não deveria ter sido chamado: %v", dh.flows)
	}
}
