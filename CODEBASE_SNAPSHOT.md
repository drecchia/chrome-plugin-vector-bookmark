# CODEBASE_SNAPSHOT.md
## Mapeamento da codebase — gerado em 2026-04-11

---

## Domínio 1 — Daemon (Go)

**Stack:** Go 1.22, `github.com/go-chi/chi/v5`, `modernc.org/sqlite`, `github.com/gorilla/websocket`, `github.com/google/uuid`

**Entidades (structs Go):**

- `nm.Session` — `Port: int`, `Token: string`
- `store.Page` — `ID: int64`, `URL: string`, `URLHash: string`, `Title: string`, `Domain: string`, `VisitTs: int64`, `DwellMs: int64`, `ModelVer: string`
- `store.IngestRequest` — `URL: string`, `Title: string`, `Text: string`, `VisitTs: int64`, `DwellMs: int64`, `Domain: string`
- `store.SearchResult` — `URL: string`, `Title: string`, `Snippet: string`, `VisitTs: int64`, `Score: float64`, `Domain: string`
- `store.ForgetRequest` — `Type: string` (`url|domain|timerange`), `Value: string`
- `embed.StubEmbedder` — implementa interface `Embedder`; `Dim() = 384`, `Version() = "stub-v0"`, retorna vetor zero
- `chunk.Chunk` — `Index: int`, `Text: string`, `Hash: string`
- `queue.Queue` — canal bufferizado (`chan store.IngestRequest`, cap 256) + goroutine worker

**Tabelas SQLite:**

```sql
pages       (id, url, url_hash UNIQUE, title, domain, visit_ts, dwell_ms, model_ver, created_at)
chunks      (id, page_id FK→pages, chunk_idx, text, text_hash, embedding BLOB, model_ver)
            UNIQUE(page_id, chunk_idx)
chunks_fts  VIRTUAL TABLE fts5(text, content='chunks', content_rowid='id')
queue       (id, url, title, text, visit_ts, dwell_ms, domain, status DEFAULT 'pending', created_at, updated_at)
```

WAL mode ativado. Foreign keys com `ON DELETE CASCADE` em `chunks.page_id`.

**Endpoints HTTP (todos em `127.0.0.1:PORT`, auth Bearer obrigatória exceto /healthz):**

| Método | Path | O que faz |
|--------|------|-----------|
| `GET` | `/healthz` | Health check, sem auth — retorna `{"ok":true}` |
| `POST` | `/ingest` | Recebe `IngestRequest`, enfileira no `Queue` |
| `GET` | `/search?q=&limit=` | Busca híbrida BM25+cosine RRF, retorna `{results:[...]}` (default limit 5, max 20) |
| `DELETE` | `/forget` | Recebe `ForgetRequest`, remove páginas por url/domain/timerange + rebuild FTS5 |
| `GET` | `/status` | Retorna `{indexed, pending, version}` |
| `GET` | `/ws` | WebSocket — push de `{type:"status", indexed, pending}` a cada 5s |
| `GET` | `/ui/*` | Serve HTML placeholder da UI local |

**Middlewares:** `middleware.Logger`, `middleware.Recoverer`, auth Bearer, CORS validando `Origin: chrome-extension://`.

**Regras de negócio implementadas:**

- Chunking: janela de 512 tokens, overlap 64, mínimo 40 tokens por chunk (tokenização por whitespace)
- Dedup: `sha1(normalize(text))` — `INSERT OR IGNORE` evita chunks duplicados entre sessões
- URL hash: `sha1(url)` como chave única de página (`url_hash`)
- Busca híbrida: BM25 via FTS5 (`bm25()` retorna negativo — menor = mais relevante) + cosine similarity brute-force sobre BLOBs float32 → fusão RRF k=60
- Queue com backpressure: se buffer cheio (cap 256), drop com log — sem blocking
- Session file: `~/.local/share/vbm/session.json` (chmod 600), criado no startup do server, removido no shutdown
- Port: aleatório via `net.Listen("tcp", "127.0.0.1:0")` a cada startup

