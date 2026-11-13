// Package refdata embute as tabelas públicas de referência da estimativa de CO2:
//
//   - USEEIO / EPA "Supply Chain GHG Emission Factors" v1.3.0 (kg CO2e por US$ de
//     2022, por NAICS 2017 de 6 dígitos). Usamos a coluna SEM margens.
//   - Municípios e UFs do IBGE com latitude e longitude
//     (github.com/kelvins/municipios-brasileiros).
//
// São dados de referência, não transacionais: os dados do pedido vêm do DataHub.
package refdata

import (
	"bytes"
	_ "embed"
	"encoding/csv"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

//go:embed data/useeio_v1.3_naics_usd2022.csv
var useeioCSV []byte

//go:embed data/municipios.csv
var municipiosCSV []byte

//go:embed data/estados.csv
var estadosCSV []byte

// FonteUSEEIO identifica a edição dos fatores, para as premissas da resposta.
const FonteUSEEIO = "USEEIO/EPA Supply Chain GHG Emission Factors v1.3.0, sem margens (kg CO2e por US$ de 2022, NAICS 2017)"

// Fator é o fator de emissão de uma categoria NAICS.
type Fator struct {
	NAICS        string  `json:"naics"`
	Titulo       string  `json:"titulo"`
	KgCO2ePorUSD float64 `json:"kg_co2e_por_usd"`
}

// Ponto é uma coordenada geográfica.
type Ponto struct {
	Nome string
	UF   string
	Lat  float64
	Lon  float64
}

var (
	fatores      = map[string]Fator{}
	codigosNAICS []string             // ordenados, para a busca de candidatos
	municipios   = map[string]Ponto{} // chave: Norm(nome)|UF
	ufs          = map[string]Ponto{} // chave: sigla
)

func init() {
	if err := carregar(); err != nil {
		panic("refdata: " + err.Error())
	}
}

func lerCSV(b []byte) ([][]string, error) {
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")) // BOM
	rows, err := csv.NewReader(bytes.NewReader(b)).ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("csv vazio")
	}
	return rows[1:], nil
}

func carregar() error {
	rows, err := lerCSV(useeioCSV)
	if err != nil {
		return fmt.Errorf("useeio: %w", err)
	}
	for _, r := range rows {
		if len(r) < 5 {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(r[4]), 64)
		if err != nil {
			return fmt.Errorf("useeio: fator inválido para %s: %w", r[0], err)
		}
		code := strings.TrimSpace(r[0])
		fatores[code] = Fator{NAICS: code, Titulo: r[1], KgCO2ePorUSD: v}
		codigosNAICS = append(codigosNAICS, code)
	}
	sort.Strings(codigosNAICS)

	rows, err = lerCSV(estadosCSV)
	if err != nil {
		return fmt.Errorf("estados: %w", err)
	}
	siglaPorCodigo := map[string]string{}
	for _, r := range rows {
		if len(r) < 5 {
			continue
		}
		lat, e1 := strconv.ParseFloat(r[3], 64)
		lon, e2 := strconv.ParseFloat(r[4], 64)
		if e1 != nil || e2 != nil {
			return fmt.Errorf("estados: coordenada inválida para %s", r[1])
		}
		siglaPorCodigo[r[0]] = r[1]
		ufs[r[1]] = Ponto{Nome: r[2], UF: r[1], Lat: lat, Lon: lon}
	}

	rows, err = lerCSV(municipiosCSV)
	if err != nil {
		return fmt.Errorf("municipios: %w", err)
	}
	for _, r := range rows {
		if len(r) < 6 {
			continue
		}
		lat, e1 := strconv.ParseFloat(r[2], 64)
		lon, e2 := strconv.ParseFloat(r[3], 64)
		if e1 != nil || e2 != nil {
			return fmt.Errorf("municipios: coordenada inválida para %s", r[1])
		}
		uf := siglaPorCodigo[r[5]]
		municipios[Norm(r[1])+"|"+uf] = Ponto{Nome: r[1], UF: uf, Lat: lat, Lon: lon}
	}
	return nil
}

var semAcento = map[rune]rune{
	'Á': 'A', 'À': 'A', 'Â': 'A', 'Ã': 'A', 'Ä': 'A',
	'É': 'E', 'È': 'E', 'Ê': 'E', 'Ë': 'E',
	'Í': 'I', 'Ì': 'I', 'Î': 'I', 'Ï': 'I',
	'Ó': 'O', 'Ò': 'O', 'Ô': 'O', 'Õ': 'O', 'Ö': 'O',
	'Ú': 'U', 'Ù': 'U', 'Û': 'U', 'Ü': 'U',
	'Ç': 'C', 'Ñ': 'N',
}

// Norm normaliza um nome de cidade para comparação: maiúsculas, sem acento,
// só letras e dígitos, espaços únicos. O cadastro do ME é texto livre.
func Norm(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if t, ok := semAcento[r]; ok {
			r = t
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// FatorNAICS devolve o fator sem margens de um NAICS 2017 de 6 dígitos.
func FatorNAICS(naics string) (Fator, bool) {
	f, ok := fatores[strings.TrimSpace(naics)]
	return f, ok
}

// CandidatosNAICS devolve até max categorias válidas que compartilham o maior
// prefixo possível com o código informado. Serve para o modelo corrigir um
// NAICS que não existe na tabela.
func CandidatosNAICS(naics string, max int) []Fator {
	naics = strings.TrimSpace(naics)
	for n := 5; n >= 2; n-- {
		if len(naics) < n {
			continue
		}
		var out []Fator
		for _, c := range codigosNAICS {
			if strings.HasPrefix(c, naics[:n]) {
				out = append(out, fatores[c])
				if len(out) == max {
					break
				}
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

// Municipio procura a coordenada de um município pelo nome e pela UF.
func Municipio(nome, uf string) (Ponto, bool) {
	p, ok := municipios[Norm(nome)+"|"+strings.ToUpper(strings.TrimSpace(uf))]
	return p, ok
}

// CentroideUF devolve a coordenada de referência de uma UF.
func CentroideUF(uf string) (Ponto, bool) {
	p, ok := ufs[strings.ToUpper(strings.TrimSpace(uf))]
	return p, ok
}
