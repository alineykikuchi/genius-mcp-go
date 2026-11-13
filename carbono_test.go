package main

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const toolCarbono = "esg_precificar_carbono"

// A tool de precificação é só conta: não lê o DataHub e não precisa de DataHub falso.
func sessaoCarbono(t *testing.T) *mcp.ClientSession {
	t.Helper()
	ts := newTestServer(t, "require")
	return connect(t, ts.URL, jwtDoUsuario(map[string]any{"tenantId": 1920633}))
}

func alternativasAB() []map[string]any {
	return []map[string]any{
		{"rotulo": "Fornecedor A", "valor_brl": 10000, "co2e_kg": 1200},
		{"rotulo": "Fornecedor B", "valor_brl": 10300, "co2e_kg": 300},
	}
}

func contemTexto(lista any, trecho string) bool {
	itens, _ := lista.([]any)
	for _, v := range itens {
		if s, ok := v.(string); ok && strings.Contains(s, trecho) {
			return true
		}
	}
	return false
}

func TestPrecificarCarbonoNaListaDeTools(t *testing.T) {
	cs := sessaoCarbono(t)
	list, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, tl := range list.Tools {
		got[tl.Name] = true
	}
	for _, nome := range []string{toolCarbono, "ping", "quem_sou_eu", "esg_itens_do_pedido", "esg_estimar_co2_do_pedido", "esg_recomendar_fornecedores"} {
		if !got[nome] {
			t.Fatalf("tool %s faltando: %v", nome, got)
		}
	}
}

func TestPrecificarCarbonoDeUmDocumento(t *testing.T) {
	cs := sessaoCarbono(t)

	// Preço padrão.
	r := callJSON(t, cs, toolCarbono, map[string]any{"co2e_kg": 9120.4, "valor_pedido_brl": 48000})
	preco := r["preco_carbono"].(map[string]any)
	if r["custo_carbono_fmt"] != "R$ 4.560,20" || r["custo_carbono_brl"].(float64) != 4560.2 {
		t.Fatalf("custo do carbono: %v", r)
	}
	if preco["origem"] != "padrao" || preco["valor_fmt"] != "R$ 500/t" || preco["valor_brl_por_t"].(float64) != 500 {
		t.Fatalf("preço padrão: %v", preco)
	}
	if r["co2e_fmt"] != "9.120 kg" || r["valor_pedido_fmt"] != "R$ 48.000" || r["custo_total_fmt"] != "R$ 52.560,20" ||
		r["peso_carbono_fmt"] != "10%" || r["peso_carbono_percentual"].(float64) != 9.5 || r["carbono_irrelevante"] != false {
		t.Fatalf("campos do documento: %v", r)
	}
	if s, _ := r["como_responder"].(string); s == "" || !strings.Contains(s, "custo com carbono") {
		t.Fatalf("como_responder: %q", s)
	}
	if !contemTexto(r["premissas"], "não é cobrado") || !contemTexto(r["premissas"], "EU ETS") {
		t.Fatalf("premissas: %v", r["premissas"])
	}

	// Preço informado pelo usuário.
	u := callJSON(t, cs, toolCarbono, map[string]any{"co2e_kg": 9120.4, "valor_pedido_brl": 48000, "preco_carbono_brl_t": 200})
	if u["preco_carbono"].(map[string]any)["origem"] != "usuario" || !contemTexto(u["premissas"], "informado por você") {
		t.Fatalf("preço informado: %v", u)
	}
	if u["custo_carbono_fmt"] != "R$ 1.824,08" {
		t.Fatalf("custo a R$ 200/t: %v", u["custo_carbono_fmt"])
	}

	// Carbono irrelevante e baixa confiança.
	i := callJSON(t, cs, toolCarbono, map[string]any{"co2e_kg": 28, "valor_pedido_brl": 10000, "confianca_baixa": true})
	if i["carbono_irrelevante"] != true || i["peso_carbono_fmt"] != "menos de 1%" || i["custo_carbono_fmt"] != "R$ 14" {
		t.Fatalf("carbono irrelevante: %v", i)
	}
	if !contemTexto(i["premissas"], "baixa confiança") || !strings.Contains(i["como_responder"].(string), "baixa confiança") {
		t.Fatalf("baixa confiança: %v", i)
	}

	// Sem valor do pedido: só CO2e e valor do carbono.
	s := callJSON(t, cs, toolCarbono, map[string]any{"co2e_kg": 28})
	if s["custo_carbono_fmt"] != "R$ 14" {
		t.Fatalf("sem valor: %v", s)
	}
	for _, campo := range []string{"valor_pedido_brl", "custo_total_brl", "custo_total_fmt", "peso_carbono_percentual", "peso_carbono_fmt"} {
		if _, tem := s[campo]; tem {
			t.Fatalf("sem valor do pedido não devia trazer %s: %v", campo, s)
		}
	}

	// A partir de 10.000 kg, o CO2e sai em toneladas.
	g := callJSON(t, cs, toolCarbono, map[string]any{"co2e_kg": 12500})
	if g["co2e_fmt"] != "12,5 t" {
		t.Fatalf("CO2e em toneladas: %v", g["co2e_fmt"])
	}
}

