package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"genius-mcp-go/internal/carbono"
	"genius-mcp-go/internal/co2"
	"genius-mcp-go/internal/datahub"
	"genius-mcp-go/internal/refdata"
)

// Recomendação de fornecedores com o CO2 do frete (spec, seção 11).
//
// Os candidatos e o score de compra (preço, frete, prazo, pontualidade,
// capacidade, cobertura) vêm do recomendador de Trunk. Esta tool acrescenta um
// critério: a distância de cada fornecedor até o local de entrega do pedido,
// que é o que muda o CO2 do frete entre fornecedores do mesmo item.

func registerRecomendacao(s *mcp.Server, d co2Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "esg_recomendar_fornecedores",
		Description: "Recomenda fornecedores para os itens de um pedido, requisição ou cotação, reordenando os candidatos pelo CO2 do frete " +
			"(distância até o local de entrega). Use quando pedirem fornecedores alternativos, mais próximos ou com menor emissão. " +
			"Primeiro chame sem itens: a resposta traz os itens do documento. Para cada item, monte o nucleo: alternativas de palavras da descrição que identificam " +
			"o item (ex.: [[\"CANETA\"],[\"BIC\",\"CRISTAL\"]]), uma palavra por elemento, sem código nem medida. No máximo 12 " +
			"alternativas somando todos os itens. Informe também o peso em kg por unidade, se já tiver estimado. " +
			"Siga o campo como_responder da resposta. " +
			"Se o usuário informar a cidade de entrega, ou se ela já veio em outra tool de ESG para o mesmo documento, passe em destino_municipio e destino_uf. " +
			"Não invente o local de entrega.",
	}, d.recomendar)
}

type BuscaItem struct {
	Item             int        `json:"item" jsonschema:"número do item, como devolvido em itens_do_documento"`
	Nucleo           [][]string `json:"nucleo" jsonschema:"alternativas que identificam o item; cada alternativa é uma lista de palavras que precisam aparecer juntas na descrição"`
	Restricoes       []string   `json:"restricoes,omitempty" jsonschema:"palavras obrigatórias (todas)"`
	Preferencias     []string   `json:"preferencias,omitempty" jsonschema:"palavras desejadas; com todas, conta como o mesmo item"`
	Excluir          []string   `json:"excluir,omitempty" jsonschema:"palavras que descartam o candidato"`
	PesoKgPorUnidade float64    `json:"peso_kg_por_unidade,omitempty" jsonschema:"peso estimado em kg de uma unidade do item; sem ele, o CO2 do frete em kg não é calculado, só a ordem"`
}

type RecomendarIn struct {
	Tipo    string      `json:"tipo,omitempty" jsonschema:"pedido (padrão), requisicao ou cotacao"`
	Numero  string      `json:"numero" jsonschema:"número do documento no ME ou no cliente (ERP)"`
	Itens   []BuscaItem `json:"itens,omitempty" jsonschema:"itens para os quais buscar fornecedores; vazio devolve só os itens do documento"`
	PesoCO2 *float64    `json:"peso_co2,omitempty" jsonschema:"peso do CO2 do frete no score final, de 0 a 1 (padrão 0.3); o restante é o score do recomendador"`
	Modal   string      `json:"modal,omitempty" jsonschema:"rodoviario (padrão), ferroviario, cabotagem ou aereo"`
	TopN    int         `json:"top_n,omitempty" jsonschema:"quantos fornecedores devolver (padrão 10, máximo 50)"`

	DestinoMunicipio string `json:"destino_municipio,omitempty" jsonschema:"município de entrega, informado pelo usuário ou vindo de outra tool de ESG para o mesmo documento; use junto com destino_uf"`
	DestinoUF        string `json:"destino_uf,omitempty" jsonschema:"sigla da UF de entrega, por exemplo SP"`

	PrecoCarbonoBRLT *float64 `json:"preco_carbono_brl_t,omitempty" jsonschema:"preço do carbono em reais por tonelada, se o usuário informar; ausente usa o padrão"`
}

