# Avaliacao Inicial v1: Camila Santos — Startup Dev

---

## Perfil

**Nome:** Camila Santos
**Papel:** Desenvolvedora fullstack, 2 anos de experiencia em startup de pesquisa
**Stack do dia a dia:** TypeScript, React, Next.js
**Contexto:** Time de 3 pesquisadores/devs que precisa de uma ferramenta de memoria semantica compartilhada para capturar conhecimento enquanto pesquisa online.
**Expectativa principal:** Ferramenta que o time inteiro possa usar — idealmente com alguma camada de compartilhamento — com API consumivel por paineis externos (Next.js) e busca semantica de verdade.

---

## Projeto 1 — ResearchSync (Uso em equipe)

### O que funciona bem

- Arquitetura local-first e single-user e bem executada para uso individual: cada dev instala daemon + extensao e comeca a usar sem configuracao de servidor
- Onboarding documentado no `GUIA.md` com dois caminhos (nativa via systemd + Docker), comandos copiavel e secao de troubleshooting
- O daemon usa porta aleatoria + token rotacionado a cada restart (`server.go:41-42`), o que elimina colisao de portas entre instancias locais
- Docker Compose com bind exclusivo em `127.0.0.1:7532` garante que cada instancia fique isolada

### O que nao funciona / esta ausente

- **Sem sincronizacao entre maquinas**: cada daemon escreve em seu proprio `~/.local/share/vbm/vbm.db` — nao ha mecanismo de replicacao, export/import ou servidor compartilhado. O roadmap cita `v0.3` para sincronizacao E2EE mas nao existe na codebase
- **Sem conceito de "team workspace"**: sem multi-usuario, sem namespacing por pessoa, sem merge de indices
- **CORS bloqueado para origens nao-extensao**: `authMiddleware` (routes.go:234) rejeita qualquer `Origin` que nao seja `chrome-extension://` — um painel Next.js do time nao consegue chamar a API sem modificacao
- **Token nao acessivel para clientes externos**: o token esta em `session.json` (chmod 600) — um desenvolvedor externo que queira montar um dashboard precisa ler esse arquivo manualmente e acoplar ao processo de cada membro do time

### Scores — Projeto 1

| Dimensao | v1 | Comentario |
|---|---|---|
| Onboarding | 3 | Bom para 1 pessoa; zero suporte a setup de equipe |
| API Ergonomics | 3 | API limpa mas CORS bloqueia clientes nao-extensao |
| Feature Completeness | 1 | Sem sincronizacao, sem multi-usuario — blocker para o cenario |
| Security Confidence | 4 | Token rotacionado, chmod 600, bind loopback — solido para 1 usuario |
| Language Quality | 4 | Go idiomatico, erros propagados corretamente |
| Operational Readiness | 3 | systemd + Docker funcionam; sem ha/replicacao |
| Documentation | 3 | Guia individual excelente; guia de equipe inexistente |

---

## Projeto 2 — APIIntegration (Painel Next.js)

### O que funciona bem

- API REST bem definida: 5 endpoints claros (`/ingest`, `/search`, `/forget`, `/status`, `/healthz`) com JSON consistente
- Tipos compartilhados em `proto/types.ts` — um painel Next.js pode importar os mesmos tipos sem duplicar (`SearchResult`, `StatusResponse`, `WsStatusMessage`)
- WebSocket em `/ws` existe e envia atualizacoes de status a cada 5s (`routes.go:186-208`)
- Respostas de erro padronizadas: `{"error":"mensagem"}` com status HTTP apropriado
- CORS middleware presente (`routes.go:252-266`) — mas so permite `chrome-extension://`

### O que nao funciona / esta ausente

