package tools

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"genius-mcp-go/internal/cambio"
	"genius-mcp-go/internal/co2"
	"genius-mcp-go/internal/datahub"
	"genius-mcp-go/internal/refdata"
	"genius-mcp-go/internal/token"
)

// Estimativa de CO2 de um pedido, requisição ou cotação (spec: docs/spec-tool1-estimativa-co2_1.md).
//
// São duas tools, porque quem classifica o item (NAICS e peso) é o modelo do
// próprio Genius, e não uma IA chamada daqui:
//
//  1. esg_itens_do_pedido      lê o pedido no DataHub e devolve os itens.
//  2. esg_estimar_co2_do_pedido recebe NAICS e peso por item, relê o pedido e calcula.
//
// A segunda relê o pedido em vez de confiar em valores vindos do modelo: preço,
// quantidade e endereços saem sempre do DataHub.

type co2Deps struct {
	dh   *datahub.Client
	http *http.Client
	// tenantClaim: nome da claim do token do Genius que traz o IdMain do
	// usuário (TENANT_CLAIM). Vazio = tenta os nomes de claimsDeTenant.
	tenantClaim string
	// tenantFiltro: "off" desliga o filtro por tenant (TENANT_FILTRO). Qualquer
	// outro valor mantém o filtro obrigatório.
	tenantFiltro string
	// precoCarbono: preço padrão do carbono, em reais por tonelada de CO2e
	// (PRECO_CARBONO_BRL_T). Zero = carbono.PrecoPadraoBRLT.
	precoCarbono float64
}

func registerCO2(s *mcp.Server, d co2Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "esg_itens_do_pedido",
		Description: "Passo 1 da análise de impacto ESG / pegada de carbono (CO2) de um pedido, requisição ou cotação de compra. " +
			"Lê o documento no ME (dados reais, da empresa do usuário) e devolve fornecedor, local de entrega e itens. " +
			"Depois de chamar esta tool, classifique CADA item em um código NAICS 2017 de 6 dígitos e estime o peso " +
			"em kg de UMA unidade do item (na unidade de medida do documento), e então chame esg_estimar_co2_do_pedido.",
	}, d.itensDoPedido)

	mcp.AddTool(s, &mcp.Tool{
		Name: "esg_estimar_co2_do_pedido",
		Description: "Passo 2 da análise de impacto ESG de um pedido, requisição ou cotação: calcula a estimativa de kg CO2e (produto + transporte) " +
			"e a relação custo x CO2. Exige a classificação de cada item (NAICS 2017 de 6 dígitos, peso em kg por unidade e " +
			"confiança de 0 a 1), feita por você a partir de esg_itens_do_pedido. Se a resposta trouxer itens com " +
			"naics_invalido, escolha um dos naics_candidatos e chame de novo. A resposta já traz o valor do carbono em reais (bloco carbono). " +
			"Siga o campo como_responder da resposta.",
	}, d.estimarCO2)
}

// ---- passo 1: itens do pedido ----------------------------------------------

type ItensIn struct {
	Tipo   string `json:"tipo,omitempty" jsonschema:"pedido (padrão), requisicao ou cotacao"`
	Numero string `json:"numero" jsonschema:"número do documento no ME ou no cliente (ERP)"`
}

type PedidoResumo struct {
	ID            string   `json:"id"`
	CodigoCliente string   `json:"codigo_cliente,omitempty"`
	DataEnvio     string   `json:"data_envio,omitempty"`
	Moeda         string   `json:"moeda,omitempty"`
	ValorTotal    *float64 `json:"valor_total,omitempty"`
	Fornecedor    string   `json:"fornecedor,omitempty"`
	Origem        string   `json:"origem,omitempty"`
	Destino       string   `json:"destino,omitempty"`
	Incoterms     string   `json:"incoterms,omitempty"`
}

