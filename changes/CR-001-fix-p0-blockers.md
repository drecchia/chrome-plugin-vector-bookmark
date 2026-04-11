# CR-001 — Correcao de todos os P0 Blockers

**Data:** 2026-04-11
**Motivacao:** Avaliacao de maturidade v1 (`docs/maturity/2026-04-11_v1/MATURITY_REPORT.md`) identificou 7 P0 blockers que impedem o uso do produto alem de prototipo. Score v1: 2.6/5, nivel Alpha.
**Resultado esperado:** Score proximo de 3.2+/5, nivel Beta.

---

## P0-01 — Omnibox quebrada silenciosamente (daemon-client.ts)

**Arquivo:** `extension/src/background/daemon-client.ts`

**Problema:** `search()` na linha 67 fazia `res.json() as Promise<SearchResult[]>`, mas o daemon retorna `{results: SearchResult[], total: number}` (tipo `SearchResponse`). O omnibox recebia um objeto, chamava `.map()` em `undefined` — zero sugestoes, zero erros visiveis.

**Mudancas:**
- Importar tipos de `proto/types.ts` em vez de redefinir localmente (removidos `IngestRequest`, `SearchResult`, `StatusResponse`, `ForgetRequest` duplicados)
- `search()` agora: `const data = await res.json() as SearchResponse; return data.results`
- `extension/tsconfig.json`: adicionado `../proto/**/*` ao `include` para o compilador TS encontrar os tipos compartilhados

---

## P0-02 — Embedder real via HTTP (embed/http.go)

**Arquivo:** `daemon/internal/embed/http.go` (novo)

**Problema:** `StubEmbedder` retornava `make([]float32, 384)` — vetores zero em tudo. Busca semantica nao existia.

**Mudancas:**
- Criado `HttpEmbedder` que chama endpoint Ollama-compativel (`POST /api/embeddings`)
- Configurado via env vars: `VBM_EMBED_URL` (ex: `http://localhost:11434/api/embeddings`), `VBM_EMBED_MODEL` (default: `nomic-embed-text`)
- Timeout de 10s no cliente HTTP
- `Version()` retorna `"http-v0"` — chunks reindexados automaticamente se model_ver mudar
- `server.go`: seleciona `HttpEmbedder` se `VBM_EMBED_URL` estiver definida; caso contrario loga WARNING e usa StubEmbedder

**Como ativar:**
```bash
# Instalar Ollama: https://ollama.ai
ollama pull nomic-embed-text
export VBM_EMBED_URL=http://localhost:11434/api/embeddings
./bin/vbmd server
```

---

## P0-03 — FTS5 rebuild sincrono removido do Ingest (sqlite.go)

**Arquivo:** `daemon/internal/store/sqlite.go`

**Problema:** `INSERT INTO chunks_fts(chunks_fts) VALUES('rebuild')` executava dentro de cada transacao de ingest — O(N) sobre todos os chunks existentes. Violava regra documentada no CLAUDE.md.

**Mudancas:**
- Removido `INSERT ... VALUES('rebuild')` do metodo `Ingest()`
- Substituido por insert incremental: apos cada `INSERT OR IGNORE INTO chunks`, se `RowsAffected() > 0` (chunk novo, nao duplicado), executa `INSERT INTO chunks_fts(rowid, text) VALUES(chunkID, text)`
- Custo por ingest: O(novos_chunks) em vez de O(todos_chunks)
- FTS5 `rebuild` permanece apenas em `Forget()` e `Cleanup()` (correto per CLAUDE.md)

---

## P0-04 — Queue drenada no shutdown (queue.go + server.go)

**Arquivos:** `daemon/internal/queue/queue.go`, `daemon/internal/server/server.go`

**Problema:** `worker()` nunca recebia sinal de parada. A cada restart do daemon, todos os itens no canal Go eram descartados silenciosamente.

**Mudancas em queue.go:**
- Adicionado `sync.WaitGroup` ao struct `Queue`
- `New()`: chama `q.wg.Add(1)` antes de `go q.worker()`
- `worker()`: chama `defer q.wg.Done()` — `for req := range q.ch` ja termina quando o canal e fechado
- Novo metodo `Close()`: fecha o canal (`close(q.ch)`)
- Novo metodo `Wait()`: bloqueia ate o worker terminar (`q.wg.Wait()`)

**Mudancas em server.go:**
- Shutdown goroutine agora, apos `srv.Shutdown()`: chama `q.Close()`, aguarda `q.Wait()` com timeout de 30s
- Log: `"draining ingest queue..."` e `"queue drained"` (ou timeout warning)

---

## P0-05 — Full scan O(N) de embeddings evitado (sqlite.go)

**Arquivo:** `daemon/internal/store/sqlite.go`

