package co2

import (
	"math"
	"testing"
)

func perto(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestHaversineSaoPauloRio(t *testing.T) {
	// São Paulo (-23.5329, -46.6395) → Rio de Janeiro (-22.9129, -43.2003): ~357 km em linha reta.
	d := Haversine(-23.5329, -46.6395, -22.9129, -43.2003)
	if !perto(d, 357, 5) {
		t.Fatalf("esperava ~357 km, veio %.1f", d)
	}
}

func TestDistanciaMinimaNoMesmoMunicipio(t *testing.T) {
	if d := DistanciaKm(-23.5, -46.6, -23.5, -46.6); d != DistanciaMinimaKm {
		t.Fatalf("mesmo ponto deveria dar a mínima, veio %.1f", d)
	}
	if d := DistanciaKm(-23.5329, -46.6395, -22.9129, -43.2003); !perto(d, 357*1.3, 8) {
		t.Fatalf("SP-Rio com sinuosidade: %.1f", d)
	}
}

func TestProduto(t *testing.T) {
	// R$ 5.400 a 5,40 = US$ 1.000; fator 0,5 → 500 kg.
	if v := Produto(5400, 5.40, 0.5); !perto(v, 500, 1e-9) {
		t.Fatalf("veio %v", v)
	}
}

func TestTransporte(t *testing.T) {
	// 160 un × 12,5 kg = 2 t; 802 km; rodoviário 0,10 → 160,4 kg (exemplo da spec).
	if v := Transporte(160, 12.5, 802, FatoresModal["rodoviario"]); !perto(v, 160.4, 1e-9) {
		t.Fatalf("veio %v", v)
	}
}
