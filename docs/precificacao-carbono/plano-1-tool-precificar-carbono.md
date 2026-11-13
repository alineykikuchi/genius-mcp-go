# Plano 1: pacote `carbono` e tool `esg_precificar_carbono`

Implementa a especificação [spec-precificacao-carbono.md](spec-precificacao-carbono.md) (versão 0.2) no repositório `genius-mcp-go`. É a parte obrigatória; o [plano 2](plano-2-ajustes-tools-existentes.md) depende dela.

## Estado do repositório

- Tools de ESG que já existem: `esg_itens_do_pedido`, `esg_estimar_co2_do_pedido` (`internal/tools/co2.go`) e `esg_recomendar_fornecedores` (`internal/tools/recomendar.go`).
- Não existe tool de precificação. Nada no código calcula valor em R$ do carbono.
- Padrão do repositório: fórmulas puras em pacote próprio (`internal/co2`), tools em `internal/tools` registradas por uma função `registerX(s, deps)` chamada em `tools.Register`, testes pelo cliente MCP na raiz (`co2_test.go`, `recomendar_test.go`), saída JSON em português com `snake_case`.

## Resultado esperado

- Pacote `internal/carbono`, só com contas e formatação, sem rede.
- Tool `esg_precificar_carbono`, que recebe o CO2e (de um documento ou de várias alternativas) e devolve o valor em R$, a comparação pronta, os campos formatados e `como_responder`.
- Preço padrão configurável por variável de ambiente.

## Arquivos

| Arquivo | Mudança |
|---|---|
| `internal/carbono/carbono.go` | Novo. Cálculo e comparação. |
| `internal/carbono/formato.go` | Novo. Formatação em pt-BR. |
| `internal/carbono/carbono_test.go` | Novo. Testes em tabela. |
| `internal/tools/carbono.go` | Novo. Tipos de entrada e saída, handler e `registerCarbono`. |
| `internal/tools/tools.go` | Lê `PRECO_CARBONO_BRL_T` e chama `registerCarbono`. |
| `carbono_test.go` (raiz) | Novo. Testes pelo cliente MCP. |
| `README.md` | Tabela de tools, guia rápido e a variável nova. |

Sem dependência nova.

## Pacote `internal/carbono`

```go
package carbono

const (
	PrecoPadraoBRLT = 500.0
	Referencia      = "EU ETS (referência fixa do ECO.ME)"
	PrecoMinComum   = 10.0   // abaixo disso, aviso de valor incomum
	PrecoMaxComum   = 5000.0 // acima disso, aviso de valor incomum
	LimiteIrrelevantePct = 1.0
)

// Custo é o resultado para um documento ou uma alternativa.
type Custo struct {
	CO2eKg          float64
	CustoCarbono    float64  // R$
	Valor           *float64 // R$, nil quando não informado
	CustoTotal      *float64 // Valor + CustoCarbono
	PesoCarbonoPct  *float64 // CustoCarbono / Valor × 100
	Irrelevante     bool     // PesoCarbonoPct < 1
}

func Precificar(co2eKg float64, valor *float64, precoBRLT float64) Custo

// Comparacao é a alternativa B contra a referência A.
type Comparacao struct {
	DifPreco       float64
	DifCO2eKg      float64
	ReducaoCO2ePct *float64 // nil quando o CO2e da referência é zero
	DifCarbono     float64
	DifCustoTotal  float64
	Situacao       string   // ver tabela
	PrecoVirada    *float64 // R$/t, nil quando não se aplica
	Veredito       string   // compensa, nao_compensa, empate
}

func Comparar(ref, alt Custo) Comparacao
```

Regras, iguais às seções 4.1 e 4.2 da especificação:

Avalie nesta ordem; a primeira condição verdadeira decide:

| Ordem | Condição (B contra a referência A) | `Situacao` | `PrecoVirada` |
|---|---|---|---|
| 1 | `valorB > valorA` e `co2eB < co2eA` | `paga_mais_emite_menos` | `DifPreco / ((co2eA − co2eB) / 1000)` |
| 2 | `valorB < valorA` e `co2eB > co2eA` | `paga_menos_emite_mais` | mesma fórmula |
| 3 | `valorB <= valorA` e `co2eB <= co2eA` | `melhor_nos_dois` | `nil` |
| 4 | qualquer outro caso | `pior_nos_dois` | `nil` |

- `Veredito`: `compensa` quando `DifCustoTotal < 0`, `nao_compensa` quando `> 0`, `empate` quando `= 0`.
- Valores monetários arredondados a 2 casas; preço de virada, ao real inteiro.
- `Comparar` só faz sentido quando as duas alternativas têm `Valor`. Sem valor, a tool não monta comparação.

## Formatação (`formato.go`)

Funções puras, usadas pelos campos `_fmt`:

| Função | Regra | Exemplos |
|---|---|---|
| `BRL(v)` | Separador de milhar `.` e decimal `,`. Valor inteiro sem centavos; quebrado com 2 casas. | `R$ 600`, `R$ 4.560,20`, `R$ 48.000` |
| `BRLComSinal(v)` | Como `BRL`, com `+ ` ou `− ` na frente. Zero sem sinal. | `+ R$ 300`, `− R$ 150` |
| `PrecoPorT(v)` | `BRL(v)` + `/t` | `R$ 500/t`, `R$ 333/t` |
| `CO2e(kg, emToneladas)` | Em kg, sem casas; em t, com 1 casa. | `1.200 kg`, `12,5 t` |
| `CO2eComSinal(...)` | Como `CO2e`, com sinal. | `− 900 kg` |
| `Pct(v)` | Sem casas; abaixo de 1, o texto fixo. | `10%`, `menos de 1%` |
| `Reducao(v)` | Redução positiva vira "menos"; negativa vira "mais". | `75% menos`, `20% mais` |

- O sinal de menos é `−` (U+2212), como na especificação.
- Unidade do CO2e: toneladas quando o **maior** valor da resposta é 10.000 kg ou mais; senão kg. A tool decide uma vez e passa a mesma escolha a todas as chamadas.

## Tool `esg_precificar_carbono` (`internal/tools/carbono.go`)

### Entrada

```go
type AlternativaCarbonoIn struct {
	Rotulo   string   `json:"rotulo" jsonschema:"nome da alternativa, por exemplo o fornecedor"`
	ValorBRL *float64 `json:"valor_brl" jsonschema:"valor da alternativa em reais"`
	CO2eKg   *float64 `json:"co2e_kg" jsonschema:"CO2e estimado da alternativa, em kg"`
}

type PrecificarIn struct {
	CO2eKg           *float64               `json:"co2e_kg,omitempty" jsonschema:"CO2e estimado em kg (total_kg de esg_estimar_co2_do_pedido); obrigatório quando não há alternativas"`
	ValorPedidoBRL   *float64               `json:"valor_pedido_brl,omitempty" jsonschema:"valor do pedido em reais; só informe se a moeda do documento for BRL"`
	PrecoCarbonoBRLT *float64               `json:"preco_carbono_brl_t,omitempty" jsonschema:"preço do carbono em reais por tonelada, informado pelo usuário; ausente usa o padrão"`
	Alternativas     []AlternativaCarbonoIn `json:"alternativas,omitempty" jsonschema:"de 2 a 5 alternativas a comparar; a primeira é a referência (pedido ou fornecedor atual)"`
	ConfiancaBaixa   bool                   `json:"confianca_baixa,omitempty" jsonschema:"true quando algum item da estimativa de CO2 veio com baixa_confianca"`
}
```

### Saída

Os nomes de campo são os da especificação (seções 5.1 e 5.2): `co2e_kg`, `co2e_fmt`, `preco_carbono` (`valor_brl_por_t`, `valor_fmt`, `origem`, `referencia`), `custo_carbono_brl`, `custo_carbono_fmt`, `valor_pedido_brl`, `valor_pedido_fmt`, `custo_total_brl`, `custo_total_fmt`, `peso_carbono_percentual`, `peso_carbono_fmt`, `carbono_irrelevante`, `alternativas`, `comparacao`, `recomendada`, `avisos`, `premissas`, `como_responder`.