type FornecedorAtual struct {
	Nome        string   `json:"nome,omitempty"`
	Origem      string   `json:"origem,omitempty"`
	DistanciaKm *float64 `json:"distancia_km"`
	CO2FreteKg  *float64 `json:"co2_frete_kg"`
}

type Alternativa struct {
	Posicao             int      `json:"posicao"`
	PosicaoSemCO2       int      `json:"posicao_sem_co2"`
	Fornecedor          string   `json:"fornecedor"`
	CNPJ                string   `json:"cnpj,omitempty"`
	Origem              string   `json:"origem,omitempty"`
	FornecedorAtual     bool     `json:"e_o_fornecedor_atual,omitempty"`
	DistanciaKm         *float64 `json:"distancia_km"`
	DistanciaPorUF      bool     `json:"distancia_por_uf,omitempty"`
	CO2FreteKg          *float64 `json:"co2_frete_kg"`
	DeltaCO2Kg          *float64 `json:"delta_co2_frete_kg_vs_atual"`
	ScoreRecomendador   float64  `json:"score_recomendador"`
	NotaCO2             float64  `json:"nota_co2"`
	ScoreFinal          float64  `json:"score_final"`
	QtdPedidos          int      `json:"qtd_pedidos"`
	UltimoPedido        string   `json:"ultimo_pedido,omitempty"`
	PrazoMedioDias      *float64 `json:"prazo_medio_dias,omitempty"`
	PctNoPrazo          *float64 `json:"pct_no_prazo,omitempty"`
	JaVendeuAoComprador bool     `json:"ja_vendeu_a_sua_empresa"`

	// Carbono do frete evitado contra o fornecedor atual, em reais: positivo
	// quando a alternativa emite menos. Só existe quando o delta existe.
	CarbonoEvitadoBRL *float64 `json:"carbono_frete_evitado_brl,omitempty"`
	CarbonoEvitadoFmt string   `json:"carbono_frete_evitado_fmt,omitempty"`
	CO2FreteFmt       string   `json:"co2_frete_fmt,omitempty"`
	DeltaCO2Fmt       string   `json:"delta_co2_frete_fmt,omitempty"`
}

type PesosRecomendacao struct {
	Recomendador float64 `json:"recomendador"`
	CO2          float64 `json:"co2"`
}

type RecomendarOut struct {
	Tipo             string                `json:"tipo"`
	Documento        PedidoResumo          `json:"documento"`
	ItensDoDocumento []ItemParaClassificar `json:"itens_do_documento,omitempty"`
	ProximoPasso     string                `json:"proximo_passo,omitempty"`
	Destino          string                `json:"destino,omitempty"`
	DestinoOrigem    string                `json:"destino_origem,omitempty"` // informado, cabecalho ou itens
	DestinoPorUF     bool                  `json:"destino_por_uf,omitempty"`
	CargaKg          *float64              `json:"carga_kg"`
	Modal            string                `json:"modal"`
	Pesos            PesosRecomendacao     `json:"pesos"`
	FornecedorAtual  FornecedorAtual       `json:"fornecedor_atual"`
	Alternativas     []Alternativa         `json:"alternativas"`
	Premissas        []string              `json:"premissas"`

	PrecoCarbono  *PrecoCarbono `json:"preco_carbono,omitempty"`
	ComoResponder string        `json:"como_responder"`
}

