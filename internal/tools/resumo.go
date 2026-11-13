package tools

import (
	"os"
	"strconv"
	"strings"

	"genius-mcp-go/internal/carbono"
)

// Resumo da estimativa de CO2 (docs/precificacao-carbono/plano-3-formato-da-resposta-e-faixas.md, parte A).
//
// O modelo não faz conta nem decide critério: o total formatado, o veredito, o
// item que concentra o impacto e a confiança já saem prontos daqui.

// Faixas padrão do veredito, em % do valor da compra.
const (
	faixaBaixoPadraoPct = 1.0
	faixaAltoPadraoPct  = 5.0
)

// FaixasCarbono são os limites do veredito: peso do carbono abaixo de
// BaixoAtePct é baixo; a partir de AltoAPartirPct é alto; entre os dois, médio.
type FaixasCarbono struct {
	BaixoAtePct    float64 `json:"baixo_ate_pct"`
	AltoAPartirPct float64 `json:"alto_a_partir_pct"`
	Origem         string  `json:"origem"` // padrao ou cliente
}

// faixasDoAmbiente lê as faixas padrão de FAIXA_CARBONO_BAIXO_PCT e
// FAIXA_CARBONO_ALTO_PCT. Variável vazia ou inválida usa o padrão dela; baixo
// maior ou igual ao alto volta as duas ao padrão.
func faixasDoAmbiente() FaixasCarbono {
	ler := func(nome string, padrao float64) float64 {
		v, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv(nome)), 64)
		if err != nil || !(v > 0) || v > 100 {
			return padrao
		}
		return v
	}
	f := FaixasCarbono{
		BaixoAtePct:    ler("FAIXA_CARBONO_BAIXO_PCT", faixaBaixoPadraoPct),
		AltoAPartirPct: ler("FAIXA_CARBONO_ALTO_PCT", faixaAltoPadraoPct),
		Origem:         "padrao",
	}
	if f.BaixoAtePct >= f.AltoAPartirPct {
		f.BaixoAtePct, f.AltoAPartirPct = faixaBaixoPadraoPct, faixaAltoPadraoPct
	}
	return f
}

// faixasCarbono devolve as faixas em uso; sem configuração, as padrão.
func (d co2Deps) faixasCarbono() FaixasCarbono {
	if d.faixas.BaixoAtePct > 0 && d.faixas.BaixoAtePct < d.faixas.AltoAPartirPct {
		return d.faixas
	}
	return FaixasCarbono{BaixoAtePct: faixaBaixoPadraoPct, AltoAPartirPct: faixaAltoPadraoPct, Origem: "padrao"}
}

type ItemPrincipal struct {
	Item      int    `json:"item"`
	Descricao string `json:"descricao"`
	PctFmt    string `json:"pct_fmt"`
}

type FaltaTransporte struct {
	Motivo string `json:"motivo"`
}

// ResumoEstimativa é o bloco resumo de esg_estimar_co2_do_pedido.
type ResumoEstimativa struct {
	TotalFmt        string           `json:"total_fmt,omitempty"`
	Veredito        string           `json:"veredito,omitempty"` // baixo, medio ou alto
	VereditoBase    string           `json:"veredito_base,omitempty"`
	Faixas          FaixasCarbono    `json:"faixas"`
	ItemPrincipal   *ItemPrincipal   `json:"item_principal,omitempty"`
	Confianca       string           `json:"confianca"` // alta, media ou baixa
	FaltaTransporte *FaltaTransporte `json:"falta_transporte,omitempty"`
}

// vereditoCarbono classifica o peso do carbono sobre o valor (em %).
func vereditoCarbono(pesoPct float64, f FaixasCarbono) string {
	switch {
	case pesoPct < f.BaixoAtePct:
		return "baixo"
	case pesoPct >= f.AltoAPartirPct:
		return "alto"
	}
	return "medio"
}

// confiancaDosItens é a média das confianças dos itens classificados, ponderada
// pelo CO2e de cada um; sem CO2e calculado, média simples.
func confiancaDosItens(itens []ItemEstimado) string {
	var soma, pesos, simples float64
	n := 0
	for _, e := range itens {
		if e.Confianca == nil {
			continue
		}
		n++
		simples += *e.Confianca
		if e.CO2eKg != nil && *e.CO2eKg > 0 {
			soma += *e.Confianca * *e.CO2eKg
			pesos += *e.CO2eKg
		}
	}
	if n == 0 {
		return "baixa"
	}
	media := simples / float64(n)
	if pesos > 0 {
		media = soma / pesos
	}
	switch {
	case media >= 0.75:
		return "alta"
	case media >= 0.5:
		return "media"
	}
	return "baixa"
}