**IPC Native Messaging:**

- Framing: uint32 little-endian (4 bytes) + JSON
- Request: `{type:"handshake", extensionId:"..."}`
- Response OK: `{type:"handshake_ok", port:N, token:"uuid"}`
- Response Error: `{type:"handshake_error", error:"..."}`
- `nm-host` mode: lê `session.json`, responde, encerra
- `server` mode: inicia servidor HTTP, escreve `session.json`, aguarda SIGTERM/SIGINT

---

## Domínio 2 — Chrome Extension (TypeScript/MV3)

**Stack:** TypeScript 5.4, Vite 5, CRXJS 2.0-beta, React 18, `@mozilla/readability` 0.5

**Permissões declaradas (manifest.json):**

```
nativeMessaging, tabs, webNavigation, storage, omnibox, idle
host_permissions: <all_urls>
incognito: "not_allowed"
```

**Content Script (`src/content/extract.ts`):**

- Executa em `document_idle` em todas as URLs
- Rastreia tempo visível via `document.visibilitychange` + `setInterval` (5s)
- Threshold: **30.000ms** de tempo visível acumulado → dispara `sendPage()` uma única vez (flag `sent`)
- Extrai texto com `Readability(document.cloneNode(true)).parse()`
- Cancela se `article.textContent < 200 chars`
- Envia `{type:'page_viewed', url, title, text, dwellMs}` ao service worker
- Detecção de campo sensível: escuta `focusin` em `input[type="password"]` → envia `{type:'page_sensitive'}`
- Guard pré-envio: checa `input[type="password"], input[autocomplete*="cc-"], input[autocomplete="one-time-code"], input[autocomplete="current-password"], input[autocomplete="new-password"]`

**Service Worker (`src/background/service-worker.ts`):**

Mensagens tratadas:
- `page_viewed` → valida URL (não `chrome://`, não `chrome-extension://`), checa denylist, chama `ingest()`
- `page_sensitive` → log, sem ação
- `popup_status` → retorna `StatusResponse` do daemon
- `popup_forget` → chama `forget()` no daemon, retorna `{ok:bool}`

Omnibox (`@recall`):
- `onInputChanged`: query ≥ 2 chars → `search(text, 5)` → `suggest()` com snippet + domínio
- `onInputEntered`: se começa com `http` → abre URL; senão no-op

Badge: pisca vermelho por 2s após ingest bem-sucedido.

**Native Bridge (`src/background/native-bridge.ts`):**

- `daemonState: {port: null, token: null}` (memória SW, nunca persistido)
- `connectDaemon()`: `chrome.runtime.sendNativeMessage('com.vbm.daemon', {type:'handshake', extensionId})` → armazena `{port, token}`
- Retorna imediatamente se já conectado
- `getDaemonBase()`: `http://127.0.0.1:${port}`
- `getAuthHeader()`: `Bearer ${token}`

**Daemon Client (`src/background/daemon-client.ts`):**

Funções exportadas (todas chamam `connectDaemon()` primeiro, exceto `healthz`):
- `ingest(req: IngestRequest): Promise<void>` — `POST /ingest`
- `search(query, limit=5): Promise<SearchResult[]>` — `GET /search`
- `forget(req: ForgetRequest): Promise<void>` — `DELETE /forget`
- `getStatus(): Promise<StatusResponse>` — `GET /status`
- `healthz(): Promise<boolean>` — `GET /healthz` sem auth

Headers em todas as chamadas autenticadas: `Content-Type: application/json`, `Authorization: Bearer ${token}`, `Origin: ${chrome.runtime.getURL('')}`

**Denylist (`src/lib/denylist.ts`):**

24 domínios bloqueados (lista literal):
`accounts.google.com, mail.google.com, docs.google.com, drive.google.com, myaccount.google.com, login.microsoftonline.com, outlook.live.com, onedrive.live.com, bankofamerica.com, chase.com, wellsfargo.com, paypal.com, venmo.com, coinbase.com, kraken.com, 1password.com, bitwarden.com, lastpass.com, dashlane.com, keepass.info, healthcare.gov, medicaid.gov, irs.gov, ssa.gov`