**Problema:** `Search()` carregava `SELECT id, text, page_id, embedding FROM chunks` (todos os BLOBs) em memoria a cada busca. Com StubEmbedder (vetores zero), desperdicava memoria sem beneficio.

**Mudancas:**
- Apos gerar o query vector, verifica se e all-zero: `isStub := all(v == 0)`
- Se stub: `denseRanks` fica vazio, busca continua apenas com BM25 — sem full scan
- Se real (HttpEmbedder com VBM_EMBED_URL): executa o dense search normalmente
- O fix arquitetural completo (ANN index, sqlite-vec) fica para CR-002

---

## P0-06 — VBM_PORT sempre em loopback (server.go)

**Arquivo:** `daemon/internal/server/server.go`

**Problema:** `listenAddr = "0.0.0.0:" + p` expunha o daemon (e todos os dados de navegacao) em todas as interfaces de rede quando `VBM_PORT` estava definida.

**Mudancas:**
- `VBM_PORT` agora sempre usa `127.0.0.1:<port>`
- Nova env var `VBM_BIND` para override explicito de interface (Docker): `VBM_BIND=0.0.0.0`
- Quando `VBM_BIND` e usado, loga WARNING para conscientizar o operador
- Log de startup atualizado para mostrar o endereco real de bind

---

## P0-07 — Retencao automatica de dados configuravel (sqlite.go + server.go)

**Arquivos:** `daemon/internal/store/sqlite.go`, `daemon/internal/server/server.go`

**Problema:** Dados de navegacao acumulavam indefinidamente. LGPD Art. 15 requer eliminacao ao fim da finalidade.

**Mudancas em sqlite.go:**
- Novo metodo `Cleanup(ttlDays int) (int64, error)`: deleta pages com `visit_ts < now - ttlDays` (CASCADE para chunks)
- Se `ttlDays <= 0`: no-op
- Apos deletar, executa `rebuild` do FTS5 (correto para cleanup em batch)
- Retorna contagem de pages deletadas

**Mudancas em server.go:**
- Leitura de `VBM_TTL_DAYS` env var (int, default 0 = desabilitado)
- Se > 0: goroutine executa `Cleanup()` no startup e a cada 24h
- Default 0 garante nao quebrar instalacoes existentes

**Como ativar (ex: 90 dias):**
```bash
export VBM_TTL_DAYS=90
./bin/vbmd server
# log: [vbmd] data retention: 90 days
```

---

## Arquivos modificados

| Arquivo | Tipo |
|---|---|
| `extension/src/background/daemon-client.ts` | Modificado |
| `extension/tsconfig.json` | Modificado |
| `daemon/internal/embed/http.go` | Criado |
| `daemon/internal/server/server.go` | Modificado |
| `daemon/internal/store/sqlite.go` | Modificado |
| `daemon/internal/queue/queue.go` | Modificado |

---

## Verificacao

```bash
# 1. Build daemon (deve compilar sem erros)
cd daemon && go build ./...

# 2. TypeScript (deve passar sem erros)
cd extension && npm run typecheck

# 3. Daemon com stub (default) — deve logar WARNING
./bin/vbmd server
# [vbmd] WARNING: VBM_EMBED_URL not set — using stub embedder (BM25-only, no semantic search)

# 4. VBM_PORT seguro — deve ligar em 127.0.0.1, nao 0.0.0.0
VBM_PORT=7700 ./bin/vbmd server
# [vbmd] server listening on 127.0.0.1:7700

# 5. Busca retorna array (P0-01)
SESSION=~/.local/share/vbm/session.json
TOKEN=$(python3 -c "import json; d=json.load(open('$HOME/.local/share/vbm/session.json')); print(d['token'])")
PORT=$(python3 -c "import json; d=json.load(open('$HOME/.local/share/vbm/session.json')); print(d['port'])")
curl -s "http://127.0.0.1:$PORT/search?q=test" -H "Authorization: Bearer $TOKEN" | python3 -c "import json,sys; d=json.load(sys.stdin); print(type(d['results']))"
# <class 'list'>

# 6. Shutdown drena fila (P0-04)
# Enfileirar ingests e enviar SIGTERM — log deve mostrar "draining ingest queue..." e "queue drained"

# 7. Cleanup TTL (P0-07)
VBM_TTL_DAYS=1 ./bin/vbmd server
# [vbmd] data retention: 1 days
# [vbmd] startup cleanup: removed N pages  (se houver paginas com mais de 1 dia)

# 8. Embedder real (P0-02) — requer Ollama rodando
ollama pull nomic-embed-text
VBM_EMBED_URL=http://localhost:11434/api/embeddings ./bin/vbmd server
# [vbmd] using HTTP embedder: http://localhost:11434/api/embeddings (model: nomic-embed-text)
```
