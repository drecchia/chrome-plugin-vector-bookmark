# Avaliacao Inicial v1: Renata Costa — Platform/DevOps Engineer

**Data:** 2026-04-11
**Versao:** v1
**Avaliadora:** Renata Costa — Engenheira de Plataforma (simulada), 7 anos de experiencia
**Foco:** Operabilidade, graceful shutdown, observabilidade, comportamento sob carga, integridade operacional

---

## Perfil

Renata atua em times de plataforma responsaveis por operar servicos de infraestrutura em producao. Sua lente e operacional: ela avalia se um servico pode ser implantado, monitorado, degradado graciosamente e recuperado de falhas sem intervencao manual. Ela diseca logs, unit files, mecanismos de shutdown e comportamento de banco antes de recomendar qualquer ferramenta para seu time.

---

## Projeto 1 — SystemdReliability

### O que funciona

- **Graceful shutdown implementado** (`server.go` L68-77): `signal.NotifyContext` captura `SIGTERM`/`SIGINT` corretamente. `http.Server.Shutdown` e chamado com timeout de 5 segundos.
- **Bind correto por padrao** (`server.go` L46): `127.0.0.1:0` — porta aleatoria no loopback. O daemon nao expoe superficie de rede desnecessaria.
- **Restart policy no unit file** (`vbmd.service` L9-10): `Restart=on-failure` com `RestartSec=3s`. Quedas nao intencionais sao recuperadas automaticamente.
- **Logs direcionados ao journald** (`vbmd.service` L13-14): `StandardOutput=journal` e `StandardError=journal`. `journalctl --user -u vbmd` funciona.
- **Session cleanup no shutdown** (`server.go` L60): `defer os.Remove(nm.SessionPath())` — o `session.json` e removido ao encerrar, evitando que a extensao tente conectar em porta fantasma.

### Gaps e riscos

- **A fila em andamento e descartada no shutdown.** `queue.go` L31: o worker goroutine faz `for req := range q.ch` — quando `srv.Shutdown` retorna, o processo termina. O canal nao e drenado antes do exit. Itens enfileirados mas nao processados sao perdidos silenciosamente. Nao ha `q.Close()` ou `q.Wait()` no fluxo de shutdown (`server.go` nao referencia metodo algum do Queue alem de `Enqueue`).
- **Sem `WaitGroup` ou mecanismo de drain na Queue** (`queue.go`): `Queue` nao expoe metodo `Close()` nem `Drain()`. A goroutine worker e uma goroutine "fire-and-forget" sem sinalizacao de termino.
- **Timeout de shutdown fixo em 5s** (`server.go` L74): se um ingest pesado estiver em curso (FTS5 rebuild pode levar segundos em bases grandes), o `shutCtx` expira e o DB e fechado por `defer s.Close()` enquanto a transacao ainda esta aberta — corrupcao improvavel (WAL protege) mas comportamento indefinido.
- **`VBM_PORT=...` liga em `0.0.0.0`** (`server.go` L48): comentario diz "intended for Docker" mas nao ha Dockerfile no projeto. Qualquer processo local pode chamar o daemon sem o token se souber a porta. Risco de seguranca para uso nativo inadvertido.
- **Sem `KillMode=mixed` no unit file**: systemd padrao e `control-group`, o que mata o processo inteiro apos `TimeoutStopSec` sem aguardar drain da fila.
- **`After=network.target` desnecessario**: o daemon nao usa rede externa; a diretiva cria dependencia que pode atrasar o start em sistemas com interfaces lentas.

### Tabela de scores — Projeto 1

| Dimensao | v1 | Comentario |
|---|---|---|
| Onboarding | 4 | install.sh funcional, systemd configurado automaticamente |
| API Ergonomics | 3 | N/A direto; unit file minimo mas funcional |
| Feature Completeness | 2 | Shutdown parcial: HTTP drena mas fila nao e drenada |
| Security Confidence | 3 | Bind loopback correto; VBM_PORT cria risco latente |
| Language Quality | 3 | signal.NotifyContext idiomatico; falta drain da goroutine worker |
| Operational Readiness | 2 | Perda de dados em shutdown e blocker operacional |
| Documentation | 2 | Nenhum runbook de shutdown/restart; comportamento da fila nao documentado |

