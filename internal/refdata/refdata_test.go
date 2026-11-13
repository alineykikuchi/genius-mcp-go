package refdata

import "testing"

func TestTabelasCarregadas(t *testing.T) {
	if len(fatores) < 1000 {
		t.Fatalf("esperava ~1016 fatores USEEIO, vieram %d", len(fatores))
	}
	if len(municipios) < 5500 {
		t.Fatalf("esperava ~5570 municípios, vieram %d", len(municipios))
	}
	if len(ufs) != 27 {
		t.Fatalf("esperava 27 UFs, vieram %d", len(ufs))
	}
}

func TestFatorNAICS(t *testing.T) {
	f, ok := FatorNAICS("111110")
	if !ok || f.KgCO2ePorUSD != 0.488 {
		t.Fatalf("111110 (sem margens) deveria ser 0.488: %+v %v", f, ok)
	}
	if _, ok := FatorNAICS("999999"); ok {
		t.Fatal("999999 não existe")
	}
}

func TestCandidatosNAICS(t *testing.T) {
	c := CandidatosNAICS("333999", 10) // não existe; deve achar vizinhos 3339xx
	if len(c) == 0 {
		t.Fatal("sem candidatos")
	}
	for _, f := range c {
		if f.NAICS[:4] != "3339" {
			t.Fatalf("candidato fora do prefixo: %s", f.NAICS)
		}
	}
}

func TestMunicipioTextoLivre(t *testing.T) {
	// Grafias como aparecem no cadastro do ME.
	for _, c := range []struct{ nome, uf string }{
		{"OTACÍLIO COSTA", "SC"},
		{"TELEMACO BORBA", "PR"},
		{"Guarulhos", "sp"},
		{"  são paulo ", "SP"},
	} {
		if _, ok := Municipio(c.nome, c.uf); !ok {
			t.Errorf("não achou %q/%s", c.nome, c.uf)
		}
	}
	if _, ok := Municipio("Cidade Inexistente", "SP"); ok {
		t.Error("achou cidade que não existe")
	}
	if _, ok := CentroideUF("sc"); !ok {
		t.Error("sem centroide de SC")
	}
}
