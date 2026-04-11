# Product Requirements Document — Vector Bookmark

| Field | Value |
|---|---|
| **Product** | Vector Bookmark (working title; codename `vbm`) |
| **Document** | PRD v0.1 |
| **Status** | Draft — pre-implementation |
| **Date** | 2026-04-11 |
| **Related** | [`PLAN.md`](./PLAN.md) — technical implementation plan |

---

## 1. Executive Summary

Vector Bookmark is a personal browsing-memory tool that replaces traditional bookmarks with automatic, semantic, time-aware recall. A thin Chrome extension captures the pages a user actually reads (passive capture with dwell gating), and a native daemon installed on the user's machine owns all the heavy lifting: text extraction, embedding, vector storage, full-text indexing, and (in later releases) page snapshots, LLM-assisted synthesis, and end-to-end-encrypted cross-device sync.

The product is **local-first by design**. v0.1 runs entirely on a single Linux machine with zero server dependency. Remote sync is a deliberate v0.3 milestone, not a day-one feature.

---

## 2. Problem Statement

Modern knowledge workers read hundreds of web pages a week but have no reliable way to retrieve what they've already read:

- **Chrome History** is keyword-only, shallow, and invisible past the last few days.
- **Manual bookmarks** depend on the user remembering to save the page — and the page they'll want in six weeks is rarely the one they thought to bookmark.
- **Read-later tools** (Pocket, Raindrop, Instapaper) have the same "explicit save" problem.
- **Note-taking tools** (Notion, Obsidian, Heptabase) optimize for writing, not recall over historical reading.
- **Rewind.ai-class products** capture everything at the OS level, are macOS-only, expensive, and carry heavy privacy concerns.

The gap is a **browser-scoped, semantic-recall-first, local-first, privacy-respecting memory** that answers questions like *"that article about tokio vs async-std I half-read about a month ago"* — without requiring the user to have remembered to save anything.

---

## 3. Goals and Non-goals

### 3.1 Goals
1. Make browsing history **semantically searchable** — retrieve pages by meaning, not just keywords.
2. **Zero-effort capture** — the user does nothing beyond installing the tool; dwell-gated passive indexing handles the rest.
3. **Privacy by default** — sensitive domains are never captured, the user can pause and forget at any time, and no data leaves the machine in v0.1.
4. **Sub-second recall** — queries over a realistic personal corpus return results with p95 latency under 500 ms.
5. **Local-first, sync-ready** — the data model, embedding pipeline, and storage layout are designed so that v0.3 sync can be added without reshaping v0.1 data.

### 3.2 Non-goals
- Replacing note-taking tools, read-later queues, or bookmark managers for collaborative use.
- Capturing non-browser activity (OS screens, terminal, desktop apps). This is a browser-scoped tool.
- Supporting enterprise/workplace deployments in v0.1 (the EULA is "personal use only"; enterprise monitoring laws are out of scope).
- Running on mobile browsers.
- Shipping a cloud service in v0.1 — the sync server is v0.3 and will be self-hostable from day one.

---

## 4. Target Users and Personas

### 4.1 Primary persona — "The Independent Researcher"
- Self-employed or embedded in a small team; works alone most days.
- Reads 50–200 web pages per working day across news, docs, GitHub, papers, and blogs.
- Already lives in dozens of open tabs; has given up on manual bookmarking.
- Trusts local-first software, skeptical of cloud tools that index personal data.
- Comfortable installing a CLI tool or running a shell script.

### 4.2 Secondary personas (considered, not prioritized for v0.1)
- Technical writers and journalists needing to recall sources.
- Academic researchers doing literature review.
- Senior software engineers tracking API docs, RFCs, and incident post-mortems across time.

### 4.3 Explicitly out-of-scope users (v0.1)
- Students (price-sensitive, short-horizon use).
- Enterprise knowledge workers under managed Chrome policies.
- Non-technical users who can't run a shell installer.

---

## 5. Jobs to Be Done

