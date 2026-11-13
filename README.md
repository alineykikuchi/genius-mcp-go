# genius-mcp-go

Servidor MCP mínimo, em Go, para ligar a um agente do **Genius** (ME).
Serve para validar o caminho completo antes da lógica real: hospedagem no Railway,
autenticação com o token do usuário ME e ligação ao agente pelo Genius Backoffice.

| Tool | O que faz |
|---|---|
| `ping` | Devolve `pong`, a mensagem enviada, a versão e a hora do servidor. |
| `quem_sou_eu` | Mostra o usuário do token que o Genius repassou (formato, usuário, validade, claims de identificação e cabeçalhos `X-ME-*`). Nunca devolve o token inteiro. |
| `esg_itens_do_pedido` | Passo 1 da estimativa de CO2. Lê o pedido, a requisição ou a cotação no ME-DataHub e devolve fornecedor, local de entrega e itens. |
| `esg_estimar_co2_do_pedido` | Passo 2. Recebe o NAICS e o peso de cada item (classificados pelo Genius) e calcula kg CO2e de produto e de transporte, com as premissas. |
| `esg_recomendar_fornecedores` | Busca fornecedores alternativos para os itens de um pedido, requisição ou cotação no recomendador do ME-DataHub e reordena a lista pelo CO2 do frete (distância até o local de entrega). |
| `esg_precificar_carbono` | Converte o CO2e estimado em valor em reais (preço de referência de R$ 500 por tonelada, ou o informado pelo usuário) e compara de 2 a 5 alternativas pelo custo com carbono (preço + carbono), com o preço de virada. |

## Guia rápido: estimativa de CO2 de um pedido, requisição ou cotação

No Genius, com um usuário comprador, peça: **"analise o impacto ESG do pedido 29594155"** (ou **"da requisição 39115867"**, **"da cotação 14505280"**).

1. O Genius chama `esg_itens_do_pedido` com `tipo` (`pedido`, padrão; `requisicao` ou `cotacao`) e `numero` (número do ME ou do cliente).
2. O próprio Genius classifica cada item em um NAICS 2017 de 6 dígitos e estima o peso em kg por unidade.
3. O Genius chama `esg_estimar_co2_do_pedido` com o mesmo `tipo` e `numero`. A tool relê o documento no DataHub e calcula:
   - **CO2 do produto** = valor do item em US$ × fator USEEIO sem margens do NAICS;
   - **CO2 do transporte** = peso (t) × distância (km) × fator do modal. A distância é a linha reta entre o município do fornecedor e o da entrega × 1,3, com mínimo de 20 km.
4. A resposta traz o total, o detalhe por item, os pesos custo x CO2 (padrão 0,7 e 0,3) e as **premissas**.
5. A resposta já traz o valor do carbono em reais (bloco `carbono`: custo do carbono e, quando o documento está em reais, custo com carbono e peso sobre o valor) e o campo `como_responder`, que diz ao Genius como montar a resposta. Com `preco_carbono_brl_t`, o usuário troca o preço de R$ 500 por tonelada.
6. A resposta ao comprador é curta (cerca de 10 linhas): o bloco `resumo` traz o total formatado, o veredito (`baixo`, `medio` ou `alto`, pelo peso do carbono sobre o valor da compra), o item que concentra o impacto e a confiança. As **premissas** continuam na saída da tool, mas só aparecem na conversa a pedido ("ver premissas", "como calculou?").

Parâmetros opcionais, que podem ser ditos na conversa: `modal` (rodoviario, ferroviario, cabotagem, aereo), `peso_custo` e `peso_co2`.

O que é dado real e o que é estimado:

| Dado | Origem |
|---|---|
| Documento, itens, valor, quantidade, fornecedor, local de entrega | ME-DataHub (GraphQL da fonte `meweb`), com token de serviço e filtro pelo tenant do usuário. Só documentos da empresa dele. Na requisição e na cotação, o valor é o preço estimado × a quantidade. |
| Câmbio R$ → US$ | PTAX de venda do Banco Central, na data do pedido (na requisição e na cotação, a data de criação). |
| Fator de emissão por categoria, coordenadas dos municípios | Tabelas públicas embutidas no binário (ver "Componentes externos"). |
| NAICS e peso de cada item | Estimados pelo modelo do Genius. A tool valida o NAICS contra a tabela e devolve candidatos quando ele não existe. |

Limites: é uma **triagem**, não um inventário auditável. O método por gasto compara categorias e pedidos; não diferencia fornecedores do mesmo item. Requisição e cotação normalmente ainda não têm fornecedor: sem origem, o CO2 do transporte não é calculado (só o do produto); para comparar o frete, use `esg_recomendar_fornecedores`. A especificação completa está em `docs/spec-tool1-estimativa-co2_1.md`.

