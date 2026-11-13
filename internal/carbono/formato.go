package carbono

import (
	"math"
	"strconv"
	"strings"
)

// Formatação em pt-BR dos campos _fmt: milhar com ponto, decimal com vírgula e
// sinal de menos U+2212.

const menos = "−"

// BRL formata um valor em reais: inteiro sem centavos ("R$ 600"), quebrado com
// duas casas ("R$ 4.560,20").
func BRL(v float64) string {
	v = arred(v)
	s := "R$ " + numero(math.Abs(v), casasBRL(v))
	if v < 0 {
		return menos + " " + s
	}
	return s
}

// BRLComSinal é BRL com "+ " ou "− " na frente. Zero sai sem sinal.
func BRLComSinal(v float64) string {
	if arred(v) > 0 {
		return "+ " + BRL(v)
	}
	return BRL(v)
}

// PrecoPorT formata um preço em reais por tonelada ("R$ 500/t").
func PrecoPorT(v float64) string { return BRL(v) + "/t" }

// CO2e formata uma massa de CO2e: em kg, sem casas ("1.200 kg"), ou em
// toneladas, com uma casa ("12,5 t"). Quem chama decide a unidade uma vez por
// resposta (UsarToneladas) e passa a mesma escolha a todas as chamadas.
func CO2e(kg float64, emToneladas bool) string {
	n, v := co2eNumero(kg, emToneladas)
	if v < 0 {
		return menos + " " + n
	}
	return n
}

// CO2eComSinal é CO2e com "+ " ou "− " na frente. Zero sai sem sinal.
func CO2eComSinal(kg float64, emToneladas bool) string {
	n, v := co2eNumero(kg, emToneladas)
	switch {
	case v < 0:
		return menos + " " + n
	case v > 0:
		return "+ " + n
	}
	return n
}

// UsarToneladas diz se a resposta mostra o CO2e em toneladas: quando o maior
// valor da resposta é de 10.000 kg ou mais.
func UsarToneladas(maiorKg float64) bool { return maiorKg >= 10000 }

// Pct formata um percentual sem casas; abaixo de 1, "menos de 1%".
func Pct(v float64) string {
	if v < 1 {
		return "menos de 1%"
	}
	return numero(math.Round(v), 0) + "%"
}

// Reducao formata a redução de CO2e: positiva vira "menos", negativa vira "mais".
func Reducao(v float64) string {
	n := numero(math.Round(math.Abs(v)), 0)
	switch {
	case n == "0":
		return "igual"
	case v < 0:
		return n + "% mais"
	}
	return n + "% menos"
}

// co2eNumero devolve o número com a unidade, sem sinal, e o valor arredondado
// (na unidade escolhida), de onde sai o sinal.
func co2eNumero(kg float64, emToneladas bool) (string, float64) {
	v, casas, unidade := kg, 0, " kg"
	if emToneladas {
		v, casas, unidade = kg/1000, 1, " t"
	}
	p := math.Pow(10, float64(casas))
	v = math.Round(v*p) / p
	return numero(math.Abs(v), casas) + unidade, v
}

func casasBRL(v float64) int {
	if v == math.Trunc(v) {
		return 0
	}
	return 2
}

// numero formata v (não negativo) com milhar "." e decimal ",".
func numero(v float64, casas int) string {
	s := strconv.FormatFloat(v, 'f', casas, 64)
	inteiro, decimal, _ := strings.Cut(s, ".")
	var b strings.Builder
	for i, d := range inteiro {
		if i > 0 && (len(inteiro)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(d)
	}
	if decimal != "" {
		b.WriteString("," + decimal)
	}
	return b.String()
}
