# Especificação: Precificação do Carbono (ECO.ME)

**Projeto:** Hackathon Techweek 2026 (ESG)
**Agente:** Genius / ECO.ME
**Versão:** 0.2 (MVP, com apresentação ao comprador)

Mudanças desta versão em relação à 0.1:

- Nova seção 6, "Apresentação ao comprador": ordem da resposta, textos, formatos e casos especiais.
- A tool passa a aceitar uma lista de alternativas e devolve a comparação pronta: diferenças, redução de CO2e e preço de virada (seções 3, 4 e 5).
- A saída ganha valores já formatados e o campo `como_responder`, para o modelo não refazer contas.
- O rótulo mostrado ao comprador passa a ser **"custo com carbono"**. O campo `custo_total_brl` continua com o mesmo nome, para não quebrar o que já foi implementado.
- Os exemplos foram trocados por casos realistas.

O cálculo do custo do carbono e a regra de preço não mudaram.

---

## 1. Objetivo

Converter o CO2e estimado de um pedido ou requisição em **valor monetário (R$)** para que o comprador compare alternativas pelo **custo com carbono** (preço + carbono), e não só pelo preço.

A tool usa um **preço padrão de referência**, baseado no mercado europeu de carbono (EU ETS). O usuário pode informar outro valor, sempre em **R$ por tonelada**.

O valor do carbono é uma **referência para comparar**. Ele não é cobrado no pedido, e a resposta ao comprador precisa dizer isso.

---

## 2. Regra de preço

| Item | Definição |
|---|---|
| Preço padrão | **R$ 500,00 por tonelada de CO2e** |
| Referência | EU ETS (mercado regulado europeu), que em 2026 oscila na faixa de € 75–85/t |
| Moeda | **Somente BRL.** A tool não faz conversão de dólar ou euro |
| Valor informado pelo usuário | Substitui o padrão **apenas na consulta atual** |
| Atualização do padrão | Manual, por configuração (não há cotação automática no MVP) |

> O valor de R$ 500/t é uma **referência fixa** cadastrada na configuração da tool. A conversão do euro para o real foi feita uma única vez, fora da tool, para definir esse número. Em tempo de execução, nenhum câmbio é aplicado.

---

## 3. Entrada

| Parâmetro | Tipo | Obrigatório | Descrição |
|---|---|---|---|
| `co2e_kg` | number | Sim, se não houver `alternativas` | CO2e estimado (saída da Tool 1), em kg |
| `valor_pedido_brl` | number | Não | Valor do pedido, para calcular o custo com carbono |
| `preco_carbono_brl_t` | number | Não | Preço informado pelo usuário em **R$/t**. Se ausente, usa o padrão (R$ 500) |
| `alternativas` | lista | Não | Alternativas a comparar. Cada uma tem `rotulo`, `valor_brl` e `co2e_kg`. A **primeira** é a referência (o pedido atual ou o fornecedor atual) |
| `confianca_baixa` | boolean | Não | `true` quando a Tool 1 marcou a estimativa de CO2e como de baixa confiança |

Validações de `preco_carbono_brl_t`:

- Deve ser um número **maior que zero**.
- Se o usuário digitar outra moeda (ex.: "US$ 50", "€ 80"), a tool **não converte**: responde pedindo o valor em reais.
- Valores fora da faixa de R$ 10 a R$ 5.000/t geram um **aviso** ("valor incomum"), mas o cálculo é feito normalmente.

Validações de `alternativas`:

- De 2 a 5 itens. Com mais de 5, a tool pede para reduzir a lista.
- Todas precisam de `valor_brl` e `co2e_kg`. Alternativa sem um dos dois fica fora da comparação e é citada no aviso.

---

## 4. Cálculo

### 4.1 Custo do carbono (por pedido ou por alternativa)

```
preco_t        = preco_carbono_brl_t  se informado
                 senão 500.00

co2e_t         = co2e_kg / 1000
custo_carbono  = co2e_t × preco_t                    # R$
custo_total    = valor_pedido_brl + custo_carbono     # R$ (se houver valor do pedido)
peso_carbono_% = custo_carbono / valor_pedido_brl × 100
```

`carbono_irrelevante` é verdadeiro quando `peso_carbono_% < 1`.

### 4.2 Comparação (quando há `alternativas`)

Para cada alternativa B, contra a referência A:

```
dif_preco        = valor_B − valor_A                       # R$
dif_co2e_kg      = co2e_B − co2e_A                         # kg
reducao_co2e_%   = (co2e_A − co2e_B) / co2e_A × 100
dif_carbono      = custo_carbono_B − custo_carbono_A       # R$
dif_custo_total  = custo_total_B − custo_total_A           # R$
preco_virada     = dif_preco / ((co2e_A − co2e_B) / 1000)  # R$/t
```

