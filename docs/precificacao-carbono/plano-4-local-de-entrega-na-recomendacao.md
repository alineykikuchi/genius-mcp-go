# Plano 4: local de entrega na recomendação de fornecedores

Ajuste na tool `esg_recomendar_fornecedores` (`internal/tools/recomendar.go`). Duas partes: corrigir de onde a tool lê o local de entrega e aceitar um local informado pelo comprador ou pelo Genius.

## O que aconteceu

Na requisição 54773517, a recomendação respondeu "o município de entrega não foi reconhecido no cabeçalho do documento" e devolveu o fornecedor sem distância nem CO2 do frete. O local de entrega existe: está no **item** da requisição (SWIFT OSASCO KM 18, Osasco/SP), não no cabeçalho.

A causa está no código:

- `esg_recomendar_fornecedores` lê o destino **só do cabeçalho** do documento (`o.DeliveryAddress`).
- `esg_estimar_co2_do_pedido` lê o cabeçalho **e** o local próprio de cada item (`locaisDosItens`, em `internal/tools/co2.go`). Por isso a estimativa da mesma requisição achou Osasco/SP e a recomendação não.

Além disso, quando o destino falta, o Genius oferece "se você confirmar a cidade de entrega, tento de novo", mas a tool não tem parâmetro para receber essa cidade.

## Resultado esperado

1. A recomendação acha o local de entrega nos itens quando o cabeçalho não tem.
2. O comprador (ou o Genius, com o destino que já veio de outra tool de ESG para o mesmo documento) pode informar a cidade e a UF de entrega.
3. Quando não há destino em lugar nenhum, a tool orienta o Genius a pedir a cidade e a UF, em vez de apresentar uma lista sem CO2.

## Arquivos

| Arquivo | Mudança |
|---|---|
| `internal/tools/recomendar.go` | Entrada nova, ordem de busca do destino, saída e `como_responder`. |
| `internal/tools/co2.go` | Só se for preciso ajustar `locaisDosItens` para o caso descrito na parte A. |
| `recomendar_test.go` | Casos novos. |
| `README.md` | Uma linha no guia rápido da recomendação. |

Sem mudança no DataHub e sem dependência nova.

## Parte A: achar o destino nos itens

Ordem de busca do destino, parando no primeiro que for reconhecido:

| Ordem | Fonte | `destino_origem` |
|---|---|---|
| 1 | Destino informado na chamada (parte B) | `informado` |
| 2 | Local de entrega do cabeçalho do documento | `cabecalho` |
| 3 | Local de entrega dos itens **buscados** | `itens` |
| — | Nenhum | vazio |

Regras da fonte 3:

1. Reaproveite `d.locaisDosItens(ctx, o)`, que já resolve o endereço de cada item pelo DataHub.
2. Considere só os itens que entraram na busca (os de `in.Itens` que existem no documento).
3. Um município só entre esses itens: use-o.
4. Mais de um município: use o que aparece em mais itens (em empate, o do item de menor número) e acrescente a premissa "Os itens buscados têm locais de entrega diferentes; a distância usou <município/UF>. Para os outros locais, busque os itens em chamadas separadas."
5. **Atenção a um caso:** `locaisDosItens` pula o item cujo local é igual ao do cabeçalho. Se o cabeçalho tem o id do local mas não tem endereço reconhecido, esses itens ficam sem endereço. Nesse caso os locais dos itens precisam ser resolvidos mesmo sendo iguais ao do cabeçalho. Ajuste a função (por exemplo, com um parâmetro) sem mudar o resultado da estimativa.
6. Se a leitura dos locais falhar, siga sem destino e registre nas premissas; não derrube a chamada.

## Parte B: destino informado

### Entrada

Acrescente em `RecomendarIn`:

```go
DestinoMunicipio string `json:"destino_municipio,omitempty" jsonschema:"município de entrega, informado pelo usuário ou vindo de outra tool de ESG para o mesmo documento; use junto com destino_uf"`
DestinoUF        string `json:"destino_uf,omitempty" jsonschema:"sigla da UF de entrega, por exemplo SP"`
```

### Regras