TLDs bloqueados: `.gov`, `.mil`

14 padrões de URL (RegExp): `/login`, `/signin`, `/auth/`, `/oauth`, `/saml`, `/sso`, `/mfa`, `/2fa`, `/verify`, `/password`, `/reset-password`, `/checkout`, `/payment`, `/account/`

Funções exportadas: `isDeniedDomain(hostname)`, `isDeniedUrl(url)`

**Popup (`src/popup/App.tsx`):**

State: `status: StatusResponse | null`, `error: string | null`, `paused: boolean`, `loading: boolean`

Ações: pause toggle, forget por URL ou domain (input + select + botão), "Abrir UI completa" (link para `http://127.0.0.1:PORT/ui`), exibe contagem de páginas indexadas + pendentes.

Comunicação via `chrome.runtime.sendMessage`.

---

## Domínio 3 — Protocolo Compartilhado (proto/types.ts)

**Interfaces TypeScript:**

```typescript
IngestRequest      { url, title, text, visitTs: number, dwellMs: number, domain }
SearchRequest      { q, limit?: number }
SearchResult       { url, title, snippet, visitTs: number, score: number, domain }
SearchResponse     { results: SearchResult[], total: number }
ForgetType         = 'url' | 'domain' | 'timerange'
ForgetRequest      { type: ForgetType, value: string }
StatusResponse     { indexed: number, pending: number, version: string }
WsStatusMessage    { type: 'status', indexed: number, pending: number }
NMHandshakeRequest { type: 'handshake', extensionId: string }
NMHandshakeOk      { type: 'handshake_ok', port: number, token: string }
NMHandshakeError   { type: 'handshake_error', error: string }
NMResponse         = NMHandshakeOk | NMHandshakeError
DaemonState        { port: number | null, token: number | null }
```

---

## Infraestrutura

**Instalação (daemon/install/):**

- `install.sh` — copia binário para `~/.local/bin/vbmd`, instala NM manifest em `~/.config/google-chrome/NativeMessagingHosts/com.vbm.daemon.json`, instala `vbmd.service` em `~/.config/systemd/user/`, executa `systemctl --user enable --now vbmd`
- `vbmd.service` — `Type=simple`, `Restart=on-failure`, `RestartSec=3s`, `ExecStart=%h/.local/bin/vbmd server`
- `native-messaging-host.json` — template com placeholders `BINARY_PATH` e `EXTENSION_ID`

**Build:**

- Daemon: `make build` → `daemon/bin/vbmd` (16MB, pure Go sem CGO)
- Extension: `npm run build` → `extension/dist/` (45 módulos, ~185KB total)

---

## Variáveis de ambiente referenciadas

Nenhuma variável de ambiente é usada no código atual. Configuração via:
- Daemon: porta aleatória em runtime, token UUID gerado no startup
- Extension: ID do daemon hardcoded como `'com.vbm.daemon'`
- Threshold de dwell hardcoded: `30_000` ms no content script
- Chunking hardcoded: `WindowTokens=512`, `OverlapTokens=64`, `MinTokens=40`

---

## Stack tecnológica

| Camada | Tecnologia |
|--------|-----------|
| Daemon | Go 1.22, chi v5, modernc/sqlite (FTS5), gorilla/websocket, google/uuid |
| Extensão | TypeScript 5.4, Vite 5, CRXJS 2.0-beta, React 18, @mozilla/readability |
| Banco | SQLite (WAL, FTS5, sem sqlite-vec — busca densa é brute-force Go) |
| Embedder | Stub (zero vectors) — ONNX/arctic-embed-xs planejado |
| IPC | Chrome Native Messaging + localhost HTTP + WebSocket |
| OS | Linux (systemd user unit) — macOS/Windows deferred |
