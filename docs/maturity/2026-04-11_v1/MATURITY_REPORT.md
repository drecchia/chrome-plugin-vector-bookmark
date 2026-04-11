# Vector Bookmark — Relatorio de Maturidade v1

**Versao avaliada:** v0.1.0 (commit inicial — sem versionamento git na raiz)
**Data:** 2026-04-11
**Avaliadores:** 4 personas independentes, 20 projetos simulados
**Avaliacao anterior:** nenhuma (esta e a avaliacao inicial)

---

## 1. Sumario Executivo

Vector Bookmark e uma extensao Chrome MV3 + daemon local Go para indexacao semantica passiva do historico de navegacao. A avaliacao inicial abrangeu 20 projetos simulados por 4 personas cobrindo uso individual, startup, compliance enterprise e operacao de plataforma.

**Resultado:** o projeto demonstra arquitetura local-first bem pensada e solida base de privacidade, mas possui **quatro blockers tecnicos criticos** que impedem a entrega da proposta de valor central em v0.1:

1. A busca semantica nao existe — `StubEmbedder` retorna vetores zero (o produto e BM25-only)
2. Um bug de tipo no cliente TypeScript quebra o omnibox silenciosamente
3. FTS5 rebuild sincrono em cada ingest degrada com volume crescente (viola regra propria do projeto)
4. A fila de ingestao nao e drenada no shutdown — perda de dados garantida a cada restart

**Score composto: 2.6 / 5 — Nivel: Alpha**

---

## 2. Matriz de Scores

### Por Persona

| Persona | Score v1 |
|---|---|
| Lucas Ferreira (Junior/Solo Dev) | 2.7 |
| Camila Santos (Startup Dev) | 2.8 |
| Fernando Oliveira (Enterprise Architect) | 2.4 |
| Renata Costa (Platform/DevOps) | 2.5 |
| **Media** | **2.6** |

### Por Dimensao (media das 4 personas)

| Dimensao | Lucas | Camila | Fernando | Renata | **Media v1** |
|---|---|---|---|---|---|
| Onboarding | 2.8 | 3.0 | 2.4 | 3.4 | **2.9** |
| API Ergonomics | 1.8 | 3.2 | 3.2 | 3.2 | **2.9** |
| Feature Completeness | 2.0 | 1.8 | 1.8 | 2.0 | **1.9** |
| Security Confidence | 4.0 | 3.8 | 2.2 | 3.0 | **3.3** |
| Language Quality | 2.8 | 3.0 | 3.8 | 2.6 | **3.1** |
| Operational Readiness | 2.4 | 2.2 | 1.8 | 1.6 | **2.0** |
| Documentation | 3.0 | 2.4 | 1.6 | 1.6 | **2.2** |
| **Score medio da persona** | **2.7** | **2.8** | **2.4** | **2.5** | **2.6** |

---

## 3. Nivel de Maturidade

### Alpha

**Justificativa:**
- A feature central do produto (busca semantica vetorial) nao e entregue — `StubEmbedder` retorna `make([]float32, 384)` (embedder.go L22), cosine similarity e sempre 0
- Um bug critico de tipo (`daemon-client.ts` L67) quebra o omnibox silenciosamente — o componente mais visivel da extensao nao funciona
- FTS5 rebuild sincrono dentro da transacao de ingest (`sqlite.go` L198) viola a regra documentada no proprio `CLAUDE.md` e degrada linearmente com o volume
- A fila de ingestao Go nao e drenada no shutdown (`queue.go` L31, `server.go` L74) — perda de dados garantida a cada restart do daemon

O projeto tem happy path parcialmente funcional (captura passiva + BM25 via curl direto), mas os caminhos principais da UI estao quebrados ou incompletos.

**O que falta para Beta:**
1. Corrigir o bug de tipo em `daemon-client.ts:67`
2. Substituir `rebuild` completo por `INSERT INTO chunks_fts(rowid, text) VALUES(?, ?)` incremental no ingest
3. Implementar drain da fila no shutdown (metodo `Close()` + `WaitGroup` no `queue.go`)
4. Implementar Pause funcional no popup (mensagem ao service worker)
5. Corrigir a porta hardcoded no link "Open full UI" (`App.tsx` L181)
6. Substituir `VBM_PORT=0.0.0.0` por logica que garanta bind apenas em `127.0.0.1`

---

## 4. Findings Remanescentes

### P0 — Blockers (7 total)