---

## Projeto 2 — PerformanceUnderLoad

### O que funciona

- **Queue com backpressure** (`queue.go` L23-28): `select { case q.ch <- req: default: log(...) }` — nunca bloqueia o caller HTTP. Drop explicito e logado.
- **Limite de resultados de busca** (`store.go` L213-217): hard cap de 20 resultados. FTS5 limitado a 50 candidatos.
- **Chunking com overlap controlado** (`chunk.go` L55): janela 512, overlap 64, minimo 40 tokens — volumes de chunks previstos por pagina.
- **Dedup de chunks** (`store.go` L188): `INSERT OR IGNORE` baseado em `(page_id, chunk_idx)` — revisita a mesma URL nao duplica chunks.

### Gaps e riscos

- **FTS5 rebuild sincrono a cada ingest** (`store.go` L198): `INSERT INTO chunks_fts(chunks_fts) VALUES('rebuild')` dentro da transacao de ingest. Com 10.000 paginas e media de 5 chunks cada (50.000 registros), o rebuild completo da tabela virtual pode levar centenas de milissegundos por ingest. A fila (cap 256) vai saturar rapidamente sob ingestao continua.
- **Busca densa carrega TODOS os chunks em memoria** (`store.go` L263): `SELECT id, text, page_id, embedding FROM chunks` sem WHERE. Com 50.000 chunks de 384 floats cada (1.536 bytes por chunk), uma busca carrega ~76 MB de BLOBs na heap Go em cada query de busca. Com 100.000 chunks, sao ~152 MB. Nao ha cache, nao ha indice ANN.
- **N+1 query no build de resultados** (`store.go` L316-327): para cada um dos `limit` resultados (ate 20), executa 2 queries adicionais (`SELECT text... FROM chunks` e `SELECT url... FROM pages`). Total: ate 41 queries por busca.
- **Sem connection pool configurado**: `sql.Open` com configuracoes padrao — `MaxOpenConns` ilimitado, `MaxIdleConns=2`. Escritas concorrentes em SQLite (single-writer) vao serializar com lock contention.
- **Worker de fila single-threaded** (`queue.go` L31): apenas uma goroutine consome a fila. Se um ingest leva 500ms (FTS5 rebuild lento), throughput maximo e 2 ingests/segundo. A fila de 256 esgota em ~2 minutos de navegacao ativa.
- **StubEmbedder retorna zeros** (`embedder.go` L22): cosine similarity entre vetores zero e 0 para todos os chunks. Dense search e completamente inutil; RRF degrada para BM25 puro, mas o codigo ainda carrega todos os BLOBs de 384*4 bytes para calcular similarity = 0.

### Tabela de scores — Projeto 2

| Dimensao | v1 | Comentario |
|---|---|---|
| Onboarding | 3 | Funciona com volumes pequenos; degrada sem aviso |
| API Ergonomics | 3 | Backpressure correto; sem metricas de latencia |
| Feature Completeness | 1 | FTS5 rebuild e full-scan sao incompativeis com escala |
| Security Confidence | 3 | Sem impacto direto de seguranca |
| Language Quality | 2 | Codigo correto mas algoritmos nao escalaveis por design |
| Operational Readiness | 1 | Sem metricas, sem limites de memoria, sem observabilidade de latencia |
| Documentation | 1 | Limites de escala nao documentados; nenhum aviso sobre degradacao |

---

## Projeto 3 — Observability

### O que funciona