- **CORS hardcoded para extensao**: `corsMiddleware` so seta `Access-Control-Allow-Origin` se origin comecar com `chrome-extension://` (routes.go:255). Um painel Next.js em `http://localhost:3000` recebe resposta sem header CORS e o browser bloqueia
- **Auth nao acessivel para externos**: sem endpoint `/token` ou mecanismo de distribuicao de token — consumidor externo precisa ler `session.json` na maquina do usuario, inviavel em time distribuido
- **SearchResponse divergente**: `proto/types.ts:31` declara `total: number` no `SearchResponse`, mas o handler Go (routes.go:129-131) nao retorna o campo `total` — inconsistencia de contrato
- **`search()` no daemon-client.ts:67** faz `res.json() as Promise<SearchResult[]>` mas o endpoint retorna `{results: [...]}` — o cliente esta bugado (nao usa `.results`)
- **Porta dinamica**: sem porta fixa por default, painel externo precisa ler `session.json` para saber onde conectar — acoplamento operacional alto
- **WebSocket sem push de eventos novos**: o `/ws` so emite `status` (indexed/pending) a cada 5s via ticker — nao ha push de `page_indexed`, impossibilitando feed em tempo real de novas capturas

### Scores — Projeto 2

| Dimensao | v1 | Comentario |
|---|---|---|
| Onboarding | 2 | Precisa ler session.json manualmente para obter token+porta |
| API Ergonomics | 3 | Endpoints claros mas CORS e auth tornam integracao externa trabalhosa |
| Feature Completeness | 2 | API existe mas CORS bloqueia uso real; bug no client TS |
| Security Confidence | 3 | Segura localmente; sem mecanismo de distribuicao segura do token |
| Language Quality | 3 | Bug de contrato SearchResponse/total; tipos duplicados em daemon-client.ts |
| Operational Readiness | 2 | Porta aleatoria complica automacao; sem versionamento de API |
| Documentation | 2 | Sem documentacao de API externa; endpoints so inferidos pelo codigo |

---

## Projeto 3 — RealTimeMonitor (Dashboard de status)

### O que funciona bem

- WebSocket funcional em `/ws` com autenticacao (coberto pelo `authMiddleware`) — conexao com token no header funciona
- Payload `{type: "status", indexed: N, pending: N}` e simples e previsivel (`routes.go:197-203`)
- `/status` REST como fallback polling: retorna `indexed`, `pending`, `version` (`routes.go:161-178`)
- Graceful shutdown implementado (`server.go:67-77`): `signal.NotifyContext` captura SIGTERM/SIGINT, `Shutdown` com timeout de 5s
- `queue.go` com canal bufferizado cap 256 e backpressure definido: ingest descartado com log se cheio — comportamento documentado no CLAUDE.md

### O que nao funciona / esta ausente

- **Sem metricas de erro**: `/status` retorna apenas `indexed` e `pending` — sem contagem de erros de ingest, sem taxa de descarte da fila, sem latencia
- **Sem alertas**: o WebSocket nao envia eventos de falha (ex: fila cheia, erro de embedding, erro de FTS rebuild) — dashboard nao tem como saber que algo falhou
- **Tick fixo de 5s sem push-on-change**: o servidor manda status a cada 5s independente de mudanca — ineficiente e sem granularidade para alertas imediatos
- **Sem histograma / serie temporal**: nao ha endpoint de metricas historicas — impossivel mostrar grafico de paginas indexadas ao longo do tempo
- **FTS rebuild sincrono dentro da transacao de ingest** (`sqlite.go:198`): `INSERT INTO chunks_fts(chunks_fts) VALUES('rebuild')` dentro do `tx` de ingest — contraria a regra documentada em CLAUDE.md ("Nunca fazer FTS5 rebuild sincrono em operacoes de ingest") e pode causar lentidao perceptivel com volumes maiores

### Scores — Projeto 3

| Dimensao | v1 | Comentario |
|---|---|---|
| Onboarding | 3 | WebSocket direto ao ponto; payload previsivel |
| API Ergonomics | 3 | Contrato WS simples; falta payload de erros |
| Feature Completeness | 2 | Status basico existe; sem metricas, sem alertas, sem historico |
| Security Confidence | 4 | Auth no WS correto; graceful shutdown presente |
| Language Quality | 3 | FTS rebuild sincrono viola regra propria do projeto |
| Operational Readiness | 2 | Sem observabilidade real; sem health checks estruturados |
| Documentation | 2 | WS documentado apenas em proto/types.ts; sem exemplos de uso |