O **preço de virada** é o preço do carbono em que as duas alternativas empatam. Ele mostra ao comprador quanto a conclusão depende da premissa de R$ 500/t.

| Situação da alternativa B | `situacao` | Preço de virada | Leitura |
|---|---|---|---|
| Mais cara e emite menos | `paga_mais_emite_menos` | Calculado | B compensa quando o carbono vale **mais** que o preço de virada |
| Mais barata e emite mais | `paga_menos_emite_mais` | Calculado | B compensa quando o carbono vale **menos** que o preço de virada |
| Mais barata (ou igual) e emite menos | `melhor_nos_dois` | Não se aplica | B compensa a qualquer preço de carbono |
| Mais cara (ou igual) e emite mais | `pior_nos_dois` | Não se aplica | B não compensa a nenhum preço de carbono |

`veredito` da alternativa, no preço em uso: `compensa` (`dif_custo_total < 0`), `nao_compensa` (`> 0`) ou `empate` (`= 0`).

`recomendada` é a alternativa de menor `custo_total`. Em empate, fica a de menor CO2e.

### 4.3 Arredondamento e formato

- Valores monetários: 2 casas decimais nos campos numéricos. Nos campos `_fmt`, valor inteiro aparece sem centavos ("R$ 600") e valor quebrado com 2 casas ("R$ 4.560,20").
- CO2e: em **kg**, sem casas, quando o maior valor da resposta é menor que 10.000 kg; em **t**, com 1 casa, a partir daí. Uma mesma comparação usa uma unidade só.
- Percentuais: sem casas decimais; abaixo de 1%, escrever "menos de 1%".
- Preço de virada: arredondado para o real inteiro.

---

## 5. Saída

Os campos da versão 0.1 continuam. Os novos são `carbono_irrelevante`, os campos terminados em `_fmt`, o bloco `comparacao` e `como_responder`.

### 5.1 Um pedido

```json
{
  "co2e_kg": 9120.4,
  "co2e_fmt": "9.120 kg",
  "preco_carbono": {
    "valor_brl_por_t": 500.00,
    "valor_fmt": "R$ 500/t",
    "origem": "padrao",
    "referencia": "EU ETS (referência fixa do ECO.ME)"
  },
  "custo_carbono_brl": 4560.20,
  "custo_carbono_fmt": "R$ 4.560,20",
  "valor_pedido_brl": 48000.00,
  "valor_pedido_fmt": "R$ 48.000",
  "custo_total_brl": 52560.20,
  "custo_total_fmt": "R$ 52.560,20",
  "peso_carbono_percentual": 9.5,
  "peso_carbono_fmt": "10%",
  "carbono_irrelevante": false,
  "premissas": [
    "Preço do carbono: R$ 500,00/t, referência fixa baseada no EU ETS.",
    "O valor do carbono é uma referência para comparar; não é cobrado no pedido."
  ],
  "como_responder": "1) Diga quanto CO2e o pedido emite (co2e_fmt) e quanto isso vale em carbono (custo_carbono_fmt), deixando claro que é valor de referência e não é cobrado. 2) Diga o peso do carbono sobre o valor do pedido (peso_carbono_fmt). 3) Ofereça ver alternativas com menos CO2e ou recalcular com outro preço por tonelada, em reais. Use os valores _fmt como vieram; não recalcule."
}
```

`origem` assume `"padrao"` ou `"usuario"`.

### 5.2 Comparação