func TestPrecificarCarbonoComparacao(t *testing.T) {
	cs := sessaoCarbono(t)

	r := callJSON(t, cs, toolCarbono, map[string]any{"alternativas": alternativasAB()})
	if r["recomendada"] != "Fornecedor B" {
		t.Fatalf("recomendada: %v", r["recomendada"])
	}
	alts := r["alternativas"].([]any)
	a, b := alts[0].(map[string]any), alts[1].(map[string]any)
	if a["referencia"] != true || b["referencia"] != false || a["custo_total_fmt"] != "R$ 10.600" || b["custo_total_fmt"] != "R$ 10.450" ||
		a["co2e_fmt"] != "1.200 kg" || b["custo_carbono_fmt"] != "R$ 150" {
		t.Fatalf("alternativas: %v", alts)
	}
	comp := r["comparacao"].([]any)
	if len(comp) != 1 {
		t.Fatalf("comparação: %v", comp)
	}
	c := comp[0].(map[string]any)
	esperado := map[string]any{
		"rotulo": "Fornecedor B", "contra": "Fornecedor A",
		"dif_preco_fmt": "+ R$ 300", "dif_co2e_fmt": "− 900 kg", "reducao_co2e_fmt": "75% menos",
		"dif_carbono_fmt": "− R$ 450", "dif_custo_total_fmt": "− R$ 150",
		"situacao": "paga_mais_emite_menos", "preco_virada_fmt": "R$ 333/t", "veredito": "compensa",
	}
	for k, v := range esperado {
		if c[k] != v {
			t.Fatalf("comparação, %s: veio %v, esperava %v", k, c[k], v)
		}
	}
	if c["preco_virada_brl_t"].(float64) != 333 || c["dif_custo_total_brl"].(float64) != -150 {
		t.Fatalf("números da comparação: %v", c)
	}
	if s, _ := r["como_responder"].(string); !strings.Contains(s, "preco_virada_fmt") || !strings.Contains(s, "Diferença") {
		t.Fatalf("como_responder da comparação: %q", s)
	}

	// Recalculo a R$ 200/t: B deixa de compensar.
	u := callJSON(t, cs, toolCarbono, map[string]any{"alternativas": alternativasAB(), "preco_carbono_brl_t": 200})
	cu := u["comparacao"].([]any)[0].(map[string]any)
	if u["recomendada"] != "Fornecedor A" || cu["veredito"] != "nao_compensa" || cu["dif_custo_total_fmt"] != "+ R$ 120" || cu["preco_virada_fmt"] != "R$ 333/t" {
		t.Fatalf("a R$ 200/t: %v", u)
	}

	// Melhor nos dois: sem preço de virada.
	m := callJSON(t, cs, toolCarbono, map[string]any{"alternativas": []map[string]any{
		{"rotulo": "Atual", "valor_brl": 10000, "co2e_kg": 1200},
		{"valor_brl": 9000, "co2e_kg": 300},
	}})
	cm := m["comparacao"].([]any)[0].(map[string]any)
	if cm["situacao"] != "melhor_nos_dois" || cm["rotulo"] != "Alternativa 2" || m["recomendada"] != "Alternativa 2" {
		t.Fatalf("melhor nos dois: %v", m)
	}
	if _, tem := cm["preco_virada_brl_t"]; tem {
		t.Fatalf("melhor nos dois não tem preço de virada: %v", cm)
	}
	if strings.Contains(m["como_responder"].(string), "preco_virada_fmt") {
		t.Fatalf("como_responder não devia pedir o preço de virada: %v", m["como_responder"])
	}
}

