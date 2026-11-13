package tools

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"genius-mcp-go/internal/carbono"
)

// Precificação do carbono (spec: docs/precificacao-carbono/spec-precificacao-carbono.md).
//
// A tool esg_precificar_carbono é só conta: recebe o CO2e já estimado (de um
// documento ou de várias alternativas) e devolve o valor em reais, a comparação
// pronta, os campos formatados (_fmt) e como_responder. As contas ficam no
// pacote carbono; aqui ficam a validação, a montagem da saída e os textos.

func registerCarbono(s *mcp.Server, d co2Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "esg_precificar_carbono",
		Description: "Converte o CO2e estimado em valor em reais (precificação do carbono) e compara alternativas pelo custo com carbono (preço + carbono). " +
			"Use depois de esg_estimar_co2_do_pedido, quando pedirem o custo do carbono, o custo com carbono ou para recalcular com outro preço por tonelada. " +
			"Para um documento, informe co2e_kg (total_kg da estimativa) e valor_pedido_brl (só se a moeda for BRL). " +
			"Para comparar, informe de 2 a 5 alternativas com rótulo, valor em reais e CO2e em kg; a primeira é a referência. " +
			"O preço padrão é uma referência fixa em reais por tonelada; se o usuário informar outro preço, passe em preco_carbono_brl_t. " +
			"A tool não converte moeda: se o usuário falar em dólar ou euro, peça o valor em reais por tonelada e não chame a tool. " +
			"O valor do carbono é uma referência e não é cobrado no pedido. Siga o campo como_responder da resposta.",
	}, d.precificarCarbono)
}

// precoCarbonoDoAmbiente lê o preço padrão de PRECO_CARBONO_BRL_T. Vazio,
// inválido ou menor ou igual a zero usa carbono.PrecoPadraoBRLT.
func precoCarbonoDoAmbiente() float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv("PRECO_CARBONO_BRL_T")), 64)
	if err != nil || !(v > 0) {
		return carbono.PrecoPadraoBRLT
	}
	return v
}

// ---- entrada ---------------------------------------------------------------

type AlternativaCarbonoIn struct {
	// Os três campos são omitempty para ficarem opcionais no schema: sem isso o
	// SDK recusa a alternativa incompleta antes do handler, e ela não chega a
	// ficar fora da comparação com aviso (ver PENDENCIAS.md, item 5).
	Rotulo   string   `json:"rotulo,omitempty" jsonschema:"nome da alternativa, por exemplo o fornecedor"`
	ValorBRL *float64 `json:"valor_brl,omitempty" jsonschema:"valor da alternativa em reais"`
	CO2eKg   *float64 `json:"co2e_kg,omitempty" jsonschema:"CO2e estimado da alternativa, em kg"`
}

type PrecificarIn struct {
	CO2eKg           *float64               `json:"co2e_kg,omitempty" jsonschema:"CO2e estimado em kg (total_kg de esg_estimar_co2_do_pedido); obrigatório quando não há alternativas"`
	ValorPedidoBRL   *float64               `json:"valor_pedido_brl,omitempty" jsonschema:"valor do pedido em reais; só informe se a moeda do documento for BRL"`
	PrecoCarbonoBRLT *float64               `json:"preco_carbono_brl_t,omitempty" jsonschema:"preço do carbono em reais por tonelada, informado pelo usuário; ausente usa o padrão"`
	Alternativas     []AlternativaCarbonoIn `json:"alternativas,omitempty" jsonschema:"de 2 a 5 alternativas a comparar; a primeira é a referência (pedido ou fornecedor atual)"`
	ConfiancaBaixa   bool                   `json:"confianca_baixa,omitempty" jsonschema:"true quando algum item da estimativa de CO2 veio com baixa_confianca"`
}

// ---- saída -----------------------------------------------------------------

// PrecoCarbono é o preço usado na conta e de onde ele veio.
type PrecoCarbono struct {
	ValorBRLPorT float64 `json:"valor_brl_por_t"`
	ValorFmt     string  `json:"valor_fmt"`
	Origem       string  `json:"origem"` // padrao ou usuario
	Referencia   string  `json:"referencia"`
}

