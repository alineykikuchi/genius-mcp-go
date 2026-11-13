# Plano 3: resposta enxuta e faixas de peso do carbono por cliente

Dois ajustes sobre o que já está implementado (planos 1 e 2). A parte A é a prioridade; a parte B é opcional e pode ficar para depois da entrega.

## Por que a resposta ainda traz premissas

No teste do pedido 39589353 o Genius listou cinco premissas e o aviso de triagem. Duas instruções mandam fazer isso:

- A instrução do agente ECO.ME no Backoffice, seção "Como responder": "apresente SEMPRE as premissas devolvidas pela ferramenta, sem omitir nenhuma".
- O `como_responder` de `esg_estimar_co2_do_pedido` (`internal/tools/co2.go`): "Apresente as premissas em uma ou duas frases, não em lista."

As duas precisam mudar. A do Backoffice vale na hora; a do código vale depois do deploy.

---

## Parte A: resposta enxuta

### A1. Instrução do agente (Backoffice, fora do repositório)

Trocar a seção "## Como responder" inteira por:

```text
## Como responder

O público é o comprador, que vai decidir. Responda curto e orientado à decisão. Quando a resposta da ferramenta trouxer o campo como_responder, siga-o. Em qualquer caso:
- no máximo 8 a 10 linhas de texto e, se houver mais de um item, UMA tabela;
- NÃO mostre premissas nem metodologia (fontes dos fatores, câmbio, distância, fator de transporte, NAICS, peso estimado, pesos custo x CO2, modal padrão). Mostre a lista completa de premissas só se o usuário pedir ("ver premissas", "como calculou?");
- incerteza: uma única linha em itálico no fim, por exemplo "Estimativa de triagem com confiança média (peso e categoria estimados).";
- não exponha detalhes de execução interna (por exemplo, um NAICS que foi trocado);
- não diga que não aprovou nem alterou o documento;
- termine com um próximo passo: fornecedores com menor CO2 de frete ou recálculo com outro preço por tonelada;
- valores nulos significam que não foi possível calcular: diga isso em uma linha, não troque por zero.
```

### A2. `esg_estimar_co2_do_pedido` devolve o resumo pronto

Arquivo: `internal/tools/co2.go`. Testes: `co2_test.go`.

O modelo não deve fazer conta nem decidir critério. Acrescente à saída:

Em cada item (`ItemEstimado`):

| Campo | Conteúdo |
|---|---|
| `co2e_kg` | `co2_produto_kg + co2_transporte_kg` do item (o que existir) |
| `co2e_fmt` | formatado com `carbono.CO2e` |
| `valor_fmt` | formatado com `carbono.BRL`, só quando a moeda é BRL |
| `pct_do_total` e `pct_fmt` | parte do item no `total_kg`; `pct_fmt` com `carbono.Pct` |

No topo (`EstimarOut`), um bloco `resumo`:

| Campo | Conteúdo |
|---|---|
| `total_fmt` | total de CO2e formatado |
| `veredito` | `baixo`, `medio` ou `alto`; ausente quando não há custo com carbono (sem valor, ou moeda fora de BRL) |
| `veredito_base` | frase pronta, por exemplo "o carbono equivale a menos de 1% do valor da compra" |
| `faixas` | `baixo_ate_pct`, `alto_a_partir_pct` e `origem` (`padrao` ou `cliente`) |
| `item_principal` | descrição e `pct_fmt` do item com mais CO2e; ausente com um item só |
| `confianca` | `alta`, `media` ou `baixa` |
| `falta_transporte` | `true` quando o CO2 do transporte não foi calculado, com `motivo` em uma frase |

Regras:

1. **Veredito** pelo peso do carbono sobre o valor (`carbono.peso_carbono_percentual`): abaixo de `baixo_ate_pct` é `baixo`; a partir de `alto_a_partir_pct` é `alto`; entre os dois, `medio`.
2. **Faixas padrão:** 1% e 5%, lidas de `FAIXA_CARBONO_BAIXO_PCT` e `FAIXA_CARBONO_ALTO_PCT` (variáveis de ambiente novas). Valor inválido, ou baixo maior ou igual ao alto, usa o padrão.
3. **Confiança:** média das confianças dos itens, ponderada pelo CO2e de cada um. A partir de 0,75 é `alta`; a partir de 0,5 é `media`; abaixo, `baixa`. Sem CO2e calculado, média simples.
4. As `premissas` continuam na saída, sem mudança: servem para quando o comprador pedir.

