# Plano 2: ajustes nas tools de ESG que já existem

Depende do [plano 1](plano-1-tool-precificar-carbono.md). Leva o valor do carbono e a orientação de resposta para dentro das tools que o comprador já usa, para a resposta sair certa em menos chamadas.

É opcional para a entrega: se o tempo apertar, o plano 1 sozinho já cobre a especificação. A ordem abaixo é a de prioridade.

## Por que ajustar

- Hoje, para saber o custo do carbono de um pedido, o Genius precisaria de três chamadas: `esg_itens_do_pedido`, `esg_estimar_co2_do_pedido` e `esg_precificar_carbono`. Com o ajuste 1, a segunda já devolve o valor.
- As tools atuais terminam a descrição com "apresente sempre as premissas", mas não dizem **como** montar a resposta. O campo `como_responder` resolve isso e também a pendência 3 de `PENDENCIAS.md` (o agente comenta a ausência de `campos_vazios`).
- Toda tool do hackathon precisa bater em dado real. `esg_precificar_carbono` é só conta; com o valor do carbono dentro de `esg_estimar_co2_do_pedido`, a precificação também aparece numa tool que lê o DataHub.

## Ajuste 1: `esg_estimar_co2_do_pedido` devolve o valor do carbono

Arquivo: `internal/tools/co2.go`. Testes: `co2_test.go`.

### Entrada

Acrescente em `EstimarIn`:

```go
PrecoCarbonoBRLT *float64 `json:"preco_carbono_brl_t,omitempty" jsonschema:"preço do carbono em reais por tonelada, se o usuário informar; ausente usa o padrão"`
```

Validação igual à do plano 1: maior que zero; fora de R$ 10 a R$ 5.000/t gera aviso.

### Saída

Acrescente em `EstimarOut`:

```go
Carbono       *CarbonoDocumento `json:"carbono,omitempty"`
ComoResponder string            `json:"como_responder"`
```

`CarbonoDocumento` é o tipo do bloco da seção 5.1 da especificação, declarado em `internal/tools/carbono.go` no plano 1 (preço, custo do carbono, custo total, peso, irrelevante e os campos `_fmt`).

### Regras

1. Só calcule `carbono` quando `co2.total_kg` existir.
2. Use `documento.valor_total` como valor do pedido **só quando a moeda do documento for BRL**. Moeda vazia é tratada como BRL, com uma premissa dizendo isso. Em outra moeda, devolva o custo do carbono sem custo total e sem percentual, e acrescente a premissa: "O valor do documento não está em reais; o custo com carbono não foi calculado."
3. `confianca_baixa` é verdadeiro quando algum item da resposta tem `baixa_confianca`.
4. Acrescente às `premissas` as duas premissas de preço do plano 1.

### `como_responder`

Monte no fim de `estimarCO2`, com estas partes, na ordem:

1. "Diga quanto CO2e o documento emite (produto e transporte) e quanto isso vale em carbono (carbono.custo_carbono_fmt), deixando claro que é valor de referência e não é cobrado."
2. Com custo total: "Diga o peso do carbono sobre o valor do documento (carbono.peso_carbono_fmt)." Com `carbono_irrelevante`: "Diga que o carbono pesa menos de 1% do valor."
3. Com item de baixa confiança: "Diga quais itens têm baixa confiança e que o resultado é uma indicação."
4. "Cite `campos_vazios` e `campos_estimados` só quando existirem na resposta; não comente a ausência deles."
5. "Apresente as premissas em uma ou duas frases, não em lista."
6. "Ofereça ver fornecedores alternativos com menos CO2e ou recalcular com outro preço por tonelada, em reais. Não recalcule nem invente valores."

### Descrição da tool

Troque o fim da descrição, de "Ao responder ao usuário, apresente sempre as premissas." para: "A resposta já traz o valor do carbono em reais (bloco carbono). Siga o campo como_responder da resposta."

### Testes a acrescentar em `co2_test.go`

| Caso | Esperado |
|---|---|
| Estimativa de ponta a ponta (teste que já existe) | A resposta traz `carbono.custo_carbono_brl` igual a `total_kg / 1000 × 500`, e `como_responder` não vazio. |
| `preco_carbono_brl_t` = 200 | `carbono.preco_carbono.origem` = `usuario`. |
| Documento em USD | `carbono` sem `custo_total_brl`; premissa de moeda presente. |
| Item com `baixa_confianca` | `como_responder` contém a parte de baixa confiança. |
| Preço negativo | Erro da tool. |

Os testes atuais não podem mudar de resultado.

## Ajuste 2: `esg_recomendar_fornecedores` mostra o carbono evitado em reais