- **`/healthz` endpoint** (`routes.go` L71-74): responde `{"ok":true}` sem autenticacao. Compativel com health checks de systemd/load balancers.
- **`/status` endpoint** (`routes.go` L161-178): retorna `indexed`, `pending`, `version` — informacao util para monitoramento basico.
- **WebSocket `/ws`** (`routes.go` L180-208): push de status a cada 5 segundos com `indexed` e `pending`. Util para o popup da extensao.
- **`middleware.Logger`** (`routes.go` L68): chi logger loga todas as requisicoes HTTP automaticamente.
- **`middleware.Recoverer`** (`routes.go` L69): panics sao capturados e retornam 500 em vez de derrubar o processo.
- **Logs via `log.Printf`** (`server.go` L62, `queue.go` L27, L34): mensagens de startup, drop de fila e erros de ingest sao logados.

### Gaps e riscos

- **Logs plaintext, nao estruturados**: `log.Printf("[vbmd] ...")` produz linhas de texto livre. Impossivel filtrar por campo (URL, page_id, erro) com `journalctl` ou agregadores como Loki/Datadog. Nenhuma biblioteca de structured logging (slog, zap, zerolog).
- **`/healthz` nao verifica o banco**: retorna `{"ok":true}` mesmo se o SQLite estiver corrompido ou inacessivel. Um health check falso-positivo e pior que nenhum health check.
- **Sem endpoint `/metrics`** (Prometheus): nenhuma metrica de latencia de ingest, tamanho da fila, numero de drops, duracao de busca. Impossivel configurar alertas.
- **WebSocket sem ping/pong**: `routes.go` L180-208 — o loop do ticker nao implementa write deadline nem ping/pong. Conexoes zumbi ficam abertas indefinidamente.
- **`/status` nao inclui metrica de fila real**: conta `queue.pending` na tabela SQLite (`store.go` L389), mas o worker usa canal Go — itens no canal nao aparecem no contador. O numero reportado pode ser zero mesmo com 256 itens pendentes no buffer.
- **Nenhuma correlacao de requests**: sem request ID, impossivel rastrear um ingest especifico do log HTTP ate o erro de ingest na fila.
- **Erros de ingest logados sem contexto suficiente**: `queue.go` L34 loga URL e erro, mas nao chunk index, page_id ou tipo de erro — dificil diagnosticar falhas repetidas.

### Tabela de scores — Projeto 3

| Dimensao | v1 | Comentario |
|---|---|---|
| Onboarding | 3 | /healthz e /status existem; faceis de usar |
| API Ergonomics | 3 | Endpoints de status uteis; WebSocket funcional |
| Feature Completeness | 2 | Falta Prometheus, structured logging, request tracing |
| Security Confidence | 3 | Healthz sem auth e correto |
| Language Quality | 2 | log.Printf e stdlib basico; sem slog/zerolog |
| Operational Readiness | 1 | Healthz falso-positivo, sem metricas, logs nao filtraveis |
| Documentation | 1 | Nenhum runbook operacional; o que fazer quando algo quebra? |

---

## Projeto 4 — NativeMessagingReliability

### O que funciona

- **Protocolo NM correto** (`host.go` L86-98): leitura com `binary.Read` (little-endian uint32) + `io.ReadFull` — implementacao fiel ao protocolo Chrome NM.
- **Protecao contra mensagens gigantes** (`host.go` L91): rejeita mensagens >1MB. Previne alocacao de memoria maliciosa.
- **Separacao clara de responsabilidades**: nm-host so le `session.json` e responde — nenhuma logica de negocio, conforme a regra documentada.
- **Erro gracioso se daemon nao esta rodando** (`host.go` L74-77): retorna `handshake_error` com mensagem legivel em vez de crash.
- **Session file com chmod 600** (`host.go` L55): `os.WriteFile(path, data, 0600)` — token nao e legivel por outros usuarios.
- **Token rotacionado a cada restart** (`server.go` L41): `uuid.New()` — sessoes antigas sao invalidadas automaticamente.

### Gaps e riscos

