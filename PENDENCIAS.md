# Pendências

Levantadas no primeiro teste da tool de estimativa de CO2 no Genius (13/11/2026), com o pedido `IHUBTEST-1793824613A`. O cálculo fechou; os itens abaixo são ajustes para antes da submissão, em ordem de importância.

## 1. Pedido de demo com dado representativo

- **Problema:** o pedido de teste tem um item "IHUBTEST item", 1 UN, R$ 10,00. Cai no NAICS genérico 339999 com confiança 0,2, então a demo só mostra o caminho de baixa confiança.
- **O que fazer:** achar (ou criar) um pedido com descrição real de material e mais de um item, e usar esse na apresentação e no guia rápido do README.
- [ ] Resolvido

## 2. "Edição a fixar" aparece na resposta ao usuário

- **Problema:** a constante `FonteModal` em `internal/co2/co2.go:17` termina com "edição a fixar". É um TODO nosso que sai como premissa do cálculo.
- **O que fazer:** fixar a edição GLEC/DEFRA de onde vem o 0,100 kg CO2e por t·km e citar na string, ou remover o trecho.
- [ ] Resolvido

## 3. Agente narra a ausência de `campos_vazios`

- **Problema:** a resposta trouxe "A resposta não trouxe lista de campos vazios". O campo `campos_vazios` é `omitempty` (`internal/tools/co2.go:162`) e o modelo comentou que ele não veio.
- **O que fazer:** instruir, no prompt do agente ou na descrição da tool, a só mencionar campos vazios quando existirem.
- **Como foi resolvido:** `esg_estimar_co2_do_pedido` passou a devolver o campo `como_responder`, que traz a instrução "Cite campos_vazios e campos_estimados só quando existirem na resposta; não comente a ausência deles", e a descrição da tool manda seguir esse campo. Falta só conferir no Genius, depois de sincronizar as tools.
- [x] Resolvido

## 4. Fator em US$ de 2022 convertido com câmbio corrente

- **Problema:** o fator USEEIO é em kg CO2e por US$ de 2022, e a conversão usa a PTAX da data de envio do pedido, sem deflacionar. A premissa exibida não declara isso.
- **O que fazer:** declarar a simplificação nas premissas (mínimo) ou aplicar um deflator do dólar de 2022 para a data do pedido.
- [ ] Resolvido

## 5. Precificação do carbono: campos da alternativa ficaram opcionais (desvio do plano 1)

- **O que mudou:** o plano 1 (`docs/precificacao-carbono/plano-1-tool-precificar-carbono.md`) declara `rotulo`, `valor_brl` e `co2e_kg` de cada alternativa de `esg_precificar_carbono` sem `omitempty`. Com isso o SDK do MCP marca os três como obrigatórios no schema e recusa a chamada, com erro genérico de validação, antes de a tool rodar. As regras 5 e 6 do próprio plano (alternativa sem valor ou sem CO2e fica fora da comparação, com aviso; rótulo vazio vira "Alternativa N") não tinham como funcionar. Os três campos passaram a ser opcionais no schema (`internal/tools/carbono.go`, `AlternativaCarbonoIn`). Decisão tomada em 13/11/2026, durante a implementação.
- **O que isso influencia:**
  - O schema que o Genius lê não diz mais que valor e CO2e são obrigatórios em cada alternativa. O modelo pode mandar uma alternativa incompleta, e a tool responde com a comparação das demais e um aviso com o rótulo da que ficou fora, em vez de um erro. Se o agente não repassar o aviso, o comprador pode não perceber que um fornecedor saiu da comparação.
  - Se a alternativa incompleta for a primeira (a referência), a referência passa a ser a próxima alternativa completa. As diferenças e o preço de virada são calculados contra ela, e um segundo aviso diz contra qual alternativa a conta foi feita. O `como_responder` manda o agente citar os avisos.
  - Com menos de duas alternativas completas, a tool continua devolvendo erro pedindo os dois dados.
- **O que fazer:** validar no Genius uma comparação com uma alternativa sem preço e conferir se a resposta cita o aviso. Se o PO preferir erro em vez de comparação parcial, basta voltar as três tags ao texto do plano e remover os testes desse caso.
- [ ] Validado pelo PO

Os itens 6 a 8 saíram da revisão da precificação do carbono (13/11/2026). O código segue a especificação e os planos; o que está em aberto é a regra.

## 6. Peso do carbono usa o valor do documento inteiro, com CO2e só dos itens calculados

- **Problema:** em `esg_estimar_co2_do_pedido`, o bloco `carbono` usa `documento.valor_total` (como manda o plano 2), mas o `total_kg` soma só os itens que tiveram CO2e calculado. Num pedido de dois itens de R$ 5.000 com só um classificado, o peso do carbono sai pela metade e o carbono pode ser dado como irrelevante sem ser.
- **O que isso influencia:** `peso_carbono_percentual`, `carbono_irrelevante` e `custo_total_brl` ficam subestimados quando há item sem classificação, sem NAICS válido ou sem valor. Com todos os itens calculados, a conta está certa.
- **O que fazer:** decidir se o valor usado passa a ser a soma dos itens que entraram no CO2e (como já é feito na intensidade em kg por R$) ou se basta uma premissa avisando que o percentual é parcial.
- [ ] Resolvido

