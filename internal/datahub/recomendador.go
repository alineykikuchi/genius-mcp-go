package datahub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Recomendador de fornecedores: flow REST recomendacao-fornecedores-recomendar,
// publicado em Trunk. Ele procura, nos pedidos da plataforma, quem já vendeu
// itens cuja descrição tem as palavras pedidas, e dá um score de 0 a 100.
//
// ATENÇÃO: os resolvers do flow não têm trava de tenant (leem pedidos de todos
// os clientes). É um protótipo, válido só em Trunk.

const caminhoRecomendar = "/recomendacao-fornecedores/recomendar"

// MaxAlternativas é o limite do flow: alternativas de núcleo por chamada,
// somando todos os itens.
const MaxAlternativas = 12

// ItemBusca descreve um item por palavras da descrição.
type ItemBusca struct {
	Item         int        `json:"item"`
	Nucleo       [][]string `json:"nucleo"`
	Quantidade   *float64   `json:"quantidade,omitempty"`
	Restricoes   []string   `json:"restricoes,omitempty"`
	Preferencias []string   `json:"preferencias,omitempty"`
	Excluir      []string   `json:"excluir,omitempty"`
}

type RecomendarReq struct {
	Itens []ItemBusca `json:"itens"`
	// IDMainComprador marca quem já vendeu a este comprador. O token de serviço
	// não tem tenant, então o flow usa este valor.
	IDMainComprador int `json:"idMainComprador,omitempty"`
	TopN            int `json:"topN,omitempty"`
	// IncluirPotenciais liga a lista de quem só cotou: é a parte lenta do flow e
	// não tem score.
	IncluirPotenciais bool `json:"incluirPotenciais"`
}

// FornecedorRecomendado é uma linha de "jaVenderam". Preço e descrição de
// exemplo existem na resposta do flow, mas não são lidos aqui: vêm de pedidos
// de outros clientes.
type FornecedorRecomendado struct {
	Posicao             int      `json:"posicao"`
	Nome                string   `json:"nomeFornecedor"`
	CNPJ                string   `json:"cnpj"`
	UF                  string   `json:"uf"`
	Municipio           string   `json:"municipio"`
	Score               float64  `json:"score"`
	QtdPedidos          int      `json:"qtdPedidos"`
	UltimoPedido        string   `json:"ultimoPedido"`
	PrazoRealMedioDias  *float64 `json:"prazoRealMedioDias"`
	PctNoPrazo          *float64 `json:"pctNoPrazo"`
	JaVendeuAoComprador int      `json:"jaVendeuAoComprador"`
}

// Recomendar chama o flow e devolve quem já vendeu os itens, na ordem do score
// do recomendador.
func (c *Client) Recomendar(ctx context.Context, in RecomendarReq) ([]FornecedorRecomendado, error) {
	body, _ := json.Marshal(in)
	status, raw, err := c.post(ctx, strings.TrimRight(c.FlowsURL, "/")+caminhoRecomendar, body)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return nil, fmt.Errorf("o DataHub recusou o token de serviço no recomendador (HTTP %d)", status)
	}
	if status != http.StatusOK {
		var e struct {
			Message string `json:"message"`
			Error   string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		if msg := strings.TrimSpace(e.Message + " " + e.Error); msg != "" {
			return nil, fmt.Errorf("o recomendador respondeu HTTP %d: %s", status, msg)
		}
		return nil, fmt.Errorf("o recomendador respondeu HTTP %d", status)
	}
	// A saída do flow pode vir direta ou dentro de "output"/"data".
	type saida struct {
		JaVenderam *[]FornecedorRecomendado `json:"jaVenderam"`
	}
	var env struct {
		saida
		Output *saida `json:"output"`
		Data   *saida `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("o recomendador devolveu uma resposta que não é JSON")
	}
	for _, s := range []*saida{&env.saida, env.Output, env.Data} {
		if s != nil && s.JaVenderam != nil {
			return *s.JaVenderam, nil
		}
	}
	return nil, fmt.Errorf("o recomendador devolveu uma resposta sem a lista de fornecedores")
}