## Guia rápido: fornecedores alternativos com menor CO2 de frete

No Genius, peça: **"recomende fornecedores alternativos com menor emissão para o pedido 29594155"**. Funciona também com requisição e cotação: **"... para a requisição 39115867"** ou **"... para a cotação 14505280"**.

1. O Genius chama `esg_recomendar_fornecedores` com `tipo` (`pedido`, padrão; `requisicao` ou `cotacao`) e `numero` (número do ME ou do cliente), sem itens. A tool lê o documento no DataHub, filtrado pela empresa do usuário, e devolve os itens ativos. Com esses itens, o Genius monta as palavras da descrição que identificam cada um (`nucleo`).
2. O Genius chama `esg_recomendar_fornecedores` de novo, com o mesmo `tipo` e `numero` e os itens. A tool relê o documento e chama o flow `recomendacao-fornecedores-recomendar`, que devolve quem já vendeu itens com essas palavras, com um score de 0 a 100 (preço, frete, prazo, pontualidade, capacidade e cobertura).
3. A tool calcula a distância de cada fornecedor até o local de entrega do documento e o CO2 do frete, e reordena a lista:
   **score final = (1 − `peso_co2`) × score do recomendador + `peso_co2` × nota de CO2 × 100**. A nota de CO2 vai de 0 a 1, e 1 é a menor distância entre os candidatos.
4. A resposta traz o fornecedor atual como referência (só no pedido: requisição e cotação normalmente ainda não têm fornecedor), as alternativas com a posição antes e depois do CO2, a diferença de CO2 do frete em relação ao atual e as **premissas**.
5. A resposta já traz o valor do carbono em reais: quando há peso da carga e fornecedor atual, cada alternativa vem com o carbono do frete evitado (`carbono_frete_evitado_brl`), ao preço de R$ 500 por tonelada ou ao informado em `preco_carbono_brl_t`. O campo `como_responder` diz ao Genius como montar a resposta. A lista não traz preço dos fornecedores: a comparação pelo custo com carbono é feita com `esg_precificar_carbono`, com os preços informados pelo comprador.

Parâmetros opcionais: `peso_co2` (padrão 0,3), `modal` e `top_n` (padrão 10).

Limites:

- **Só Trunk.** Os resolvers do recomendador não filtram por tenant: o score é calculado sobre o histórico de pedidos de todos os clientes. A tool não devolve preço nem descrição de pedidos de outros clientes, mas o flow é um protótipo e não deve ser apontado para dados reais.
- O CO2 do produto não entra na comparação: no método por gasto ele não diferencia fornecedores do mesmo item. O que muda entre fornecedores é o frete.
- O CO2 só reordena os `top_n` candidatos que o recomendador devolveu; não traz para a lista quem ficou fora desse corte.
- A busca é por palavras da descrição do item, no máximo 12 alternativas por chamada, somando todos os itens.

## Guia rápido: precificação do carbono

A tool `esg_precificar_carbono` converte o CO2e estimado em reais, para o comprador comparar alternativas pelo **custo com carbono** (preço + carbono) e não só pelo preço. O valor do carbono é uma **referência para comparar**: não é cobrado no pedido.

**1. Custo de carbono de um documento.** No Genius, peça: **"qual o custo de carbono do pedido 29594155?"**

1. O Genius estima o CO2e do documento (`esg_itens_do_pedido` e `esg_estimar_co2_do_pedido`, como no primeiro guia).
2. O Genius chama `esg_precificar_carbono` com `co2e_kg` (o `total_kg` da estimativa) e `valor_pedido_brl` (só quando a moeda do documento é BRL).
3. A resposta traz o CO2e, o valor do carbono, o custo com carbono e o peso do carbono sobre o valor do pedido. Exemplo: "O pedido emite cerca de 9.120 kg de CO2e. A R$ 500/t, isso equivale a R$ 4.560,20 de carbono, cerca de 10% do valor do pedido. É um valor de referência (EU ETS) e não é cobrado."

**2. Comparação, com recálculo.** Peça: **"compare o fornecedor A (R$ 10.000, 1.200 kg de CO2e) com o fornecedor B (R$ 10.300, 300 kg de CO2e)"**.

1. O Genius chama `esg_precificar_carbono` com `alternativas` (de 2 a 5, cada uma com `rotulo`, `valor_brl` e `co2e_kg`). A primeira é a referência: o pedido ou o fornecedor atual.
2. A resposta traz, para cada alternativa contra a referência, a diferença de preço, de CO2e e de custo com carbono, o **preço de virada** (o preço do carbono em que as duas empatam) e a alternativa recomendada. No exemplo: B custa R$ 300 a mais, emite 75% menos e fica R$ 150 à frente; compensa enquanto o carbono valer mais de R$ 333/t.
3. Em seguida, peça: **"recalcule com R$ 200 por tonelada"**. O Genius chama a tool de novo com `preco_carbono_brl_t` = 200, e B deixa de compensar: fica R$ 120 atrás de A (R$ 10.360 contra R$ 10.240 de custo com carbono).

