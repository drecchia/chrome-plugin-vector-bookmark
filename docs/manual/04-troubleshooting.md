# 4. Troubleshooting & FAQ

---

## 4.1 Symptom matrix

| Symptom | First thing to check |
|---|---|
| Badge is **red** | Daemon is down — see 4.2 |
| Badge is **yellow** on every site | The hostname matches your blacklist |
| Badge stays grey, never goes blue | Dwell threshold not reached, or the tab is incognito |
| Page indexed but not in search results | Stub embedder + word never appears in BM25; configure embeddings (see 4.5) |
| `/ingest` returns 503 with `mode=llm_summary` | `VBM_LLM_MODEL` / `VBM_EMBED_URL` not set; see [02-configuration.md](02-configuration.md) |
| Every request returns `401 unauthorized` | Daemon has `VBM_AUTH_TOKEN` set — paste the same value into popup → Settings → Auth token, or unset it on the daemon side |
| Daemon UI (`/ui`) loads but every panel is empty / 401 | Open the page once with `?token=<your-token>` (e.g. `http://host:7532/?token=abc`). The UI captures the token into `sessionStorage`, strips it from the URL, and injects it into every subsequent request. If the token expires or you skip the URL param, the UI prompts you for it on the first 401 |
| ✨ Suggest tags returns "no LLM configured" | Same — `VBM_LLM_MODEL` missing |
| Re-embed button not visible | No pages indexed yet — the button only shows once `status.indexed > 0` |
| Daemon won't start in Docker | See 4.3 |
| `npm run build` fails on the extension | See 4.4 |

---

## 4.2 Daemon offline

```bash
# Is the container running?
docker compose ps

# Is the port reachable?
curl http://127.0.0.1:7532/healthz

# Logs
docker compose logs --tail 100 vbmd
```

On native installs:

```bash
systemctl --user status vbmd
journalctl --user -u vbmd -n 50

# If "User not lingering", the systemd-user instance dies on logout:
sudo loginctl enable-linger $USER
```

If the popup still says offline after the daemon is up, double-check that the **port in the popup settings matches `VBM_PORT`** (default 7532).

---

## 4.3 Container won't start

Common causes:

- **Port already in use.** Another process is bound to `127.0.0.1:7532`. Find it: `ss -ltnp 'sport = :7532'`. Either stop it or change `VBM_PORT` and the host-side port in `docker-compose.yml`.
- **Volume permission denied.** The container's user can't write to `~/.local/share/vbm`. Fix with `chmod 700 ~/.local/share/vbm` (the directory is created on first boot; if you precreated it as root, recreate it with your user).
- **Image build fails.** Run `docker compose build --no-cache vbmd` and read the actual error. Most failures are transient network errors against Go module proxies.

---

## 4.4 Extension build fails

```bash
cd extension
rm -rf node_modules dist
npm install
npm run typecheck       # surfaces any TS error fast
npm run build
```

If `typecheck` is green but `build` fails, the issue is usually a Vite plugin version mismatch — pin to the versions in `package.json` (`@crxjs/vite-plugin@^2.0.0-beta.29`).

---

## 4.5 Search is shallow / misses obvious matches

Two layers, two failure modes:

1. **No embeddings configured.** Open the popup → if you see **Re-embed pages**, click it after configuring `VBM_EMBED_URL`. Until that runs, every query is BM25 only.
2. **The page was indexed in `meta_only` mode.** Body text isn't searchable. Re-index from the popup with `full_text` mode.

For very large indexes (>50k chunks), brute-force cosine starts to feel slow. Adding an HNSW or IVF index to SQLite is on the roadmap; for now, prune with `VBM_TTL_DAYS` or manual `Forget` calls.

---

## 4.6 "I switched embedding providers — now what?"

Pages indexed against the old provider have embeddings in the **old** vector space. Cosine across spaces is meaningless. Two options:

- **Re-embed everything** with the new provider via the popup button. Cost is one embedding per chunk.
- **Wipe and start over**: `docker compose down && rm -rf ~/.local/share/vbm/`. Faster if your index is small.

The `model_ver` column on every chunk records which model produced its embedding, so the daemon can detect mixes — but the public re-embed path treats it as all-or-nothing.

---

## 4.7 Resetting everything

```bash
# Stop daemon
docker compose down       # or: systemctl --user stop vbmd

# Remove all indexed data
rm -rf ~/.local/share/vbm/

# Remove extension settings (in Chrome)
chrome://extensions → Vector Bookmark → Details → Site settings → Clear data
# Or: uninstall and reinstall the extension.

# Bring it back up
docker compose up -d
```

This is irreversible and loses every captured page, tag, and blacklist entry.

---

## 4.8 FAQ

**Does any of my data leave the machine?**
Not unless you configured `VBM_EMBED_URL`. Without it, the daemon uses the stub embedder and nothing reaches the network. With it, page text is sent to that endpoint for embeddings, summaries (`llm_summary` mode only), and tag suggestions (✨). Other captured pages are not eagerly sent.

**Is incognito captured?**
No. The manifest declares `incognito: "not_allowed"` and that is **not** user-configurable.

**Are passwords or credit card fields captured?**
The content script never reads `<input type="password">`. Field-aware redaction is best-effort for credit-card-shaped fields.

**Can I export my data?**

```bash
# Raw database backup
cp ~/.local/share/vbm/vbm.db ~/backup-$(date +%F).db

# Structured JSON (no embeddings)
curl -s http://127.0.0.1:7532/export > export.json
```

**Can two users on the same machine see each other's data?**
Each user has their own `~/.local/share/vbm/` and their own daemon. The directory inherits home-dir permissions (typically `700`). A user with `sudo` can read it — Vector Bookmark is not a defense against root.

**How long is data retained?**
Indefinitely by default. Set `VBM_TTL_DAYS=N` to enable a daily sweep that deletes pages older than N days.

**What happens if the daemon crashes mid-ingest?**
The ingest queue (cap 256) drains on graceful shutdown (SIGTERM, 30s timeout). A `SIGKILL` loses anything in flight. SQLite runs in WAL mode, so the database itself does not corrupt.

**Can I run two instances of `vbmd` on the same machine?**
Yes — set different `VBM_PORT` and `VBM_DATA_DIR` for each. Set the corresponding port in each Chrome profile's extension settings.

**Where do I report bugs?**
<https://github.com/drecchia/chrome-plugin-vector-bookmark/issues>
