# genius-mcp-go

Servidor MCP mínimo, em Go, para ligar a um agente do **Genius** (ME).
Serve para validar o caminho completo antes da lógica real: hospedagem no Railway,
autenticação com o token do usuário ME e ligação ao agente pelo Genius Backoffice.

| Tool | O que faz |
|---|---|
| `ping` | Devolve `pong`, a mensagem enviada, a versão e a hora do servidor. |
| `quem_sou_eu` | Mostra o usuário do token que o Genius repassou (formato, usuário, validade, claims de identificação e cabeçalhos `X-ME-*`). Nunca devolve o token inteiro. |

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