// CarbonoDocumento é o valor do carbono de um documento (seção 5.1 da spec).
type CarbonoDocumento struct {
	CO2eKg                float64      `json:"co2e_kg"`
	CO2eFmt               string       `json:"co2e_fmt"`
	PrecoCarbono          PrecoCarbono `json:"preco_carbono"`
	CustoCarbonoBRL       float64      `json:"custo_carbono_brl"`
	CustoCarbonoFmt       string       `json:"custo_carbono_fmt"`
	ValorPedidoBRL        *float64     `json:"valor_pedido_brl,omitempty"`
	ValorPedidoFmt        string       `json:"valor_pedido_fmt,omitempty"`
	CustoTotalBRL         *float64     `json:"custo_total_brl,omitempty"`
	CustoTotalFmt         string       `json:"custo_total_fmt,omitempty"`
	PesoCarbonoPercentual *float64     `json:"peso_carbono_percentual,omitempty"`
	PesoCarbonoFmt        string       `json:"peso_carbono_fmt,omitempty"`
	CarbonoIrrelevante    bool         `json:"carbono_irrelevante"`
}

type AlternativaCarbonoOut struct {
	Rotulo          string  `json:"rotulo"`
	Referencia      bool    `json:"referencia"`
	ValorBRL        float64 `json:"valor_brl"`
	ValorFmt        string  `json:"valor_fmt"`
	CO2eKg          float64 `json:"co2e_kg"`
	CO2eFmt         string  `json:"co2e_fmt"`
	CustoCarbonoBRL float64 `json:"custo_carbono_brl"`
	CustoCarbonoFmt string  `json:"custo_carbono_fmt"`
	CustoTotalBRL   float64 `json:"custo_total_brl"`
	CustoTotalFmt   string  `json:"custo_total_fmt"`
}

type ComparacaoCarbonoOut struct {
	Rotulo           string   `json:"rotulo"`
	Contra           string   `json:"contra"`
	DifPrecoBRL      float64  `json:"dif_preco_brl"`
	DifPrecoFmt      string   `json:"dif_preco_fmt"`
	DifCO2eKg        float64  `json:"dif_co2e_kg"`
	DifCO2eFmt       string   `json:"dif_co2e_fmt"`
	ReducaoCO2ePct   *float64 `json:"reducao_co2e_percentual,omitempty"`
	ReducaoCO2eFmt   string   `json:"reducao_co2e_fmt,omitempty"`
	DifCarbonoBRL    float64  `json:"dif_carbono_brl"`
	DifCarbonoFmt    string   `json:"dif_carbono_fmt"`
	DifCustoTotalBRL float64  `json:"dif_custo_total_brl"`
	DifCustoTotalFmt string   `json:"dif_custo_total_fmt"`
	Situacao         string   `json:"situacao"`
	PrecoViradaBRLT  *float64 `json:"preco_virada_brl_t,omitempty"`
	PrecoViradaFmt   string   `json:"preco_virada_fmt,omitempty"`
	Veredito         string   `json:"veredito"`
}

// PrecificarOut junta as duas formas da resposta: um documento (seção 5.1 da
// spec, campos do topo) ou comparação (seção 5.2: alternativas, comparacao e
// recomendada). Os campos da forma que não foi usada não aparecem.
type PrecificarOut struct {
	CO2eKg                *float64                `json:"co2e_kg,omitempty"`
	CO2eFmt               string                  `json:"co2e_fmt,omitempty"`
	PrecoCarbono          PrecoCarbono            `json:"preco_carbono"`
	CustoCarbonoBRL       *float64                `json:"custo_carbono_brl,omitempty"`
	CustoCarbonoFmt       string                  `json:"custo_carbono_fmt,omitempty"`
	ValorPedidoBRL        *float64                `json:"valor_pedido_brl,omitempty"`
	ValorPedidoFmt        string                  `json:"valor_pedido_fmt,omitempty"`
	CustoTotalBRL         *float64                `json:"custo_total_brl,omitempty"`
	CustoTotalFmt         string                  `json:"custo_total_fmt,omitempty"`
	PesoCarbonoPercentual *float64                `json:"peso_carbono_percentual,omitempty"`
	PesoCarbonoFmt        string                  `json:"peso_carbono_fmt,omitempty"`
	CarbonoIrrelevante    *bool                   `json:"carbono_irrelevante,omitempty"`
	Alternativas          []AlternativaCarbonoOut `json:"alternativas,omitempty"`
	Comparacao            []ComparacaoCarbonoOut  `json:"comparacao,omitempty"`
	Recomendada           string                  `json:"recomendada,omitempty"`
	Avisos                []string                `json:"avisos,omitempty"`
	Premissas             []string                `json:"premissas"`
	ComoResponder         string                  `json:"como_responder"`
}

