# Especificação — Tool 1: Estimativa de CO2 (Genius ESG)

**Projeto:** Hackathon Techweek 2026 — ESG
**Agente:** Genius
**Versão:** 0.2 (MVP, revisada)

Mudanças desta versão em relação à 0.1:

- O CO2 do produto passa a usar o fator USEEIO **sem margens**, para não contar o transporte duas vezes (seções 5 e 7.1).
- A distância passa a ser entre **municípios**, porque o ViaCEP não devolve coordenadas (seções 5 e 7.2).
- Os campos da seção 4 trazem o que já foi encontrado no DataHub de Trunk.
- Novas seções: "Limites da estimativa" (10) e "Ligação com a Tool 2" (11).
- Os pontos a confirmar (seção 13) foram atualizados com o que já se sabe.

---

## 1. Objetivo

Estimar a pegada de carbono (kg CO2e) de um **pedido** ou **requisição** no Mercado Eletrônico e retornar a relação **custo x CO2**, com pesos configuráveis. A saída serve de insumo para a Tool 2 (recomendação de fornecedores alternativos).

A estimativa soma duas parcelas:

- **CO2 do produto:** método por gasto (*spend-based*) com fatores **USEEIO** (EPA), sem margens.
- **CO2 do transporte:** peso × distância × fator do modal.

O resultado é uma **triagem**, não um inventário auditável (ver seção 10).

---

## 2. Fluxo

```
Comprador → Genius: "analise o impacto ESG do pedido 12345"
  │
  ├─ 1. Buscar dados no DataHub (pedido/requisição, itens, fornecedor, local de entrega)
  ├─ 2. Enriquecer com LLM (NAICS, peso estimado, confiança)
  ├─ 3. Calcular distância (município de origem → município de destino)
  ├─ 4. Calcular CO2 produto + CO2 transporte
  ├─ 5. Calcular score custo x CO2 com pesos
  └─ 6. Retornar resultado + premissas
```

---

## 3. Entrada da tool

| Parâmetro | Tipo | Obrigatório | Descrição |
|---|---|---|---|
| `documento_tipo` | `"pedido"` \| `"requisicao"` | Sim | Tipo de documento a analisar |
| `documento_id` | string | Sim | Número do pedido ou da requisição |
| `peso_custo` | number (0–1) | Não (padrão 0,7) | Peso do custo no score |
| `peso_co2` | number (0–1) | Não (padrão 0,3) | Peso do CO2 no score (`peso_custo + peso_co2 = 1`) |
| `modal` | `"rodoviario"` \| `"ferroviario"` \| `"cabotagem"` \| `"aereo"` | Não (padrão rodoviário) | Modal de transporte, se não vier do DataHub |

A empresa compradora não é parâmetro: vem do token do usuário.

---

## 4. Busca de dados na fonte (DataHub)

Todos os dados transacionais vêm do **DataHub**. A coluna "No DataHub de Trunk" traz o que já foi visto no catálogo; o que está como "a confirmar" ainda não foi conferido.

Já existem flows REST publicados de detalhe de pedido (`doc-detail-order`) e de requisição (`doc-detail-request`). Eles podem servir de fonte para esta tool, mas os campos que devolvem ainda não foram conferidos.

### 4.1 Cabeçalho do pedido / requisição

| Dado | Uso | No DataHub de Trunk |
|---|---|---|
| ID do pedido / requisição | Chave da consulta | Pedido: `Pedidos.Pedido` (entidade `order`, campo `id`). Requisição: a confirmar |
| Empresa compradora (cliente) | Escopo / filtro de tenant | Pedido: `tenantId` (IdMain do comprador) |
| Data de emissão | Câmbio e ano de referência | Pedido: `sentDate` (`Pedidos.DataEnvio`) |
| Moeda | Conversão para US$ | Pedido: `currency`. Em Trunk há valores em branco e fora do padrão |
| Valor total | Conferência | Pedido: `total` |
| Status | Ignorar cancelados | Pedido: `refusalStatus` (1 = recusado, 5 = cancelado) |

### 4.2 Itens do pedido / requisição