| # | Finding | Arquivo | Linha | Personas |
|---|---|---|---|---|
| P0-01 | `daemon-client.ts` faz cast de `{results:[]}` como `SearchResult[]` — omnibox quebrado silenciosamente | `daemon-client.ts` | 67 | Todas |
| P0-02 | `StubEmbedder` retorna vetores zero — busca semantica nao existe, RRF e BM25-only | `embedder.go` | 22 | Todas |
| P0-03 | FTS5 `rebuild` sincrono dentro da transacao de ingest — throughput O(N) sobre todos os chunks | `sqlite.go` | 198 | Todas |
| P0-04 | Fila Go nao drenada no shutdown — perda de dados garantida a cada restart do daemon | `queue.go` + `server.go` | 31 / 74 | Renata |
| P0-05 | Busca densa carrega TODOS os chunks em memoria (`SELECT ... FROM chunks` sem WHERE) — O(N) inviavel com >10k paginas | `sqlite.go` | 263 | Renata, Camila |
| P0-06 | `VBM_PORT` env var faz bind em `0.0.0.0` — dados de navegacao expostos na rede local | `server.go` | 47-49 | Fernando, Renata |
| P0-07 | Sem politica de retencao automatica — LGPD Art. 15 nao atendido para uso corporativo | `sqlite.go` | ausencia | Fernando |

### P1 — Importantes (15 total)

| # | Finding | Arquivo |
|---|---|---|
| P1-01 | Pause no popup e visual apenas — estado local React, nao enviado ao service worker | `App.tsx` L153 |
| P1-02 | Porta hardcoded `7700` no link "Open full UI" — quebrado em toda instalacao nativa | `App.tsx` L181 |
| P1-03 | `Forget` nao limpa tabela `queue` — dados persistem apos exclusao do usuario | `sqlite.go` L355-381 |
| P1-04 | `/healthz` retorna `{"ok":true}` mesmo com banco inacessivel — falso-positivo | `routes.go` L71-74 |
| P1-05 | Schema sem versionamento — migracao para v0.2 requer intervencao manual | `sqlite.go` L131-134 |
| P1-06 | N+1 queries no build de resultados de busca — ate 40 queries por request de busca | `sqlite.go` L316-327 |
| P1-07 | CORS hardcoded para `chrome-extension://` — bloqueia consumidores externos (dashboard, API) | `routes.go` L254-257 |
| P1-08 | Tipos duplicados em `daemon-client.ts` — viola contrato arquitetural com `proto/types.ts` | `daemon-client.ts` L3-28 |
| P1-09 | Sem bancos brasileiros na denylist — Itau, Bradesco, Nubank, BB, CEF ausentes | `denylist.ts` |
| P1-10 | WebSocket sem ping/pong e sem write deadline — conexoes zumbi acumulam | `routes.go` L180-208 |
| P1-11 | `vbmd.service` sem `PrivateNetwork=true` — hardening de OS nao aplicado | `vbmd.service` |
| P1-12 | `os.UserHomeDir()` com erro descartado em `SessionPath()` — falha silenciosa sem HOME | `host.go` L33 |
| P1-13 | Logs nao estruturados — `log.Printf` impossibilita filtragem por campo em producao | varios |
| P1-14 | NM manifest aceita `REPLACE_ME` como Extension ID — abre acesso a qualquer extensao | `install.sh` L39 |
| P1-15 | Sem endpoint `/export` — portabilidade LGPD Art. 18 nao atendida | `routes.go` (ausencia) |

### P2 — Nice-to-have (10 selecionados)

| # | Finding |
|---|---|
| P2-01 | `REPLACE_TOKEN` literal no HTML da UI embutida (`routes.go` L54) |
| P2-02 | `queue` table nunca limpa apos processamento — segunda copia de todas as URLs |
| P2-03 | `db.SetMaxOpenConns(1)` ausente — explicitar que SQLite e single-writer |
| P2-04 | Sem debounce no omnibox — cada keystroke dispara fetch |
| P2-05 | Snippet de 200 chars insuficiente para docs tecnicas |
| P2-06 | `install.sh` sem verificacao de pre-requisitos (go, node, systemctl) |
| P2-07 | `After=network.target` desnecessario no unit file — atrasa start |
| P2-08 | Sem endpoint `/metrics` Prometheus |
| P2-09 | Token do SW cacheado sem expiracao/revalidacao apos restart do daemon |
| P2-10 | URL completa com query string indexada sem sanitizacao (tokens, session IDs) |

---

## 5. Pontos Fortes

1. **Arquitetura local-first verificada**: nenhuma requisicao externa detectada. `StubEmbedder` sem rede, extensao so faz fetch para `127.0.0.1` (`native-bridge.ts` L53), bind padrao `127.0.0.1:0` (`server.go` L46).

2. **Modelo de autenticacao solido**: token UUID v4 rotacionado a cada restart (`server.go` L41), armazenado com `chmod 600` (`host.go` L55), transmitido via Native Messaging (IPC autenticado pelo Chrome), nunca em `chrome.storage`.

3. **Privacy-by-default funcional**: `incognito:not_allowed` no manifest, denylist de 24 dominios + `.gov`/`.mil` + 14 padroes checada antes de qualquer ingest (`service-worker.ts` L40), deteccao de campos sensiveis com cancelamento de captura (`extract.ts` L25-29).

4. **Forget com CASCADE implementado**: tres modalidades (url, domain, timerange) com `ON DELETE CASCADE` para chunks (`sqlite.go` L355-381).

5. **Graceful shutdown do HTTP server**: `signal.NotifyContext` + `http.Server.Shutdown` com timeout de 5s (`server.go` L68-77). Session cleanup no exit (`server.go` L60).