```json
{
  "preco_carbono": { "valor_brl_por_t": 500.00, "valor_fmt": "R$ 500/t", "origem": "padrao", "referencia": "EU ETS (referência fixa do ECO.ME)" },
  "alternativas": [
    {
      "rotulo": "Fornecedor A", "referencia": true,
      "valor_brl": 10000.00, "valor_fmt": "R$ 10.000",
      "co2e_kg": 1200, "co2e_fmt": "1.200 kg",
      "custo_carbono_brl": 600.00, "custo_carbono_fmt": "R$ 600",
      "custo_total_brl": 10600.00, "custo_total_fmt": "R$ 10.600"
    },
    {
      "rotulo": "Fornecedor B", "referencia": false,
      "valor_brl": 10300.00, "valor_fmt": "R$ 10.300",
      "co2e_kg": 300, "co2e_fmt": "300 kg",
      "custo_carbono_brl": 150.00, "custo_carbono_fmt": "R$ 150",
      "custo_total_brl": 10450.00, "custo_total_fmt": "R$ 10.450"
    }
  ],
  "comparacao": [
    {
      "rotulo": "Fornecedor B", "contra": "Fornecedor A",
      "dif_preco_brl": 300.00, "dif_preco_fmt": "+ R$ 300",
      "dif_co2e_kg": -900, "dif_co2e_fmt": "− 900 kg",
      "reducao_co2e_percentual": 75, "reducao_co2e_fmt": "75% menos",
      "dif_carbono_brl": -450.00, "dif_carbono_fmt": "− R$ 450",
      "dif_custo_total_brl": -150.00, "dif_custo_total_fmt": "− R$ 150",
      "situacao": "paga_mais_emite_menos",
      "preco_virada_brl_t": 333, "preco_virada_fmt": "R$ 333/t",
      "veredito": "compensa"
    }
  ],
  "recomendada": "Fornecedor B",
  "premissas": [
    "Preço do carbono: R$ 500,00/t, referência fixa baseada no EU ETS.",
    "O valor do carbono é uma referência para comparar; não é cobrado no pedido."
  ],
  "como_responder": "1) Primeira frase: qual alternativa compensa (recomendada) e por quanto (dif_custo_total_fmt), citando o que ela custa a mais ou a menos (dif_preco_fmt) e quanto CO2e evita (reducao_co2e_fmt). 2) Tabela curta com Preço, CO2e estimado, Carbono (referência) e Custo com carbono, com a coluna Diferença. 3) Uma frase com o preço de virada (preco_virada_fmt). 4) Uma frase dizendo que o carbono é valor de referência e não é cobrado, com a oferta de recalcular com outro preço por tonelada. Use os valores _fmt como vieram; não recalcule nem invente valores."
}
```

---

## 6. Apresentação ao comprador

### 6.1 Regras

1. **Comece pela decisão.** A primeira frase diz qual alternativa compensa e por quanto. Método e preço de referência vão para o fim.
2. **O carbono não é cobrança.** Na primeira vez em que o valor aparece, diga que é uma referência para comparar e que não é cobrado no pedido.
3. **Rótulo "custo com carbono".** Não use "custo total" na resposta: dá a entender que é o valor a pagar.
4. **Mostre a diferença.** O comprador decide pelo que paga a mais ou a menos, pelo CO2e que evita e pelo saldo.
5. **Mostre o preço de virada** sempre que ele existir.
6. **CO2e é estimativa.** Use "cerca de" ou "estimado" junto ao número.
7. **Premissas em uma linha**, seguidas da oferta de recalcular com outro preço.
8. **Não refaça contas.** Use os campos `_fmt` como vieram da tool.
9. **Uma moeda só.** Se o comprador falar em dólar ou euro, peça o valor em reais por tonelada.

### 6.2 Ordem da resposta

| Bloco | Conteúdo | Quando |
|---|---|---|
| 1. Resposta direta | Uma ou duas frases com a conclusão | Sempre |
| 2. Tabela | Preço, CO2e estimado, Carbono (referência), Custo com carbono, Diferença | Só em comparação |
| 3. Preço de virada | Uma frase | Quando `preco_virada` existe |
| 4. Premissa e oferta | Uma frase: valor de referência, não cobrado, e a oferta de recalcular | Sempre |

### 6.3 Um pedido

> O pedido emite cerca de **9.120 kg de CO2e**. A R$ 500/t, isso equivale a **R$ 4.560,20** de carbono, cerca de 10% do valor do pedido. É um valor de referência (EU ETS) e não é cobrado.
> Quer ver alternativas com menos CO2e ou recalcular com outro preço por tonelada?

### 6.4 Comparação

> **O Fornecedor B compensa:** custa R$ 300 a mais, mas emite 75% menos CO2e. Com o carbono a R$ 500/t, ele fica R$ 150 à frente.
>
> | | Fornecedor A | Fornecedor B | Diferença |
> |---|---|---|---|
> | Preço | R$ 10.000 | R$ 10.300 | + R$ 300 |
> | CO2e estimado | 1.200 kg | 300 kg | − 900 kg |
> | Carbono (referência) | R$ 600 | R$ 150 | − R$ 450 |
> | **Custo com carbono** | **R$ 10.600** | **R$ 10.450** | **− R$ 150** |
>
> B compensa enquanto o carbono valer mais de R$ 333/t. O valor do carbono é uma referência (EU ETS) e não é cobrado no pedido. Quer recalcular com outro preço por tonelada?

