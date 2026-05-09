# Vector Bookmark — User Manual

Semantic memory for everything you read on the web. Find pages by **meaning**, not just exact words — locally on your machine.

> **Quick example.** You read an article comparing `tokio` vs. `async-std` three weeks ago and forgot to bookmark it. Just type `@recall tokio async-std comparison` in the Chrome address bar — the article comes back.

---

## How it works

```
┌─────────────────┐    localhost HTTP    ┌────────────────────┐
│  Chrome         │ ──────────────────▶  │  vbmd daemon       │
│  Extension      │                      │  (Go process)      │
│  (capture + UI) │                      │  SQLite + BM25 +   │
└─────────────────┘                      │  vector index      │
                                         └────────────────────┘
```

Two pieces, both running on your own machine:

- **Chrome extension** — observes the pages you read for ≥10 seconds, sends them to the daemon, exposes search via popup and `@recall` omnibox.
- **`vbmd` daemon** — Go process, holds the SQLite database, runs hybrid search (BM25 + vector embeddings, fused with RRF), serves a local web UI.

No accounts, no cloud, no telemetry. The daemon talks to an OpenAI-compatible LLM endpoint of your choice — OpenRouter (default in this guide), Ollama for fully local, or any other provider.

---

## Quick start (5 minutes)

You need **one** thing before starting: an OpenRouter API key (free signup, ~US$ 1 covers thousands of pages). Get one at <https://openrouter.ai/>.

### 1. Save a `docker-compose.yml`

```yaml
services:
  vbmd:
    image: drecchia/vbmd:latest
    container_name: vbmd
    ports:
      - "127.0.0.1:7532:7532"
    volumes:
      - ${HOME}/.local/share/vbm:/data
    environment:
      VBM_EMBED_URL: "https://openrouter.ai/api/v1/embeddings"
      VBM_EMBED_API_KEY: "sk-or-PASTE-YOUR-KEY-HERE"
      VBM_EMBED_MODEL: "openai/text-embedding-3-small"
      VBM_LLM_MODEL: "openai/gpt-4o-mini"
    restart: unless-stopped
```

**Edit the `VBM_EMBED_API_KEY` line** with your OpenRouter key before saving.

### 2. Start the daemon

```bash
docker compose up -d
curl http://127.0.0.1:7532/healthz   # → {"ok":true}
```

### 3. Install the extension

Chrome Web Store → search "Vector Bookmark" → **Add to Chrome**. Pin to the toolbar.

### 4. Use it

Browse normally. After 10s on a page, the badge turns green — that page is now indexed with full semantic search. Search via the popup or type `@recall <query>` in the address bar.

---

## Manual sections

| File | What's in it |
|---|---|
| [01-installation.md](01-installation.md) | Step-by-step install with config baked in (Docker, native, Windows) |
| [02-configuration.md](02-configuration.md) | Advanced: Ollama alternative, full env-var table, custom prompts |
| [03-usage.md](03-usage.md) | Daily use: popup, ingest modes, tags, search, omnibox, timeline |
| [04-troubleshooting.md](04-troubleshooting.md) | When things break: badge states, logs, FAQ, full reset |

---

## Privacy at a glance

- Incognito tabs are **never** captured (declared in the manifest, not configurable).
- Password fields and known login/checkout URLs are excluded.
- The user-managed blacklist blocks any host you don't want indexed.
- The daemon binds to `127.0.0.1` only — nothing accepts external connections.
- Page text is sent to your configured LLM endpoint **only** for the operations you trigger (embeddings, summaries, tag suggestions). Stay 100% local with Ollama if your threat model demands it — see [02-configuration.md](02-configuration.md).

Full policy: [PRIVACY.md](../../PRIVACY.md).