func (d co2Deps) recomendar(ctx context.Context, req *mcp.CallToolRequest, in RecomendarIn) (*mcp.CallToolResult, RecomendarOut, error) {
	out := RecomendarOut{}

	precoCarbono, avisosPreco, err := d.resolverPrecoCarbono(in.PrecoCarbonoBRLT)
	if err != nil {
		return nil, out, err
	}

	pesoCO2 := 0.3
	if in.PesoCO2 != nil {
		pesoCO2 = *in.PesoCO2
	}
	if pesoCO2 < 0 || pesoCO2 > 1 {
		return nil, out, fmt.Errorf("peso_co2 inválido: fica entre 0 e 1")
	}
	modal := strings.ToLower(strings.TrimSpace(in.Modal))
	modalAssumido := modal == ""
	if modalAssumido {
		modal = "rodoviario"
	}
	fatorModal, ok := co2.FatoresModal[modal]
	if !ok {
		return nil, out, fmt.Errorf("modal %q inválido: use rodoviario, ferroviario, cabotagem ou aereo", in.Modal)
	}
	informado, temInformado, err := destinoInformado(in.DestinoMunicipio, in.DestinoUF)
	if err != nil {
		return nil, out, err
	}
	topN := in.TopN
	switch {
	case topN <= 0:
		topN = 10
	case topN > 50:
		topN = 50
	}

	tipo, nomeDoc, o, err := d.carregarDocumento(ctx, req, in.Tipo, in.Numero)
	if err != nil {
		return nil, out, err
	}
	out.Tipo = tipo
	out.Documento = resumo(o)
	out.Modal = modal
	out.Pesos = PesosRecomendacao{Recomendador: arred(1 - pesoCO2), CO2: arred(pesoCO2)}
	premissas := newPremissas()

	// Sem itens: devolve os itens do documento para o modelo montar o núcleo.
	if len(in.Itens) == 0 {
		for _, it := range o.Items {
			if it.Canceled != nil && *it.Canceled {
				continue
			}
			out.ItensDoDocumento = append(out.ItensDoDocumento, ItemParaClassificar{
				Item: it.Item, Codigo: it.Code, Descricao: it.Description, Complemento: it.DescriptionComplement,
				Quantidade: it.Quantity, Unidade: it.UnitOfMeasure,
			})
		}
		if len(out.ItensDoDocumento) == 0 {
			return nil, out, fmt.Errorf("%s %s não tem itens ativos para buscar fornecedores", nomeDoc, o.ID)
		}
		out.Alternativas = []Alternativa{}
		out.ProximoPasso = "Para cada item, monte o nucleo (alternativas de palavras da descrição) e, se puder, estime o peso em kg de uma unidade. " +
			"Depois chame esg_recomendar_fornecedores de novo com o mesmo tipo, o mesmo numero e a lista de itens."
		out.Premissas = []string{}
		out.ComoResponder = "Ainda não há fornecedores para apresentar: siga o campo proximo_passo e chame a tool de novo com os itens."
		return nil, out, nil
	}

	// Itens da busca: as palavras vêm do modelo; a quantidade, do documento.
	doPedido := map[int]datahub.Item{}
	for _, it := range o.Items {
		if it.Canceled != nil && *it.Canceled {
			continue
		}
		doPedido[it.Item] = it
	}
	var busca []datahub.ItemBusca
	var cargaKg float64
	alternativas, cargaCompleta := 0, true
	for _, b := range in.Itens {
		it, existe := doPedido[b.Item]
		nucleo := limparNucleo(b.Nucleo)
		if !existe || len(nucleo) == 0 {
			premissas.add("Há itens informados que não estão no documento (ou estão cancelados) ou vieram sem palavras de núcleo: ficaram fora da busca.")
			continue
		}
		alternativas += len(nucleo)
		busca = append(busca, datahub.ItemBusca{
			Item: b.Item, Nucleo: nucleo, Quantidade: it.Quantity,
			Restricoes: limparPalavras(b.Restricoes), Preferencias: limparPalavras(b.Preferencias), Excluir: limparPalavras(b.Excluir),
		})
		if b.PesoKgPorUnidade > 0 && it.Quantity != nil {
			cargaKg += *it.Quantity * b.PesoKgPorUnidade
		} else {
			cargaCompleta = false
		}
	}
	if len(busca) == 0 {
		return nil, out, fmt.Errorf("informe ao menos um item do documento com nucleo (palavras da descrição que identificam o item)")
	}
	if alternativas > datahub.MaxAlternativas {
		return nil, out, fmt.Errorf("são %d alternativas de núcleo; o recomendador aceita no máximo %d por chamada, somando todos os itens: "+
			"reduza as alternativas ou busque menos itens por vez", alternativas, datahub.MaxAlternativas)
	}
	if cargaCompleta {
		out.CargaKg = ptr(arred(cargaKg))
	} else {
		premissas.add("Sem o peso de todos os itens buscados, o CO2 do frete em kg não foi calculado. A ordem não muda: para a mesma carga, o CO2 do frete é proporcional à distância.")
	}

	rq := datahub.RecomendarReq{Itens: busca, TopN: topN}
	if o.TenantID != nil {
		rq.IDMainComprador = *o.TenantID
	}
	candidatos, err := d.dh.Recomendar(ctx, rq)
	if err != nil {
		return nil, out, err
	}

	// Destino: o informado na chamada, depois o cabeçalho do documento, depois o
	// local de entrega dos itens buscados. O informado vale só para esta chamada.
	destino, origemDestino := d.destinoDoDocumento(ctx, o, busca, premissas, !temInformado)
	if temInformado {
		rotulo := informado.rotulo
		if informado.porUF {
			rotulo = juntar(in.DestinoMunicipio, strings.ToUpper(in.DestinoUF))
			premissas.add("Município de entrega informado não reconhecido: a distância usou o ponto de referência da UF.")
		}
		if destino.ok && destino.rotulo != informado.rotulo {
			premissas.add(fmt.Sprintf("Destino informado (%s) diferente do local de entrega do documento (%s).", rotulo, destino.rotulo))
		}
		destino, origemDestino = informado, "informado"
	}
	out.Destino = destino.rotulo
	out.DestinoOrigem = origemDestino
	out.DestinoPorUF = destino.ok && destino.porUF
	if !destino.ok {
		premissas.add("Município e UF de entrega ausentes ou não reconhecidos: sem distância, a nota de CO2 ficou neutra (0,5) para todos e a ordem é a do recomendador.")
	}
	distancia := func(origem local) *float64 {
		if !origem.ok || !destino.ok {
			return nil
		}
		return ptr(arred(co2.DistanciaKm(origem.p.Lat, origem.p.Lon, destino.p.Lat, destino.p.Lon)))
	}
	frete := func(km *float64) *float64 {
		if km == nil || out.CargaKg == nil {
			return nil
		}
		return ptr(arred(co2.Transporte(1, *out.CargaKg, *km, fatorModal)))
	}

	// Fornecedor atual (só no pedido): referência da comparação.
	atual := localizar(cidadeUF(o.Supplier))
	out.FornecedorAtual = FornecedorAtual{Nome: out.Documento.Fornecedor, Origem: atual.rotulo, DistanciaKm: distancia(atual)}
	out.FornecedorAtual.CO2FreteKg = frete(out.FornecedorAtual.DistanciaKm)
	nomeAtual := refdata.Norm(out.Documento.Fornecedor)
	if o.Supplier == nil {
		premissas.add(fmt.Sprintf("Documento (%s) ainda sem fornecedor: sem referência, a diferença de CO2 contra o fornecedor atual não é calculada.", nomeDoc))
	}

	var minKm, maxKm float64
	temKm := false
	for _, f := range candidatos {
		origem := localizar(f.Municipio, f.UF)
		a := Alternativa{
			PosicaoSemCO2: f.Posicao, Fornecedor: strings.TrimSpace(f.Nome), CNPJ: strings.TrimSpace(f.CNPJ), Origem: origem.rotulo,
			ScoreRecomendador: f.Score, QtdPedidos: f.QtdPedidos, UltimoPedido: f.UltimoPedido,
			PrazoMedioDias: f.PrazoRealMedioDias, PctNoPrazo: f.PctNoPrazo, JaVendeuAoComprador: f.JaVendeuAoComprador > 0,
			DistanciaKm: distancia(origem),
		}
		a.FornecedorAtual = nomeAtual != "" && refdata.Norm(a.Fornecedor) == nomeAtual
		if a.DistanciaKm != nil {
			a.DistanciaPorUF = origem.porUF
			if origem.porUF {
				premissas.add("Município de alguns fornecedores não reconhecido: a distância deles usou o ponto de referência da UF.")
			}
			if !temKm || *a.DistanciaKm < minKm {
				minKm = *a.DistanciaKm
			}
			if !temKm || *a.DistanciaKm > maxKm {
				maxKm = *a.DistanciaKm
			}
			temKm = true
		} else if destino.ok {
			premissas.add("Município e UF de alguns fornecedores ausentes ou não reconhecidos: sem distância, a nota de CO2 deles ficou neutra (0,5).")
		}
		a.CO2FreteKg = frete(a.DistanciaKm)
		if a.CO2FreteKg != nil && out.FornecedorAtual.CO2FreteKg != nil {
			a.DeltaCO2Kg = ptr(arred(*a.CO2FreteKg - *out.FornecedorAtual.CO2FreteKg))
		}
		out.Alternativas = append(out.Alternativas, a)
	}

	// Nota de CO2: 1 = menor distância entre os candidatos; sem distância = 0,5.
	// É o mesmo sentido das notas do recomendador (1 = melhor).
	for i := range out.Alternativas {
		a := &out.Alternativas[i]
		switch {
		case a.DistanciaKm == nil:
			a.NotaCO2 = 0.5
		case maxKm == minKm:
			a.NotaCO2 = 1
		default:
			a.NotaCO2 = arred(1 - (*a.DistanciaKm-minKm)/(maxKm-minKm))
		}
		a.ScoreFinal = arred((1-pesoCO2)*a.ScoreRecomendador + pesoCO2*a.NotaCO2*100)
	}
	sort.SliceStable(out.Alternativas, func(i, j int) bool {
		return out.Alternativas[i].ScoreFinal > out.Alternativas[j].ScoreFinal
	})
	for i := range out.Alternativas {
		out.Alternativas[i].Posicao = i + 1
	}
	if out.Alternativas == nil {
		out.Alternativas = []Alternativa{}
		premissas.add("Nenhum fornecedor encontrado com essas palavras no histórico: tente palavras mais gerais no nucleo.")
	}

	m := "informado"
	if modalAssumido {
		m = "assumido (o documento não traz o modal)"
	}
	premissas.add("Candidatos: fornecedores que já venderam itens com essas palavras na descrição, nos últimos 36 meses (recomendador de fornecedores, flow do ME-DataHub). " +
		"Protótipo válido só em Trunk: o recomendador lê o histórico de pedidos de todos os clientes.")
	premissas.add("Score do recomendador (0 a 100): preço, frete, prazo, pontualidade, capacidade e cobertura, com pesos iguais.")
	premissas.add(fmt.Sprintf("Score final = %.0f%% do score do recomendador + %.0f%% da nota de CO2. A nota de CO2 vai de 0 a 1 (1 = menor distância entre os candidatos).",
		(1-pesoCO2)*100, pesoCO2*100))
	premissas.add(fmt.Sprintf("O CO2 só reordena os %d primeiros candidatos do recomendador; não traz para a lista quem ficou fora desse corte.", topN))
	fonteDestino := ""
	switch origemDestino {
	case "informado":
		fonteDestino = " (informado pelo usuário)"
	case "cabecalho":
		fonteDestino = " (cabeçalho do documento)"
	case "itens":
		fonteDestino = " (local de entrega dos itens)"
	}
	premissas.add(fmt.Sprintf("Distância = linha reta entre o município do fornecedor e o da entrega%s × %.1f, com mínimo de %.0f km; modal %s %s; fator de %.3f kg CO2e por t·km (%s).",
		fonteDestino, co2.FatorSinuosidade, co2.DistanciaMinimaKm, modal, m, fatorModal, co2.FonteModal))
	premissas.add("O CO2 do produto não entra na comparação: no método por gasto ele não diferencia fornecedores do mesmo item. Entre fornecedores, o que muda é o frete, que costuma ser parcela pequena do total.")
	premissas.add("Peso dos itens estimado por IA. Resultado de triagem: não substitui inventário de emissões auditado nem a homologação do fornecedor.")

	// Carbono do frete em reais: só quando há peso da carga (CO2 do frete em kg).
	temDelta := false
	if out.CargaKg != nil {
		maiorKg := 0.0
		if out.FornecedorAtual.CO2FreteKg != nil {
			maiorKg = *out.FornecedorAtual.CO2FreteKg
		}
		for _, a := range out.Alternativas {
			if a.CO2FreteKg != nil {
				maiorKg = max(maiorKg, *a.CO2FreteKg)
			}
		}
		emT := carbono.UsarToneladas(maiorKg)
		for i := range out.Alternativas {
			a := &out.Alternativas[i]
			if a.CO2FreteKg != nil {
				a.CO2FreteFmt = carbono.CO2e(*a.CO2FreteKg, emT)
			}
			if a.DeltaCO2Kg != nil {
				temDelta = true
				a.DeltaCO2Fmt = carbono.CO2eComSinal(*a.DeltaCO2Kg, emT)
				a.CarbonoEvitadoBRL = ptr(arred(-*a.DeltaCO2Kg / 1000 * precoCarbono.ValorBRLPorT))
				a.CarbonoEvitadoFmt = carbono.BRL(*a.CarbonoEvitadoBRL)
			}
		}
		// Preço e premissas de preço só aparecem quando há valor em reais na resposta.
		if temDelta {
			out.PrecoCarbono = &precoCarbono
			for _, p := range premissasCarbono(precoCarbono, false) {
				premissas.add(p)
			}
			for _, a := range avisosPreco {
				premissas.add(a)
			}
		}
	}
	out.ComoResponder = comoResponderRecomendacao(out.CargaKg != nil, temDelta, precoCarbono.Origem == "usuario", len(avisosPreco) > 0, out.DestinoOrigem, out.DestinoPorUF)
	out.Premissas = premissas.lista
	return nil, out, nil
}