Com três ou mais alternativas, a tabela tem uma coluna por alternativa, sem a coluna Diferença. As diferenças contra a referência vão em uma frase por alternativa, abaixo da tabela.

### 6.5 Casos especiais

| Caso | Como responder |
|---|---|
| Carbono irrelevante (`carbono_irrelevante = true`) | Sem tabela. "O carbono pesa menos de 1% do valor do pedido (R$ 14 em R$ 10.000). Ele não muda a escolha entre as alternativas." |
| Alternativa melhor nos dois (`melhor_nos_dois`) | "O Fornecedor B é mais barato e emite menos. Ele compensa a qualquer preço de carbono." Sem frase de preço de virada. |
| Alternativa pior nos dois (`pior_nos_dois`) | "O Fornecedor B é mais caro e emite mais. Ele não compensa." |
| Empate no custo com carbono | Dizer que empatam no custo com carbono e apontar a de menor CO2e. |
| Preço informado pelo usuário | Dizer "valor informado por você" no lugar de "referência (EU ETS)". |
| Preço fora da faixa de R$ 10 a R$ 5.000/t | Fazer a conta e acrescentar uma frase: "Esse preço por tonelada é incomum; confira se está em reais por tonelada." |
| Estimativa de baixa confiança (`confianca_baixa = true`) | Mostrar os números e acrescentar: "A estimativa de CO2e deste pedido tem baixa confiança; use o resultado como indicação, não como base para trocar de fornecedor." |
| Sem valor do pedido | Mostrar só o CO2e e o valor do carbono; não citar custo com carbono nem percentual. |

---

## 7. Exemplos de interação

**Com o preço padrão**

> **Comprador:** Qual o custo de carbono do pré-pedido JBS-67243704?
> **Genius:** O pré-pedido emite cerca de **28 kg de CO2e**. A R$ 500/t, isso equivale a **R$ 14** de carbono, um valor de referência (EU ETS) que não é cobrado. Quer ver alternativas com menos CO2e ou recalcular com outro preço por tonelada?

**Comparação, com recálculo**

> **Comprador:** Compare os fornecedores A e B.
> **Genius:** *(resposta da seção 6.4)*
> **Comprador:** Recalcule com R$ 200 por tonelada.
> **Genius:** Com **R$ 200/t** (valor informado por você), o Fornecedor B deixa de compensar: fica R$ 120 atrás do A (R$ 10.360 contra R$ 10.240 de custo com carbono). Ele só compensa com o carbono acima de R$ 333/t.

**Com moeda estrangeira**

> **Comprador:** Use US$ 50 por tonelada.
> **Genius:** Para manter o cálculo sem câmbio, informe o preço em **reais por tonelada** (ex.: R$ 250/t).

---

## 8. Uso na comparação de fornecedores (Tool 2)

A Tool 2 passa as alternativas em `alternativas`, com o fornecedor atual em primeiro lugar. A tool aplica o **mesmo preço** (padrão ou informado) a todas e devolve o bloco `comparacao`.

| | Fornecedor A | Fornecedor B | Diferença |
|---|---|---|---|
| Preço | R$ 10.000 | R$ 10.300 | + R$ 300 |
| CO2e | 1.200 kg | 300 kg | − 900 kg |
| Custo do carbono (R$ 500/t) | R$ 600 | R$ 150 | − R$ 450 |
| **Custo com carbono** | **R$ 10.600** | **R$ 10.450** | **− R$ 150** |

Preço de virada: R$ 333/t.

O preço do carbono sempre aparece nas premissas da resposta.

---

## 9. Fora do escopo (MVP)

- Cotação automática do EU ETS ou do mercado brasileiro (SBCE).
- Conversão de moedas.
- Preço interno salvo por cliente (o valor do usuário vale só para a consulta).
- Créditos de compensação e custo social do carbono.

---

## 10. Evoluções

1. Salvar o preço interno de carbono por cliente (configuração de ESG da empresa).
2. Atualizar o preço de referência periodicamente a partir de uma fonte pública.
3. Usar o preço do SBCE quando o mercado brasileiro tiver cotação.

---

## 11. Pontos a confirmar

1. Se a tool já implementada devolve `custo_total_brl`. Esta versão mantém o nome do campo e muda só o rótulo mostrado ao comprador.
2. Se o Genius exibe tabela em todas as telas em que o agente roda. Onde não exibir, a comparação vira uma lista com uma linha por alternativa.
3. Se a Tool 1 já devolve um indicador de baixa confiança que possa ser repassado em `confianca_baixa`.
4. O limite de 1% para tratar o carbono como irrelevante.
