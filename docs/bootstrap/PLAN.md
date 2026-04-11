# Vector Bookmark — Native Daemon + Thin Chrome Extension

## Context

Reimagine browser bookmarks as an auto-indexed personal memory: every page the user dwells on is captured (URL, extracted text, timeline, optional snapshot), embedded into a vector store, and made searchable via natural language. Cross-device sync via a self-hostable server.

**Architecture pivot (user decision, 2026-04-11):** Originally scoped as a browser-heavy MV3 extension. User flagged — correctly — that it's too much to cram into a Chrome extension. Pivoting to a **native daemon + thin extension** split (same pattern as Rewind, Zotero Connector, 1Password): daemon owns all the heavy lifting (embedding, vector DB, snapshots, LLM, sync, storage); the extension is a minimal capture/display client that talks to the daemon over local IPC.

Motivation: MV3 constraints (SW 30s idle death, offscreen doc complexity, OPFS quotas, WASM embedding perf, IDB transaction semantics) all evaporate when the data layer lives out-of-process. Daemon outlives browser sessions, uses real ONNX runtime (3–10× faster than WASM), real filesystem for snapshots, and serves as a single source of truth for future sync.

6 dev personas (Chrome-ext eng, ML/RAG eng, backend architect, privacy/security eng, product/UX, pragmatic skeptic) weighed in on the original browser-only design — their product scope, MVP cut-list, and privacy hard-lines still apply. This revision keeps the product ("researcher-anchored passive capture + semantic recall") and re-homes the implementation.

Project directory is empty — pure greenfield.

---

## Architecture

```
┌──────────────────────┐                             ┌──────────────────────────┐
│   Chrome Extension   │                             │     Native Daemon        │
│   (thin — ~1kLoC)    │  ◀── Native Messaging ───▶  │   (all heavy lifting)    │
│                      │       stdio handshake       │                          │
│  - Readability       │        {port, token}        │  - Embedder (ONNX)       │
│  - Denylist/pause    │                             │  - Vector DB (sqlite-vec)│
│  - Omnibox @recall   │  ──── localhost HTTP ────▶  │  - FTS5 + hybrid search  │
│  - Popup             │       /ingest /search       │  - Durable ingest queue  │
│  - Native bridge     │       /forget /status       │  - Snapshot store (FS)   │
│                      │  ◀─── localhost WebSocket ─ │  - LLM bridge (v0.2)     │
│                      │       indexing status push  │  - Local web UI          │
└──────────────────────┘                             │  - Sync client (v0.3)    │
                                                     └──────────────────────────┘
                                                           binds 127.0.0.1 only
```

**IPC split (hybrid):**
- **Native Messaging (stdio)** — extension spawns daemon on first use via Chrome's Native Messaging API. Daemon replies with `{port, token}`. Used only for the privileged handshake. Chrome's NM mechanism solves the "where is the daemon, what's the secret?" discovery problem cleanly.
- **Localhost HTTP (loopback + bearer token)** — bulk data flow (ingest, search, forget). Daemon binds `127.0.0.1` only, rejects requests without the handshake token, origin-checks against the extension's `chrome-extension://` ID.
- **Localhost WebSocket** — server-push for indexing progress, badge state, quota warnings.

Why hybrid and not pure NM: Native Messaging caps payloads at ~1MB/message and throughput is bad for streaming text. Why not pure localhost HTTP: discovery + initial auth are ugly without NM.

**Daemon lifecycle (v0.1 = Linux only):**
- Linux: systemd user unit (`~/.config/systemd/user/vbmd.service`)
- macOS: LaunchAgent — **v0.2**
- Windows: Run-key / Task Scheduler — **v0.2+**
- Chrome auto-launches via Native Messaging if daemon isn't running — so cold-start still works even if the systemd user unit isn't enabled.

---

## Scope split — Extension vs Daemon

### Chrome Extension (thin, ~1k lines TS)
- MV3, TypeScript + Vite + CRXJS
- `webNavigation.onCompleted` → dwell tracker (>30s) → denylist check → Readability extract → POST to daemon
- Content script: Readability.js + password-field detector (pauses capture at the tab level BEFORE data leaves)
- Omnibox handler (`@recall <query>`) → GET `/search` → render suggestions
- React popup: pause toggle, per-domain opt-in status, "forget" shortcut, "open full UI" button (→ `http://127.0.0.1:$PORT/ui`)
- Native Messaging client: one-shot handshake per browser session, caches `{port, token}` in SW memory only (never `chrome.storage`)
- **Deliberately NOT in the extension**: embedding, vector DB, snapshots, offscreen doc, Transformers.js, OPFS, LLM, sync. All of that moves to the daemon.