| Dado | Uso | No DataHub de Trunk |
|---|---|---|
| Código do item / material | Identificação | Item do pedido: código do produto |
| Descrição do item | Entrada do LLM para o NAICS | Item do pedido: `Descricao` e `Complemento` |
| NCM | Apoio ao mapeamento para o NAICS | Existe no item de pré-pedido (`ncm`). No item de pedido: a confirmar |
| Categoria / família de compra | Apoio ao mapeamento | Grupo do produto, quando o item tem produto cadastrado |
| Quantidade | Cálculo do peso | Item do pedido: `Quantidade` |
| Unidade de medida | Cálculo do peso | Item do pedido: `Unidade` |
| Peso unitário / bruto | Transporte | **Não encontrado** no catálogo. O peso será estimado pelo LLM |
| Valor unitário / total do item | CO2 do produto (*spend-based*) | Item do pedido: `Preco` × `Quantidade` |

> Requisições podem não ter fornecedor nem preço definidos. Nesse caso, usar o **preço estimado** da requisição e o **fornecedor sugerido / último fornecedor do item**, se disponível; senão, calcular só o CO2 do produto e sinalizar.

> Itens soltos, digitados em texto livre, não têm produto cadastrado. Para eles só há a descrição, sem grupo nem NCM.

### 4.3 Fornecedor

| Dado | Uso | No DataHub de Trunk |
|---|---|---|
| ID / CNPJ do fornecedor | Identificação | `Usuarios.ID`, `Usuarios.CGC` |
| Razão social | Exibição | `Usuarios.Nome` |
| CEP / endereço de origem | Distância (origem) | CEP: a confirmar |
| Cidade / UF | Distância (origem) | `Usuarios.Municipio`, `Usuarios.Estado` |
| Certificações (ISO 14001 etc.) | Insumo da Tool 2 | A confirmar |

### 4.4 Entrega

| Dado | Uso | No DataHub de Trunk |
|---|---|---|
| Local de entrega | Distância (destino) | Pedido: `deliveryPlaceId`, no cabeçalho. Pré-pedido: também por item. O cadastro de locais tem cidade e UF; CEP a confirmar |
| Planta / centro de custo | Fallback do endereço | A confirmar |
| Incoterm / tipo de frete (CIF/FOB) | Premissa do transporte | Pedido: `incoterms` |
| Modal (se existir) | Fator de emissão | Não encontrado. O pedido tem transportadora (`carrierSupplierId`), mas não o modal |

### 4.5 Regras da busca

- Filtrar sempre pela **empresa do usuário** (multi-tenant).
- Ignorar itens cancelados.
- Se houver vários locais de entrega por item, calcular a distância **por item**.
- Registrar no resultado quais campos vieram **vazios** e quais foram **estimados**.

---

## 5. Fontes de referência (fora do DataHub)

| Fonte | Uso |
|---|---|
| **USEEIO — EPA "Supply Chain GHG Emission Factors"** (CSV) | Fator kg CO2e / US$ por NAICS. Coluna: *Supply Chain Emission Factors **without** Margins* (US$ 2022) |
| Fatores de transporte (GLEC / DEFRA) | kg CO2e / t·km por modal |
| Câmbio BRL→USD (BCB/PTAX) | Conversão do valor (média do ano ou data do pedido) |
| Tabela de municípios do IBGE com latitude e longitude | Coordenadas para a distância |
| ViaCEP (opcional) | Só para achar o município a partir de um CEP |

**Por que o fator sem margens.** O fator "com margens" já inclui emissões de transporte e distribuição. Somar o CO2 do transporte calculado aqui por cima dele contaria parte do frete duas vezes. Alternativa, se o time preferir manter o fator com margens: mostrar o CO2 do transporte como indicador separado, fora do total.

**Por que distância entre municípios.** O ViaCEP não devolve latitude nem longitude; devolve o código IBGE do município. A tabela de municípios do IBGE resolve a coordenada. O DataHub de Trunk já tem um flow publicado que consulta o ViaCEP.

Fatores de transporte do MVP:

| Modal | kg CO2e / t·km |
|---|---|
| Rodoviário | 0,10 |
| Ferroviário | 0,03 |
| Cabotagem | 0,015 |
| Aéreo | 0,80 |

Os valores estão na ordem de grandeza esperada, mas falta fixar e citar a edição da fonte (GLEC ou DEFRA, e o ano).