// destinoInformado valida o destino informado na chamada: município e UF vêm
// juntos e a UF precisa existir. Município desconhecido cai no ponto da UF.
func destinoInformado(municipio, uf string) (l local, tem bool, err error) {
	municipio, uf = strings.TrimSpace(municipio), strings.ToUpper(strings.TrimSpace(uf))
	if municipio == "" && uf == "" {
		return local{}, false, nil
	}
	if municipio == "" || uf == "" {
		return local{}, false, fmt.Errorf("Informe o município e a UF de entrega.")
	}
	if _, ok := refdata.CentroideUF(uf); !ok {
		return local{}, false, fmt.Errorf("UF de entrega inválida: use a sigla, por exemplo SP.")
	}
	return localizar(municipio, uf), true, nil
}

// destinoDoDocumento acha o local de entrega no documento: primeiro o do
// cabeçalho; sem ele, o dos itens buscados (o que aparece em mais itens; em
// empate, o do item de menor número). Devolve também a origem: "cabecalho",
// "itens" ou vazio. Com avisar, registra nas premissas a falha de leitura e os
// locais diferentes.
func (d co2Deps) destinoDoDocumento(ctx context.Context, o *datahub.Order, busca []datahub.ItemBusca, premissas *premissas, avisar bool) (local, string) {
	if l := localizar(enderecoCidadeUF(o.DeliveryAddress)); l.ok {
		return l, "cabecalho"
	}
	// Inclui os itens com o mesmo local do cabeçalho: ele não tem endereço reconhecido.
	locais, err := d.locaisDosItens(ctx, o, true)
	if err != nil {
		if avisar {
			premissas.add("Não foi possível ler o local de entrega dos itens: a recomendação seguiu sem local de entrega.")
		}
		return local{}, ""
	}
	numeros := make([]int, 0, len(busca))
	for _, b := range busca {
		numeros = append(numeros, b.Item)
	}
	sort.Ints(numeros)
	vezes := map[string]int{}
	for _, n := range numeros {
		if l, ok := locais[n]; ok && l.ok {
			vezes[l.rotulo]++
		}
	}
	var melhor local
	for _, n := range numeros {
		if l, ok := locais[n]; ok && l.ok && (!melhor.ok || vezes[l.rotulo] > vezes[melhor.rotulo]) {
			melhor = l
		}
	}
	if !melhor.ok {
		return local{}, ""
	}
	if len(vezes) > 1 && avisar {
		premissas.add(fmt.Sprintf("Os itens buscados têm locais de entrega diferentes; a distância usou %s. Para os outros locais, busque os itens em chamadas separadas.", melhor.rotulo))
	}
	return melhor, "itens"
}