type ItemParaClassificar struct {
	Item        int      `json:"item"`
	Codigo      string   `json:"codigo,omitempty"`
	Descricao   string   `json:"descricao"`
	Complemento string   `json:"complemento,omitempty"`
	GrupoCompra string   `json:"grupo_compras,omitempty"`
	Quantidade  *float64 `json:"quantidade,omitempty"`
	Unidade     string   `json:"unidade,omitempty"`
	Valor       *float64 `json:"valor,omitempty"`
}

type ItensOut struct {
	Tipo         string                `json:"tipo"`
	Documento    PedidoResumo          `json:"documento"`
	Itens        []ItemParaClassificar `json:"itens"`
	Cancelados   int                   `json:"itens_cancelados_ignorados"`
	ProximoPasso string                `json:"proximo_passo"`
}

func (d co2Deps) itensDoPedido(ctx context.Context, req *mcp.CallToolRequest, in ItensIn) (*mcp.CallToolResult, ItensOut, error) {
	out := ItensOut{}
	tipo, _, o, err := d.carregarDocumento(ctx, req, in.Tipo, in.Numero)
	if err != nil {
		return nil, out, err
	}
	out.Tipo = tipo
	out.Documento = resumo(o)
	for _, it := range o.Items {
		if it.Canceled != nil && *it.Canceled {
			out.Cancelados++
			continue
		}
		out.Itens = append(out.Itens, ItemParaClassificar{
			Item: it.Item, Codigo: it.Code, Descricao: it.Description, Complemento: it.DescriptionComplement,
			GrupoCompra: it.PurchaseGroup, Quantidade: it.Quantity, Unidade: it.UnitOfMeasure, Valor: valorItem(it),
		})
	}
	out.ProximoPasso = "Para cada item, escolha o NAICS 2017 de 6 dígitos da atividade que PRODUZ o item, estime o peso em kg " +
		"de uma unidade e dê uma confiança de 0 a 1. Depois chame esg_estimar_co2_do_pedido com o mesmo tipo, o mesmo numero e a lista de itens."
	return nil, out, nil
}

// ---- passo 2: estimativa ---------------------------------------------------

type Classificacao struct {
	Item             int     `json:"item" jsonschema:"número do item, como devolvido por esg_itens_do_pedido"`
	NAICS            string  `json:"naics" jsonschema:"código NAICS 2017 de 6 dígitos"`
	PesoKgPorUnidade float64 `json:"peso_kg_por_unidade" jsonschema:"peso estimado em kg de uma unidade do item"`
	Confianca        float64 `json:"confianca" jsonschema:"confiança da classificação, de 0 a 1"`
}

type EstimarIn struct {
	Tipo      string          `json:"tipo,omitempty" jsonschema:"o mesmo tipo usado em esg_itens_do_pedido: pedido (padrão), requisicao ou cotacao"`
	Numero    string          `json:"numero" jsonschema:"o mesmo número usado em esg_itens_do_pedido"`
	Itens     []Classificacao `json:"itens" jsonschema:"classificação de cada item do documento"`
	PesoCusto *float64        `json:"peso_custo,omitempty" jsonschema:"peso do custo no score, de 0 a 1 (padrão 0.7)"`
	PesoCO2   *float64        `json:"peso_co2,omitempty" jsonschema:"peso do CO2 no score, de 0 a 1 (padrão 0.3); a soma dos dois pesos é 1"`
	Modal     string          `json:"modal,omitempty" jsonschema:"rodoviario (padrão), ferroviario, cabotagem ou aereo"`

	PrecoCarbonoBRLT *float64 `json:"preco_carbono_brl_t,omitempty" jsonschema:"preço do carbono em reais por tonelada, se o usuário informar; ausente usa o padrão"`
}

type CO2Totais struct {
	TotalKg             *float64 `json:"total_kg"`
	ProdutoKg           *float64 `json:"produto_kg"`
	TransporteKg        *float64 `json:"transporte_kg"`
	IntensidadeKgPorBRL *float64 `json:"intensidade_kg_por_brl"`
}