// ---- handler ---------------------------------------------------------------

func (d co2Deps) precificarCarbono(_ context.Context, _ *mcp.CallToolRequest, in PrecificarIn) (*mcp.CallToolResult, PrecificarOut, error) {
	out := PrecificarOut{}
	preco, avisos, err := d.resolverPrecoCarbono(in.PrecoCarbonoBRLT)
	if err != nil {
		return nil, out, err
	}
	out.PrecoCarbono = preco
	precoIncomum := len(avisos) > 0

	if len(in.Alternativas) > 0 {
		avisosAlt, err := compararAlternativas(&out, in.Alternativas, preco)
		if err != nil {
			return nil, out, err
		}
		avisos = append(avisos, avisosAlt...)
		out.ComoResponder = comoResponderComparacao(out, precoIncomum, in.ConfiancaBaixa)
	} else {
		if in.CO2eKg == nil {
			return nil, out, fmt.Errorf("informe co2e_kg (o total_kg de esg_estimar_co2_do_pedido) ou de 2 a 5 alternativas para comparar")
		}
		if *in.CO2eKg < 0 {
			return nil, out, fmt.Errorf("co2e_kg não pode ser negativo")
		}
		if in.ValorPedidoBRL != nil && *in.ValorPedidoBRL < 0 {
			return nil, out, fmt.Errorf("valor_pedido_brl não pode ser negativo")
		}
		doc := novoCarbonoDocumento(*in.CO2eKg, in.ValorPedidoBRL, preco)
		out.CO2eKg, out.CO2eFmt = &doc.CO2eKg, doc.CO2eFmt
		out.CustoCarbonoBRL, out.CustoCarbonoFmt = &doc.CustoCarbonoBRL, doc.CustoCarbonoFmt
		out.ValorPedidoBRL, out.ValorPedidoFmt = doc.ValorPedidoBRL, doc.ValorPedidoFmt
		out.CustoTotalBRL, out.CustoTotalFmt = doc.CustoTotalBRL, doc.CustoTotalFmt
		out.PesoCarbonoPercentual, out.PesoCarbonoFmt = doc.PesoCarbonoPercentual, doc.PesoCarbonoFmt
		out.CarbonoIrrelevante = &doc.CarbonoIrrelevante
		out.ComoResponder = comoResponderDocumento(doc, precoIncomum, in.ConfiancaBaixa)
	}

	out.Avisos = avisos
	out.Premissas = premissasCarbono(preco, in.ConfiancaBaixa)
	return nil, out, nil
}

// ---- preço e premissas -----------------------------------------------------

const avisoPrecoIncomum = "Preço por tonelada incomum; confira se está em reais por tonelada."

// resolverPrecoCarbono devolve o preço da consulta: o informado pelo usuário,
// ou o padrão da configuração. Preço fora da faixa comum não é erro: vira aviso.
func (d co2Deps) resolverPrecoCarbono(informado *float64) (PrecoCarbono, []string, error) {
	p := PrecoCarbono{ValorBRLPorT: d.precoCarbono, Origem: "padrao", Referencia: carbono.Referencia}
	if !(p.ValorBRLPorT > 0) {
		p.ValorBRLPorT = carbono.PrecoPadraoBRLT
	}
	var avisos []string
	if informado != nil {
		if !(*informado > 0) {
			return p, nil, fmt.Errorf("Informe o preço do carbono em reais por tonelada, maior que zero.")
		}
		p.ValorBRLPorT, p.Origem, p.Referencia = *informado, "usuario", "valor informado por você"
		if *informado < carbono.PrecoMinComum || *informado > carbono.PrecoMaxComum {
			avisos = append(avisos, avisoPrecoIncomum)
		}
	}
	p.ValorFmt = carbono.PrecoPorT(p.ValorBRLPorT)
	return p, avisos, nil
}

// premissasCarbono devolve as premissas de preço, sempre na mesma ordem.
func premissasCarbono(p PrecoCarbono, confiancaBaixa bool) []string {
	origem := "referência fixa baseada no EU ETS"
	if p.Origem == "usuario" {
		origem = "informado por você"
	}
	lista := []string{
		fmt.Sprintf("Preço do carbono: %s, %s.", p.ValorFmt, origem),
		"O valor do carbono é uma referência para comparar; não é cobrado no pedido.",
	}
	if confiancaBaixa {
		lista = append(lista, "A estimativa de CO2e tem baixa confiança; use o resultado como indicação.")
	}
	return lista
}

