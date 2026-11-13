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
- [ ] Resolvido

## 4. Fator em US$ de 2022 convertido com câmbio corrente

- **Problema:** o fator USEEIO é em kg CO2e por US$ de 2022, e a conversão usa a PTAX da data de envio do pedido, sem deflacionar. A premissa exibida não declara isso.
- **O que fazer:** declarar a simplificação nas premissas (mínimo) ou aplicar um deflator do dólar de 2022 para a data do pedido.
- [ ] Resolvido