**3. Preço em outra moeda.** Peça: **"use US$ 50 por tonelada"**. A tool não converte moeda: o Genius pede o preço em **reais por tonelada** (ex.: R$ 250/t) e não chama a tool.

O que é dado real e o que é premissa:

| Dado | Origem |
|---|---|
| CO2e e valor do documento | Saída de `esg_estimar_co2_do_pedido`, que lê o documento no ME-DataHub. Na comparação, o preço de cada alternativa é informado pelo comprador. |
| Preço do carbono | Referência fixa de R$ 500 por tonelada de CO2e, baseada no EU ETS, configurável em `PRECO_CARBONO_BRL_T`. O preço informado pelo usuário vale só para a consulta. |

Regras da resposta: os campos terminados em `_fmt` já vêm formatados (o modelo não refaz contas) e o campo `como_responder` diz como montar a resposta em cada caso. Preço fora da faixa de R$ 10 a R$ 5.000/t gera aviso, mas a conta é feita. Alternativa sem valor ou sem CO2e fica fora da comparação, com aviso.

Limites: não há cotação automática do carbono nem conversão de moeda, e o preço informado não fica salvo por cliente. A especificação completa está em `docs/precificacao-carbono/spec-precificacao-carbono.md`.

## Componentes externos

| Componente | Uso | Licença / origem |
|---|---|---|
| [modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk) | Servidor MCP | MIT |
| USEEIO / EPA *Supply Chain GHG Emission Factors* v1.3.0 (`internal/refdata/data/useeio_v1.3_naics_usd2022.csv`) | Fator kg CO2e por US$ de 2022, por NAICS 2017 | Dado público da EPA (EUA) |
| [kelvins/municipios-brasileiros](https://github.com/kelvins/municipios-brasileiros) (`municipios.csv`, `estados.csv`) | Latitude e longitude dos municípios e das UFs (base IBGE) | MIT |
| API PTAX do Banco Central (Olinda) | Câmbio R$ → US$, consultado a cada estimativa | Dado público, sem credencial |
| ME-DataHub | Dados do pedido (GraphQL) e recomendador de fornecedores (flow REST `recomendacao-fornecedores-recomendar`, resolver `recomendacao-fornecedores-ja-venderam` v2) | Plataforma ME |

Rotas: `POST /mcp` (MCP via Streamable HTTP, sem estado) e `GET /healthz`.

## Como a autenticação funciona

1. A pessoa entra na plataforma ME e conversa com o Genius.
2. O MCP é cadastrado no Genius Backoffice com `credential_mode: me_access_token`. Assim, o Genius chama este servidor enviando o token **do usuário logado** em `Authorization: Bearer <token>`.
3. Este servidor recusa (HTTP 401) chamadas sem token ou com JWT expirado e entrega as claims às tools.
4. As tools de CO2 leem o ME-DataHub com um **token de serviço**, obtido com `CLIENT_ID` e `CLIENT_SECRET` (client credentials) e renovado sozinho antes de vencer. Esse token enxerga todos os clientes, então a tool lê o **tenant (IdMain) do usuário** nas claims do token do Genius e o usa como filtro obrigatório em toda consulta de pedido. Sem tenant no token, a tool não consulta nada.

> ⚠️ A assinatura do JWT **não** é verificada aqui (`internal/token`). As tools de CO2 **confiam na claim de tenant** desse token para filtrar os pedidos. Limitação conhecida do MVP: quem conhecer a URL do MCP e montar um JWT com o tenant de outra empresa consegue ler pedidos dela. Antes de usar fora do hackathon, valide a assinatura contra o JWKS do IdP da ME.

Nenhum segredo fica no código nem no repositório. O token chega a cada chamada e não é gravado em log.

## Rodar localmente

Precisa do Go 1.25 ou mais novo.

```bash
go test ./...                 # testes
AUTH_MODE=off go run .        # sobe em http://localhost:8080 sem exigir token
```

Para testar com uma interface, use o MCP Inspector:

```bash
npx @modelcontextprotocol/inspector
# Transport: Streamable HTTP   URL: http://localhost:8080/mcp
```

Ou com curl:

```bash
curl -s http://localhost:8080/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
```

| Variável | Padrão | Para quê |
|---|---|---|
| `PORT` | `8080` | Porta HTTP. O Railway define sozinho. |
| `AUTH_MODE` | `require` | `require` exige o Bearer token; `off` só para teste local. |
| `CLIENT_ID` | (sem padrão) | Client de serviço do ME-DataHub. **Obrigatória** para as tools de CO2. |
| `CLIENT_SECRET` | (sem padrão) | Secret do client. **Obrigatória**; só em variável de ambiente, nunca no repositório. |
| `DATAHUB_GRAPHQL_URL` | `https://trunk.datahub.me.com.br/_api/dp/meweb/graphql` | GraphQL da fonte `meweb` do ME-DataHub. Troque para apontar a outro ambiente. |
| `DATAHUB_FLOWS_URL` | `https://trunk.datahub.me.com.br/_api/flows` | Raiz dos flows REST do ME-DataHub, usada pelo recomendador de fornecedores. |
| `DATAHUB_TOKEN_URL` | `https://trunk.datahub.me.com.br/oauth/token` | Endpoint de token do ME-DataHub. |
| `TENANT_CLAIM` | (vazio) | Nome da claim do token do Genius que traz o IdMain do usuário. Vazio tenta `tenantId`, `tenant_id`, `idMain`, `id_main`, `workflowId`, `workflow_id`. |
| `TENANT_FILTRO` | (ligado) | `off` desliga o filtro por tenant. Só para demonstração: a tool passa a ler pedidos de qualquer cliente. |
| `PRECO_CARBONO_BRL_T` | `500` | Preço de referência do carbono, em reais por tonelada de CO2e. |
| `FAIXA_CARBONO_BAIXO_PCT` | `1` | Veredito da estimativa: peso do carbono sobre o valor da compra abaixo deste percentual é `baixo`. |
| `FAIXA_CARBONO_ALTO_PCT` | `5` | Veredito da estimativa: a partir deste percentual é `alto`; entre as duas faixas, `medio`. Valor inválido, ou baixo maior ou igual ao alto, usa o padrão (1 e 5). |

## 1. Subir no GitHub

Crie um repositório **privado** vazio no GitHub (sem README) e rode, na pasta do projeto:

```bash
git init -b main
git add .
git commit -m "MCP mínimo para agente do Genius"
git remote add origin https://github.com/<seu-usuario>/genius-mcp-go.git
git push -u origin main
```

O workflow em `.github/workflows/ci.yml` roda `go vet` e os testes a cada push.

## 2. Publicar no Railway

1. No Railway: **New Project → Deploy from GitHub repo** e escolha o repositório.
   O `railway.json` já diz para usar o `Dockerfile` e checar `/healthz`.
2. Em **Settings → Networking → Generate Domain**, gere a URL pública.
3. Abra `https://<seu-dominio>.up.railway.app/healthz`. A resposta deve ser `{"status":"ok",...}`.
4. Em **Variables**, crie `CLIENT_ID` e `CLIENT_SECRET` (client de serviço do ME-DataHub). `AUTH_MODE` já vem como `require`.

Cada push na `main` faz um deploy novo.

## 3. Ligar ao Genius (Genius Backoffice, TRUNK)

O Claude pode fazer estes passos pelo conector Genius Backoffice. Pela tela, são:

1. **Criar o agente.** Exemplo: nome "MCP de Teste", tipo Lead Agent (`direct`), clientes `1804160` e instrução dizendo quando usar `ping` e `quem_sou_eu`.
2. **Cadastrar o MCP de sistema** ligado a esse agente:
   - endpoint `https://<seu-dominio>.up.railway.app/mcp`
   - `credential_mode`: `me_access_token`
   - header `Authorization`, prefixo `Bearer `
   - `is_system: true`, com agents = o agente criado
   - clientes `1804160`
3. **Ativar o MCP.** Ele nasce em `revisao` e só chega ao Genius depois de `ativo`.
4. **Testar no Genius** com um usuário do cliente 1804160: "faça um ping no MCP de teste" e "quem sou eu?".
5. Se algo falhar, veja a saúde do MCP e os logs do Genius no backoffice, e os logs do serviço no Railway.

## Criar uma tool nova

Copie o modelo de `internal/tools/tools.go`:

```go
type MinhaIn struct {
    Numero string `json:"numero" jsonschema:"número do pedido"`
}

mcp.AddTool(s, &mcp.Tool{
    Name:        "consultar_pedido",
    Description: "Quando usar: … (é isso que o modelo lê para decidir chamar a tool)",
}, func(ctx context.Context, req *mcp.CallToolRequest, in MinhaIn) (*mcp.CallToolResult, MinhaOut, error) {
    raw := req.GetExtra().TokenInfo.Extra[token.ExtraRaw].(string)
    // chame a API da ME com: Authorization: Bearer <raw>
    …
})
```

Depois do deploy, sincronize as tools do MCP no backoffice para o Genius ver a tool nova.
