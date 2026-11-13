# Hackathon ME: contexto

Time de 4 devs. Tema/agente ainda **não definido**. Este arquivo guarda as regras do hackathon (tiradas dos slides oficiais) pra guiar a escolha e o desenvolvimento.

## A missão

Criar um **agente de IA** que resolva uma dor real ou execute uma tarefa de ponta a ponta sozinho.

**Só valem agents no contexto de:**
- e-Procurement
- e-Sourcing
- CLM (Contract Lifecycle Management)
- GF
- Gestão de Materiais
- Logística

## Stack de ferramentas

O agente é criado no Genius, seu código é um MCP server hospedado na Vercel ou no Railway, e ele acessa dados e modelos pelo DataHub.

| Peça | Papel |
|---|---|
| **MCP server** | O código do agente. Versionado no GitHub. |
| **Vercel ou Railway** | Hospedagem e deploy do MCP server. |
| **ME Genius** | Plataforma de orquestração de agentes. O agente é criado aqui; o Genius é o harness que o executa, conectado ao MCP server. Submissão na **Lojinha do Genius** no dia 13. |
| **ME-DataHub** | Plataforma de dados de domínio. GraphQL federado que reúne os domínios do ME; acesso via MCPs. |
| **OpenRouter** (opcional) | Só se alguma tool do agente precisar de IA própria (OCR e afins). |

Fluxo: `Genius (agente) -> MCP server (Vercel/Railway) -> DataHub MCPs / APIs ME`

## Objetivos do hackathon

1. **Criar um agente de ponta a ponta**: estruturar um MCP server, definir tools, entender como são chamadas, fazer deploy.
2. **Dominar o ME Genius**: criação de agentes, publicação na Lojinha, interação entre agentes customizados e os Lead Agents.
3. **Explorar o Data Hub**: GraphQL federado, MCPs que facilitam consumo das APIs, criação de fluxos e geração de novos MCPs.
4. **Entregar valor ao cliente**: simplificar tarefas do dia a dia. Soluções pequenas e bem resolvidas já fazem diferença.
5. **Olhar além do ME**: inspiração de mercado, referências de fora.
6. **Construir em equipe**: entregável concreto ao final.

## Critérios de validação

A entrega é o **agente publicado na Loja do ME Genius**.

1. **Funcionamento**: executa a tarefa de ponta a ponta, mesmo que simplificada. Protótipos mockados, sem execução real, **não são aceitos**.
2. **Integração com o produto**: conectado à plataforma ME via MCPs do Data Hub, APIs ou mecanismos oficiais. Simulações e dados fictícios **não** contam como integração real.
3. **Publicação na Loja do Genius**: disponível até **18h do dia 13**, com:
   - MCP server no ar (Vercel ou Railway)
   - repositório versionado no GitHub
   - guia rápido de uso
4. **Escopo realista**: o apresentado tem que ter sido feito durante o hackathon. Commits do período podem ser auditados; componentes externos devem ser listados.
5. **Segurança e conformidade**: sem expor dados sensíveis, sem violar compliance/LGPD. Senhas, chaves e tokens **nunca hardcoded** (usar env vars).

## Regras práticas pro desenvolvimento

- Toda tool do MCP precisa bater em dado real (DataHub/API ME), nada de mock.
- Segredos só via variáveis de ambiente (Vercel/Railway). `.env` no `.gitignore`.
- Commits pequenos e frequentes (auditoria do período).
- Manter lista de dependências/componentes externos no README.
- README precisa ter o guia rápido de uso.
- Escopo pequeno e bem resolvido > escopo grande e quebrado.