// ---- um documento ----------------------------------------------------------

// novoCarbonoDocumento calcula e formata o valor do carbono de um documento.
// Valor nil ou zero: sem custo com carbono e sem percentual.
func novoCarbonoDocumento(co2eKg float64, valor *float64, p PrecoCarbono) CarbonoDocumento {
	if valor != nil && *valor <= 0 {
		valor = nil
	}
	c := carbono.Precificar(co2eKg, valor, p.ValorBRLPorT)
	doc := CarbonoDocumento{
		CO2eKg: co2eKg, CO2eFmt: carbono.CO2e(co2eKg, carbono.UsarToneladas(co2eKg)),
		PrecoCarbono:    p,
		CustoCarbonoBRL: c.CustoCarbono, CustoCarbonoFmt: carbono.BRL(c.CustoCarbono),
		CarbonoIrrelevante: c.Irrelevante,
	}
	if c.Valor != nil {
		doc.ValorPedidoBRL, doc.ValorPedidoFmt = c.Valor, carbono.BRL(*c.Valor)
		doc.CustoTotalBRL, doc.CustoTotalFmt = c.CustoTotal, carbono.BRL(*c.CustoTotal)
	}
	if c.PesoCarbonoPct != nil {
		doc.PesoCarbonoPercentual, doc.PesoCarbonoFmt = c.PesoCarbonoPct, carbono.Pct(*c.PesoCarbonoPct)
	}
	return doc
}

// ---- comparação ------------------------------------------------------------

// compararAlternativas valida as alternativas, precifica cada uma, compara
// cada uma com a primeira (a referência) e escolhe a recomendada. Devolve os
// avisos das alternativas que ficaram fora.
func compararAlternativas(out *PrecificarOut, in []AlternativaCarbonoIn, p PrecoCarbono) ([]string, error) {
	switch {
	case len(in) == 1:
		return nil, fmt.Errorf("para comparar são necessárias pelo menos duas alternativas: informe a segunda")
	case len(in) > 5:
		return nil, fmt.Errorf("a comparação aceita no máximo 5 alternativas: reduza a lista")
	}

	type valida struct {
		rotulo string
		custo  carbono.Custo
	}
	var validas []valida
	var avisos []string
	maiorKg := 0.0
	for i, a := range in {
		rotulo := strings.TrimSpace(a.Rotulo)
		if rotulo == "" {
			rotulo = fmt.Sprintf("Alternativa %d", i+1)
		}
		if a.ValorBRL == nil || a.CO2eKg == nil {
			avisos = append(avisos, fmt.Sprintf("A alternativa %q ficou fora da comparação: falta o valor em reais ou o CO2e.", rotulo))
			continue
		}
		if *a.ValorBRL < 0 || *a.CO2eKg < 0 {
			return nil, fmt.Errorf("a alternativa %q tem valor ou CO2e negativo", rotulo)
		}
		validas = append(validas, valida{rotulo, carbono.Precificar(*a.CO2eKg, a.ValorBRL, p.ValorBRLPorT)})
		maiorKg = max(maiorKg, *a.CO2eKg)
	}
	if len(validas) < 2 {
		return nil, fmt.Errorf("restaram menos de duas alternativas com valor em reais e CO2e: informe os dois dados de cada alternativa")
	}

	emT := carbono.UsarToneladas(maiorKg)
	ref := validas[0]
	melhor := 0
	for i, v := range validas {
		out.Alternativas = append(out.Alternativas, AlternativaCarbonoOut{
			Rotulo: v.rotulo, Referencia: i == 0,
			ValorBRL: *v.custo.Valor, ValorFmt: carbono.BRL(*v.custo.Valor),
			CO2eKg: v.custo.CO2eKg, CO2eFmt: carbono.CO2e(v.custo.CO2eKg, emT),
			CustoCarbonoBRL: v.custo.CustoCarbono, CustoCarbonoFmt: carbono.BRL(v.custo.CustoCarbono),
			CustoTotalBRL: *v.custo.CustoTotal, CustoTotalFmt: carbono.BRL(*v.custo.CustoTotal),
		})
		// Recomendada: menor custo com carbono; em empate, menor CO2e.
		m := validas[melhor].custo
		if *v.custo.CustoTotal < *m.CustoTotal || (*v.custo.CustoTotal == *m.CustoTotal && v.custo.CO2eKg < m.CO2eKg) {
			melhor = i
		}
		if i == 0 {
			continue
		}
		c := carbono.Comparar(ref.custo, v.custo)
		co := ComparacaoCarbonoOut{
			Rotulo: v.rotulo, Contra: ref.rotulo,
			DifPrecoBRL: c.DifPreco, DifPrecoFmt: carbono.BRLComSinal(c.DifPreco),
			DifCO2eKg: c.DifCO2eKg, DifCO2eFmt: carbono.CO2eComSinal(c.DifCO2eKg, emT),
			DifCarbonoBRL: c.DifCarbono, DifCarbonoFmt: carbono.BRLComSinal(c.DifCarbono),
			DifCustoTotalBRL: c.DifCustoTotal, DifCustoTotalFmt: carbono.BRLComSinal(c.DifCustoTotal),
			Situacao: c.Situacao, Veredito: c.Veredito,
		}
		if c.ReducaoCO2ePct != nil {
			co.ReducaoCO2ePct, co.ReducaoCO2eFmt = c.ReducaoCO2ePct, carbono.Reducao(*c.ReducaoCO2ePct)
		}
		if c.PrecoVirada != nil {
			co.PrecoViradaBRLT, co.PrecoViradaFmt = c.PrecoVirada, carbono.PrecoPorT(*c.PrecoVirada)
		}
		out.Comparacao = append(out.Comparacao, co)
	}
	out.Recomendada = validas[melhor].rotulo
	return avisos, nil
}

