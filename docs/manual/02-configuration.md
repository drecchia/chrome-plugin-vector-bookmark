# 2. Advanced configuration

If you followed [01-installation.md](01-installation.md) you already have a working setup with OpenRouter. This page covers the **rest** of the knobs:

- Switching to fully-local embeddings (Ollama).
- The complete environment-variable reference.
- Custom prompts for summaries and tag suggestions.
- Extension-side settings.

You don't need any of this to get started. Skip whatever doesn't apply.

---

## 2.1 The env file (native install only)

When running the daemon **outside** Docker, env vars come from a file the daemon reads on startup:

- Linux / WSL: `~/.config/vbm/env`
- Windows: `%APPDATA%\vbm\env`

Format is `KEY=value`, one per line, no quoting. Comments with `#` are allowed. Restart the daemon after edits (`systemctl --user restart vbmd`).

Inside Docker, env vars come from the `environment:` block of `docker-compose.yml` — this section does not apply.

---

## 2.2 Switching models

Anything OpenAI-compatible works. To change models, edit the `VBM_EMBED_MODEL` and `VBM_LLM_MODEL` values and recreate the container.

Some alternatives on OpenRouter:

| Use case | Model | Cost (per 1M tokens) |
|---|---|---|
| Higher-quality embeddings | `openai/text-embedding-3-large` | US$ 0.13 |
| Higher-quality summaries | `openai/gpt-4o` | US$ 2.50 in / US$ 10 out |
| Cheaper everything | `mistralai/mistral-small` + `mistralai/mistral-embed` | < US$ 0.10 |

> **If you switch the embedding model after indexing pages**, the existing embeddings live in the *old* vector space — cosine across spaces is meaningless. Open the popup → **Re-embed pages (semantic search)** to re-process them with the new model. For tiny indexes it's faster to wipe and reindex.

---

## 2.3 Alternative — Ollama (fully local)

If you don't want any page text leaving your machine, run Ollama locally:

```bash
curl -fsSL https://ollama.com/install.sh | sh
ollama pull nomic-embed-text
ollama pull llama3.2:3b      # for summaries / tag suggestions
```

For Docker, edit your `docker-compose.yml` env block:

```yaml
environment:
  # Use host.docker.internal on Docker Desktop; on Linux native Docker
  # add: extra_hosts: ["host.docker.internal:host-gateway"]
  VBM_EMBED_URL: "http://host.docker.internal:11434/api/embeddings"
  VBM_EMBED_MODEL: "nomic-embed-text"
  VBM_LLM_MODEL: "llama3.2:3b"
  # Drop VBM_EMBED_API_KEY — Ollama doesn't auth
```

For native installs, edit `~/.config/vbm/env`:

```
VBM_EMBED_URL=http://127.0.0.1:11434/api/embeddings
VBM_EMBED_MODEL=nomic-embed-text
VBM_LLM_MODEL=llama3.2:3b
```

Restart the daemon and run **Re-embed pages** in the popup if you're switching from a previous provider.

> **Tradeoff.** Quality is below `text-embedding-3-small` and a full-size LLM, especially for tag suggestions. The privacy gain is real if your threat model demands it; the CPU/GPU cost is non-trivial — expect noticeable load while ingesting.

---

## 2.4 All daemon environment variables

The daemon prints all of these in its startup banner so you can confirm what loaded.

| Variable | Default | Purpose |
|---|---|---|
| `VBM_PORT` | `7532` | TCP port the daemon binds to |
| `VBM_BIND` | `127.0.0.1` | Bind address. `0.0.0.0` is **only** acceptable inside a container with a loopback-only host port mapping |
| `VBM_DATA_DIR` | `~/.local/share/vbm` | Override data directory. Used in containers (distroless lacks `$HOME`) |
| `VBM_EMBED_URL` | unset | Embedding endpoint URL. Unset → stub embedder (BM25-only search) |
| `VBM_EMBED_API_KEY` | unset | Bearer token for `VBM_EMBED_URL` |
| `VBM_EMBED_MODEL` | unset | Model name passed in the embed payload |
| `VBM_LLM_MODEL` | unset | Chat model for summaries (`llm_summary` ingest mode) and tag suggestions (✨) |
| `VBM_TTL_DAYS` | unset | Auto-delete pages older than N days (LGPD/GDPR-friendly) |
| `VBM_LOG_LEVEL` | `info` | One of `debug`, `info`, `warn`, `error` |
| `VBM_LLM_PROMPT_SUMMARIZE_FILE` | unset | Path to a markdown file overriding the built-in summarize prompt |
| `VBM_LLM_PROMPT_SUGGEST_TAGS_FILE` | unset | Path to a markdown file overriding the built-in tag-suggest prompt |
| `VBM_AUTH_TOKEN` | unset | Bearer token required on all routes except `/healthz` and `/metrics`. Unset → open access (safe for `127.0.0.1`-only setups) |

---

## 2.5 Auth token (publicly exposed deployments)

The default bind is `127.0.0.1`, so on a single-user machine no auth is needed and `VBM_AUTH_TOKEN` should stay unset. **The moment the daemon is reachable from anywhere other than your loopback** — Docker with a non-loopback port mapping, K8s `Service`, reverse proxy, tunnels — set this:

```
VBM_AUTH_TOKEN=<long-random-string>
```

Generate one once and reuse it (`openssl rand -hex 32`). Then enter the same value in the extension popup → Settings → **Auth token**.

How it's enforced:

- HTTP: clients must send `Authorization: Bearer <token>`.
- WebSocket (`/ws`): browsers can't set custom headers on the handshake, so `?token=<token>` is also accepted.
- Daemon UI (`/`, `/ui`): open it once with `?token=<token>` in the URL — the UI captures the token into `sessionStorage`, strips it from the address bar, and injects `Authorization` on every subsequent fetch. If you forget the URL param the UI prompts on the first 401.
- `/healthz` and `/metrics` stay public so probes/scrapers don't break.

Caveat: a `?token=` query string can leak into HTTP access logs and proxy logs. The daemon itself only logs request paths, but if you front it with another proxy, audit its log policy.

---

## 2.6 Custom prompts

The summarize and tag-suggest prompts are externalized so you can tune the voice without rebuilding the daemon:

```bash
cat > ~/.config/vbm/summarize.md <<'EOF'
You are a concise note-taker. Produce a 2-paragraph summary of the page below.
Focus on the actionable claim, not the marketing fluff.
EOF
```

Then point to it (native install env file shown — for Docker, mount the file and reference its in-container path):

```
VBM_LLM_PROMPT_SUMMARIZE_FILE=/home/you/.config/vbm/summarize.md
```

`VBM_LLM_PROMPT_SUGGEST_TAGS_FILE` works the same way. The daemon falls back to the embedded default if the file path is missing or unreadable. Restart the daemon after changing prompt files.

---

## 2.7 Extension settings

Open the popup and find the **Settings** section:

| Setting | Default | What it controls |
|---|---|---|
| Daemon host | `127.0.0.1` | Where the extension looks for `vbmd` |
| Daemon port | `7532` | Match this with `VBM_PORT` |
| Auth token | (empty) | Match this with `VBM_AUTH_TOKEN`. Leave empty when the daemon has no token configured |
| Dwell threshold (ms) | `10000` | Minimum visible time before a page is auto-indexed |
| Blacklist | (empty) | Hostname patterns the extension never sends to the daemon |

Settings persist in `chrome.storage.local` and apply immediately — the content script hot-reloads the dwell threshold without a tab refresh.
