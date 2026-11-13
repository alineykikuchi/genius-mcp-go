# Guia de execução: precificação do carbono

Como executar os dois planos no repositório `genius-mcp-go` com o Claude Code, com build e testes como portão de cada fase.

| Arquivo | Para quê |
|---|---|
| [spec-precificacao-carbono.md](spec-precificacao-carbono.md) | A especificação (versão 0.2): cálculo, saída e apresentação ao comprador. |
| [plano-1-tool-precificar-carbono.md](plano-1-tool-precificar-carbono.md) | Pacote `carbono` e a tool nova `esg_precificar_carbono`. **Obrigatório.** |
| [plano-2-ajustes-tools-existentes.md](plano-2-ajustes-tools-existentes.md) | Valor do carbono e `como_responder` nas tools que já existem. **Opcional**, em ordem de prioridade. |

## Prioridade

O `CLAUDE.md` do repositório pede escopo pequeno e bem resolvido, e a publicação tem prazo. Execute nesta ordem e pare onde o tempo acabar; cada fase termina com tudo funcionando.

| Ordem | Fase | Entrega | Pode parar aqui? |
|---|---|---|---|
| 1 | Pacote `carbono` | Contas e formatação testadas | Não |
| 2 | Tool `esg_precificar_carbono` | Especificação atendida | Sim |
| 3 | README | Guia rápido da tool nova | Sim |
| 4 | Ajuste 1 do plano 2 | `esg_estimar_co2_do_pedido` já devolve o valor do carbono | Sim |
| 5 | Ajuste 2 do plano 2 | `esg_recomendar_fornecedores` mostra o carbono evitado | Sim |
| 6 | Ajuste 3 do plano 2 | README e `PENDENCIAS.md` | Sim |
| 7 | Revisão | Lista de problemas | Sim |

## Antes de começar

1. **Confira o estado do git.** Pelo acesso remoto, seis arquivos apareciam como modificados (`README.md`, `co2_test.go`, `internal/datahub/datahub.go`, `internal/tools/co2.go`, `internal/tools/recomendar.go`, `recomendar_test.go`), com o mesmo número de linhas inseridas e removidas. Isso costuma ser só diferença de fim de linha. Rode `git status` no seu terminal: se estiver limpo, siga; se houver trabalho de verdade não commitado, faça o commit antes.
2. **A pasta `Claude outputs/` não entra no repositório.** A especificação que vale é a cópia em `docs/precificacao-carbono/`.
3. Rode o portão abaixo. Se algo falhar antes de qualquer mudança, pare e resolva primeiro.

## Portão de build e testes

Ao fim de **cada** fase. A fase só termina quando os quatro passam.

```bash
gofmt -l .        # não pode listar nenhum arquivo
go vet ./...
go build ./...
go test ./...
```

## Regras

1. **Commits pequenos e frequentes**, um por fase, no padrão do repositório (`feat: ...`, `docs: ...`). Os commits do período podem ser auditados.
2. **Não faça push sem o meu ok.** Push na `main` publica no Railway.
3. **Segredos só em variável de ambiente.** Nada de chave, token ou senha no código ou nos testes.
4. **Sem dependência nova.**
5. **Não mude o resultado dos testes que já existem** nem o comportamento das tools atuais, além do que os planos pedem.
6. **Nenhum teste chama a rede de verdade.** Use os falsos que já existem em `co2_test.go` (`novoDataHubFake`, `fakePTAX`).
7. Mensagens, comentários e nomes de teste em português.

## Fases

### Fases 1 a 3: plano 1 (sessão principal)

1. **Fase 1:** `internal/carbono` com `carbono.go`, `formato.go` e `carbono_test.go`. Portão. Commit: `feat: pacote carbono (precificação, comparação e formatação)`.
2. **Fase 2:** `internal/tools/carbono.go`, a variável `PRECO_CARBONO_BRL_T` em `tools.go` e `carbono_test.go` na raiz. Portão. Commit: `feat: tool esg_precificar_carbono`.
3. **Fase 3:** README (tabela de tools, variável e guia rápido). Commit: `docs: guia rápido da precificação do carbono`.

Teste local depois da fase 2:

```bash
AUTH_MODE=off go run .
```

No PowerShell: `$env:AUTH_MODE="off"; go run .`

```bash
curl -s http://localhost:8080/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"esg_precificar_carbono","arguments":{"alternativas":[{"rotulo":"Fornecedor A","valor_brl":10000,"co2e_kg":1200},{"rotulo":"Fornecedor B","valor_brl":10300,"co2e_kg":300}]}}}'
```

Esperado: `recomendada` = `Fornecedor B`, `preco_virada_fmt` = `R$ 333/t`, `dif_custo_total_fmt` = `− R$ 150`.

### Fases 4 e 5: ajustes 1 e 2 do plano 2