6. **Codigo Go idiomatico**: erros propagados com `fmt.Errorf("contexto: %w", err)`, sem `log.Fatal` em packages internos, bind explicito. Language Quality media de 3.1/5 — a dimensao mais alta.

7. **Onboarding em pt-BR**: `GUIA.md` com secoes de troubleshooting, comandos copy-paste, dois caminhos (nativo + Docker), media de 2.9/5.

---

## 6. Consenso entre Personas

### Todas concordaram (positivo)
- A arquitetura local-first e privacidade basica (token, chmod 600, bind loopback, denylist, incognito) sao o ponto mais solido do projeto
- O codigo Go e de qualidade razoavel para um POC — erros propagados, estrutura de packages correta
- O GUIA.md em pt-BR esta acima da media para projetos solo
- A interface `Embedder` com `Version()` e uma decisao arquitetural correta que vai facilitar a migracao futura

### Todas concordaram (negativo)
- A busca semantica nao existe (StubEmbedder) — a proposta de valor central nao e entregue em v0.1
- O bug em `daemon-client.ts:67` (mismatch de tipo no search) quebra o omnibox silenciosamente
- FTS5 rebuild sincrono viola a propria regra documentada e vai causar problemas com volume
- O botao Pause no popup e cosmético — nao funciona
- A porta hardcoded `7700` no popup vai enganar usuarios da instalacao nativa

---

## 7. Recomendacoes por Perfil

| Perfil | Recomendacao | Condicoes |
|---|---|---|
| Junior/Solo (Lucas) | **Adotar com ressalvas** | Corrigir P0-01 (bug TS) para ter omnibox funcional. Aceitar que busca e BM25-only ate embedder real. Cenario de texto (artigos, docs) funciona bem. |
| Startup Dev (Camila) | **Nao adotar ainda** | Sem multi-usuario, sem sincronizacao, CORS bloqueia dashboard externo, busca semantica ausente. Reavaliar apos v0.2. |
| Enterprise Architect (Fernando) | **Reprovado** | 2 P0 de compliance (VBM_PORT + retencao), 8+ P1 de LGPD/SOC2. Requer roadmap de compliance antes de qualquer piloto corporativo. |
| Platform/DevOps (Renata) | **Nao pronto para producao** | Perda de dados no shutdown, escala de busca O(N), ingest degradado por FTS5 rebuild. Adequado apenas para uso pessoal com < 500 paginas. |

---

## 8. Roadmap para Proximo Nivel (Alpha → Beta)

Ordenado por impacto/esforco:

**Sprint 1 — Correcoes criticas (bugs que nao deveriam ter chegado em v0.1)**
1. `daemon-client.ts:67` — corrigir retorno de `search()` para extrair `.results` do objeto wrapper
2. `App.tsx:153` — implementar Pause via `chrome.runtime.sendMessage` ao service worker
3. `App.tsx:181` — usar porta do daemonState em vez de hardcode 7700
4. `server.go:47-49` — garantir que `VBM_PORT` aceite apenas `host:port` com bind em `127.0.0.1` explicitamente

**Sprint 2 — Integridade operacional**
5. `queue.go` — adicionar metodo `Close()` + `WaitGroup` para drain gracioso da fila no shutdown
6. `sqlite.go:198` — substituir `rebuild` completo por `INSERT INTO chunks_fts` incremental no ingest
7. `sqlite.go:316-327` — refatorar N+1 queries em JOIN unico
8. `routes.go:71-74` — `/healthz` deve verificar conectividade do banco

**Sprint 3 — Feature completeness basica**
9. Implementar embedder real (onnxruntime-go local ou API externa configuravel)
10. Adicionar endpoint `GET /pages` para listagem do historico indexado
11. Schema versionamento — tabela `schema_version` + migracoes numeradas
12. Corrigir `Forget` para incluir tabela `queue`

**Sprint 4 — Compliance e privacidade**
13. TTL configuravel com cleanup automatico (ex: 90 dias)
14. Endpoint `/export` para portabilidade LGPD
15. Adicionar bancos brasileiros na denylist
16. `vbmd.service` — adicionar `PrivateNetwork=true`

---

## 9. Veredicto Final

**Score composto v1: 2.6 / 5**
**Nivel: Alpha**
**Production-ready:** Nao

O Vector Bookmark e um POC local-first com base arquitetural correta e consciencia de privacidade demonstrada. Para uso pessoal de texto (artigos, documentacao) com volume baixo e expectativas calibradas (BM25-only, sem omnibox), e utilizavel apos a correcao do P0-01.

A trajetoria para Beta e clara e factivel: quatro bugs criticos (P0-01 a P0-04) sao correcoes de codigo, nao redesign arquitetural. O P0-02 (embedder real) e o unico item que requer pesquisa de integracao (ONNX local) e representa a transicao de "search engine pessoal com BM25" para "memoria semantica" — a proposta central do produto.

---

*Relatorio gerado por avaliacao simulada em 2026-04-11 | 4 personas | 20 projetos | Vector Bookmark v0.1.0*