| # | Job | Current Workaround | Success Looks Like |
|---|---|---|---|
| JTBD-1 | "Find that thing I half-read N weeks ago" | Scroll Chrome history, give up, re-Google | Fuzzy semantic query returns the article in top-3, under 500 ms |
| JTBD-2 | "Show me everything I've read about topic X" | Grep notes, open 10 tabs | Clustered, time-ordered result list with snippets |
| JTBD-3 | "Don't record sensitive stuff silently" | Use incognito manually | Automatic exclusion + visible recording indicator |
| JTBD-4 | "Forget this page / domain / time range" | Delete Chrome history manually | One-click forget, propagates through vector index |
| JTBD-5 | "Recall something the page no longer shows" (v0.2) | Wayback Machine, hope it's indexed | Local snapshot kept for starred pages |
| JTBD-6 | "Ask my reading history a question" (v0.2) | Paste text into an LLM chat manually | Local LLM synthesis with cited sources |
| JTBD-7 | "Use the same memory from my other machine" (v0.3) | Browser sync (keyword only) | E2EE sync across devices |

---

## 6. Functional Requirements

Each requirement is tagged `FR-n`, with a target milestone in brackets.

### 6.1 Capture
- **FR-1** [v0.1] The extension MUST capture URL, title, extracted main text, visit timestamp, and dwell time for every page where dwell > 30 s (configurable).
- **FR-2** [v0.1] Text extraction MUST use Readability.js; non-article pages MUST fall back to a safe DOM walk.
- **FR-3** [v0.1] The system MUST deduplicate captures keyed by `sha1(normalized_text)` so that revisits do not bloat the index.
- **FR-4** [v0.1] The extension MUST perform URL canonicalization (strip tracking params, fragment handling) before sending to the daemon.
- **FR-5** [v0.2] On user star, the extension MUST capture a SingleFile HTML snapshot and POST it to the daemon for long-term storage.
- **FR-6** [v0.2] The system MUST support PDF text extraction and YouTube transcript capture as opt-in content types.

### 6.2 Indexing and Search
- **FR-7** [v0.1] The daemon MUST chunk extracted text into 512-token windows with 64-token overlap, skipping chunks shorter than 40 tokens.
- **FR-8** [v0.1] The daemon MUST embed each chunk using a local ONNX model (`Snowflake/snowflake-arctic-embed-xs`) and store the vector alongside the chunk.
- **FR-9** [v0.1] The daemon MUST support hybrid search combining BM25 (via SQLite FTS5) and dense cosine similarity (via sqlite-vec), fused with Reciprocal Rank Fusion (k=60).
- **FR-10** [v0.1] Every chunk row MUST carry a `model_ver` stamp so future re-embeds can migrate incrementally.
- **FR-11** [v0.2] The daemon MAY support LLM-assisted answer synthesis over retrieved chunks, with citations back to source URLs.

### 6.3 Search Surfaces (Extension)
- **FR-12** [v0.1] The extension MUST expose an omnibox keyword (`@recall <query>`) that forwards to the daemon and renders top-5 suggestions with snippet, domain, and visit date.
- **FR-13** [v0.1] The extension popup MUST show: current recording state, per-domain opt-in status, a pause control, a "forget" shortcut, and a link to the daemon's local web UI.
- **FR-14** [v0.1] The daemon MUST serve a minimal local web UI at `http://127.0.0.1:$PORT/ui` with list, search, and forget-panel capabilities.
- **FR-15** [v0.2] The local web UI MUST add a dwell-weighted timeline heatmap and topic-cluster browsing.

### 6.4 Control and Deletion
- **FR-16** [v0.1] The user MUST be able to delete captures by URL, domain, regex, and time range. Deletion MUST tombstone metadata AND physically remove the corresponding rows from the sqlite-vec virtual table.
- **FR-17** [v0.1] The user MUST be able to pause recording for 1 hour, 1 day, or indefinitely with a single click.
- **FR-18** [v0.1] The first capture attempt on a new eTLD+1 MUST prompt the user to opt-in; no silent global opt-in is allowed.