### A3. `como_responder` da estimativa

Substitua o texto atual por partes montadas conforme o caso, nesta ordem:

1. "Primeira linha: o total de CO2e (resumo.total_fmt) e o veredito (resumo.veredito), com a base (resumo.veredito_base). Sem veredito, só o total."
2. Com mais de um item: "Uma frase dizendo qual item concentra o impacto (resumo.item_principal). Depois UMA tabela com Item, Valor, CO2e e % do total, usando os campos _fmt." Com um item só: "Não use tabela; cite produto e transporte em uma frase."
3. "Uma frase com o valor do carbono (carbono.custo_carbono_fmt), dizendo que é referência e não é cobrado."
4. Com `falta_transporte`: "Uma linha de aviso dizendo que falta o transporte e o motivo (resumo.falta_transporte.motivo)."
5. "Termine com o próximo passo: oferecer fornecedores com menor CO2 de frete ou recálculo com outro preço por tonelada."
6. "Última linha, em itálico: 'Estimativa de triagem com confiança <resumo.confianca> (peso e categoria estimados).'"
7. "NÃO mostre premissas, NAICS, peso estimado, câmbio, fatores, pesos custo x CO2 nem modal. Só apresente a lista de premissas, completa, se o usuário pedir. Não descreva passos internos nem diga que não alterou o documento. No máximo 10 linhas. Não recalcule nem invente valores."

### A4. As outras duas tools

- `internal/tools/recomendar.go`: no `como_responder`, troque a parte que manda apresentar as premissas por "Não mostre premissas nem metodologia; só se o usuário pedir."
- `internal/tools/carbono.go`: mantenha só a frase de que o valor do carbono é referência e não é cobrado; sem lista de premissas.
- Confira as três descrições de tool: nenhuma pode dizer "apresente sempre as premissas".

### A5. Testes

| Caso | Esperado |
|---|---|
| Estimativa com dois itens | `resumo.item_principal` é o de maior CO2e; a soma dos `pct_do_total` dá 100 (com tolerância de arredondamento) |
| Estimativa com um item | sem `item_principal`; `como_responder` contém a parte "não use tabela" |
| Peso do carbono de 0,5%, 3% e 8% | veredito `baixo`, `medio` e `alto` |
| Documento sem valor ou em USD | sem `veredito` |
| Requisição sem fornecedor | `falta_transporte` com motivo |
| Confianças 0,9; 0,6; 0,3 | `alta`, `media`, `baixa` |
| `FAIXA_CARBONO_BAIXO_PCT=2` e `FAIXA_CARBONO_ALTO_PCT=10` | faixas mudam; `origem` = `padrao` |
| Qualquer estimativa | `como_responder` contém "NÃO mostre premissas"; `premissas` continua preenchido |

Os testes que já existem não podem mudar de resultado, exceto os que conferem o texto antigo de `como_responder`.

### A6. Documentação

README: variáveis novas e uma linha no guia rápido dizendo que as premissas só aparecem a pedido.

---

## Parte B (opcional): faixas salvas por cliente

O comprador define as faixas da empresa pelo Genius e elas ficam gravadas. Sem faixa gravada, vale o padrão da parte A.

### Onde gravar

O ME-DataHub já tem a entidade certa na fonte `meweb`: **`parametro_workflow`** (tabela `ParametroWorkflow`), um par nome/valor por cliente.

| Item | Valor |
|---|---|
| Chave primária | `tenantId` + `key` |
| Campo `value` | texto, até 1.000 caracteres |
| Operações | `list`, `detail`, `create`, `patch`, `delete` |
| Raízes GraphQL | `parametroWorkflow` (um), `parametroWorkflows` (lista), `createParametroWorkflow`, `patchParametroWorkflow` |
| Escopo | por cliente, pela coluna `ID` |

Não é preciso criar nada no DataHub. O servidor já consulta o GraphQL da fonte `meweb` (`internal/datahub/datahub.go`).

- Chave: `esg.carbono.faixas`
- Valor: `{"baixo_ate_pct":1,"alto_a_partir_pct":5}`

### Antes de codar, confirme

1. O formato dos argumentos das três operações (`parametroWorkflow`, `createParametroWorkflow`, `patchParametroWorkflow`). Monte uma chamada de leitura real contra Trunk antes de escrever o cliente.
2. Se o token de serviço do servidor pode **gravar** nessa entidade.
3. Com o time dono da tabela: se uma chave nova com esse nome é aceitável e qual o tamanho máximo do nome.