---

## 6. Enriquecimento com LLM

Para cada item, o LLM recebe `descricao`, `ncm`, `categoria`, `quantidade` e `unidade`, e devolve:

```json
{
  "naics": "331110",
  "naics_descricao": "Iron and steel mills",
  "confianca": 0.82,
  "peso_estimado_kg_por_unidade": 12.5,
  "justificativa": "Item descrito como chapa de aço laminada..."
}
```

Regras:

- O NAICS deve existir na tabela USEEIO (validar após a resposta; se não existir, refazer com uma lista de candidatos).
- Pré-filtrar candidatos (por NCM→NAICS ou embeddings) para reduzir alucinação.
- Só estimar peso se `peso_bruto` não vier do DataHub. Como o campo de peso não foi encontrado, na prática o peso será sempre estimado.
- Abaixo de `confianca < 0.5`, marcar o item como **baixa confiança**.

O peso estimado afeta o número absoluto de kg de CO2 do transporte. Ele não afeta a comparação entre fornecedores do mesmo item, porque a carga é a mesma para todos (ver seção 11).

---

## 7. Cálculos

### 7.1 CO2 do produto
```
valor_usd     = valor_total_item_brl / cambio_brl_usd
co2_produto   = valor_usd × fator_useeio_sem_margens[naics]   # kg CO2e
```
*(Opcional: deflacionar para US$ 2022 pelo CPI dos EUA.)*

### 7.2 Distância
```
d_reta        = haversine(latlong(municipio_fornecedor), latlong(municipio_entrega))
distancia_km  = d_reta × 1.3                              # fator de sinuosidade rodoviária
```
Fornecedor e entrega no mesmo município dão distância zero. Usar uma distância mínima, a definir.

### 7.3 CO2 do transporte
```
peso_t          = quantidade × peso_unitario_kg / 1000
co2_transporte  = peso_t × distancia_km × fator_modal     # kg CO2e
```

### 7.4 Total e score
```
co2_total = Σ (co2_produto + co2_transporte) dos itens

score = peso_custo × custo_norm + peso_co2 × co2_norm
```
A normalização (0–1) é feita no conjunto em comparação: pedido atual + alternativas da Tool 2. Isoladamente, a Tool 1 retorna os valores absolutos e a intensidade `kg CO2e / R$`.

---

## 8. Saída

```json
{
  "documento": { "tipo": "pedido", "id": "12345", "fornecedor": "Aço Sul Ltda", "valor_total_brl": 48000 },
  "co2": {
    "total_kg": 9120.4,
    "produto_kg": 8960.0,
    "transporte_kg": 160.4,
    "intensidade_kg_por_brl": 0.19
  },
  "itens": [
    {
      "item": "Chapa aço 3mm",
      "naics": "331110",
      "confianca": 0.82,
      "valor_brl": 48000,
      "peso_kg": 2000,
      "distancia_km": 802,
      "co2_produto_kg": 8960.0,
      "co2_transporte_kg": 160.4,
      "campos_estimados": ["peso_kg"]
    }
  ],
  "pesos": { "custo": 0.7, "co2": 0.3 },
  "premissas": [
    "Fatores de produto: USEEIO/EPA sem margens (economia dos EUA, US$ 2022), convertidos pelo câmbio de 5,40.",
    "Categoria (NAICS) mapeada por IA; confiança indicada por item.",
    "Distância = linha reta entre os municípios × 1,3; modal rodoviário assumido.",
    "Peso estimado por IA, porque o pedido não traz peso.",
    "Estimativa por gasto: compara categorias, não diferencia fornecedores do mesmo item.",
    "Resultado de triagem: não substitui inventário de emissões auditado."
  ]
}
```

Os números do exemplo são ilustrativos.

---

## 9. Tratamento de exceções

| Situação | Comportamento |
|---|---|
| Pedido/requisição não encontrado | Erro claro ao comprador |
| Município do fornecedor ou de entrega ausente ou não reconhecido | Usar o centroide da UF; se não houver UF, `co2_transporte = null` e premissa sinalizada |
| Valor ausente (requisição sem preço) | Usar preço estimado; senão, `co2_produto = null` |
| NAICS inválido ou confiança baixa | Marcar o item e seguir, sinalizando na resposta |
| Moeda diferente de BRL | Converter direto para USD |
| Moeda em branco ou fora do padrão | Assumir BRL e sinalizar na premissa |