### 6.5 Sync (v0.3)
- **FR-19** [v0.3] The daemon MUST support syncing captures, metadata, and snapshots to a self-hostable remote server under client-side end-to-end encryption.
- **FR-20** [v0.3] Sync MUST use an append-only event log with HLC timestamps and content-addressed blob storage; embeddings MUST be recomputed per device rather than shipped across devices.
- **FR-21** [v0.3] The remote server MUST be distributable as a single `docker-compose up` self-host bundle.

---

## 7. Non-Functional Requirements

| # | Area | Requirement | Milestone |
|---|---|---|---|
| NFR-1 | **Latency** | Search p50 ≤ 200 ms, p95 ≤ 500 ms over a 500-chunk index | v0.1 |
| NFR-2 | **Throughput** | Ingest ≥ 20 pages/min without browser jank | v0.1 |
| NFR-3 | **Footprint (daemon)** | RSS ≤ 400 MB with 2,000 pages indexed | v0.1 |
| NFR-4 | **Footprint (SQLite file)** | ≤ 200 MB per 2,000 pages (text + vectors, no snapshots) | v0.1 |
| NFR-5 | **Install UX** | Single command installs daemon + systemd unit + NM manifest | v0.1 |
| NFR-6 | **Cold start** | Daemon ready to accept `/ingest` ≤ 2 s after Chrome-triggered launch | v0.1 |
| NFR-7 | **Reliability** | Durable ingest queue survives daemon crash; no lost captures after ACK | v0.1 |
| NFR-8 | **Portability** | v0.1 runs on any mainstream x86_64 Linux with Chrome | v0.1 |
| NFR-9 | **Extensibility** | Daemon API stable enough to support a Firefox/Arc extension without data migration | v0.2 |
| NFR-10 | **Sync bandwidth** | Initial sync of 5,000 pages ≤ 50 MB transferred (text, not snapshots) | v0.3 |

---

## 8. UX and Interaction Surfaces

### 8.1 Extension surfaces
- **Toolbar badge** — visible recording indicator (red = recording, gray = paused). Single-click opens popup.
- **Popup** — pause toggle, current domain opt-in status, forget shortcut, "open full UI" button.
- **Omnibox keyword** (`@recall`) — primary quick-search path. Zero friction, muscle-memory compatible with Chrome.
- **Content script** — invisible; handles extraction and password-field pause.

### 8.2 Daemon-hosted local web UI
- **v0.1** — list, search (with snippet + domain + date), forget panel, quota view.
- **v0.2** — timeline heatmap, topic clusters, starring/notes, snapshot viewer, LLM answer mode.

### 8.3 First-run flow
1. User runs `./install.sh`. Script installs the daemon binary, NM host manifest, and systemd user unit; starts the daemon.
2. User loads the unpacked extension. Popup shows "Connected to daemon (port N)".
3. User visits first page. Extension prompts: "Start capturing pages from `example.com`?" — opt-in per eTLD+1.
4. User sees the recording badge turn red. Capture begins after 30 s dwell.

---

## 9. Architecture Overview

See [`PLAN.md`](./PLAN.md) for full technical detail. One-paragraph summary:

Vector Bookmark is split into a **thin Chrome MV3 extension** (TypeScript, Vite + CRXJS) and a **native Go daemon** (single static binary) that communicate over a hybrid local IPC channel — Chrome Native Messaging (stdio) for privileged handshake, localhost HTTP with a bearer token for bulk data flow, and localhost WebSocket for server-push status. The extension handles capture (Readability.js extraction, denylist, password-field pause) and display (omnibox, popup); the daemon handles embedding (ONNX runtime, arctic-embed-xs), storage (SQLite + sqlite-vec + FTS5), hybrid search (BM25 + dense, RRF fusion), and the local web UI (embedded via `go:embed`). All data stays on the user's machine in v0.1.

---

## 10. Privacy, Security and Compliance Requirements

This section is **binding**; no v0.1 ship without meeting every item.

### 10.1 Capture exclusions (enforced in the extension, before data leaves the tab)
- **PSR-1** Default-deny domain denylist shipped with the extension: banks, brokerages, `.gov`, health portals, `accounts.*`, `login.*`, password managers, OAuth/SAML flows, webmail, known SSO providers.
- **PSR-2** Runtime detection of sensitive pages via `<input type=password>`, `autocomplete=one-time-code`, WebAuthn API usage, and known-sensitive URL patterns.
- **PSR-3** Incognito windows: extension declares `"incognito": "not_allowed"` in its manifest.
- **PSR-4** Capture is paused while a password or credit-card field is focused or filled on the active page.