// ---- como_responder --------------------------------------------------------

const (
	fimComoResponder = "Use os valores _fmt como vieram; não recalcule nem invente valores. " +
		"Chame o resultado de 'custo com carbono', nunca de 'custo total'."
	ofertaCarbono = "Ofereça ver alternativas com menos CO2e ou recalcular com outro preço por tonelada, em reais."
)

// fraseDoPreco diz como chamar o preço do carbono na resposta.
func fraseDoPreco(p PrecoCarbono) string {
	if p.Origem == "usuario" {
		return "O preço por tonelada foi informado pelo usuário: escreva 'valor informado por você' no lugar de 'referência (EU ETS)', e diga que o valor do carbono não é cobrado no pedido."
	}
	return "Diga que o valor do carbono é uma referência (EU ETS) para comparar e que não é cobrado no pedido."
}

// frasesExtras cobre o preço fora da faixa comum e a estimativa de baixa confiança.
func frasesExtras(precoIncomum, confiancaBaixa bool) []string {
	var extras []string
	if precoIncomum {
		extras = append(extras, "Acrescente a frase: 'Esse preço por tonelada é incomum; confira se está em reais por tonelada.'")
	}
	if confiancaBaixa {
		extras = append(extras, "Acrescente a frase: 'A estimativa de CO2e tem baixa confiança; use o resultado como indicação, não como base para trocar de fornecedor.' "+
			"Não recomende troca de fornecedor só pelo CO2e.")
	}
	return extras
}

// numerar junta as partes como "1) ... 2) ..." e fecha com o texto fixo.
func numerar(partes []string) string {
	var b strings.Builder
	for i, p := range partes {
		fmt.Fprintf(&b, "%d) %s ", i+1, p)
	}
	b.WriteString(fimComoResponder)
	return b.String()
}

func comoResponderDocumento(doc CarbonoDocumento, precoIncomum, confiancaBaixa bool) string {
	var partes []string
	switch {
	case doc.CarbonoIrrelevante:
		partes = append(partes,
			"Diga que o carbono pesa menos de 1% do valor do pedido (custo_carbono_fmt em valor_pedido_fmt) e que ele não muda a escolha entre alternativas. Não monte tabela.",
			fraseDoPreco(doc.PrecoCarbono))
	case doc.PesoCarbonoPercentual != nil:
		partes = append(partes,
			"Diga quanto CO2e o pedido emite (co2e_fmt), com 'cerca de' ou 'estimado', e quanto isso vale em carbono (custo_carbono_fmt) ao preço usado (preco_carbono.valor_fmt). "+fraseDoPreco(doc.PrecoCarbono),
			"Diga o peso do carbono sobre o valor do pedido (peso_carbono_fmt).")
	default:
		partes = append(partes,
			"Diga quanto CO2e o pedido emite (co2e_fmt), com 'cerca de' ou 'estimado', e quanto isso vale em carbono (custo_carbono_fmt) ao preço usado (preco_carbono.valor_fmt). "+fraseDoPreco(doc.PrecoCarbono),
			"O valor do pedido não foi informado: mostre só o CO2e e o valor do carbono; não cite custo com carbono nem percentual.")
	}
	partes = append(partes, frasesExtras(precoIncomum, confiancaBaixa)...)
	partes = append(partes, ofertaCarbono)
	return numerar(partes)
}