- Campos numéricos opcionais são ponteiros com `omitempty`. Listas vazias não aparecem.
- `preco_carbono.origem`: `padrao` ou `usuario`. Com `usuario`, `referencia` vira "valor informado por você".
- Declare um tipo `PrecoCarbono` e um tipo `CarbonoDocumento` (o bloco da seção 5.1) exportados neste arquivo: o plano 2 reutiliza os dois nas outras tools.

### Validação

1. Sem `alternativas`, `co2e_kg` é obrigatório e não pode ser negativo.
2. `preco_carbono_brl_t`, quando informado, é maior que zero. Erro: "Informe o preço do carbono em reais por tonelada, maior que zero."
3. Preço fora de R$ 10 a R$ 5.000/t não é erro: entra em `avisos` ("Preço por tonelada incomum; confira se está em reais por tonelada.").
4. `alternativas`: de 2 a 5. Com 1, erro pedindo a segunda. Com mais de 5, erro pedindo para reduzir.
5. Alternativa sem `valor_brl` ou sem `co2e_kg` fica fora da comparação e entra em `avisos` com o rótulo. Se sobrar menos de 2, erro.
6. Rótulo vazio vira "Alternativa N".

### Cálculo

1. `preco` = o informado, ou `d.precoCarbono` (ver "Configuração").
2. Sem alternativas: `carbono.Precificar(co2e, valor, preco)` e monta a saída da seção 5.1.
3. Com alternativas: precifica cada uma, compara cada uma contra a primeira, escolhe `recomendada` (menor custo total; em empate, menor CO2e) e monta a saída da seção 5.2.
4. `premissas`, sempre nesta ordem:
   - "Preço do carbono: R$ 500/t, referência fixa baseada no EU ETS." (ou "Preço do carbono: R$ X/t, informado por você.")
   - "O valor do carbono é uma referência para comparar; não é cobrado no pedido."
   - Com `confianca_baixa`: "A estimativa de CO2e tem baixa confiança; use o resultado como indicação."

### `como_responder`

Texto fixo por caso, montado no handler. Use os textos da especificação (seção 5) como base e troque conforme o caso da seção 6.5:

| Caso | O que o texto manda fazer |
|---|---|
| Um documento | Seção 6.3 da especificação. |
| Um documento, `carbono_irrelevante` | Dizer que o carbono pesa menos de 1% e não muda a escolha. Sem tabela. |
| Um documento sem valor | Só CO2e e valor do carbono; não citar custo com carbono nem percentual. |
| Comparação de duas | Seção 6.4: frase de decisão, tabela com a coluna Diferença, preço de virada, premissa e oferta. |
| Comparação de três ou mais | Tabela com uma coluna por alternativa, sem a coluna Diferença; uma frase por alternativa. |
| `melhor_nos_dois` ou `pior_nos_dois` | Sem frase de preço de virada. |
| `confianca_baixa` | Acrescentar a frase de baixa confiança e não recomendar troca só pelo CO2e. |

Todo texto termina com: "Use os valores _fmt como vieram; não recalcule nem invente valores. Chame o resultado de 'custo com carbono', nunca de 'custo total'."

### Descrição da tool

```text
Converte o CO2e estimado em valor em reais (precificação do carbono) e compara alternativas pelo custo com carbono (preço + carbono). Use depois de esg_estimar_co2_do_pedido, quando pedirem o custo do carbono, o custo com carbono ou para recalcular com outro preço por tonelada. Para um documento, informe co2e_kg (total_kg da estimativa) e valor_pedido_brl (só se a moeda for BRL). Para comparar, informe de 2 a 5 alternativas com rótulo, valor em reais e CO2e em kg; a primeira é a referência. O preço padrão é uma referência fixa em reais por tonelada; se o usuário informar outro preço, passe em preco_carbono_brl_t. A tool não converte moeda: se o usuário falar em dólar ou euro, peça o valor em reais por tonelada e não chame a tool. O valor do carbono é uma referência e não é cobrado no pedido. Siga o campo como_responder da resposta.
```