Os dois ajustes mexem em arquivos diferentes e só dependem do plano 1, então podem ir em **dois subagentes em paralelo**, cada um num worktree próprio do git criado a partir do commit da fase 3. Sem worktree isolado, faça em sequência na sessão principal.

| Subagente | Arquivos que pode editar |
|---|---|
| A: ajuste 1 | `internal/tools/co2.go`, `co2_test.go` |
| B: ajuste 2 | `internal/tools/recomendar.go`, `recomendar_test.go` |

Instrução para o subagente A (a do B troca o ajuste e os arquivos):

```text
Implemente o "Ajuste 1" de docs/precificacao-carbono/plano-2-ajustes-tools-existentes.md no repositório genius-mcp-go.

Leia antes: docs/precificacao-carbono/spec-precificacao-carbono.md, o plano 2 inteiro, internal/carbono e internal/tools/carbono.go (já implementados), e o arquivo que você vai editar.

Limites:
- Só edite internal/tools/co2.go e co2_test.go.
- Os testes que já existem não podem mudar de resultado.
- Sem dependência nova. Nenhum teste chama a rede de verdade.

Antes de terminar, rode e faça passar: gofmt -l . ; go vet ./... ; go build ./... ; go test ./...

Devolva: os arquivos alterados, a saída resumida dos quatro comandos e qualquer desvio do plano, com o motivo. Não faça commit nem push.
```

Ao receber cada resultado, a sessão principal confere os arquivos alterados, roda o portão de novo e faz o commit: `feat: esg_estimar_co2_do_pedido devolve o valor do carbono` e `feat: esg_recomendar_fornecedores mostra o carbono do frete evitado`.

### Fase 6: documentação

README e `PENDENCIAS.md`, conforme o ajuste 3. Commit: `docs: como_responder e valor do carbono nas tools de ESG`.

### Fase 7: revisão (um subagente novo, só leitura)

```text
Revise a implementação da precificação do carbono no repositório genius-mcp-go contra docs/precificacao-carbono/spec-precificacao-carbono.md e os dois planos da mesma pasta. Não edite arquivos.

Confira, nesta ordem:
1. As contas batem com a seção 4 da especificação? Teste de cabeça o caso A (10.000; 1.200 kg) e B (10.300; 300 kg) a R$ 500/t e a R$ 200/t.
2. Há divisão por zero possível (valor do pedido zero, CO2e da referência zero, diferença de CO2e zero)?
3. Os campos _fmt seguem as regras de formatação do plano 1 (milhar com ponto, decimal com vírgula, sinal de menos U+2212, kg ou t)?
4. O texto de como_responder cobre os casos da seção 6.5 da especificação?
5. Algum teste que já existia mudou de resultado? Algum teste chama a rede?
6. Há segredo, token ou chave no código ou nos testes?
7. Falta algum caso de teste listado nos planos?

Rode go vet ./... e go test ./... e inclua o resultado.

Devolva uma lista de problemas, do mais grave ao menos grave, com arquivo, linha e o cenário em que dá errado. Se não achar nada em um ponto, diga isso.
```

A sessão principal corrige o que for confirmado, roda o portão e faz o commit das correções.

## Pedido para colar no Claude Code

```text
Implemente a precificação do carbono neste repositório seguindo docs/precificacao-carbono/guia-execucao.md.

Leia antes a especificação e os dois planos da mesma pasta, e o CLAUDE.md da raiz.

Execute as fases na ordem do guia. Rode o portão de build e testes ao fim de cada fase e só avance quando passar. Faça um commit local por fase. Não faça push.

Comece pelas fases 1 a 3 (plano 1) e me avise quando terminarem, com o resultado do portão. Só então siga para as fases 4 a 7.

Se algum ponto do plano não bater com o código, pare e me pergunte antes de desviar.
```

## Depois da implementação

1. Push na `main`. O Railway publica sozinho; confira `/healthz`.
2. No Railway, a variável `PRECO_CARBONO_BRL_T` só é necessária se o preço padrão não for R$ 500.
3. No Genius Backoffice de Trunk: sincronizar as tools do MCP e acrescentar à instrução do agente o texto do ajuste 3 do plano 2.
4. Testar no Genius os três exemplos da seção 7 da especificação: custo de carbono de um documento, comparação com recálculo a R$ 200/t e pedido com preço em dólar.

## Pronto quando

- [ ] O portão passa no estado final.
- [ ] `tools/list` devolve `esg_precificar_carbono` e as cinco tools que já existiam.
- [ ] Todos os casos de teste do plano 1 existem e passam.
- [ ] O README tem o guia rápido da precificação e a variável nova.
- [ ] Os três exemplos da seção 7 da especificação deram o resultado esperado no Genius, ou o desvio está anotado em `PENDENCIAS.md`.
