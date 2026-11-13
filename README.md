# genius-mcp-go

Servidor MCP mínimo, em Go, para ligar a um agente do **Genius** (ME).
Serve para validar o caminho completo antes da lógica real: hospedagem no Railway,
autenticação com o token do usuário ME e ligação ao agente pelo Genius Backoffice.

| Tool | O que faz |
|---|---|
| `ping` | Devolve `pong`, a mensagem enviada, a versão e a hora do servidor. |
| `quem_sou_eu` | Mostra o usuário do token que o Genius repassou (formato, usuário, validade, claims de identificação e cabeçalhos `X-ME-*`). Nunca devolve o token inteiro. |
| `esg_itens_do_pedido` | Passo 1 da estimativa de CO2. Lê o pedido no ME-DataHub e devolve fornecedor, local de entrega e itens. |
| `esg_estimar_co2_do_pedido` | Passo 2. Recebe o NAICS e o peso de cada item (classificados pelo Genius) e calcula kg CO2e de produto e de transporte, com as premissas. |

## Guia rápido: estimativa de CO2 de um pedido

No Genius, com um usuário comprador, peça: **"analise o impacto ESG do pedido 29594155"**.

1. O Genius chama `esg_itens_do_pedido` com o número do pedido (número do ME ou número do pedido no cliente).
2. O próprio Genius classifica cada item em um NAICS 2017 de 6 dígitos e estima o peso em kg por unidade.
3. O Genius chama `esg_estimar_co2_do_pedido`. A tool relê o pedido no DataHub e calcula:
   - **CO2 do produto** = valor do item em US$ × fator USEEIO sem margens do NAICS;
   - **CO2 do transporte** = peso (t) × distância (km) × fator do modal. A distância é a linha reta entre o município do fornecedor e o da entrega × 1,3, com mínimo de 20 km.
4. A resposta traz o total, o detalhe por item, os pesos custo x CO2 (padrão 0,7 e 0,3) e as **premissas**.

Parâmetros opcionais, que podem ser ditos na conversa: `modal` (rodoviario, ferroviario, cabotagem, aereo), `peso_custo` e `peso_co2`.

O que é dado real e o que é estimado:

| Dado | Origem |
|---|---|
| Pedido, itens, valor, quantidade, fornecedor, local de entrega | ME-DataHub (GraphQL da fonte `meweb`), com o token do usuário. Só pedidos da empresa dele. |
| Câmbio R$ → US$ | PTAX de venda do Banco Central, na data do pedido. |
| Fator de emissão por categoria, coordenadas dos municípios | Tabelas públicas embutidas no binário (ver "Componentes externos"). |
| NAICS e peso de cada item | Estimados pelo modelo do Genius. A tool valida o NAICS contra a tabela e devolve candidatos quando ele não existe. |

Limites: é uma **triagem**, não um inventário auditável. O método por gasto compara categorias e pedidos; não diferencia fornecedores do mesmo item. A especificação completa está em `docs/spec-tool1-estimativa-co2_1.md`. Nesta versão só há **pedido**; requisição ficou fora.

## Componentes externos

| Componente | Uso | Licença / origem |
|---|---|---|
| [modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk) | Servidor MCP | MIT |
| USEEIO / EPA *Supply Chain GHG Emission Factors* v1.3.0 (`internal/refdata/data/useeio_v1.3_naics_usd2022.csv`) | Fator kg CO2e por US$ de 2022, por NAICS 2017 | Dado público da EPA (EUA) |
| [kelvins/municipios-brasileiros](https://github.com/kelvins/municipios-brasileiros) (`municipios.csv`, `estados.csv`) | Latitude e longitude dos municípios e das UFs (base IBGE) | MIT |
| API PTAX do Banco Central (Olinda) | Câmbio R$ → US$, consultado a cada estimativa | Dado público, sem credencial |
| ME-DataHub | Dados do pedido | Plataforma ME |

Rotas: `POST /mcp` (MCP via Streamable HTTP, sem estado) e `GET /healthz`.

## Como a autenticação funciona

1. A pessoa entra na plataforma ME e conversa com o Genius.
2. O MCP é cadastrado no Genius Backoffice com `credential_mode: me_access_token`. Assim, o Genius chama este servidor enviando o token **do usuário logado** em `Authorization: Bearer <token>`.
3. Este servidor recusa (HTTP 401) chamadas sem token ou com JWT expirado e entrega as claims às tools.
4. Para buscar dados, a tool **repassa o mesmo token** às APIs da ME. Quem decide o que o usuário pode ver é a API da ME, então o agente só enxerga o que aquele usuário já enxerga.

> ⚠️ A assinatura do JWT **não** é verificada aqui (`internal/token`). Isso basta enquanto as tools só ecoam dados ou repassam o token às APIs da ME. Antes de criar uma tool que use as claims para autorizar algo localmente, valide a assinatura contra o JWKS do IdP da ME.

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
| `DATAHUB_GRAPHQL_URL` | `https://trunk.datahub.me.com.br/_api/dp/meweb/graphql` | GraphQL da fonte `meweb` do ME-DataHub. Troque para apontar a outro ambiente. |

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
4. Não precisa criar variável nenhuma: `AUTH_MODE` já vem como `require`.

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
