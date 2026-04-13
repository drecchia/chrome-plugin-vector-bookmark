# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Visão geral do projeto

Vector Bookmark é um sistema de memória semântica de navegação composto por dois artefatos: uma extensão Chrome MV3 (TypeScript) e um daemon nativo Go (`vbmd`). A extensão captura passivamente páginas em que o usuário passa ≥10s (configurável), extrai meta tags HTML e envia ao daemon via HTTP local. O daemon indexa o conteúdo em SQLite com busca híbrida BM25+vetores e expõe uma API REST em `127.0.0.1:7532` (padrão). Toda a stack roda exclusivamente na máquina do usuário.

## Stack

| Componente | Tecnologia |
|---|---|
| Daemon | Go 1.22, `chi/v5`, `modernc.org/sqlite` (FTS5, pure Go), `gorilla/websocket` |
| Extensão | TypeScript 5.4, Vite 5, CRXJS 2.0-beta, React 18, `@mozilla/readability` |
| Banco | SQLite (WAL mode, FTS5 virtual table, embeddings como BLOB float32) |
| IPC | localhost HTTP (porta padrão 7532) + WebSocket |

## Comandos

```bash
# Daemon
cd daemon
make build        # compila → daemon/bin/vbmd
make run          # build + executa ./bin/vbmd server
make test         # go test ./...
make install      # instala + inicia via systemd
make tidy         # go mod tidy

# Extensão
cd extension
npm install
npm run build     # gera extension/dist/
npm run dev       # watch mode (Vite)
npm run typecheck # tsc --noEmit sem gerar arquivos

# Verificar daemon rodando
curl http://127.0.0.1:7532/healthz
curl http://127.0.0.1:7532/status
```

Carregar extensão: `chrome://extensions/` → Developer mode → Load unpacked → `extension/dist/`

## Arquitetura

### Fluxo de captura passiva (dwell)
```
Content script (extract.ts)
  → acumula tempo visível via Page Visibility API
  → após dwellThreshold (default 10s): envia page_visited {url, title, dwellMs, meta}
Service Worker (service-worker.ts)
  → verifica blacklist do usuário (isBlockedByUser)
  → POST /visit ao daemon
  → badge azul "visit recorded"
```

### Fluxo de index manual (popup)
```
Popup (App.tsx) → popup_force_index
  → SW injeta content script se necessário
  → envia force_extract ao content script
  → Readability extrai texto + extractMeta() extrai meta tags
  → envia page_viewed {url, title, text, meta}
  → SW prepend meta fields ao texto → POST /ingest ao daemon
  → badge verde "page indexed"
```

### Badge state machine (service-worker.ts)
Estados: `tracking(0) < disconnected(1) < blocked(2) < visited(3) < indexed(4)`

`setBadge` nunca faz downgrade de um estado para prioridade menor. O estado é resetado em cada nova navegação (`onUpdated status=loading`). Isso evita que `dwell_started` sobreescreva um badge `visited` em revisitas.

### Daemon (Go)
- `cmd/vbmd/main.go` — entrypoint, modo `server`
- `internal/server/routes.go` — todos os handlers HTTP (chi router)
- `internal/store/sqlite.go` — schema, migrations, Ingest, RecordVisit, Search (BM25+cosine RRF), Forget
- `internal/chunk/chunk.go` — sliding window chunker (512 tokens, overlap 64, mínimo 40)
- `internal/embed/` — interface Embedder + StubEmbedder (zeros para dev)
- `internal/queue/queue.go` — canal bufferizado cap 256, worker de ingest

### Extensão (TypeScript)
- `src/content/extract.ts` — dwell tracking, extractMeta(), force extract via Readability
- `src/background/service-worker.ts` — orquestrador: badge, mensagens, blacklist
- `src/background/daemon-client.ts` — todas as chamadas HTTP ao daemon
- `src/background/native-bridge.ts` — config storage (host, port, dwell threshold)
- `src/background/default-blacklist.json` — blacklist padrão semeada no daemon (apenas padrões local/private IP)
- `src/popup/App.tsx` — UI React inline-styled (sem CSS externo)
- `proto/types.ts` — tipos compartilhados da API HTTP (importar daqui, nunca duplicar)

## Padrões obrigatórios

**Go (daemon):**
- Pacotes em `internal/` — nunca expor tipos entre pacotes via interface pública desnecessária
- Erros sempre propagados com `fmt.Errorf("contexto: %w", err)` — sem `log.Fatal` dentro de packages
- Todo handler HTTP retorna JSON; erros retornam `{"error":"mensagem"}` com status apropriado
- Bind exclusivo em `127.0.0.1` — nunca `0.0.0.0`

**TypeScript (extensão):**
- Tipos da API definidos em `proto/types.ts` — não duplicar em arquivos da extensão
- Config do daemon (`host`/`port`) lida de `chrome.storage.local` via `getDaemonConfig()` em `native-bridge.ts`
- Blacklist checada no SW **após** receber `page_visited` via `isBlockedByUser(hostname)`

**Ambos:**
- Timestamps sempre em Unix milliseconds (int64/number)
- SHA1 hex para hashing de conteúdo (url_hash, text_hash)
- `model_ver` obrigatório em todo registro de chunk — valor atual: `"stub-v0"`

## Regras de negócio críticas

1. **Dwell mínimo**: 10.000ms de tempo visível acumulado (não tempo de clock). Configurável via popup → salvo em `chrome.storage.local` como `vbmDwellMs`. Content script escuta `storage.onChanged` para hot-update sem reload.
2. **Blacklist**: apenas a blacklist gerenciada pelo usuário (tabela `blacklist` no daemon). Não há denylist estática. Default seeds: apenas padrões de IP local/privado.
3. **Dedup de conteúdo**: `sha1(normalize(text))` — chunks com mesmo hash são descartados (`INSERT OR IGNORE`).
4. **Dedup de página**: `sha1(url)` como `url_hash UNIQUE` — revisitas atualizam a página existente.
5. **Chunking**: janela 512 tokens, overlap 64, mínimo 40 tokens. Texto < 200 chars descartado no content script.
6. **Queue com backpressure**: canal cap 256. Se cheio, ingest descartado com log.
7. **Busca híbrida RRF**: BM25 via FTS5 + cosine brute-force → RRF k=60. Limite: 5 default, 20 max.
8. **Meta indexing**: no index manual (force_extract), meta tags (description, keywords, ogDescription, author) são prefixados ao texto antes do chunking.

## Entidades principais (SQLite)

```
pages       id, url, url_hash (UNIQUE), title, domain, visit_ts (ms), dwell_ms, model_ver, indexed, meta_json, star_rank (não usado), created_at
chunks      id, page_id (FK→pages CASCADE), chunk_idx, text, text_hash, embedding (BLOB f32), model_ver
chunks_fts  virtual FTS5 sobre chunks.text (porter stemmer)
queue       id, url, title, text, visit_ts, dwell_ms, domain, status, created_at, updated_at
blacklist   pattern (PRIMARY KEY), created_at
```

## O que nunca fazer

- **Nunca** fazer bind do daemon em `0.0.0.0` — somente `127.0.0.1`
- **Nunca** remover `incognito: "not_allowed"` do manifest
- **Nunca** usar `log.Fatal` dentro de packages `internal/` — retornar erro para o caller
- **Nunca** fazer FTS5 rebuild síncrono em operações de ingest — apenas em `Forget`
- **Nunca** persistir embeddings sem o campo `model_ver`
- **Nunca** duplicar tipos do `proto/types.ts` na extensão
- **Nunca** commitar `daemon/bin/`, `extension/dist/`, `extension/node_modules/`, `*.db`