// comoResponderRecomendacao monta a orientação de resposta da recomendação.
// temPeso: há CO2 do frete em kg. temDelta: há fornecedor atual para comparar.
// destinoOrigem: de onde veio o local de entrega (vazio = não encontrado).
func comoResponderRecomendacao(temPeso, temDelta, precoDoUsuario, precoIncomum bool, destinoOrigem string, destinoPorUF bool) string {
	var partes []string
	switch {
	case destinoOrigem == "":
		partes = append(partes, "O local de entrega não foi encontrado no documento. Apresente os fornecedores na ordem do recomendador, "+
			"em uma tabela curta com Fornecedor e Origem, diga em uma frase que falta o local de entrega para calcular distância e CO2 do frete, "+
			"e peça a cidade e a UF de entrega. Quando o usuário responder, chame a tool de novo com destino_municipio e destino_uf.")
	case !temPeso:
		partes = append(partes, "O CO2e em kg não foi calculado porque falta o peso; apresente só a ordem por distância, "+
			"em uma tabela curta com Fornecedor, Origem e Distância.")
	case !temDelta:
		partes = append(partes,
			"Comece pela alternativa com menor CO2e de frete que ainda está bem colocada (posicao), dizendo o CO2e do frete (co2_frete_fmt). "+
				"Não há fornecedor atual para comparar: não cite CO2e evitado nem carbono evitado.",
			"Mostre uma tabela curta com Fornecedor, Origem, Distância e CO2e do frete.")
	default:
		p := "Comece pela alternativa com menor CO2e de frete que ainda está bem colocada (posicao), dizendo quanto CO2e evita contra o fornecedor atual (delta_co2_frete_fmt) " +
			"e quanto isso vale em carbono (carbono_frete_evitado_fmt), valor de referência que não é cobrado. " +
			"Delta com sinal de mais (e carbono evitado negativo) quer dizer que a alternativa emite mais que o fornecedor atual: nesse caso diga que ela emite mais, não que evita. " +
			"Se nenhuma alternativa emitir menos, diga que o fornecedor atual já tem o menor CO2e de frete."
		if precoDoUsuario {
			p += " O preço por tonelada foi informado pelo usuário: escreva 'valor informado por você' no lugar de 'valor de referência'."
		}
		partes = append(partes, p,
			"Mostre uma tabela curta com Fornecedor, Origem, Distância, CO2e do frete e Carbono evitado.")
	}
	if destinoOrigem == "informado" {
		partes = append(partes, "Diga que a distância usou o local de entrega informado (destino).")
	}
	if destinoPorUF {
		partes = append(partes, "Diga que o município não foi reconhecido e que a distância é aproximada, pelo ponto de referência da UF.")
	}
	partes = append(partes,
		"Diga quem já vendeu à empresa do usuário (ja_vendeu_a_sua_empresa).",
		"Esta lista não traz preço dos fornecedores. Não compare custo com carbono aqui; para isso, peça os preços ao comprador e use esg_precificar_carbono.")
	partes = append(partes, frasesExtras(precoIncomum && temDelta, false)...)
	partes = append(partes, "Não mostre premissas nem metodologia; só se o usuário pedir. Não recalcule nem invente valores.")
	return numerar(partes)
}

// limparNucleo normaliza as alternativas e descarta as vazias.
func limparNucleo(alts [][]string) [][]string {
	var out [][]string
	for _, a := range alts {
		if p := limparPalavras(a); len(p) > 0 {
			out = append(out, p)
		}
	}
	return out
}

// limparPalavras deixa uma palavra por elemento, em maiúsculas: o recomendador
// casa palavra a palavra, e um elemento com espaço nunca casaria.
func limparPalavras(ws []string) []string {
	var out []string
	for _, w := range ws {
		w = strings.NewReplacer(`"`, " ", `\`, " ").Replace(w)
		out = append(out, strings.Fields(strings.ToUpper(w))...)
	}
	return out
}
