package carbono

import (
	"math"
	"testing"
)

func p(v float64) *float64 { return &v }

func igual(a, b float64) bool { return math.Abs(a-b) < 0.005 }

func TestPrecificar(t *testing.T) {
	casos := []struct {
		nome        string
		co2eKg      float64
		valor       *float64
		preco       float64
		carbono     float64
		total       *float64
		peso        *float64
		irrelevante bool
	}{
		{"pedido com valor", 9120.4, p(48000), 500, 4560.20, p(52560.20), p(9.5), false},
		{"carbono irrelevante", 28, p(10000), 500, 14, p(10014), p(0.14), true},
		{"sem valor do pedido", 28, nil, 500, 14, nil, nil, false},
		{"valor zero não divide por zero", 28, p(0), 500, 14, p(14), nil, false},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got := Precificar(c.co2eKg, c.valor, c.preco)
			if !igual(got.CustoCarbono, c.carbono) {
				t.Fatalf("custo do carbono: veio %v, esperava %v", got.CustoCarbono, c.carbono)
			}
			conferirPtr(t, "custo total", got.CustoTotal, c.total)
			conferirPtr(t, "peso do carbono", got.PesoCarbonoPct, c.peso)
			if got.Irrelevante != c.irrelevante {
				t.Fatalf("irrelevante: veio %v, esperava %v", got.Irrelevante, c.irrelevante)
			}
		})
	}
}

func conferirPtr(t *testing.T, nome string, got, want *float64) {
	t.Helper()
	switch {
	case got == nil && want == nil:
	case got == nil || want == nil:
		t.Fatalf("%s: veio %v, esperava %v", nome, got, want)
	case !igual(*got, *want):
		t.Fatalf("%s: veio %v, esperava %v", nome, *got, *want)
	}
}

func TestComparar(t *testing.T) {
	casos := []struct {
		nome                 string
		valorA, co2A         float64
		valorB, co2B         float64
		preco                float64
		difPreco, difCO2     float64
		reducao              *float64
		difCarbono, difTotal float64
		situacao             string
		virada               *float64
		veredito             string
	}{
		{"mais cara e emite menos, a 500", 10000, 1200, 10300, 300, 500,
			300, -900, p(75), -450, -150, PagaMaisEmiteMenos, p(333), Compensa},
		{"o mesmo par, a 200", 10000, 1200, 10300, 300, 200,
			300, -900, p(75), -180, 120, PagaMaisEmiteMenos, p(333), NaoCompensa},
		{"mais barata e emite menos", 10000, 1200, 9000, 300, 500,
			-1000, -900, p(75), -450, -1450, MelhorNosDois, nil, Compensa},
		{"mais cara e emite mais", 10000, 300, 10300, 1200, 500,
			300, 900, p(-300), 450, 750, PiorNosDois, nil, NaoCompensa},
		{"mais barata e emite mais", 10300, 300, 10000, 1200, 500,
			-300, 900, p(-300), 450, 150, PagaMenosEmiteMais, p(333), NaoCompensa},
		{"referência com CO2e zero", 10000, 0, 10300, 300, 500,
			300, 300, nil, 150, 450, PiorNosDois, nil, NaoCompensa},
		{"iguais nos dois", 10000, 300, 10000, 300, 500,
			0, 0, p(0), 0, 0, MelhorNosDois, nil, Empate},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			a := Precificar(c.co2A, p(c.valorA), c.preco)
			b := Precificar(c.co2B, p(c.valorB), c.preco)
			got := Comparar(a, b)
			if !igual(got.DifPreco, c.difPreco) || !igual(got.DifCO2eKg, c.difCO2) ||
				!igual(got.DifCarbono, c.difCarbono) || !igual(got.DifCustoTotal, c.difTotal) {
				t.Fatalf("diferenças: %+v", got)
			}
			conferirPtr(t, "redução de CO2e", got.ReducaoCO2ePct, c.reducao)
			conferirPtr(t, "preço de virada", got.PrecoVirada, c.virada)
			if got.Situacao != c.situacao || got.Veredito != c.veredito {
				t.Fatalf("situação %q e veredito %q; esperava %q e %q", got.Situacao, got.Veredito, c.situacao, c.veredito)
			}
		})
	}
}

func TestTotaisDoParA200(t *testing.T) {
	a := Precificar(1200, p(10000), 200)
	b := Precificar(300, p(10300), 200)
	if !igual(*a.CustoTotal, 10240) || !igual(*b.CustoTotal, 10360) {
		t.Fatalf("totais: %v e %v", *a.CustoTotal, *b.CustoTotal)
	}
}

func TestFormatos(t *testing.T) {
	casos := []struct{ nome, got, want string }{
		{"BRL inteiro", BRL(600), "R$ 600"},
		{"BRL quebrado", BRL(4560.2), "R$ 4.560,20"},
		{"BRL milhar", BRL(48000), "R$ 48.000"},
		{"BRL milhão", BRL(1234567.891), "R$ 1.234.567,89"},
		{"BRL com sinal positivo", BRLComSinal(300), "+ R$ 300"},
		{"BRL com sinal negativo", BRLComSinal(-150), "− R$ 150"},
		{"BRL com sinal zero", BRLComSinal(0), "R$ 0"},
		{"preço por tonelada", PrecoPorT(500), "R$ 500/t"},
		{"preço de virada", PrecoPorT(333), "R$ 333/t"},
		{"CO2e em kg", CO2e(1200, false), "1.200 kg"},
		{"CO2e em kg arredonda", CO2e(9120.4, false), "9.120 kg"},
		{"CO2e em t", CO2e(12500, true), "12,5 t"},
		{"CO2e com sinal", CO2eComSinal(-900, false), "− 900 kg"},
		{"CO2e com sinal positivo em t", CO2eComSinal(12500, true), "+ 12,5 t"},
		{"percentual", Pct(9.5), "10%"},
		{"percentual abaixo de 1", Pct(0.14), "menos de 1%"},
		{"redução", Reducao(75), "75% menos"},
		{"aumento", Reducao(-20), "20% mais"},
	}
	for _, c := range casos {
		if c.got != c.want {
			t.Errorf("%s: veio %q, esperava %q", c.nome, c.got, c.want)
		}
	}
}