---

## 10. Limites da estimativa

- **O CO2 do produto não distingue fornecedores.** No método por gasto, o CO2 é valor × fator da categoria. Para o mesmo item, o fornecedor mais barato sempre parece o mais limpo, só por ser mais barato. Esse número serve para comparar categorias e pedidos, não fornecedores.
- **No transporte, o que muda entre fornecedores é a distância.** O peso da carga é o mesmo para qualquer fornecedor do item.
- **É triagem, não inventário auditável.** Os fatores são da economia dos EUA, convertidos por câmbio, e a categoria e o peso são estimados por IA. O resultado ajuda a priorizar e a comparar; não sustenta um número de relatório auditado.
- **O transporte costuma ser parcela pequena do total.** No exemplo da seção 8, são 160 kg em 9.120 kg. Trocar de fornecedor muda pouco o total, e a resposta deve dizer isso.

---

## 11. Ligação com a Tool 2 (recomendação de fornecedores)

A Tool 2 pode partir do recomendador de fornecedores que já existe em Trunk (flow `recomendacao-fornecedores-recomendar`), com o CO2 do frete como mais um critério.

- **Critério que diferencia:** só o CO2 do transporte entra na comparação entre fornecedores. O CO2 do produto fica como informação do item.
- **O que falta no recomendador:** o resolver de quem já vendeu devolve a UF do fornecedor e a nota de cada critério. Falta devolver o município.
- **Onde calcular a distância:** o banco do ME não tem coordenadas, então a distância não sai do SQL. O servidor MCP em Go pode embutir a tabela de municípios, calcular a distância e o CO2 do frete e reordenar a lista com um peso de CO2.
- **Pesos:** os dois pesos desta especificação (custo 0,7 e CO2 0,3) viram um caso particular dos pesos do recomendador, que já aceita preço, frete, prazo, pontualidade, capacidade e cobertura.
- **Tenant:** esta especificação exige filtro pela empresa do usuário. Os resolvers do recomendador leem todos os clientes, sem trava, e hoje servem só para Trunk.

---

## 12. Fora do escopo (MVP)

- Fatores específicos do Brasil (EXIOBASE-BR): evolução futura.
- Dados primários de emissão do fornecedor (EPD / inventário GHG).
- Emissões de uso e fim de vida do produto.
- Rotas reais de transporte (API de roteirização).

---

## 13. Pontos a confirmar

| Ponto | Situação |
|---|---|
| 1. Nomes reais das tabelas e campos | Pedido, item do pedido e fornecedor: vistos (seção 4). Requisição: a confirmar. Campos devolvidos pelos flows `doc-detail-order` e `doc-detail-request`: a confirmar. |
| 2. Peso bruto e NCM nos itens | Peso: não encontrado no catálogo. NCM: existe no item de pré-pedido; no item de pedido, a confirmar. |
| 3. Local de entrega no cabeçalho ou no item | Pedido: cabeçalho. Pré-pedido: também por item. CEP no cadastro de locais: a confirmar; cidade e UF existem. |
| 4. Forma de acesso do Genius ao DataHub | Duas opções viáveis: servidor MCP em Go chamando flows REST com o token do usuário (como no recomendador), ou flows do DataHub com gatilho MCP direto. A decidir. |
| 5. Fonte de geocodificação | ViaCEP já é usado num flow de Trunk, mas não devolve coordenadas. Falta escolher e carregar a tabela de municípios com latitude e longitude. |
| 6. Fator USEEIO com ou sem margens | Esta versão adota sem margens. Confirmar com o time. |
| 7. Edição dos fatores de transporte | Fixar a fonte (GLEC ou DEFRA) e o ano. |
| 8. Distância mínima para o mesmo município | A definir. |
| 9. Sentido da normalização do score | Definir se `custo_norm` e `co2_norm` valem 1 para o melhor ou para o pior, e manter igual à Tool 2. |
| 10. Onde roda o LLM | Modelo do próprio Genius, um passo de IA no DataHub ou uma chamada no servidor Go. A decidir, junto com a política da ME sobre enviar descrições a provedor externo. |