type ItemEstimado struct {
	Item            int             `json:"item"`
	Descricao       string          `json:"descricao"`
	NAICS           string          `json:"naics,omitempty"`
	NAICSDescricao  string          `json:"naics_descricao,omitempty"`
	NAICSInvalido   bool            `json:"naics_invalido,omitempty"`
	NAICSCandidatos []refdata.Fator `json:"naics_candidatos,omitempty"`
	Confianca       *float64        `json:"confianca,omitempty"`
	BaixaConfianca  bool            `json:"baixa_confianca,omitempty"`
	Valor           *float64        `json:"valor"`
	Moeda           string          `json:"moeda"`
	PesoKg          *float64        `json:"peso_kg"`
	DistanciaKm     *float64        `json:"distancia_km"`
	Destino         string          `json:"destino,omitempty"`
	CO2ProdutoKg    *float64        `json:"co2_produto_kg"`
	CO2TransporteKg *float64        `json:"co2_transporte_kg"`
	CamposEstimados []string        `json:"campos_estimados,omitempty"`
	CamposVazios    []string        `json:"campos_vazios,omitempty"`
}

type Pesos struct {
	Custo float64 `json:"custo"`
	CO2   float64 `json:"co2"`
}

type EstimarOut struct {
	Tipo      string         `json:"tipo"`
	Documento PedidoResumo   `json:"documento"`
	CO2       CO2Totais      `json:"co2"`
	Itens     []ItemEstimado `json:"itens"`
	Pesos     Pesos          `json:"pesos"`
	Modal     string         `json:"modal"`
	Cambio    *float64       `json:"cambio_brl_por_usd,omitempty"`
	Premissas []string       `json:"premissas"`

	Carbono       *CarbonoDocumento `json:"carbono,omitempty"`
	ComoResponder string            `json:"como_responder"`
}