Arquivo: `internal/tools/recomendar.go`. Testes: `recomendar_test.go`.

A tool já calcula `delta_co2_frete_kg_vs_atual` por alternativa. Falta traduzir isso em reais.

**Limite conhecido:** o recomendador não devolve preço por alternativa. O código não lê o preço de propósito, porque ele vem de pedidos de outros clientes (`internal/datahub/recomendador.go`). Por isso esta tool **não** monta a comparação de custo com carbono entre fornecedores; ela mostra só o carbono do frete evitado. A comparação completa da seção 6.4 da especificação só acontece quando o comprador tem os dois preços e o Genius chama `esg_precificar_carbono` com `alternativas`.

### Entrada

Acrescente em `RecomendarIn` o mesmo `preco_carbono_brl_t` do ajuste 1.

### Saída

Em `Alternativa`:

```go
CarbonoEvitadoBRL *float64 `json:"carbono_frete_evitado_brl,omitempty"`
CarbonoEvitadoFmt string   `json:"carbono_frete_evitado_fmt,omitempty"`
CO2FreteFmt       string   `json:"co2_frete_fmt,omitempty"`
DeltaCO2Fmt       string   `json:"delta_co2_frete_fmt,omitempty"`
```

Em `RecomendarOut`:

```go
PrecoCarbono  *PrecoCarbono `json:"preco_carbono,omitempty"`
ComoResponder string        `json:"como_responder"`
```

### Regras

1. `carbono_frete_evitado_brl` = `−delta_co2_frete_kg_vs_atual / 1000 × preço`. Positivo quando a alternativa emite menos que o fornecedor atual. Só existe quando o delta existe (ou seja, quando há peso da carga).
2. Formate com as funções do pacote `carbono`. Valor negativo (a alternativa emite mais) aparece com sinal.
3. Acrescente às `premissas` as duas premissas de preço do plano 1.

### `como_responder`

1. "Comece pela alternativa com menor CO2e de frete que ainda está bem colocada (posicao), dizendo quanto CO2e evita contra o fornecedor atual (delta_co2_frete_fmt) e quanto isso vale em carbono (carbono_frete_evitado_fmt), valor de referência que não é cobrado."
2. "Mostre uma tabela curta com Fornecedor, Origem, Distância, CO2e do frete e Carbono evitado."
3. "Diga quem já vendeu à empresa do usuário (ja_vendeu_a_sua_empresa)."
4. "Esta lista não traz preço dos fornecedores. Não compare custo com carbono aqui; para isso, peça os preços ao comprador e use esg_precificar_carbono."
5. Sem peso da carga: "O CO2e em kg não foi calculado porque falta o peso; apresente só a ordem por distância."
6. "Apresente as premissas em uma ou duas frases. Não recalcule nem invente valores."

### Descrição da tool

Troque o fim, de "Ao responder ao usuário, apresente sempre as premissas." para: "Siga o campo como_responder da resposta."

### Testes a acrescentar em `recomendar_test.go`

| Caso | Esperado |
|---|---|
| Recomendação com peso informado (teste que já existe) | Cada alternativa com delta traz `carbono_frete_evitado_brl` = `−delta / 1000 × 500` e o `_fmt`. |
| Sem peso | Sem os campos de carbono evitado; `como_responder` contém a parte do peso ausente. |
| `preco_carbono_brl_t` = 200 | Valores recalculados; `preco_carbono.origem` = `usuario`. |

## Ajuste 3: documentação

1. `README.md`: nos dois guias rápidos, uma linha dizendo que a resposta já traz o valor do carbono em reais.
2. `PENDENCIAS.md`: marque a pendência 3 como resolvida, citando o `como_responder`.
3. Instrução do agente no Genius Backoffice (fora do repositório). Texto a acrescentar:

```text
Siga sempre o campo como_responder devolvido pelas tools de ESG.
O valor do carbono é uma referência para comparar e não é cobrado no pedido: diga isso na primeira vez em que o valor aparecer.
Chame o resultado de "custo com carbono", nunca de "custo total".
Se o usuário informar preço do carbono em dólar ou euro, peça o valor em reais por tonelada e não chame a tool.
Para comparar fornecedores pelo custo com carbono, peça os preços ao comprador e chame esg_precificar_carbono com as alternativas; o fornecedor atual vai primeiro.
```

## Pronto quando

- `gofmt -l .` limpo; `go vet ./...`, `go build ./...` e `go test ./...` passam.
- Os testes que já existiam passam sem alteração de resultado.
- `esg_estimar_co2_do_pedido`, `esg_recomendar_fornecedores` e `esg_precificar_carbono` devolvem `como_responder`.