- **`session.json` contem o token em plaintext** (`host.go` L79-83): o nm-host le e envia o token para a extensao via stdout. O token e transmitido por NM (pipe de processo) — seguro em si, mas qualquer processo rodando como o mesmo usuario pode ler `~/.local/share/vbm/session.json` diretamente (chmod 600 so protege de outros usuarios, nao do mesmo UID).
- **`search()` em `daemon-client.ts` retorna tipo incorreto** (`daemon-client.ts` L67): `res.json() as Promise<SearchResult[]>` — a API retorna `{"results": [...]}` (`routes.go` L130-144), nao um array direto. O cast vai retornar o objeto wrapper, nao o array. Bug silencioso de tipo.
- **`connectDaemon()` nao e analisada**: a implementacao esta em `native-bridge.ts` (nao lido), mas todos os clientes chamam `await connectDaemon()` antes de cada fetch — se o daemon reiniciar com nova porta, a reconexao depende inteiramente da logica desse arquivo.
- **Sem retry no nm-host**: `RunHost` le uma mensagem, responde e sai. Se a leitura falhar (pipe fechado prematuramente pelo Chrome), o erro e retornado mas a extensao pode nao receber resposta — comportamento depende do Chrome.
- **`os.UserHomeDir()` ignora erro** (`host.go` L33 — `SessionPath()`): `home, _ := os.UserHomeDir()` — erro descartado. Em ambientes sem HOME definido, `SessionPath()` retorna caminho incorreto silenciosamente.
- **Nenhum timeout de leitura no nm-host**: `readMessage` usa `io.ReadFull` sem deadline. Se o Chrome enviar o header de tamanho mas nao o payload, o processo fica bloqueado indefinidamente.

### Tabela de scores — Projeto 4

| Dimensao | v1 | Comentario |
|---|---|---|
| Onboarding | 3 | install.sh configura NM manifest automaticamente |
| API Ergonomics | 4 | Protocolo NM implementado corretamente; handshake claro |
| Feature Completeness | 3 | Cobre o fluxo principal; falta reconexao robusta |
| Security Confidence | 3 | chmod 600 correto; token em plaintext e limitacao do design |
| Language Quality | 3 | Go correto com erro ignorado em SessionPath (L33) |
| Operational Readiness | 2 | Sem timeout de leitura; bug de tipo no client TS |
| Documentation | 2 | Fluxo NM descrito no CLAUDE.md mas sem troubleshooting |

---

## Projeto 5 — DatabaseIntegrity

### O que funciona

- **WAL mode habilitado** (`store.go` L116): `PRAGMA journal_mode=WAL` — leituras concorrentes nao bloqueiam escritas. Crash-safe para operacoes de escrita.
- **Foreign keys ON** (`store.go` L120): `PRAGMA foreign_keys=ON` — `DELETE CASCADE` de pages para chunks funciona corretamente.
- **Upsert atomico de paginas** (`store.go` L158-167): `INSERT ... ON CONFLICT(url_hash) DO UPDATE` dentro de transacao — revisitas sao idempotentes.
- **`defer tx.Rollback()`** (`store.go` L155): padrao correto Go — se qualquer operacao falhar antes do `Commit()`, a transacao e revertida automaticamente.
- **Dedup de chunks** (`store.go` L188): `INSERT OR IGNORE` por `(page_id, chunk_idx)` — chunks identicos nao sao duplicados.
- **`dataDir` criado com 0700** (`store.go` L108): diretorio do banco restrito ao usuario proprietario.

### Gaps e riscos