### 10.2 Consent and control
- **PSR-5** Per-eTLD+1 opt-in prompt on first capture attempt. No silent opt-ins.
- **PSR-6** A visible recording indicator must always be present in the toolbar.
- **PSR-7** A one-click pause must be available from the popup at all times.
- **PSR-8** A "forget" operation must support URL, domain, regex, and time-range scopes and must physically remove the targeted rows from both the metadata and the vector index.

### 10.3 IPC security (daemon-specific)
- **PSR-9** The daemon HTTP server MUST bind `127.0.0.1` only. Startup MUST abort if a non-loopback bind is attempted.
- **PSR-10** Every non-`/healthz` route MUST require `Authorization: Bearer <token>`, where the token is provisioned via the Native Messaging handshake and rotates per browser session.
- **PSR-11** CORS MUST reject any `Origin` other than the extension's `chrome-extension://<id>/`. No wildcards.
- **PSR-12** The daemon MUST refuse to start if another process is already bound to its target port.
- **PSR-13** Bearer tokens MUST NOT be persisted to `chrome.storage` or any on-disk location; they live only in service-worker memory on the extension side.

### 10.4 LLM data handling (v0.2)
- **PSR-14** Local LLM (via Ollama bridge) is the default synthesis backend.
- **PSR-15** If a cloud LLM is enabled, each query MUST explicitly show the user the chunks about to be sent and require confirmation.
- **PSR-16** A PII redaction pass MUST run over outbound chunks before any cloud LLM call.

### 10.5 Cross-device sync (v0.3)
- **PSR-17** The sync server MUST see ciphertext only; all snapshots, text, and metadata MUST be encrypted client-side with XChaCha20-Poly1305.
- **PSR-18** Key material MUST be derived from a user passphrase via Argon2id. Loss of passphrase = loss of data, stated explicitly in UX.
- **PSR-19** Sync is opt-in. Local-only remains the default configuration.

### 10.6 Compliance posture
- **PSR-20** Personal-use EULA only. Enterprise and managed-Chrome deployment are out of scope.
- **PSR-21** GDPR: user is data subject and controller — supported via the forget operation (Article 17 equivalent).
- **PSR-22** No telemetry, no analytics, no crash reports phone home in v0.1.

---

## 11. Out of Scope (v0.1)

- Page snapshots (HTML archives, screenshots) — deferred to v0.2.
- LLM-generated answers — deferred to v0.2.
- Cross-device sync and remote server — deferred to v0.3.
- Topic cluster visualization and dwell-weighted timeline — deferred to v0.2.
- macOS and Windows support — deferred to v0.2+.
- PDFs, YouTube transcripts, Gmail, Google Docs — deferred to v0.2+.
- Reranker model — added only if recall quality proves insufficient.
- Firefox, Arc, Safari extensions — API is designed to accommodate them, but ports are post-v0.3.
- Mobile browser capture.

---

## 12. Release Plan

### v0.1 — "Local semantic recall, Linux only"
Capture, extract, embed, store, search. Extension + daemon + installer. Linux only. Minimal web UI. No snapshots, no LLM, no sync.

**Exit criteria**: every functional requirement tagged [v0.1] met; every NFR met; every PSR satisfied; the dogfood checklist in `PLAN.md` §Verification passes end-to-end.

### v0.2 — "Snapshots, LLM, richer UI, macOS"
SingleFile snapshots for starred pages. Timeline and cluster views in the local web UI. Ollama bridge for local LLM synthesis; BYO cloud key opt-in with redaction. PDF/YouTube capture. macOS installer (unsigned, then notarized).

### v0.3 — "Cross-device sync, self-host"
Self-hostable sync server (Go + Postgres + pgvector + object store). Client-side E2EE. Event-log sync with content-addressed blobs. `docker-compose.yml` first-class self-host. Windows installer.

---

## 13. Success Metrics