---

## Projeto 4 — SmartSearch (Busca semantica real)

### O que funciona bem

- Arquitetura hibrida BM25+cosine+RRF esta corretamente implementada (`sqlite.go:212-341`)
- FTS5 via `modernc.org/sqlite` (pure Go) — sem dependencia de SQLite do sistema
- RRF fusion com k=60 (`sqlite.go:343-352`) — parametro canonico da literatura
- Chunking sliding window 512/64/40 tokens implementado (`chunk/chunk.go` referenciado)
- Dedup de conteudo via `text_hash` com `INSERT OR IGNORE` evita reindexar conteudo identico
- Interface `Embedder` com `Version()` permite migracao futura de modelo sem quebrar schema

### O que nao funciona / esta ausente

- **StubEmbedder retorna vetor zero** (`embedder.go:21-23`): `make([]float32, 384)` — toda busca vetorial calcula cosseno de zeros, resultando em score 0 para todos os chunks. A busca e efetivamente BM25-only; o "hibrido" e ilusorio em v0.1
- **Brute-force O(n) no dense search** (`sqlite.go:263`): `SELECT id, text, page_id, embedding FROM chunks` carrega TODOS os chunks em memoria para calcular cosseno — nao escala com volumes maiores (ex: 100k chunks = ~150MB de BLOBs lidos por query)
- **N+1 queries no build de resultados** (`sqlite.go:315-326`): para cada chunk no top-k, executa 2 queries separadas (`SELECT text` + `SELECT url,title`). Com limit=20 sao ate 40 queries sequenciais
- **FTS5 nao tolerante a erros de digitacao**: query exata ao FTS5 — "tokio assync" nao encontra "tokio async"
- **TODO explicito no codigo** (`embedder.go:22`): `// TODO: replace with onnxruntime-go + Snowflake/snowflake-arctic-embed-xs` — o proprio codigo admite que a feature principal nao esta implementada

### Scores — Projeto 4

| Dimensao | v1 | Comentario |
|---|---|---|
| Onboarding | 3 | Busca funciona (BM25); expectativa de vetorial nao e atendida |
| API Ergonomics | 4 | Endpoint /search simples, limit configuravel, snippets incluidos |
| Feature Completeness | 2 | Busca hibrida real nao funciona; stub embedder = BM25-only |
| Security Confidence | 4 | Sem risco de seguranca especifico aqui |
| Language Quality | 3 | Arquitetura correta; N+1 e brute-force sao problemas de qualidade |
| Operational Readiness | 2 | Nao escala; sem index vetorial; sem telemetria de qualidade de busca |
| Documentation | 2 | Stub nao documentado para o usuario final; TODO no codigo |

---

## Projeto 5 — ExtensionDX (Experiencia de desenvolvimento da extensao)

### O que funciona bem

- TypeScript 5.4 com Vite 5 + CRXJS 2.0-beta — setup moderno, HMR funciona no dev mode
- `proto/types.ts` e o lugar certo para os tipos compartilhados — boa intencao arquitetural
- `service-worker.ts` e organizado: separacao clara entre captura (`page_viewed`), popup (`popup_status`, `popup_forget`) e omnibox
- Omnibox `@recall` implementado com sugestoes ricas (url + snippet) (`service-worker.ts:95-113`)
- Denylist separada em `src/lib/denylist.ts` — facil de customizar
- Popup funcional em React com pause/forget/status em ~230 linhas, sem biblioteca de UI pesada

### O que nao funciona / esta ausente

