// Package co2 tem as fórmulas da estimativa (spec da Tool 1, seção 7).
// Só contas: não busca dado nenhum.
package co2

import "math"

const (
	// FatorSinuosidade converte linha reta em distância rodoviária aproximada.
	FatorSinuosidade = 1.3
	// DistanciaMinimaKm vale quando origem e destino são o mesmo município
	// (ou muito próximos), onde a linha reta dá zero.
	DistanciaMinimaKm = 20.0
	raioTerraKm       = 6371.0
)

// FonteModal identifica de onde vêm os fatores de transporte.
const FonteModal = "ordem de grandeza GLEC/DEFRA (kg CO2e por t·km); edição a fixar"

// FatoresModal: kg CO2e por tonelada-quilômetro.
var FatoresModal = map[string]float64{
	"rodoviario":  0.10,
	"ferroviario": 0.03,
	"cabotagem":   0.015,
	"aereo":       0.80,
}

// Haversine devolve a distância em linha reta, em km.
func Haversine(lat1, lon1, lat2, lon2 float64) float64 {
	rad := func(g float64) float64 { return g * math.Pi / 180 }
	dLat := rad(lat2 - lat1)
	dLon := rad(lon2 - lon1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(rad(lat1))*math.Cos(rad(lat2))*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * raioTerraKm * math.Asin(math.Sqrt(a))
}

// DistanciaKm aplica a sinuosidade e a distância mínima à linha reta.
func DistanciaKm(lat1, lon1, lat2, lon2 float64) float64 {
	return math.Max(Haversine(lat1, lon1, lat2, lon2)*FatorSinuosidade, DistanciaMinimaKm)
}

// Produto: CO2 do produto pelo método de gasto. valor na moeda local,
// cambio em moeda local por US$ (use 1 se o valor já está em US$).
func Produto(valor, cambio, fatorKgPorUSD float64) float64 {
	return valor / cambio * fatorKgPorUSD
}

// Transporte: peso (t) × distância (km) × fator do modal.
func Transporte(quantidade, pesoUnitarioKg, distanciaKm, fatorModal float64) float64 {
	return quantidade * pesoUnitarioKg / 1000 * distanciaKm * fatorModal
}
