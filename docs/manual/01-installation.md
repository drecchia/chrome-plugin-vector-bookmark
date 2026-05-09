# 1. Installation

End state: a `vbmd` daemon running locally on port 7532, the Chrome extension connected to it, and **semantic search working from page one** — not in some second-step "now configure it" phase.

To get there in the right order:

1. Get an OpenRouter API key (1 minute).
2. Drop a `docker-compose.yml` with your key already filled in (1 minute).
3. `docker compose up -d` (10 seconds — the prebuilt image pulls from Docker Hub).
4. Install the Chrome extension.

That's the whole flow. Ollama, native install, and Windows are appendices at the end.

---

## 1.1 Get an OpenRouter API key

1. Sign up at <https://openrouter.ai/> (Google/GitHub login is fine).
2. Top up a few dollars. Typical usage costs cents per month — `text-embedding-3-small` is US$ 0.02 per 1M input tokens, `gpt-4o-mini` is US$ 0.15 / 1M input.
3. **Keys** → Create key → copy the `sk-or-...` value. Treat it like a password.

If you'd rather run everything offline (no key, no cloud), skip ahead to [§ 2.3 Ollama](02-configuration.md#23-alternative--ollama-fully-local) — the rest of this page assumes OpenRouter.

---

## 1.2 Save a `docker-compose.yml` with your config baked in

Create a working directory anywhere — `~/vbm/` is fine. Inside it, save this file as `docker-compose.yml`:

```yaml
services:
  vbmd:
    image: drecchia/vbmd:latest
    container_name: vbmd

    # Loopback only. Never expose this on a public interface without
    # adding auth in front.
    ports:
      - "127.0.0.1:7532:7532"

    # Persist vbm.db across container restarts. Same path the native
    # install uses, so a switch is a no-op for your data.
    volumes:
      - ${HOME}/.local/share/vbm:/data

    environment:
      # ---- EDIT THIS LINE: paste your OpenRouter key ----
      VBM_EMBED_API_KEY: "sk-or-PASTE-YOUR-KEY-HERE"

      # The two model + URL settings below are the recommended defaults.
      # Change them only if you understand the tradeoffs.
      VBM_EMBED_URL:   "https://openrouter.ai/api/v1/embeddings"
      VBM_EMBED_MODEL: "openai/text-embedding-3-small"
      VBM_LLM_MODEL:   "openai/gpt-4o-mini"

      # Optional — uncomment if you want to delete pages older than N days.
      # VBM_TTL_DAYS: "180"
      # VBM_LOG_LEVEL: "info"

    restart: unless-stopped

    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"
```

**Before saving, replace `sk-or-PASTE-YOUR-KEY-HERE` with your actual key.** This is the one and only mandatory edit.

> **Why pre-filled config instead of "run first, configure later"?** Without `VBM_EMBED_*` set, the daemon falls back to a stub embedder that returns zero vectors — search degrades to BM25 (keyword) only and the LLM features (`llm_summary` mode, ✨ tag suggestions) return `503`. By configuring on first boot you avoid the "why doesn't search work?" surprise and the re-embed step that comes with switching providers later.

---

## 1.3 Start the daemon

```bash
cd ~/vbm        # wherever you saved docker-compose.yml
docker compose up -d
```

What happens:

- Pulls `drecchia/vbmd:latest` from Docker Hub (one-time, ~30 MB).
- Starts the container with port `7532` mapped to `127.0.0.1:7532` (loopback only — never exposed to your LAN).
- Mounts `~/.local/share/vbm/` from the host into `/data` inside the container.

Verify:

```bash
curl http://127.0.0.1:7532/healthz
# → {"ok":true}

docker compose logs --tail 50 vbmd
# Look for: "embedder initialized" with model openai/text-embedding-3-small
# and "llm initialized" with model openai/gpt-4o-mini
```

If the logs show "stub embedder" instead, your env vars didn't take — re-check the spelling and re-run `docker compose up -d` (Compose recreates the container when env vars change).

---

## 1.4 Install the Chrome extension

### From the Chrome Web Store (preferred)

1. Open the [Chrome Web Store listing](https://chromewebstore.google.com/) and search "Vector Bookmark".
2. Click **Add to Chrome**.
3. Pin the extension to the toolbar.

### Unpacked (development / pre-store)

```bash
cd extension
npm install
npm run build           # output → extension/dist/
```

Then in Chrome:

1. Open `chrome://extensions/`.
2. Enable **Developer mode** (top right).
3. **Load unpacked** → select `extension/dist/`.
4. Pin the extension to the toolbar.

The icon should appear with a green status badge and a page counter (zero so far).

---

## 1.5 First sanity check

1. Open any article (e.g., a Wikipedia page).
2. Stay on the tab for at least **10 seconds**.
3. The badge turns blue ("visit recorded") and shortly after green ("indexed").
4. Open the popup → **Search** tab → type a related phrase (not the exact title — that would prove BM25 works, not embeddings). Example: visit a page about WASM, then search for "browser sandboxed runtime". The page should rank.
5. As an extra sanity check, type `@recall <phrase>` in the address bar — same results should appear as omnibox suggestions.

If something doesn't match this flow, jump to [04-troubleshooting.md](04-troubleshooting.md).

---

## 1.6 Day-to-day daemon commands

| Command | What it does |
|---|---|
| `docker compose up -d` | Start (or recreate after edits to compose) the daemon |
| `docker compose down` | Stop and remove the container — data persists in `~/.local/share/vbm/` |
| `docker compose restart` | Restart without recreating (after edits to env vars **already** in compose) |
| `docker compose logs -f vbmd` | Tail logs |
| `docker compose pull && docker compose up -d` | Pull a newer image and recreate the container |

To wipe everything:

```bash
docker compose down
rm -rf ~/.local/share/vbm/
```

---

## 1.7 Appendix — Native install (no Docker)

If you'd rather run the binary under `systemd --user`:

### Prerequisites

- Linux with `systemd` user instance (most desktop distros)
- Go 1.22+ to build, Node.js 20+ for the extension build

### Steps

```bash
git clone https://github.com/drecchia/chrome-plugin-vector-bookmark
cd chrome-plugin-vector-bookmark/daemon

make build              # binary → daemon/bin/vbmd

# Configure before installing — same env vars as the Docker case
mkdir -p ~/.config/vbm
cat > ~/.config/vbm/env <<'EOF'
VBM_EMBED_URL=https://openrouter.ai/api/v1/embeddings
VBM_EMBED_API_KEY=sk-or-PASTE-YOUR-KEY-HERE
VBM_EMBED_MODEL=openai/text-embedding-3-small
VBM_LLM_MODEL=openai/gpt-4o-mini
EOF

make install            # copies vbmd to ~/.local/bin and starts the systemd service

systemctl --user status vbmd
journalctl --user -u vbmd -f      # logs

# Make it survive logout
sudo loginctl enable-linger $USER
```

Uninstall:

```bash
cd daemon && make uninstall      # removes binary + service; data in ~/.local/share/vbm/ untouched
```

---

## 1.8 Appendix — Windows

Two supported paths:

- **WSL2 + Docker Desktop** — same instructions as 1.2 / 1.3, run from the WSL shell.
- **Native binary**: cross-compile from WSL with `./build-windows.sh`, then run `vbmd.exe server` from PowerShell. Data goes to `%APPDATA%\vbm\`. Env file lives at `%APPDATA%\vbm\env` with the same shape as the Linux version. There is no installer / service wrapper today; run from a terminal or use Task Scheduler.
