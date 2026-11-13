// Package carbono converte CO2e em valor em reais (precificação do carbono) e
// compara alternativas pelo custo com carbono (preço + carbono). Só contas e
// formatação, sem rede. Especificação: docs/precificacao-carbono/spec-precificacao-carbono.md.
package carbono

import "math"

const (
	// PrecoPadraoBRLT é o preço de referência, em reais por tonelada de CO2e.
	PrecoPadraoBRLT = 500.0
	Referencia      = "EU ETS (referência fixa do ECO.ME)"
	// Fora da faixa PrecoMinComum a PrecoMaxComum o preço gera aviso de valor incomum.
	PrecoMinComum = 10.0
	PrecoMaxComum = 5000.0
	// Abaixo deste percentual do valor do pedido o carbono é irrelevante.
	LimiteIrrelevantePct = 1.0
)

// Situações da alternativa contra a referência.
const (
	PagaMaisEmiteMenos = "paga_mais_emite_menos"
	PagaMenosEmiteMais = "paga_menos_emite_mais"
	MelhorNosDois      = "melhor_nos_dois"
	PiorNosDois        = "pior_nos_dois"
)

// Vereditos da alternativa no preço em uso.
const (
	Compensa    = "compensa"
	NaoCompensa = "nao_compensa"
	Empate      = "empate"
)

// Custo é o resultado para um documento ou uma alternativa.
type Custo struct {
	CO2eKg         float64
	CustoCarbono   float64  // R$
	Valor          *float64 // R$, nil quando não informado
	CustoTotal     *float64 // Valor + CustoCarbono
	PesoCarbonoPct *float64 // CustoCarbono / Valor × 100; nil sem valor ou com valor zero
	Irrelevante    bool     // PesoCarbonoPct < LimiteIrrelevantePct
}

// Precificar calcula o custo do carbono de co2eKg ao preço precoBRLT (R$/t) e,
// quando há valor, o custo com carbono e o peso do carbono sobre o valor.
func Precificar(co2eKg float64, valor *float64, precoBRLT float64) Custo {
	c := Custo{CO2eKg: co2eKg, CustoCarbono: arred(co2eKg / 1000 * precoBRLT)}
	if valor == nil {
		return c
	}
	v := *valor
	c.Valor = &v
	total := arred(v + c.CustoCarbono)
	c.CustoTotal = &total
	if v > 0 {
		peso := c.CustoCarbono / v * 100
		c.Irrelevante = peso < LimiteIrrelevantePct
		peso = arred(peso)
		c.PesoCarbonoPct = &peso
	}
	return c
}

// Comparacao é a alternativa B contra a referência A.
type Comparacao struct {
	DifPreco       float64
	DifCO2eKg      float64
	ReducaoCO2ePct *float64 // nil quando o CO2e da referência é zero
	DifCarbono     float64
	DifCustoTotal  float64
	Situacao       string
	PrecoVirada    *float64 // R$/t, nil quando não se aplica
	Veredito       string
}

// Comparar compara a alternativa alt com a referência ref. Só faz sentido
// quando as duas têm Valor; valor ausente conta como zero.
func Comparar(ref, alt Custo) Comparacao {
	valorA, valorB := valorDe(ref), valorDe(alt)
	co2A, co2B := ref.CO2eKg, alt.CO2eKg

	c := Comparacao{
		DifPreco:      arred(valorB - valorA),
		DifCO2eKg:     co2B - co2A,
		DifCarbono:    arred(alt.CustoCarbono - ref.CustoCarbono),
		DifCustoTotal: arred((valorB + alt.CustoCarbono) - (valorA + ref.CustoCarbono)),
	}
	if co2A != 0 {
		r := arred((co2A - co2B) / co2A * 100)
		c.ReducaoCO2ePct = &r
	}

	// A primeira condição verdadeira decide. Nas duas primeiras o CO2e difere,
	// então a divisão do preço de virada nunca é por zero.
	switch {
	case valorB > valorA && co2B < co2A:
		c.Situacao = PagaMaisEmiteMenos
		c.PrecoVirada = virada(valorB-valorA, co2A-co2B)
	case valorB < valorA && co2B > co2A:
		c.Situacao = PagaMenosEmiteMais
		c.PrecoVirada = virada(valorB-valorA, co2A-co2B)
	case valorB <= valorA && co2B <= co2A:
		c.Situacao = MelhorNosDois
	default:
		c.Situacao = PiorNosDois
	}

	switch {
	case c.DifCustoTotal < 0:
		c.Veredito = Compensa
	case c.DifCustoTotal > 0:
		c.Veredito = NaoCompensa
	default:
		c.Veredito = Empate
	}
	return c
}

// virada é o preço do carbono, em R$/t, em que as duas alternativas empatam.
func virada(difPreco, co2EvitadoKg float64) *float64 {
	v := math.Round(difPreco / (co2EvitadoKg / 1000))
	return &v
}

func valorDe(c Custo) float64 {
	if c.Valor == nil {
		return 0
	}
	return *c.Valor
}

func arred(v float64) float64 { return math.Round(v*100) / 100 }
