// Package cambio busca a cotação PTAX (R$ por US$) na API pública do Banco
// Central (Olinda). Sem credencial.
package cambio

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// BaseURL pode ser trocada nos testes.
var BaseURL = "https://olinda.bcb.gov.br/olinda/servico/PTAX/versao/v1/odata"

// Cotacao é a PTAX de venda usada na conversão.
type Cotacao struct {
	Taxa float64 // R$ por US$
	Data string  // data da cotação (AAAA-MM-DD)
	// ForaDaData é true quando a data pedida está no futuro e usamos a
	// cotação mais recente disponível.
	ForaDaData bool
}

// PTAX devolve a última cotação de venda até a data de referência (olha até
// 10 dias para trás, por causa de fins de semana e feriados).
func PTAX(ctx context.Context, hc *http.Client, ref time.Time) (Cotacao, error) {
	out := Cotacao{}
	if now := time.Now(); ref.IsZero() || ref.After(now) {
		ref = now
		out.ForaDaData = true
	}
	const f = "01-02-2006"
	url := fmt.Sprintf("%s/CotacaoDolarPeriodo(dataInicial=@dataInicial,dataFinalCotacao=@dataFinalCotacao)"+
		"?@dataInicial=%%27%s%%27&@dataFinalCotacao=%%27%s%%27&$top=1&$orderby=dataHoraCotacao%%20desc"+
		"&$format=json&$select=cotacaoVenda,dataHoraCotacao",
		BaseURL, ref.AddDate(0, 0, -10).Format(f), ref.Format(f))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return out, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return out, fmt.Errorf("PTAX indisponível: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("PTAX respondeu HTTP %d", resp.StatusCode)
	}
	var body struct {
		Value []struct {
			CotacaoVenda    float64 `json:"cotacaoVenda"`
			DataHoraCotacao string  `json:"dataHoraCotacao"`
		} `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return out, fmt.Errorf("PTAX: resposta inválida: %w", err)
	}
	if len(body.Value) == 0 || body.Value[0].CotacaoVenda <= 0 {
		return out, fmt.Errorf("PTAX sem cotação até %s", ref.Format("2006-01-02"))
	}
	out.Taxa = body.Value[0].CotacaoVenda
	if d := body.Value[0].DataHoraCotacao; len(d) >= 10 {
		out.Data = d[:10]
	}
	return out, nil
}