- **Tipos duplicados em `daemon-client.ts`** (`daemon-client.ts:3-28`): `IngestRequest`, `SearchResult`, `StatusResponse`, `ForgetRequest` sao redefinidos localmente — viola regra explicita do CLAUDE.md ("Nunca duplicar tipos do proto/types.ts")
- **Bug de retorno do search** (`daemon-client.ts:67`): `res.json() as Promise<SearchResult[]>` mas o endpoint retorna `{results: SearchResult[]}` — busca via daemon-client retorna objeto, nao array
- **Pause nao implementado**: `App.tsx:153` tem botao Pause que chama `setPaused(p => !p)` mas nao envia mensagem ao service worker — o estado e local ao popup e perdido ao fechar; nenhuma logica de pausa existe no SW
- **"Open full UI" com porta hardcoded** (`App.tsx:181`): `const port = 7700 // fallback` — na instalacao nativa a porta e aleatoria; o link vai abrir pagina errada se porta diferir de 7700
- **CRXJS 2.0-beta**: dependencia em versao beta pode ter breaking changes — risco para manutencao a longo prazo
- **Sem testes**: nenhum arquivo de teste encontrado na extensao

### Scores — Projeto 5

| Dimensao | v1 | Comentario |
|---|---|---|
| Onboarding | 4 | `npm install && npm run dev` funciona; estrutura clara |
| API Ergonomics | 3 | proto/types.ts e boa ideia mas nao e seguido no daemon-client |
| Feature Completeness | 2 | Pause nao funciona; bug no search client; link UI quebrado |
| Security Confidence | 4 | incognito:not_allowed, denylist, sem storage do token |
| Language Quality | 2 | Tipos duplicados, bug de retorno, feature nao implementada |
| Operational Readiness | 2 | Sem testes, CRXJS beta, bugs em features visiveis |
| Documentation | 3 | Guia usuario bom; sem docs de desenvolvimento da extensao |

---

## Findings

### P0 — Blockers

**P0-1: Busca vetorial nao funciona (StubEmbedder)**
`embedder.go:21` retorna `make([]float32, 384)` — vetor zero para todo texto. Cosseno entre zeros e zero. A feature central do produto (busca semantica) e efetivamente BM25-only. Qualquer avaliacao da "busca hibrida" esta avaliando apenas FTS5.
_Impacto: Projeto 4 (SmartSearch) inteiramente comprometido._

**P0-2: CORS bloqueia integracao externa**
`routes.go:254-257`: `corsMiddleware` so define `Access-Control-Allow-Origin` para origens `chrome-extension://`. Qualquer cliente web externo (Next.js, dashboard) recebe resposta sem header CORS — browser bloqueia. Nao ha como integrar sem modificar o daemon.
_Impacto: Projeto 2 (APIIntegration) bloqueado._

**P0-3: Bug no daemon-client search**
`daemon-client.ts:67`: `res.json() as Promise<SearchResult[]>` — o endpoint retorna `{results: [...]}` mas o client faz cast direto para array. Quem chama `search()` recebe um objeto com campo `results`, nao o array esperado. Busca via omnibox pode retornar resultados vazios dependendo do consumidor.
_Impacto: Projeto 5 (ExtensionDX), Projeto 4 (SmartSearch)._

### P1 — Importantes

**P1-1: Pause nao implementado na extensao**
`App.tsx:153-155`: botao Pause e visual apenas — `setPaused` e estado local do componente React, nao persiste, nao envia mensagem ao SW. Usuario clica em Pause e acha que parou a captura mas nao parou.

**P1-2: Porta hardcoded no popup**
`App.tsx:181`: `const port = 7700` usado para abrir `/ui` — na instalacao nativa a porta e aleatoria. Link quebra em qualquer instalacao nativa padrao.

**P1-3: FTS rebuild sincrono dentro de ingest**
`sqlite.go:198`: `INSERT INTO chunks_fts(chunks_fts) VALUES('rebuild')` dentro da transacao de ingest — viola regra do proprio CLAUDE.md e degrada latencia de ingest com volume crescente.