// resumirEstimativa preenche os campos prontos de cada item (co2e, _fmt e
// parte do total) e monta o bloco resumo. doc é nil quando não há total de CO2e.
func resumirEstimativa(out *EstimarOut, doc *CarbonoDocumento, faixas FaixasCarbono, falta *FaltaTransporte) {
	r := ResumoEstimativa{Faixas: faixas, FaltaTransporte: falta}

	total := 0.0
	if out.CO2.TotalKg != nil {
		total = *out.CO2.TotalKg
	}
	emT := carbono.UsarToneladas(total)
	principal := -1
	for i := range out.Itens {
		e := &out.Itens[i]
		if e.Valor != nil && e.Moeda == "BRL" {
			e.ValorFmt = carbono.BRL(*e.Valor)
		}
		if e.CO2ProdutoKg == nil && e.CO2TransporteKg == nil {
			continue
		}
		kg := 0.0
		if e.CO2ProdutoKg != nil {
			kg += *e.CO2ProdutoKg
		}
		if e.CO2TransporteKg != nil {
			kg += *e.CO2TransporteKg
		}
		e.CO2eKg, e.CO2eFmt = ptr(arred(kg)), carbono.CO2e(kg, emT)
		if total > 0 {
			e.PctDoTotal = ptr(arred(kg / total * 100))
			e.PctFmt = carbono.Pct(*e.PctDoTotal)
		}
		if principal < 0 || kg > *out.Itens[principal].CO2eKg {
			principal = i
		}
	}

	if out.CO2.TotalKg != nil {
		r.TotalFmt = carbono.CO2e(total, emT)
	}
	if doc != nil && doc.PesoCarbonoPercentual != nil {
		r.Veredito = vereditoCarbono(*doc.PesoCarbonoPercentual, faixas)
		r.VereditoBase = "o carbono equivale a " + doc.PesoCarbonoFmt + " do valor da compra"
	}
	if len(out.Itens) > 1 && principal >= 0 && out.Itens[principal].PctFmt != "" {
		p := out.Itens[principal]
		r.ItemPrincipal = &ItemPrincipal{Item: p.Item, Descricao: p.Descricao, PctFmt: p.PctFmt}
	}
	r.Confianca = confiancaDosItens(out.Itens)
	out.Resumo = r
}

// confiancaPorExtenso é a confiança como aparece no texto ao comprador.
func confiancaPorExtenso(c string) string {
	if c == "media" {
		return "média"
	}
	return c
}

// comoResponderEstimativa monta a orientação de resposta da estimativa de CO2.
// doc é nil quando não houve total de CO2e para precificar.
func comoResponderEstimativa(doc *CarbonoDocumento, r ResumoEstimativa, itens int, precoIncomum bool) string {
	var partes []string
	if doc == nil {
		partes = append(partes, "Diga em uma frase que o CO2e do documento não pôde ser calculado e o motivo principal "+
			"(campos_vazios e naics_invalido dos itens). Não cite valor do carbono.")
	} else {
		if r.Veredito != "" {
			partes = append(partes, "Primeira linha: o total de CO2e (resumo.total_fmt) e o veredito (resumo.veredito), com a base (resumo.veredito_base).")
		} else {
			partes = append(partes, "Primeira linha: só o total de CO2e (resumo.total_fmt). Não há veredito nem percentual: o documento não tem valor em reais.")
		}
		if itens > 1 {
			partes = append(partes, "Uma frase dizendo qual item concentra o impacto (resumo.item_principal). "+
				"Depois UMA tabela com Item, Valor, CO2e e % do total, usando os campos _fmt.")
		} else {
			partes = append(partes, "Não use tabela; cite produto e transporte em uma frase.")
		}
		p := "Uma frase com o valor do carbono (carbono.custo_carbono_fmt), dizendo que é referência e não é cobrado."
		if doc.PrecoCarbono.Origem == "usuario" {
			p = "Uma frase com o valor do carbono (carbono.custo_carbono_fmt), dizendo que usa o preço por tonelada informado pelo usuário ('valor informado por você') e que não é cobrado."
		}
		partes = append(partes, p)
		if r.FaltaTransporte != nil {
			partes = append(partes, "Uma linha de aviso dizendo que falta o transporte e o motivo (resumo.falta_transporte.motivo).")
		}
		partes = append(partes, frasesExtras(precoIncomum, false)...)
		partes = append(partes, "Termine com o próximo passo: oferecer fornecedores com menor CO2 de frete ou recálculo com outro preço por tonelada.")
	}
	partes = append(partes,
		"Última linha, em itálico: 'Estimativa de triagem com confiança "+confiancaPorExtenso(r.Confianca)+" (peso e categoria estimados).'",
		"NÃO mostre premissas, NAICS, peso estimado, câmbio, fatores, pesos custo x CO2 nem modal. Só apresente a lista de premissas, completa, se o usuário pedir. "+
			"Não descreva passos internos nem diga que não alterou o documento. No máximo 10 linhas. Não recalcule nem invente valores.")
	return numerar(partes)
}