func (d co2Deps) estimarCO2(ctx context.Context, req *mcp.CallToolRequest, in EstimarIn) (*mcp.CallToolResult, EstimarOut, error) {
	out := EstimarOut{}

	pesos, err := resolverPesos(in.PesoCusto, in.PesoCO2)
	if err != nil {
		return nil, out, err
	}
	precoCarbono, avisosPreco, err := d.resolverPrecoCarbono(in.PrecoCarbonoBRLT)
	if err != nil {
		return nil, out, err
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

	tipo, nomeDoc, o, err := d.carregarDocumento(ctx, req, in.Tipo, in.Numero)
	if err != nil {
		return nil, out, err
	}
	out.Tipo = tipo
	out.Documento = resumo(o)
	out.Pesos = pesos
	out.Modal = modal

	premissas := newPremissas()
	premissas.add("Fatores de produto: " + refdata.FonteUSEEIO + ". São da economia dos EUA, convertidos por câmbio.")
	premissas.add("Categoria (NAICS) e peso de cada item estimados por IA; a confiança vem indicada por item.")
	if tipo != "pedido" {
		premissas.add(fmt.Sprintf("Na %s, o valor de cada item é o preço estimado × a quantidade, e a data usada no câmbio é a de criação.", nomeDoc))
	}
	if o.Supplier == nil {
		premissas.add(fmt.Sprintf("Documento (%s) ainda sem fornecedor: sem origem, o CO2 do transporte não foi calculado. "+
			"Para comparar o frete entre fornecedores possíveis, use esg_recomendar_fornecedores.", nomeDoc))
	}

	classe := map[int]Classificacao{}
	for _, c := range in.Itens {
		classe[c.Item] = c
	}

	// Origem: município do fornecedor.
	origem := localizar(cidadeUF(o.Supplier))
	// Destino: local do cabeçalho; o item pode ter um local próprio.
	destinoCab := localizar(enderecoCidadeUF(o.DeliveryAddress))
	locaisItem, err := d.locaisDosItens(ctx, o)
	if err != nil {
		premissas.add("Não foi possível ler o local de entrega próprio de alguns itens; foi usado o local do cabeçalho do documento.")
	}

	// Câmbio: uma cotação PTAX para o documento todo.
	var cot *cambio.Cotacao
	if c, err := cambio.PTAX(ctx, d.http, dataEnvio(o)); err == nil {
		cot = &c
		out.Cambio = &c.Taxa
		if c.ForaDaData {
			premissas.add(fmt.Sprintf("Câmbio: PTAX de venda de %s (R$ %.4f por US$), a mais recente disponível, porque a data do documento está no futuro ou ausente.", c.Data, c.Taxa))
		} else {
			premissas.add(fmt.Sprintf("Câmbio: PTAX de venda de %s (R$ %.4f por US$), Banco Central.", c.Data, c.Taxa))
		}
	} else {
		premissas.add("Câmbio PTAX indisponível no momento: o CO2 do produto de itens em reais não foi calculado.")
	}

	var somaProd, somaTransp, somaValorBRL float64
	var temProd, temTransp bool

	for _, it := range o.Items {
		if it.Canceled != nil && *it.Canceled {
			continue
		}
		e := ItemEstimado{Item: it.Item, Descricao: it.Description, Valor: valorItem(it)}

		// Moeda: cabeçalho, depois item, depois BRL (sinalizado).
		e.Moeda = strings.ToUpper(strings.TrimSpace(o.Currency))
		if e.Moeda == "" {
			e.Moeda = strings.ToUpper(strings.TrimSpace(it.Currency))
		}
		if e.Moeda == "" {
			e.Moeda = "BRL"
			e.CamposVazios = append(e.CamposVazios, "moeda")
			premissas.add("Moeda em branco no documento: assumido BRL.")
		}

		c, classificado := classe[it.Item]
		var fator refdata.Fator
		var fatorOK bool
		if !classificado {
			e.CamposVazios = append(e.CamposVazios, "naics", "peso_kg")
			premissas.add("Há itens sem classificação (NAICS/peso): não entraram no total.")
		} else {
			conf := c.Confianca
			e.Confianca = &conf
			e.BaixaConfianca = conf < 0.5
			e.NAICS = strings.TrimSpace(c.NAICS)
			fator, fatorOK = refdata.FatorNAICS(e.NAICS)
			if fatorOK {
				e.NAICSDescricao = fator.Titulo
			} else {
				e.NAICSInvalido = true
				e.NAICSCandidatos = refdata.CandidatosNAICS(e.NAICS, 12)
				premissas.add("Há itens com NAICS que não existe na tabela USEEIO: o CO2 do produto deles não foi calculado.")
			}
			e.CamposEstimados = append(e.CamposEstimados, "naics", "peso_kg")
		}

		// CO2 do produto (método por gasto).
		switch {
		case e.Valor == nil || *e.Valor <= 0:
			e.CamposVazios = append(e.CamposVazios, "valor")
		case !fatorOK:
		case e.Moeda == "USD":
			e.CO2ProdutoKg = ptr(arred(co2.Produto(*e.Valor, 1, fator.KgCO2ePorUSD)))
		case e.Moeda == "BRL" && cot != nil:
			e.CO2ProdutoKg = ptr(arred(co2.Produto(*e.Valor, cot.Taxa, fator.KgCO2ePorUSD)))
		case e.Moeda != "BRL":
			premissas.add("Há itens em moeda diferente de BRL e USD: sem conversão no MVP, o CO2 do produto deles não foi calculado.")
		}

		// CO2 do transporte.
		destino := destinoCab
		if l, ok := locaisItem[it.Item]; ok {
			destino = l
		}
		e.Destino = destino.rotulo
		if classificado && c.PesoKgPorUnidade > 0 && it.Quantity != nil {
			e.PesoKg = ptr(arred(*it.Quantity * c.PesoKgPorUnidade))
		}
		switch {
		case o.Supplier == nil:
			e.CamposVazios = append(e.CamposVazios, "distancia_km")
		case !origem.ok || !destino.ok:
			e.CamposVazios = append(e.CamposVazios, "distancia_km")
			premissas.add("Município ou UF do fornecedor ou da entrega ausente ou não reconhecido em alguns itens: o CO2 do transporte deles não foi calculado.")
		default:
			km := co2.DistanciaKm(origem.p.Lat, origem.p.Lon, destino.p.Lat, destino.p.Lon)
			e.DistanciaKm = ptr(arred(km))
			if origem.porUF || destino.porUF {
				e.CamposEstimados = append(e.CamposEstimados, "distancia_km")
				premissas.add("Município não reconhecido em alguns itens: a distância usou o ponto de referência da UF.")
			}
			if e.PesoKg != nil {
				e.CO2TransporteKg = ptr(arred(co2.Transporte(*it.Quantity, c.PesoKgPorUnidade, km, fatorModal)))
			}
		}

		if e.CO2ProdutoKg != nil {
			somaProd += *e.CO2ProdutoKg
			temProd = true
		}
		if e.CO2TransporteKg != nil {
			somaTransp += *e.CO2TransporteKg
			temTransp = true
		}
		if e.Valor != nil && (e.CO2ProdutoKg != nil || e.CO2TransporteKg != nil) {
			switch {
			case e.Moeda == "BRL":
				somaValorBRL += *e.Valor
			case e.Moeda == "USD" && cot != nil:
				somaValorBRL += *e.Valor * cot.Taxa
			}
		}
		out.Itens = append(out.Itens, e)
	}

	if temProd {
		out.CO2.ProdutoKg = ptr(arred(somaProd))
	}
	if temTransp {
		out.CO2.TransporteKg = ptr(arred(somaTransp))
	}
	if temProd || temTransp {
		out.CO2.TotalKg = ptr(arred(somaProd + somaTransp))
		if somaValorBRL > 0 {
			out.CO2.IntensidadeKgPorBRL = ptr(math.Round((somaProd+somaTransp)/somaValorBRL*10000) / 10000)
		}
	}

	m := "informado"
	if modalAssumido {
		m = "assumido (o documento não traz o modal)"
	}
	premissas.add(fmt.Sprintf("Distância = linha reta entre os municípios × %.1f, com mínimo de %.0f km; modal %s %s; fator de %.3f kg CO2e por t·km (%s).",
		co2.FatorSinuosidade, co2.DistanciaMinimaKm, modal, m, fatorModal, co2.FonteModal))
	premissas.add("Estimativa por gasto: compara categorias e pedidos, não diferencia fornecedores do mesmo item. Entre fornecedores, o que muda é a distância do transporte.")
	premissas.add("O score custo x CO2 só é normalizado quando há alternativas para comparar; aqui são devolvidos os valores absolutos e a intensidade em kg CO2e por R$.")
	premissas.add("Resultado de triagem: não substitui inventário de emissões auditado.")

	// Valor do carbono em reais: só quando há total de CO2e.
	confiancaBaixa := false
	for _, e := range out.Itens {
		confiancaBaixa = confiancaBaixa || e.BaixaConfianca
	}
	if out.CO2.TotalKg != nil {
		// O valor do documento só entra na conta quando está em reais.
		var valor *float64
		switch strings.ToUpper(strings.TrimSpace(o.Currency)) {
		case "BRL":
			valor = o.Total
		case "":
			valor = o.Total
			if valor != nil {
				premissas.add("Moeda do documento em branco: o valor do documento foi tratado como reais no custo com carbono.")
			}
		default:
			premissas.add("O valor do documento não está em reais; o custo com carbono não foi calculado.")
		}
		doc := novoCarbonoDocumento(*out.CO2.TotalKg, valor, precoCarbono)
		out.Carbono = &doc
		for _, p := range premissasCarbono(precoCarbono, false) {
			premissas.add(p)
		}
		for _, a := range avisosPreco {
			premissas.add(a)
		}
	}
	out.ComoResponder = comoResponderEstimativa(out.Carbono, len(avisosPreco) > 0, confiancaBaixa)
	out.Premissas = premissas.lista
	return nil, out, nil
}

// comoResponderEstimativa monta a orientação de resposta da estimativa de CO2.
// doc é nil quando não houve total de CO2e para precificar.
func comoResponderEstimativa(doc *CarbonoDocumento, precoIncomum, confiancaBaixa bool) string {
	var partes []string
	if doc == nil {
		partes = append(partes, "Diga que o CO2e do documento não pôde ser calculado e explique o motivo com as premissas e os campos_vazios dos itens. Não cite valor do carbono.")
	} else {
		p := "Diga quanto CO2e o documento emite (produto e transporte) e quanto isso vale em carbono (carbono.custo_carbono_fmt), deixando claro que é valor de referência e não é cobrado."
		if doc.PrecoCarbono.Origem == "usuario" {
			p += " O preço por tonelada foi informado pelo usuário: escreva 'valor informado por você' no lugar de 'referência (EU ETS)'."
		}
		partes = append(partes, p)
		switch {
		case doc.CarbonoIrrelevante:
			partes = append(partes, "Diga que o carbono pesa menos de 1% do valor.")
		case doc.PesoCarbonoPercentual != nil:
			partes = append(partes, "Diga o peso do carbono sobre o valor do documento (carbono.peso_carbono_fmt).")
		default:
			partes = append(partes, "Não cite custo com carbono nem percentual: não há valor do documento em reais.")
		}
	}
	if confiancaBaixa {
		partes = append(partes, "Diga quais itens têm baixa confiança e que o resultado é uma indicação.")
	}
	partes = append(partes, frasesExtras(precoIncomum && doc != nil, false)...)
	partes = append(partes,
		"Cite campos_vazios e campos_estimados só quando existirem na resposta; não comente a ausência deles.",
		"Apresente as premissas em uma ou duas frases, não em lista.")
	if doc == nil {
		partes = append(partes, "Não recalcule nem invente valores.")
	} else {
		partes = append(partes, "Ofereça ver fornecedores alternativos com menos CO2e ou recalcular com outro preço por tonelada, em reais. Não recalcule nem invente valores.")
	}
	return numerar(partes)
}

// ---- apoio -----------------------------------------------------------------

// claimsDeTenant: nomes de claim tentados, nesta ordem, quando TENANT_CLAIM não
// está definida.
var claimsDeTenant = []string{"tenantId", "tenant_id", "idMain", "IdMain", "id_main", "idmain", "workflowId", "workflow_id"}

// tenantDoUsuario lê o tenant (IdMain) da empresa do usuário no token que o
// Genius repassou. O DataHub é consultado com um token de serviço que enxerga
// todos os clientes, então é este valor que restringe a leitura.
//
// Devolve nil só quando o filtro foi desligado de propósito (TENANT_FILTRO=off).
func (d co2Deps) tenantDoUsuario(req *mcp.CallToolRequest) (*int, error) {
	if d.tenantFiltro == "off" {
		return nil, nil
	}
	var claims map[string]any
	if extra := req.GetExtra(); extra != nil && extra.TokenInfo != nil {
		claims, _ = extra.TokenInfo.Extra[token.ExtraClaims].(map[string]any)
	}
	nomes := claimsDeTenant
	if d.tenantClaim != "" {
		nomes = []string{d.tenantClaim}
	}
	for _, n := range nomes {
		switch v := claims[n].(type) {
		case float64:
			if v > 0 {
				return ptr(int(v)), nil
			}
		case string:
			if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && i > 0 {
				return &i, nil
			}
		}
	}
	return nil, fmt.Errorf("não foi possível identificar a empresa do usuário no token recebido do Genius; " +
		"sem isso a consulta não é feita. Configure TENANT_CLAIM no servidor com o nome da claim que traz o IdMain " +
		"(a tool quem_sou_eu mostra as claims do token)")
}

// carregarDocumento valida o tipo (pedido, requisicao ou cotacao), lê o
// documento da empresa do usuário e recusa os cancelados. nome é o tipo como
// aparece nas mensagens.
func (d co2Deps) carregarDocumento(ctx context.Context, req *mcp.CallToolRequest, tipoIn, numero string) (tipo, nome string, o *datahub.Order, err error) {
	switch strings.ToLower(strings.TrimSpace(tipoIn)) {
	case "", "pedido":
		o, err = d.carregarPedido(ctx, req, numero)
		return "pedido", "pedido", o, err
	case "requisicao", "requisição":
		tipo, nome = "requisicao", "requisição"
	case "cotacao", "cotação":
		tipo, nome = "cotacao", "cotação"
	default:
		return "", "", nil, fmt.Errorf("tipo %q inválido: use pedido, requisicao ou cotacao", tipoIn)
	}
	tenant, err := d.tenantDoUsuario(req)
	if err != nil {
		return tipo, nome, nil, err
	}
	busca := d.dh.Request
	if tipo == "cotacao" {
		busca = d.dh.RFQ
	}
	o, cancelado, err := busca(ctx, tenant, numero)
	if errors.Is(err, datahub.ErrNaoEncontrado) {
		return tipo, nome, nil, fmt.Errorf("%s %q não encontrada para a sua empresa", nome, numero)
	}
	if err != nil {
		return tipo, nome, nil, err
	}
	if cancelado {
		return tipo, nome, nil, fmt.Errorf("a %s %s está cancelada: não há o que analisar", nome, o.ID)
	}
	return tipo, nome, o, nil
}

// carregarPedido lê o pedido da empresa do usuário e recusa cancelados.
func (d co2Deps) carregarPedido(ctx context.Context, req *mcp.CallToolRequest, numero string) (*datahub.Order, error) {
	tenant, err := d.tenantDoUsuario(req)
	if err != nil {
		return nil, err
	}
	o, err := d.dh.Order(ctx, tenant, numero)
	if errors.Is(err, datahub.ErrNaoEncontrado) {
		return nil, fmt.Errorf("pedido %q não encontrado para a sua empresa", numero)
	}
	if err != nil {
		return nil, err
	}
	if o.RefusalStatus != nil {
		switch *o.RefusalStatus {
		case 1:
			return nil, fmt.Errorf("o pedido %s foi recusado pelo fornecedor: não há o que estimar", o.ID)
		case 5:
			return nil, fmt.Errorf("o pedido %s está cancelado: não há o que estimar", o.ID)
		}
	}
	return o, nil
}

// locaisDosItens resolve o local de entrega dos itens que têm um local próprio,
// diferente do cabeçalho.
func (d co2Deps) locaisDosItens(ctx context.Context, o *datahub.Order) (map[int]local, error) {
	out := map[int]local{}
	if o.TenantID == nil {
		return out, nil
	}
	set := map[int]bool{}
	for _, it := range o.Items {
		if it.DeliveryPlaceID == nil || *it.DeliveryPlaceID <= 0 {
			continue
		}
		if o.DeliveryPlaceID != nil && *it.DeliveryPlaceID == *o.DeliveryPlaceID {
			continue
		}
		set[*it.DeliveryPlaceID] = true
	}
	if len(set) == 0 {
		return out, nil
	}
	ids := make([]int, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	addrs, err := d.dh.Addresses(ctx, *o.TenantID, ids)
	if err != nil {
		return out, err
	}
	for _, it := range o.Items {
		if it.DeliveryPlaceID == nil {
			continue
		}
		if a, ok := addrs[*it.DeliveryPlaceID]; ok {
			out[it.Item] = localizar(a.City, a.State)
		}
	}
	return out, nil
}

type local struct {
	p      refdata.Ponto
	ok     bool
	porUF  bool // município não reconhecido: usou o ponto de referência da UF
	rotulo string
}

func localizar(cidade, uf string) local {
	cidade, uf = strings.TrimSpace(cidade), strings.ToUpper(strings.TrimSpace(uf))
	if p, ok := refdata.Municipio(cidade, uf); ok {
		return local{p: p, ok: true, rotulo: p.Nome + "/" + p.UF}
	}
	if p, ok := refdata.CentroideUF(uf); ok {
		return local{p: p, ok: true, porUF: true, rotulo: uf + " (município não reconhecido)"}
	}
	return local{}
}

func cidadeUF(p *datahub.Party) (string, string) {
	if p == nil {
		return "", ""
	}
	return p.City, p.State
}

func enderecoCidadeUF(a *datahub.Address) (string, string) {
	if a == nil {
		return "", ""
	}
	return a.City, a.State
}

func resumo(o *datahub.Order) PedidoResumo {
	r := PedidoResumo{
		ID: string(o.ID), CodigoCliente: o.ClientCode, DataEnvio: o.SentDate,
		Moeda: o.Currency, ValorTotal: o.Total, Incoterms: o.Incoterms,
	}
	if o.Supplier != nil {
		r.Fornecedor = o.Supplier.CompanyName
		if r.Fornecedor == "" {
			r.Fornecedor = o.Supplier.Name
		}
		r.Origem = juntar(o.Supplier.City, o.Supplier.State)
	}
	if o.DeliveryAddress != nil {
		r.Destino = juntar(o.DeliveryAddress.City, o.DeliveryAddress.State)
	}
	return r
}

func juntar(cidade, uf string) string {
	cidade, uf = strings.TrimSpace(cidade), strings.TrimSpace(uf)
	switch {
	case cidade != "" && uf != "":
		return cidade + "/" + uf
	case cidade != "":
		return cidade
	}
	return uf
}

// valorItem: total líquido do item (o mesmo que soma no total do pedido); sem
// ele, preço × quantidade.
func valorItem(it datahub.Item) *float64 {
	if it.Total != nil && *it.Total > 0 {
		return ptr(arred(*it.Total))
	}
	if it.Price != nil && it.Quantity != nil {
		return ptr(arred(*it.Price * *it.Quantity))
	}
	return nil
}

func dataEnvio(o *datahub.Order) time.Time {
	t, err := time.Parse(time.RFC3339, o.SentDate)
	if err != nil {
		return time.Time{}
	}
	return t
}

func resolverPesos(custo, carbono *float64) (Pesos, error) {
	p := Pesos{Custo: 0.7, CO2: 0.3}
	switch {
	case custo != nil && carbono != nil:
		p = Pesos{Custo: *custo, CO2: *carbono}
	case custo != nil:
		p = Pesos{Custo: *custo, CO2: 1 - *custo}
	case carbono != nil:
		p = Pesos{Custo: 1 - *carbono, CO2: *carbono}
	}
	if p.Custo < 0 || p.CO2 < 0 || math.Abs(p.Custo+p.CO2-1) > 0.001 {
		return p, fmt.Errorf("pesos inválidos: peso_custo e peso_co2 ficam entre 0 e 1 e somam 1")
	}
	p.Custo, p.CO2 = math.Round(p.Custo*1000)/1000, math.Round(p.CO2*1000)/1000
	return p, nil
}

func arred(v float64) float64 { return math.Round(v*100) / 100 }

func ptr[T any](v T) *T { return &v }

// premissas guarda as premissas sem repetir.
type premissas struct {
	lista []string
	visto map[string]bool
}

func newPremissas() *premissas { return &premissas{visto: map[string]bool{}} }

func (p *premissas) add(s string) {
	if p.visto[s] {
		return
	}
	p.visto[s] = true
	p.lista = append(p.lista, s)
}