The product is local-first and has no telemetry; metrics are evaluated by the dogfood user against their own index.

| Metric | Target (v0.1) | Target (v0.2) |
|---|---|---|
| Fuzzy-recall top-3 success rate on a held-out test set of 25 "I remember reading X" queries | ≥ 70% | ≥ 85% |
| Search p95 latency over 2,000-page index | ≤ 500 ms | ≤ 500 ms |
| Pages captured per week by the dogfood user without manual effort | ≥ 200 | ≥ 400 |
| False-positive captures on sensitive domains | 0 | 0 |
| Daily user intent satisfied without fall-through to Google/Chrome History | ≥ 3× | ≥ 5× |
| Install-to-first-successful-query time | ≤ 10 min | ≤ 5 min |

---

## 14. Risks and Open Questions

### 14.1 Top risks (see `PLAN.md` §Top risks for full detail)
1. **Installation friction** — two-artifact install is a known drop-off point. Mitigation: single shell script, Native Messaging cold-start, idempotent installer.
2. **Silent capture of auth/banking/health** — legal and trust killer. Mitigation: extension-side denylist + password-field pause + WebAuthn detector, hard-gated for v0.1.
3. **Embedding model drift** — model upgrades invalidate the index. Mitigation: `model_ver` stamps + background re-embed worker.
4. **IPC hijack** — another local process could hit the daemon port. Mitigation: bearer token, origin check, loopback-only bind.
5. **Storage bloat (v0.2)** — snapshots will fill disk. Mitigation: TTL + per-domain quota in the daemon from day one.
6. **Cross-browser single-implementation risk** — if the daemon API isn't clean, a Firefox port will require rewrites. Mitigation: treat the daemon HTTP API as a public interface from v0.1.

### 14.2 Open questions (to be resolved during or after v0.1)
- **OQ-1** Does arctic-embed-xs quality hold up for non-English pages? If not, switch to a multilingual embedder and eat the re-embed cost.
- **OQ-2** What's the right default dwell threshold? 30 s is a guess; tune from dogfood data.
- **OQ-3** How much text per page should we index? Readability output can be 50 KB+ on long articles — chunk budget per page may need a cap.
- **OQ-4** For v0.3, does the sync server host the re-embed compute, or does each device re-embed independently? (Current plan: each device re-embeds.)
- **OQ-5** Do we surface a "privacy report" in the popup (what we captured today, which domains, which were blocked)? Likely yes, v0.2.

---

## 15. Decisions Locked In (2026-04-11)

- **Daemon language**: Go (single static binary, cross-compile, decent ONNX story)
- **IPC**: hybrid — Native Messaging for handshake, localhost HTTP for bulk, WebSocket for status push
- **OS scope for v0.1**: Linux only (macOS → v0.2, Windows → v0.2+)
- **Snapshots in MVP**: deferred to v0.2
- **LLM synthesis in MVP**: deferred to v0.2 (ranked hits only in v0.1)
- **Cross-device sync in MVP**: deferred to v0.3
- **Architecture**: native daemon + thin extension (not browser-only MV3)

---

## 16. Glossary

| Term | Meaning |
|---|---|
| **Chunk** | A 512-token window of extracted page text; the atomic unit of embedding and retrieval. |
| **Dwell** | Time the user spends actively on a page; capture is gated by a minimum dwell threshold. |
| **eTLD+1** | Effective top-level domain plus one label (e.g. `example.co.uk`). Used for opt-in scoping. |
| **FTS5** | SQLite's full-text-search extension; powers the BM25 leg of hybrid search. |
| **HLC** | Hybrid Logical Clock; provides monotonic timestamps for event-log sync. |
| **NM (Native Messaging)** | Chrome API that lets an extension spawn and communicate with a native host process over stdio. |
| **RRF** | Reciprocal Rank Fusion; combines rankings from BM25 and dense retrieval. |
| **sqlite-vec** | SQLite loadable extension providing vector columns and nearest-neighbor search. |
| **SingleFile** | Technique/library for serializing a web page into a single self-contained HTML file. |
| **vbm / vbmd** | Project codename ("vector bookmark") and daemon binary name. |