- **FTS5 rebuild dentro da transacao de ingest** (`store.go` L198): `INSERT INTO chunks_fts(chunks_fts) VALUES('rebuild')` e executado dentro da mesma transacao que insere os chunks. O rebuild FTS5 nao e transacional da mesma forma que tabelas normais — se o processo morrer entre o rebuild e o `Commit()`, o FTS pode ficar dessincronizado com a tabela `chunks`.
- **`migrate()` nao e versionada** (`store.go` L131-134): apenas executa `CREATE TABLE IF NOT EXISTS`. Nao ha tabela de versao de schema, nao ha migracao incremental. Alterar o schema em v0.2 exige script manual ou wipe do banco.
- **Sem `SetMaxOpenConns(1)`**: SQLite e single-writer. Com o pool padrao Go (`MaxOpenConns=0` = ilimitado), escritas concorrentes vao resultar em `SQLITE_BUSY`. O worker de fila e single-goroutine agora, mas se isso mudar, ha risco de lock contention.
- **`LastInsertId` workaround fragil** (`store.go` L172-179): `ON CONFLICT DO UPDATE` nao retorna `LastInsertId` no modernc/sqlite — o codigo faz fallback para `SELECT id FROM pages WHERE url_hash = ?`. Correto mas cria uma query extra dentro da transacao.
- **Sem `PRAGMA synchronous`**: valor padrao no WAL mode e `NORMAL` — aceitavel, mas nao documentado como escolha explicita.
- **FTS5 pode ficar fora de sincronia**: como o FTS usa `content='chunks'` (external content table), delecoes via `Forget()` exigem `rebuild` manual (`store.go` L379). Se o processo morrer durante `Forget()` apos o DELETE mas antes do rebuild, o FTS continuara retornando resultados de chunks deletados.
- **Sem indice em `chunks.page_id`**: todas as queries de busca fazem join entre `chunks_fts` e `chunks` por rowid (eficiente), mas `SELECT text, page_id FROM chunks WHERE id = ?` (L317) e N+1 sem aproveitar indice — a PK ja cobre isso, mas `SELECT ... FROM pages WHERE id = ?` (L323) tambem e N+1.

### Tabela de scores — Projeto 5

| Dimensao | v1 | Comentario |
|---|---|---|
| Onboarding | 4 | Schema criado automaticamente; WAL e FK habilitados |
| API Ergonomics | 3 | API de store clara; migrate() simplista |
| Feature Completeness | 2 | Sem migracao versionada; FTS pode dessincronizar |
| Security Confidence | 3 | dataDir 0700; sem criptografia (escopo v0.1) |
| Language Quality | 3 | Padroes Go corretos; workaround de LastInsertId documentado |
| Operational Readiness | 2 | Sem versao de schema; FTS rebuild nao atomico |
| Documentation | 2 | Schema documentado no CLAUDE.md; riscos de integridade nao documentados |

---

## Findings

### P0 — Blockers Operacionais

| ID | Arquivo | Linha | Descricao |
|---|---|---|---|
| P0-01 | `queue.go` + `server.go` | L31 / L74 | **Perda de dados no shutdown**: fila nao e drenada antes do processo terminar. Itens no canal Go sao descartados silenciosamente. |
| P0-02 | `store.go` | L263 | **Full table scan de embeddings em cada busca**: `SELECT id, text, page_id, embedding FROM chunks` sem WHERE carrega todos os BLOBs em memoria. Inviavel com 10.000+ paginas. |
| P0-03 | `store.go` | L198 | **FTS5 rebuild sincrono a cada ingest**: torna o throughput de ingest O(N) onde N = total de chunks. Fila satura com volume moderado de navegacao. |
| P0-04 | `daemon-client.ts` | L67 | **Bug de tipo no search()**: `res.json() as Promise<SearchResult[]>` retorna `{results: [...]}` nao `SearchResult[]`. Omnibox e resultados de busca quebrados silenciosamente. |

### P1 — Importantes