func TestPrecificarCarbonoAlternativaIncompleta(t *testing.T) {
	cs := sessaoCarbono(t)
	r := callJSON(t, cs, toolCarbono, map[string]any{"alternativas": []map[string]any{
		{"rotulo": "Fornecedor A", "valor_brl": 10000, "co2e_kg": 1200},
		{"rotulo": "Fornecedor C", "co2e_kg": 500},
		{"rotulo": "Fornecedor B", "valor_brl": 10300, "co2e_kg": 300},
	}})
	if len(r["alternativas"].([]any)) != 2 || len(r["comparacao"].([]any)) != 1 {
		t.Fatalf("devia comparar só as duas completas: %v", r)
	}
	if !contemTexto(r["avisos"], "Fornecedor C") {
		t.Fatalf("aviso com o rótulo da alternativa incompleta: %v", r["avisos"])
	}
	if !strings.Contains(r["como_responder"].(string), "avisos") {
		t.Fatalf("como_responder devia mandar citar os avisos: %v", r["como_responder"])
	}

	// A primeira (a referência) incompleta: a referência passa a ser a próxima
	// completa, e o aviso diz contra quem as diferenças foram calculadas.
	p := callJSON(t, cs, toolCarbono, map[string]any{"alternativas": []map[string]any{
		{"rotulo": "Atual", "co2e_kg": 500},
		{"rotulo": "Fornecedor A", "valor_brl": 10000, "co2e_kg": 1200},
		{"rotulo": "Fornecedor B", "valor_brl": 10300, "co2e_kg": 300},
	}})
	if c := p["comparacao"].([]any)[0].(map[string]any); c["contra"] != "Fornecedor A" || p["alternativas"].([]any)[0].(map[string]any)["referencia"] != true {
		t.Fatalf("referência devia passar a ser o Fornecedor A: %v", p)
	}
	if !contemTexto(p["avisos"], "Atual") || !contemTexto(p["avisos"], `calculados contra "Fornecedor A"`) {
		t.Fatalf("aviso da referência trocada: %v", p["avisos"])
	}
}

func TestPrecificarCarbonoEntradaInvalida(t *testing.T) {
	cs := sessaoCarbono(t)
	seis := make([]map[string]any, 6)
	for i := range seis {
		seis[i] = map[string]any{"rotulo": "F", "valor_brl": 100, "co2e_kg": 10}
	}
	casos := []struct {
		nome   string
		args   map[string]any
		trecho string
	}{
		{"preço zero", map[string]any{"co2e_kg": 100, "preco_carbono_brl_t": 0}, "reais por tonelada"},
		{"preço negativo", map[string]any{"co2e_kg": 100, "preco_carbono_brl_t": -50}, "reais por tonelada"},
		{"sem co2e_kg e sem alternativas", map[string]any{}, "co2e_kg"},
		{"co2e_kg negativo", map[string]any{"co2e_kg": -1}, "co2e_kg"},
		{"uma alternativa", map[string]any{"alternativas": alternativasAB()[:1]}, "segunda"},
		{"seis alternativas", map[string]any{"alternativas": seis}, "reduza"},
		{"só uma alternativa completa", map[string]any{"alternativas": []map[string]any{
			{"rotulo": "A", "valor_brl": 100, "co2e_kg": 10}, {"rotulo": "B", "valor_brl": 100},
		}}, "duas alternativas"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if msg := erroDaTool(t, cs, toolCarbono, c.args); !strings.Contains(msg, c.trecho) {
				t.Fatalf("mensagem sem %q: %s", c.trecho, msg)
			}
		})
	}
}

func TestPrecificarCarbonoPrecoIncomum(t *testing.T) {
	cs := sessaoCarbono(t)
	r := callJSON(t, cs, toolCarbono, map[string]any{"co2e_kg": 1000, "preco_carbono_brl_t": 8000})
	if r["custo_carbono_fmt"] != "R$ 8.000" || !contemTexto(r["avisos"], "incomum") {
		t.Fatalf("preço incomum: %v", r)
	}
	if !strings.Contains(r["como_responder"].(string), "incomum") {
		t.Fatalf("como_responder sem a frase do preço incomum: %v", r["como_responder"])
	}
}

func TestPrecoPadraoDoCarbonoPorVariavel(t *testing.T) {
	t.Setenv("PRECO_CARBONO_BRL_T", "300")
	cs := sessaoCarbono(t)
	r := callJSON(t, cs, toolCarbono, map[string]any{"co2e_kg": 1000})
	preco := r["preco_carbono"].(map[string]any)
	if preco["valor_brl_por_t"].(float64) != 300 || preco["origem"] != "padrao" || r["custo_carbono_fmt"] != "R$ 300" {
		t.Fatalf("preço padrão por variável: %v", r)
	}

	// Valor inválido volta ao padrão de R$ 500.
	t.Setenv("PRECO_CARBONO_BRL_T", "abc")
	cs2 := connect(t, newTestServer(t, "require").URL, jwtDoUsuario(map[string]any{"tenantId": 1920633}))
	r2 := callJSON(t, cs2, toolCarbono, map[string]any{"co2e_kg": 1000})
	if r2["preco_carbono"].(map[string]any)["valor_brl_por_t"].(float64) != 500 {
		t.Fatalf("variável inválida devia usar R$ 500: %v", r2)
	}
}