## Configuração

- `internal/tools/tools.go`: acrescente `precoCarbono float64` em `co2Deps`, lido de `PRECO_CARBONO_BRL_T`. Vazio, inválido ou menor ou igual a zero usa `carbono.PrecoPadraoBRLT`.
- Chame `registerCarbono(s, deps)` depois de `registerRecomendacao`.
- README, tabela de variáveis: `PRECO_CARBONO_BRL_T`, padrão `500`, "Preço de referência do carbono, em reais por tonelada de CO2e."

## Testes

### Pacote (`internal/carbono/carbono_test.go`), em tabela

| Caso | Esperado |
|---|---|
| 9.120,4 kg a R$ 500/t, valor R$ 48.000 | carbono 4.560,20; total 52.560,20; peso 9,5; não irrelevante |
| 28 kg a R$ 500/t, valor R$ 10.000 | carbono 14,00; peso 0,14; irrelevante |
| 28 kg, sem valor | carbono 14,00; total e peso `nil` |
| A (10.000; 1.200 kg) e B (10.300; 300 kg) a R$ 500/t | dif. preço 300; dif. CO2e −900; redução 75; dif. carbono −450; dif. total −150; `paga_mais_emite_menos`; virada 333; `compensa` |
| O mesmo par a R$ 200/t | totais 10.240 e 10.360; dif. total 120; `nao_compensa`; virada 333 |
| B mais barata e emite menos | `melhor_nos_dois`; virada `nil`; `compensa` |
| B mais cara e emite mais | `pior_nos_dois`; virada `nil`; `nao_compensa` |
| B mais barata e emite mais | `paga_menos_emite_mais`; virada calculada |
| Referência com CO2e zero | redução `nil`, sem divisão por zero |
| Formatos | `BRL(600)` = `R$ 600`; `BRL(4560.2)` = `R$ 4.560,20`; `BRLComSinal(-150)` = `− R$ 150`; `CO2e(1200, false)` = `1.200 kg`; `CO2e(12500, true)` = `12,5 t`; `Pct(9.5)` = `10%`; `Pct(0.14)` = `menos de 1%`; `Reducao(75)` = `75% menos` |

### Pelo cliente MCP (`carbono_test.go`, na raiz)

Siga o padrão de `co2_test.go` (`newTestServer`, `connect`, `callJSON`, `erroDaTool`, `jwtDoUsuario`). Esta tool não chama o DataHub; não é preciso DataHub falso.

| Caso | Esperado |
|---|---|
| `tools/list` | Contém `esg_precificar_carbono`. |
| Um documento, preço padrão | `custo_carbono_fmt` = `R$ 4.560,20`; `origem` = `padrao`; `como_responder` não vazio. |
| Preço informado (200) | `origem` = `usuario`; premissa com "informado por você". |
| Comparação A e B | `recomendada` = `Fornecedor B`; `preco_virada_fmt` = `R$ 333/t`; `dif_custo_total_fmt` = `− R$ 150`. |
| Preço 0 ou negativo | Erro da tool com a mensagem de reais por tonelada. |
| Preço 8.000 | Calcula e traz o aviso de valor incomum. |
| 1 alternativa; 6 alternativas | Erro da tool. |
| Alternativa sem valor entre 3 | Comparação com as outras 2 e aviso com o rótulo. |
| `PRECO_CARBONO_BRL_T=300` (`t.Setenv`) | Preço padrão passa a 300. |
| Sem `co2e_kg` e sem alternativas | Erro da tool. |

## Pronto quando

- `gofmt -l .` não lista arquivo; `go vet ./...`, `go build ./...` e `go test ./...` passam.
- `tools/list` devolve a tool nova, e as cinco tools que já existiam continuam iguais.
- README atualizado: tabela de tools, variável e um guia rápido com os três exemplos da seção 7 da especificação.