| ID | Arquivo | Linha | Descricao |
|---|---|---|---|
| P1-01 | `routes.go` | L71-74 | **`/healthz` nao verifica o banco**: falso-positivo se SQLite inacessivel. |
| P1-02 | `server.go` | L47-49 | **`VBM_PORT` liga em `0.0.0.0`**: superficie de rede exposta sem Dockerfile presente no projeto. |
| P1-03 | `store.go` | L131-134 | **Schema sem versionamento**: migracao futura exige intervencao manual ou wipe. |
| P1-04 | `store.go` | L316-327 | **N+1 queries no build de resultados**: 2 queries por resultado, ate 40 queries adicionais por busca. |
| P1-05 | `host.go` | L33 | **Erro de `os.UserHomeDir()` descartado em `SessionPath()`**: falha silenciosa em ambientes sem HOME. |
| P1-06 | Geral | — | **Logs nao estruturados**: `log.Printf` nao permite filtragem por campo em producao. |
| P1-07 | `routes.go` | L186-207 | **WebSocket sem ping/pong e sem write deadline**: conexoes zumbi acumulam. |

### P2 — Nice-to-Have

| ID | Arquivo | Descricao |
|---|---|---|
| P2-01 | `vbmd.service` | Adicionar `TimeoutStopSec=15` para dar tempo ao drain da fila. |
| P2-02 | `vbmd.service` | Remover `After=network.target` — dependencia desnecessaria. |
| P2-03 | `store.go` | Adicionar `db.SetMaxOpenConns(1)` para ser explicito sobre single-writer. |
| P2-04 | Geral | Endpoint `/metrics` Prometheus com `ingest_total`, `ingest_duration_seconds`, `queue_depth`, `search_duration_seconds`. |
| P2-05 | `embed/embedder.go` | StubEmbedder retorna zeros — dense search desperdicada; adicionar aviso no log de startup. |
| P2-06 | `daemon-client.ts` | Adicionar timeout nas chamadas fetch para evitar hang se daemon travar. |
| P2-07 | `store.go` | Considerar `INSERT INTO chunks_fts` incremental ao inves de `rebuild` completo. |

---

## Conclusao

### Veredicto: NAO PRONTO PARA PRODUCAO

O Vector Bookmark e um POC tecnicamente bem estruturado para v0.1: o protocolo de autenticacao esta correto, o bind no loopback e adequado, o graceful shutdown do HTTP server existe, e os padroes Go sao geralmente idiomaticos. Para uso pessoal com volume baixo (< 500 paginas), funciona.

Porem, ha quatro blockers operacionais que impedem recomendacao para producao:

1. **Perda de dados garantida no restart**: toda vez que o daemon e reiniciado (atualizacao, crash-recovery pelo systemd), itens na fila sao descartados sem notificacao.
2. **Escala de busca nao-existente**: uma unica query de busca com 50.000 chunks carrega >76MB na heap e executa dezenas de queries adicionais. Latencia de busca vai exceder 10 segundos com uso normal de algumas semanas.
3. **Ingest travado por FTS5 rebuild**: com volume moderado, o throughput de ingest cai abaixo de 1 pagina/segundo, saturando a fila e descartando dados.
4. **Bug de tipo no search client TypeScript**: a funcionalidade principal (omnibox) esta quebrada silenciosamente.

Esses nao sao problemas de tuning — sao problemas de design que requerem mudancas arquiteturais (drain de fila no shutdown, FTS5 incremental, ANN index ou paginacao para dense search, correcao do tipo de retorno no TS).

---

## Score Medio

| Dimensao | Proj 1 | Proj 2 | Proj 3 | Proj 4 | Proj 5 | Media |
|---|---|---|---|---|---|---|
| Onboarding | 4 | 3 | 3 | 3 | 4 | **3.4** |
| API Ergonomics | 3 | 3 | 3 | 4 | 3 | **3.2** |
| Feature Completeness | 2 | 1 | 2 | 3 | 2 | **2.0** |
| Security Confidence | 3 | 3 | 3 | 3 | 3 | **3.0** |
| Language Quality | 3 | 2 | 2 | 3 | 3 | **2.6** |
| Operational Readiness | 2 | 1 | 1 | 2 | 2 | **1.6** |
| Documentation | 2 | 1 | 1 | 2 | 2 | **1.6** |
| **Media do Projeto** | **2.7** | **2.0** | **2.1** | **2.9** | **2.7** | **2.5** |

---

**Score v1: 2.5 / 5**