Se o item 2 falhar, pare a parte B: a parte A já funciona com as faixas padrão.

### Arquivos

| Arquivo | Mudança |
|---|---|
| `internal/datahub/parametros.go` | Novo. `LerParametro(ctx, tenantID, chave)` e `GravarParametro(ctx, tenantID, chave, valor)` (cria ou atualiza). |
| `internal/tools/faixas.go` | Novo. As duas tools e `registerFaixas`. |
| `internal/tools/tools.go` | Chama `registerFaixas`. |
| `internal/tools/co2.go` | A estimativa lê as faixas do cliente antes de calcular o veredito. |
| `faixas_test.go` (raiz) | Novo. Testes com o DataHub falso. |

### Tools

| Tool | Entrada | O que faz |
|---|---|---|
| `esg_faixas_carbono` | nenhuma | Devolve as faixas do cliente do usuário e a `origem` (`cliente` ou `padrao`). |
| `esg_definir_faixas_carbono` | `baixo_ate_pct`, `alto_a_partir_pct`, `confirmado` | Grava as faixas do cliente do usuário. |

Regras:

1. O cliente vem **sempre** do token do usuário (`tenantDoUsuario`, que já existe em `co2.go`). Nunca da entrada da tool. Sem cliente no token, ou com `TENANT_FILTRO=off`, a gravação é recusada.
2. Validação: `0 < baixo_ate_pct < alto_a_partir_pct <= 100`.
3. `confirmado` precisa ser `true`. Sem isso, a tool não grava e devolve o texto para o modelo confirmar com o usuário: "Isso vale para toda a empresa. Confirma faixa baixa até X% e alta a partir de Y%?"
4. A resposta da gravação traz as faixas gravadas e a frase "Faixas salvas para a sua empresa."
5. A estimativa lê as faixas com uma consulta a mais. Se a leitura falhar, usa o padrão e segue; a falha entra nas `premissas`, não derruba o cálculo.
6. Com `origem` = `padrao`, o `como_responder` da estimativa acrescenta, no próximo passo: "Diga que o veredito usa as faixas padrão e ofereça definir as faixas da empresa."

**Ponto em aberto:** qualquer usuário do cliente consegue mudar a faixa da empresa inteira pelo chat. No MVP a única trava é a confirmação. O certo depois é restringir a gravação a um perfil (administrador).

### Descrição da tool de gravação

```text
Grava as faixas de peso do carbono da empresa do usuário, usadas no veredito (baixo, médio ou alto) da estimativa de CO2. Use só quando o usuário pedir para definir ou mudar as faixas. Antes de gravar, repita os dois valores ao usuário, diga que valem para toda a empresa e peça confirmação; só então chame com confirmado=true.
```

### Testes

| Caso | Esperado |
|---|---|
| Ler sem faixa gravada | faixas padrão, `origem` = `padrao` |
| Gravar sem `confirmado` | não chama o DataHub falso; devolve o pedido de confirmação |
| Gravar com `confirmado` | o DataHub falso recebe a criação com o `tenantId` do token e a chave certa |
| Gravar de novo | atualização, não criação |
| `baixo` maior ou igual a `alto`; valor acima de 100 | erro da tool |
| Sem cliente no token | erro da tool, sem chamar o DataHub falso |
| Estimativa com faixa gravada (2% e 10%) | veredito pelas faixas do cliente; `origem` = `cliente` |
| Leitura das faixas falha | estimativa segue com o padrão e premissa de aviso |

---

## Pedido para colar no Claude Code

```text
Implemente a parte A de docs/precificacao-carbono/plano-3-formato-da-resposta-e-faixas.md neste repositório.

Leia antes o plano inteiro, a especificação da mesma pasta e os arquivos internal/tools/co2.go, carbono.go e recomendar.go.

Siga as regras e o portão de build e testes de docs/precificacao-carbono/guia-execucao.md (gofmt -l . ; go vet ./... ; go build ./... ; go test ./...). Um commit local ao fim. Não faça push.

Não implemente a parte B agora. Se algum ponto do plano não bater com o código, pare e me pergunte.
```

## Pronto quando

- O portão passa.
- A resposta do Genius ao pedido de teste cabe em cerca de 10 linhas, sem premissas, e termina com o próximo passo.
- Ao pedir "ver premissas", a lista completa aparece.