func comoResponderComparacao(out PrecificarOut, precoIncomum, confiancaBaixa bool) string {
	ref := out.Alternativas[0].Rotulo
	var partes []string

	if len(out.Comparacao) == 1 {
		c := out.Comparacao[0]
		switch {
		case c.Veredito == carbono.Empate:
			partes = append(partes, "Primeira frase: diga que as duas alternativas empatam no custo com carbono e aponte a de menor CO2e (recomendada).")
		case c.Situacao == carbono.MelhorNosDois:
			partes = append(partes, fmt.Sprintf("Primeira frase: diga que %q é mais barata (dif_preco_fmt) e emite menos (reducao_co2e_fmt) que %q, e que compensa a qualquer preço de carbono.", c.Rotulo, ref))
		case c.Situacao == carbono.PiorNosDois:
			partes = append(partes, fmt.Sprintf("Primeira frase: diga que %q é mais cara (dif_preco_fmt) e emite mais que %q, e que não compensa a nenhum preço de carbono.", c.Rotulo, ref))
		case c.Veredito == carbono.Compensa:
			partes = append(partes, fmt.Sprintf("Primeira frase: diga que %q compensa (recomendada) e por quanto fica à frente (dif_custo_total_fmt), citando o que ela custa a mais ou a menos (dif_preco_fmt) e quanto CO2e evita ou emite a mais (reducao_co2e_fmt).", c.Rotulo))
		default:
			partes = append(partes, fmt.Sprintf("Primeira frase: diga que %q não compensa ao preço usado e por quanto fica atrás de %q (dif_custo_total_fmt), citando o que ela custa a mais ou a menos (dif_preco_fmt) e quanto CO2e evita ou emite a mais (reducao_co2e_fmt).", c.Rotulo, ref))
		}
		partes = append(partes, "Tabela curta com as linhas Preço, CO2e estimado, Carbono (referência) e Custo com carbono, uma coluna por alternativa e a coluna Diferença (dif_preco_fmt, dif_co2e_fmt, dif_carbono_fmt, dif_custo_total_fmt).")
		switch c.Situacao {
		case carbono.PagaMaisEmiteMenos:
			partes = append(partes, fmt.Sprintf("Uma frase com o preço de virada (preco_virada_fmt): %q compensa enquanto o carbono valer mais que esse preço por tonelada.", c.Rotulo))
		case carbono.PagaMenosEmiteMais:
			partes = append(partes, fmt.Sprintf("Uma frase com o preço de virada (preco_virada_fmt): %q compensa enquanto o carbono valer menos que esse preço por tonelada.", c.Rotulo))
		default:
			partes = append(partes, "Não escreva frase de preço de virada: ele não se aplica a esta comparação.")
		}
	} else {
		partes = append(partes,
			"Primeira frase: diga qual alternativa tem o menor custo com carbono (recomendada). Se houver empate no custo com carbono, diga isso e aponte a de menor CO2e.",
			"Tabela com as linhas Preço, CO2e estimado, Carbono (referência) e Custo com carbono, uma coluna por alternativa, sem a coluna Diferença.",
			fmt.Sprintf("Abaixo da tabela, uma frase por alternativa com as diferenças contra a referência %q (dif_preco_fmt, reducao_co2e_fmt, dif_custo_total_fmt). "+
				"Cite o preço de virada só nas alternativas que trazem preco_virada_fmt: com situacao paga_mais_emite_menos, ela compensa enquanto o carbono valer mais que esse preço; "+
				"com paga_menos_emite_mais, enquanto valer menos. Com melhor_nos_dois, diga que compensa a qualquer preço de carbono; com pior_nos_dois, que não compensa.", ref))
	}

	partes = append(partes, "Uma frase de premissa. "+fraseDoPreco(out.PrecoCarbono)+" Use 'cerca de' ou 'estimado' junto ao CO2e.")
	partes = append(partes, frasesExtras(precoIncomum, confiancaBaixa)...)
	partes = append(partes, "Ofereça recalcular com outro preço por tonelada, em reais.")
	return numerar(partes)
}