## 7. CO2e abaixo de 1 kg aparece como "0 kg"

- **Problema:** a especificação (seção 4.3) manda mostrar o CO2e em kg sem casas decimais. Com carga leve, o CO2e do frete fica abaixo de 1 kg: a resposta mostra "0 kg" ao lado de um valor em reais diferente de zero (ex.: delta de −0,3 kg vira "0 kg" e "R$ 0,15"). Em toneladas acontece o mesmo com diferenças abaixo de 50 kg ("0,0 t").
- **O que isso influencia:** principalmente `esg_recomendar_fornecedores` com itens leves: a frase "evita 0 kg, que vale R$ 0,15" confunde o comprador.
- **O que fazer:** definir a regra para valores pequenos (ex.: uma casa decimal abaixo de 10 kg, ou "menos de 1 kg").
- [ ] Resolvido

## 8. Carbono irrelevante não é tratado na comparação

- **Problema:** a seção 6.5 da especificação pede resposta sem tabela quando o carbono pesa menos de 1% do valor. O plano 1 só aplica isso a um documento; na comparação entre alternativas a tool devolve sempre a tabela e o preço de virada, mesmo quando o carbono é irrelevante em todas.
- **O que fazer:** decidir se a comparação também ganha esse caso (campo e texto em `como_responder`).
- [ ] Resolvido

Os itens 9 a 11 saíram da implementação do plano 4, local de entrega na recomendação (`docs/precificacao-carbono/plano-4-local-de-entrega-na-recomendacao.md`, 13/11/2026).

## 9. Local de entrega na recomendação: falta validar no Genius

- **O que mudou:** `esg_recomendar_fornecedores` passou a achar o destino nos itens buscados quando o cabeçalho não tem endereço reconhecido, e a aceitar `destino_municipio` e `destino_uf`. Os testes automatizados passam; o teste no Genius ainda não foi feito.
- **O que fazer:** sincronizar as tools do MCP no Genius Backoffice de Trunk (a tool ganhou dois parâmetros e a descrição mudou) e conferir os dois critérios do plano: a recomendação para a requisição 54773517 traz a distância e o CO2 do frete até Osasco/SP; ao informar outra cidade de entrega no chat, a lista é recalculada com ela.
- [ ] Validado no Genius

## 10. Local de entrega na recomendação: decisões tomadas onde o plano 4 era omisso

- **O que foi decidido:**
  - `destino_por_uf` e a frase correspondente do `como_responder` aparecem sempre que o destino cai no ponto de referência da UF, venha ele do cabeçalho, dos itens ou do informado. A premissa "Município de entrega informado não reconhecido" fica só para o informado.
  - Sem destino, a premissa de distância sai sem parêntese ("o da entrega"), em vez de citar o cabeçalho.
  - Município sem UF (ou o contrário) e UF inválida devolvem erro antes de ler o documento, inclusive na chamada sem itens.
  - Com destino informado, os avisos de locais diferentes entre os itens e de falha na leitura dos locais não aparecem, porque o destino do documento não foi usado.
  - Com o cabeçalho reconhecido, ele vale mesmo que os itens buscados tenham um local próprio diferente (é a ordem de busca do plano). A estimativa (`esg_estimar_co2_do_pedido`) usa o local de cada item, então as duas tools podem mostrar destinos diferentes para o mesmo pedido.
  - O DataHub falso dos testes (`co2_test.go`) passou a responder a consulta de locais e ganhou cinco requisições de teste; o plano supunha que isso já existia.
- **O que fazer:** revisar as decisões; a do cabeçalho contra o local próprio do item é a que mais pode surpreender o comprador.
- [ ] Revisado

## 11. Portão de build: Go fora do PATH e `gofmt` com CRLF

- **Problema:** a máquina onde o plano 4 foi executado não tem Go instalado; o portão rodou na imagem Docker `golang:1.25`. Além disso, `gofmt -l .` lista os arquivos `.go` só por causa do fim de linha: a cópia de trabalho está em CRLF (`core.autocrlf=true`) e o repositório em LF. O formato foi conferido removendo o CR antes.
- **O que isso influencia:** o primeiro comando do portão do guia de execução (`gofmt -l .` não pode listar nenhum arquivo) nunca passa nessa máquina, mesmo com o código formatado.
- **O que fazer:** fixar o fim de linha em LF para `.go` com um `.gitattributes` (`*.go text eol=lf`) e renormalizar a cópia de trabalho, ou ajustar o guia.
- [ ] Resolvido