1. Os dois campos vêm juntos. Só um deles: erro "Informe o município e a UF de entrega."
2. UF que não existe: erro "UF de entrega inválida: use a sigla, por exemplo SP."
3. Município não reconhecido com UF válida: use o ponto de referência da UF (como a tool já faz com fornecedores), marque `destino_por_uf` e acrescente a premissa "Município de entrega informado não reconhecido: a distância usou o ponto de referência da UF."
4. O destino informado tem prioridade sobre o do documento. Se o documento tiver outro local de entrega reconhecido, acrescente a premissa "Destino informado (<X>) diferente do local de entrega do documento (<Y>)."
5. O destino informado vale só para a chamada. Nada é gravado no documento.

### Saída

Em `RecomendarOut`, ao lado do `destino` que já existe:

```go
DestinoOrigem string `json:"destino_origem,omitempty"` // informado, cabecalho ou itens
DestinoPorUF  bool   `json:"destino_por_uf,omitempty"`
```

Na premissa de distância, troque "o da entrega (cabeçalho do documento)" pela origem real do destino: "informado pelo usuário", "cabeçalho do documento" ou "local de entrega dos itens".

### `como_responder`

- **Sem destino:** troque a orientação atual por: "O local de entrega não foi encontrado no documento. Apresente os fornecedores na ordem do recomendador, em uma tabela curta com Fornecedor e Origem, diga em uma frase que falta o local de entrega para calcular distância e CO2 do frete, e peça a cidade e a UF de entrega. Quando o usuário responder, chame a tool de novo com destino_municipio e destino_uf."
- **Destino informado:** acrescente "Diga que a distância usou o local de entrega informado (destino)."
- **Destino pela UF:** acrescente "Diga que o município não foi reconhecido e que a distância é aproximada, pelo ponto de referência da UF."

### Descrição da tool

Acrescente ao fim: "Se o usuário informar a cidade de entrega, ou se ela já veio em outra tool de ESG para o mesmo documento, passe em destino_municipio e destino_uf. Não invente o local de entrega."

## Testes

Siga o padrão de `recomendar_test.go` e, para o local por item, o que o teste da estimativa já faz com o DataHub falso.

| Caso | Esperado |
|---|---|
| Pedido com destino no cabeçalho (teste que já existe) | Mesmo resultado; `destino_origem` = `cabecalho`. |
| Requisição sem destino no cabeçalho e com local no item | Distância calculada; `destino_origem` = `itens`. |
| Cabeçalho com id do local mas sem endereço, item com o mesmo local | Destino resolvido pelos itens. |
| Dois itens buscados com locais diferentes | Usa o mais frequente; premissa dos locais diferentes. |
| `destino_municipio` = "Osasco", `destino_uf` = "SP" em documento sem destino | Distância calculada; `destino_origem` = `informado`. |
| Destino informado diferente do cabeçalho | Usa o informado; premissa da diferença. |
| Só `destino_municipio` | Erro da tool. |
| `destino_uf` = "XX" | Erro da tool. |
| Município desconhecido com UF válida | `destino_por_uf` = `true`; distância pelo ponto da UF. |
| Sem destino em lugar nenhum | Nota de CO2 neutra; `como_responder` pede a cidade e a UF. |
| Leitura dos locais dos itens falha | Chamada segue sem destino; premissa de aviso. |

Os testes que já existem não podem mudar de resultado, exceto os que conferem o texto antigo da premissa de distância.

## Extensão opcional

A mesma entrada (`destino_municipio` e `destino_uf`) pode entrar em `esg_estimar_co2_do_pedido`, para documentos em que nem o cabeçalho nem os itens têm local de entrega. Não faz parte deste plano.

## Pedido para colar no Claude Code

```text
Implemente docs/precificacao-carbono/plano-4-local-de-entrega-na-recomendacao.md neste repositório.

Leia antes o plano inteiro e os arquivos internal/tools/recomendar.go, internal/tools/co2.go (locaisDosItens e localizar) e recomendar_test.go.

Siga as regras e o portão de build e testes de docs/precificacao-carbono/guia-execucao.md (gofmt -l . ; go vet ./... ; go build ./... ; go test ./...). Um commit local ao fim. Não faça push.

Não implemente a extensão opcional. Se algum ponto do plano não bater com o código, pare e me pergunte.
```

## Pronto quando

- O portão passa.
- No Genius, a recomendação para a requisição 54773517 traz a distância e o CO2 do frete até Osasco/SP.
- Ao informar outra cidade de entrega no chat, a lista é recalculada com ela.