### Native Daemon (Go, single static binary)
- Vector store: **sqlite-vec** loadable extension on top of a single sqlite file — stores embeddings + FTS5 index for BM25 hybrid. Hybrid search via RRF fusion (k=60). WAL mode.
- Embedder: **onnxruntime-go** running `Snowflake/snowflake-arctic-embed-xs` quantized ONNX (~30MB weights, ~50ms/chunk on CPU)
- Chunker: 512-token sliding window, 64 overlap, min 40 tokens, text normalize + `sha1(normalized_text)` dedup hash
- Durable ingest queue (sqlite-backed) — survives daemon restarts, ACKs to extension only after persist
- Storage root: `~/.local/share/vbm/` (Linux — XDG). macOS (`~/Library/Application Support/vbm/`) and Windows (`%APPDATA%\vbm\`) deferred to v0.2+
- Local web UI served at `http://127.0.0.1:$PORT/ui` — rich experience lives here, not in the popup. Embedded via `go:embed` from a Vite+React build.
- HTTP API: `/ingest`, `/search`, `/forget`, `/status`, `/config`, `/ui/*`
- Native Messaging host: stdio JSON protocol, handshake only
- v0.2 adds: `/snapshot` endpoint (receives SingleFile HTML → zstd → content-hash-keyed FS blob), LLM bridge (Ollama auto-detect at `127.0.0.1:11434`), PDF/YouTube extractors
- v0.3 adds: sync client (E2EE XChaCha20-Poly1305, Argon2id KDF, event-log replay, content-addressed blob dedup)

---

## Hard privacy non-negotiables (unchanged from original)

1. **Default-deny sensitive domains** — shipped denylist: banks, brokerages, `.gov`, health portals, `accounts.*`, `login.*`, password managers, OAuth/SAML flows. Auto-detected via URL regex, `<input type=password>`, `autocomplete=one-time-code`, WebAuthn API usage. **Enforced in the extension BEFORE data leaves the tab** — not in the daemon.
2. **Incognito refusal** — `manifest.json → "incognito": "not_allowed"`.
3. **Pause capture** while any password/CC field is focused or filled on the active page.
4. **Visible recording indicator** — toolbar badge (red=recording, gray=paused). One-click pause (1h/day/forever).
5. **Forget panel** — delete by URL / domain / time range / regex. Daemon must tombstone AND remove from sqlite-vec virtual table, not just metadata soft-delete.
6. **Per-eTLD+1 opt-in prompt** on first capture — never a silent global opt-in.
7. **No cloud LLM by default** — if enabled (v0.2), explicit per-query confirmation showing exact chunks being sent + PII redaction pass.
8. **E2EE before any sync** — v0.3 gate. Server sees ciphertext only. Passphrase loss = data loss, stated plainly.

### New IPC security requirements (daemon-specific)
9. Daemon HTTP server binds `127.0.0.1` only — never `0.0.0.0` or `::`. Loopback check at startup.
10. Short-lived bearer token from Native Messaging handshake, rotated per browser session. Never persisted.
11. CORS: daemon rejects any `Origin` that isn't the extension's `chrome-extension://<id>/`.
12. `Authorization: Bearer <token>` required on every HTTP route except `/healthz`. Rate-limited.
13. Daemon refuses to start if another process is already bound to its port (no silent conflict).

---

## MVP v0.1 scope

**Goal:** prove semantic recall over browsing history, end-to-end, one OS, one browser.

### In-scope
- Chrome extension (capture + denylist + omnibox + popup) — thin
- Go daemon binary: Native Messaging host, localhost HTTP server, sqlite-vec store, ONNX embedder, hybrid BM25+dense search (RRF), forget API, durable ingest queue
- Installer script for **Linux only** (macOS + Windows deferred) — drops binary, NM host manifest, systemd user unit
- Minimal local web UI: list, search, forget panel. No timeline/clusters yet.
- Model version stamping (`model_ver` column) for future re-embed migrations
- URL canonicalization + content-hash dedup

### Out of scope for v0.1
- Snapshots → **v0.2**
- LLM synthesis → **v0.2**
- Cross-device sync + remote server → **v0.3**
- macOS installer → **v0.2**
- Windows installer → **v0.2+**
- Rich timeline / topic cluster viz → **v0.2**
- PDFs, YouTube transcripts, Gmail, Google Docs → **v0.2+** with per-type opt-in
- Reranker model → add only if recall quality is insufficient

### v0.2
- SingleFile HTML snapshots captured by extension → POST `/snapshot` → zstd → FS blob store keyed by content hash
- Rich local web UI: dwell-weighted timeline heatmap, topic clusters, starring
- LLM synthesis via Ollama bridge (local default); BYO cloud API key opt-in with redaction + confirmation
- PDF text extraction (`pdfcpu` or `ledongthuc/pdf`) and YouTube timedtext scraper
- macOS installer (LaunchAgent, unsigned first, notarization later)

### v0.3
- Self-hosted sync server in a separate repo — Go + Postgres + pgvector + R2/MinIO + OAuth/passkeys
- E2EE client layer in the daemon, zero-knowledge server mode
- Event-log sync (HLC timestamps), content-addressed blob dedup
- `docker-compose.yml` one-command self-host as first-class path

---

## Recommended v0.1 stack

**Extension**: TypeScript + Vite + CRXJS, Readability.js, React popup (minimal), Native Messaging client, `fetch()` against `http://127.0.0.1:$PORT` with bearer auth.

**Daemon**: Go 1.22+, `chi` router, `mattn/go-sqlite3` + sqlite-vec loadable extension, `onnxruntime-go` (or `yalue/onnxruntime_go`) for the embedder, `gorilla/websocket` for status push, stdio JSON for Native Messaging. Local web UI is Vite+React+Tailwind, embedded via `go:embed`. Single static binary (~50MB including model weights).

**Packaging**: Makefile targets (`make install` / `make uninstall`) for dev. Real installer artifacts (`.pkg` macOS / `.deb` + `.tar.gz` Linux) land in v0.1.1.

---

## Top risks (updated for daemon architecture)

1. **Installation friction** (NEW, high) — two artifacts (ext + daemon) plus per-OS setup. If first-run isn't "one command then load unpacked," the product dies. Mitigation: Native Messaging lets Chrome auto-launch the daemon IF the host manifest is installed — so the installer only needs to drop the binary + manifest + login item. Idempotent, single-command, no sudo.
2. **Silent capture of auth/banking/health** — legal and trust killer. Enforced in the **extension**, before data leaves the tab. Hardcoded denylist + password-field pause + WebAuthn detector. Hard-gate on this for v0.1.
3. **Embedding model drift / migration** — changing embedder invalidates the whole index. Mitigation: stamp `model_ver` per row; on upgrade, background re-embed worker in the daemon, old index stays queryable until done.
4. **IPC auth / localhost hijack** (NEW) — another local process could POST to `127.0.0.1:$PORT/ingest`. Mitigation: bearer token from Native Messaging handshake, `chrome-extension://` origin check, never wildcard CORS, daemon refuses non-loopback binds.
5. **Daemon crash / partial writes** — sqlite WAL + transactional ingest queue. Extension only drops the pending item after daemon ACK.
6. **Cross-platform distribution** — deferred. v0.1 is Linux-only so signing/notarization isn't in scope. macOS (v0.2) will ship unsigned first with `xattr -d com.apple.quarantine` instructions; Apple notarization is a v0.2.1+ concern.
7. **Storage bloat (v0.2)** — real filesystem has no OPFS cap, but users will notice a multi-GB dir. TTL + per-domain quota UI in the daemon from the start, not deferred.

Dropped from original risk list:
- *MV3 service worker death mid-embed* — no longer applies (embedding is out-of-process)
- *Indexing perf tanking the browser* — no longer applies (daemon runs on its own threads)

---

## Critical files to create (v0.1)

### Extension tree
- `extension/manifest.json` — MV3, `incognito: not_allowed`, perms: `nativeMessaging`, `<all_urls>`, `tabs`, `webNavigation`, `omnibox`, `storage`
- `extension/src/background/service-worker.ts` — webNavigation + dwell + denylist + NM handshake orchestration + POST to daemon
- `extension/src/background/native-bridge.ts` — stdio NM client, token cache (SW memory only)
- `extension/src/background/daemon-client.ts` — typed `fetch` wrapper (`ingest`, `search`, `forget`, `status`) with bearer auth + WS subscriber
- `extension/src/content/extract.ts` — Readability.js + password-field / WebAuthn detector, postMessage to SW
- `extension/src/lib/denylist.ts` — shipped denylist + runtime matcher
- `extension/src/popup/` — React popup (pause, opt-in, forget, "open full UI")
- `extension/src/omnibox/handler.ts` — `@recall` keyword → `/search` → suggestion cards
- `extension/vite.config.ts`, `tsconfig.json`, `package.json`

### Daemon tree
- `daemon/cmd/vbmd/main.go` — entrypoint; decides NM stdio mode vs server mode from argv
- `daemon/internal/nm/host.go` — Native Messaging stdio protocol, handshake returns `{port, token}`
- `daemon/internal/server/server.go` — chi router, loopback-bind check, CORS origin check, bearer auth middleware, WS upgrader
- `daemon/internal/server/routes.go` — `/ingest`, `/search`, `/forget`, `/status`, `/config`, `/healthz`, `/ui/*`
- `daemon/internal/embed/onnx.go` — ONNX runtime wrapper for arctic-embed-xs, batching, warmup
- `daemon/internal/store/sqlite.go` — sqlite-vec + FTS5, schema, upsert, hybrid search (RRF k=60), forget/tombstone
- `daemon/internal/chunk/chunk.go` — 512/64 chunker, text normalize, dedup hash
- `daemon/internal/queue/queue.go` — durable sqlite-backed ingest queue
- `daemon/internal/ui/` — embedded Vite+React build (`go:embed`)
- `daemon/install/vbmd.service` — Linux systemd user unit template
- `daemon/install/native-messaging-host.json` — NM host manifest template (Linux path: `~/.config/google-chrome/NativeMessagingHosts/`)
- `daemon/install/install.sh` — Linux installer: drops binary to `~/.local/bin/vbmd`, renders + installs NM manifest + systemd user unit, registers extension ID allowlist, `systemctl --user enable --now vbmd`
- `daemon/Makefile` — build, install, uninstall, package

### Shared
- `proto/` — JSON schemas for ingest/search/status (Go struct tags + generated TS types)
- `README.md` — dev-mode setup (`make install` → load unpacked ext)

---

## Verification plan

End-to-end dogfood checklist for v0.1 (Linux):

1. `make install` → drops daemon binary, NM host manifest, systemd user unit. `systemctl --user status vbmd` → active. Daemon auto-starts on next login.
2. Load unpacked extension → popup shows "Connected to daemon (port N)." Bearer token present only in SW memory (not `chrome.storage`).
3. **Denylist sanity** — visit `accounts.google.com`, a bank, a `.gov` page → daemon logs show NO `/ingest` calls received. Extension did the drop.
4. **Password-field pause** — focus a password input → badge flips to paused mid-visit.
5. **Incognito refusal** — open incognito → extension disabled.
6. **Happy path** — visit 20 article pages with dwell >30s each → daemon `/ingest` hit 20×, sqlite DB grows, embedding row count matches chunk count.
7. **Omnibox recall** — `@recall async rust tokio` → top-5 results. Target: p50 <200ms, p95 <500ms over 500 chunks (out-of-process ONNX beats WASM by a lot).
8. **Hybrid quality spot-check** — semantic-only queries ("that post comparing X and Y last month") should return the right article in top-3.
9. **Forget** — delete URL, domain, time range → subsequent searches return zero hits; `select count(*) from chunks where url=?` returns 0 (index row actually removed).
10. **Daemon restart resilience** — `kill vbmd` mid-ingest → systemd respawns (`Restart=on-failure`) → queue resumes, no dupes.
11. **IPC auth** — from an external process: `curl http://127.0.0.1:$PORT/search` → 401. `curl -H "Origin: http://evil"` → rejected. Confirm no wildcard CORS.
12. **Loopback bind** — `ss -tlnp` / `lsof -iTCP -sTCP:LISTEN` shows daemon on `127.0.0.1:$PORT` only, not `0.0.0.0`.
13. **Model version stamp** — `sqlite3 vbm.db "select distinct model_ver from chunks"` → expected single version.
14. **Perf budget** — 2000 pages indexed → daemon RSS <400MB, sqlite file <200MB, zero browser jank during background embed (daemon owns all CPU hotspots).

No automated tests blocking v0.1. Add Go unit tests for: chunker, denylist matcher, hybrid search ranking (RRF fusion), NM stdio protocol framing, bearer auth middleware.

---

## Decisions locked in (2026-04-11)

- **Daemon language**: Go
- **IPC**: hybrid — Native Messaging for handshake, localhost HTTP for bulk, WebSocket for status push
- **OS scope for v0.1**: Linux only (macOS → v0.2, Windows → v0.2+)
- **Snapshots in MVP**: deferred to v0.2
- **LLM synthesis in MVP**: deferred to v0.2 (ranked hits only in v0.1)
- **Cross-device sync in MVP**: deferred to v0.3