**P1-4: Sem sincronizacao / uso em equipe**
Zero infraestrutura para compartilhar indices entre membros do time. Cada daemon e uma ilha. Roadmap promete v0.3 mas sem previsao.

**P1-5: N+1 queries no build de resultados de busca**
`sqlite.go:315-326`: ate 40 queries SQLite sequenciais para retornar 20 resultados. Deve ser refatorado para JOIN.

**P1-6: Tipos duplicados em daemon-client.ts**
`daemon-client.ts:3-28` redefine 4 tipos que existem em `proto/types.ts` — viola contrato arquitetural e cria risco de divergencia silenciosa.

### P2 — Nice-to-have

**P2-1: SearchResponse.total ausente na resposta Go**
`proto/types.ts:31` declara `total: number` mas `routes.go` nao o retorna — contrato quebrado.

**P2-2: Brute-force O(n) no dense search**
`sqlite.go:263`: carrega todos os chunks para calcular cosseno — nao escala. Solucao futura: sqlite-vec ou FAISS.

**P2-3: UI embutida com token hardcoded no HTML**
`routes.go:54`: `headers: {'Authorization': 'Bearer REPLACE_TOKEN'}` — UI local nao funciona sem substituicao manual do token.

**P2-4: Sem testes automatizados (daemon e extensao)**
Nenhum arquivo `*_test.go` relevante ou `*.test.ts` encontrado nos paths principais.

**P2-5: CRXJS 2.0-beta como dependencia de build**
Versao beta pode ter breaking changes — considerar estabilidade para producao.

---

## Conclusao

Vector Bookmark e um POC tecnicamente interessante com uma arquitetura local-first bem pensada para uso individual. O schema SQLite, a estrutura de chunking, o protocolo Native Messaging e o graceful shutdown demonstram cuidado de engenharia.

Para o cenario da Camila — time de 3 pesquisadores que precisa de memoria semantica compartilhada — o projeto tem **dois blockers fundamentais**:

1. **A busca semantica nao existe em v0.1**: o `StubEmbedder` retorna vetores zero. O que o usuario experimenta e busca por keyword (BM25), nao por conceito. A feature que justifica o nome "Vector Bookmark" esta em TODO.

2. **Nao ha uso em equipe**: cada membro do time tem seu proprio silos de dados. Nao ha mecanismo de sincronizacao, compartilhamento ou servidor central. O roadmap cita v0.3 mas sem codigo e sem data.

Alem disso, ha um bug visivel (Pause nao funciona, link /ui com porta errada) e um bug menos visivel mas serio no cliente de busca TypeScript que pode fazer a extensao retornar silenciosamente resultados incorretos.

**Recomendacao para o time:** Nao adotar em producao agora. Vale monitorar o projeto ate que o embedder real seja implementado (P0-1) e alguma forma de compartilhamento seja disponibilizada (P1-4). Para uso individual imediato de um desenvolvedor que queira experimentar BM25 local, o onboarding e aceitavel.

---

## Score Medio

| Dimensao | P1 ResearchSync | P2 APIIntegration | P3 RealTimeMonitor | P4 SmartSearch | P5 ExtensionDX | Media |
|---|---|---|---|---|---|---|
| Onboarding | 3 | 2 | 3 | 3 | 4 | **3.0** |
| API Ergonomics | 3 | 3 | 3 | 4 | 3 | **3.2** |
| Feature Completeness | 1 | 2 | 2 | 2 | 2 | **1.8** |
| Security Confidence | 4 | 3 | 4 | 4 | 4 | **3.8** |
| Language Quality | 4 | 3 | 3 | 3 | 2 | **3.0** |
| Operational Readiness | 3 | 2 | 2 | 2 | 2 | **2.2** |
| Documentation | 3 | 2 | 2 | 2 | 3 | **2.4** |
| **Media do Projeto** | **3.0** | **2.4** | **2.7** | **2.9** | **2.9** | **2.8** |

---

Score v1: 2.8 / 5
