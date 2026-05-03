# Vector Bookmark

Semantic recall for everything you've read. Chrome extension + local Go daemon.

```
Extension ──HTTP──▶ vbmd (127.0.0.1:7532) ──▶ SQLite (BM25 + embeddings)
```

## Quick start

### Linux

```bash
./build-linux.sh   # build daemon + extension
./dev.sh           # build + start daemon (prints extension load path)
```

### Windows

```bash
# From WSL — cross-compile the exe:
./build-windows.sh
```

```powershell
# From PowerShell — start daemon and get load path:
.\dev.ps1
```

Load the extension: `chrome://extensions/` → **Load unpacked** → `extension/dist/`

---

## Config

Edit `~/.config/vbm/env` (Linux) or `%APPDATA%\vbm\env` (Windows). Loaded at startup.

```ini
VBM_PORT=7532              # default — change if port is taken
VBM_EMBED_URL=http://127.0.0.1:11434/api/embeddings  # Ollama for real semantic search
VBM_TTL_DAYS=90            # auto-delete pages older than N days
VBM_LOG_LEVEL=info         # debug / info / warn / error
```

If you change `VBM_PORT`, update it in the extension popup (Daemon section).

## Install as service (Linux)

```bash
cd daemon && make install   # installs binary + systemd user unit
```

## Uninstall

```bash
cd daemon && make uninstall
rm -rf ~/.local/share/vbm/  # optional — deletes all indexed data
```

## Dev commands

```bash
cd daemon && make run        # run tests then build + start daemon
cd extension && npm run dev  # watch mode for extension
cd extension && npm test     # run extension unit tests
cd daemon && go test ./...   # run daemon unit tests
curl http://127.0.0.1:7532/healthz
curl http://127.0.0.1:7532/metrics
```

---

## Docker

The daemon ships as a single Alpine-based image (~19 MB). Unit tests run
inside the build stage, so a failing test prevents a broken image from being
published.

### Docker Compose (recommended for local use)

```bash
docker compose up -d          # build image + start daemon
docker compose logs -f vbmd   # follow logs
docker compose down           # stop
```

Data is persisted to `~/.local/share/vbm/` on the host (same path as the
native install, so switching between modes keeps your history).

To enable semantic search / LLM features, set the relevant env vars before
starting:

```bash
VBM_EMBED_API_KEY=sk-... \
VBM_EMBED_URL=https://api.openai.com/v1/embeddings \
docker compose up -d
```

Or uncomment the matching lines in `docker-compose.yml`.

### Build and publish to Docker Hub

```bash
DOCKERHUB_USER=<your-user> ./publish-docker.sh [tag]

# Non-interactive (CI):
DOCKERHUB_USER=alice DOCKERHUB_TOKEN=dckr_pat_xxx ./publish-docker.sh v0.1.0
```

The script refuses to publish from a dirty working tree. Override with
`ALLOW_DIRTY=1`. Both `:<tag>` and `:latest` are pushed unless `SKIP_LATEST=1`.

### Run a pre-built image manually

```bash
docker run --rm -p 127.0.0.1:7532:7532 \
  -v ~/.local/share/vbm:/data \
  -e VBM_EMBED_URL=http://host-gateway:11434/api/embeddings \
  <your-user>/vbmd:latest
```

---

## Kubernetes

Manifests live in `k8s/`. They create a dedicated `vbm` namespace, a 1 Gi
PersistentVolume (hostPath — swap for a cloud storage class in production), and
a ClusterIP Service on port 7532.

### Deploy

1. **Replace the image placeholder** in `k8s/deployment.yaml`:

   ```bash
   sed -i 's/<DOCKERHUB_USER>/alice/' k8s/deployment.yaml
   ```

2. **(Optional) Create the LLM secret** if you want semantic search:

   ```bash
   kubectl create secret generic vbm-llm-secret \
     --namespace vbm \
     --from-literal=VBM_EMBED_API_KEY=sk-...
   ```

   Then uncomment the `VBM_EMBED_URL` and `VBM_EMBED_API_KEY` env blocks in
   `k8s/deployment.yaml`.

3. **Apply all manifests**:

   ```bash
   kubectl apply -f k8s/
   ```

4. **Check health**:

   ```bash
   kubectl -n vbm get pods
   kubectl -n vbm port-forward svc/vbmd 7532:7532
   curl http://127.0.0.1:7532/healthz
   ```

### Tear down

```bash
kubectl delete -f k8s/
# PV uses reclaimPolicy: Retain — delete manually if you want to wipe data:
kubectl delete pv vbm-data-pv
```

---

## Privacy

- All data stays on your machine (`~/.local/share/vbm/vbm.db`)
- Incognito windows: never captured
- Sensitive domains (banks, `.gov`, auth pages): blocked by default
- `DELETE /forget` removes any page or domain permanently
