# Vector Bookmark

Semantic recall for everything you've read. Chrome extension + native daemon.

## Architecture

```
Chrome Extension (thin) ──NM handshake──▶ vbmd daemon (Go)
                         ◀──────────────── port + token
Chrome Extension         ──── HTTP ──────▶ /ingest /search /forget
                         ◀── WebSocket ─── status push
```

## Quick start (Linux)

### Prerequisites
- Go 1.22+
- Node.js 20+
- Chrome (or Chromium)

### 1. Build and install the daemon

```bash
cd daemon
make build
make install
# Follow the prompts — you'll need your Chrome extension ID (step 3)
```

### 2. Build the extension

```bash
cd extension
npm install
npm run build
```

### 3. Load the extension in Chrome

1. Open `chrome://extensions/`
2. Enable **Developer mode**
3. Click **Load unpacked** → select `extension/dist/`
4. Copy the extension ID

### 4. Update the Native Messaging manifest with your extension ID

```bash
cd daemon
EXTENSION_ID=your_id_here bash install/install.sh
```

Or manually edit `~/.config/google-chrome/NativeMessagingHosts/com.vbm.daemon.json`.

### 5. Restart Chrome

The extension popup should show "Connected to daemon".

## Quick start (Windows 10/11)

### Prerequisites
- Go 1.22+ **or** WSL2 (for cross-compiling only)
- Node.js 20+ (for building the extension)
- Chrome or Chromium

### 1. Build the daemon

From WSL2 or any Go environment:

```bash
cd daemon
make build-windows    # produces bin/vbmd.exe
```

Or cross-compile directly:

```bash
GOOS=windows GOARCH=amd64 go build -o bin/vbmd.exe ./cmd/vbmd/
```

### 2. Build the extension

```bash
cd extension
npm install
npm run build
```

### 3. Install (PowerShell — no admin required)

```powershell
cd daemon
.\install\install.ps1
# Prompts for the Chrome Extension ID (copy from chrome://extensions/ after step 4)
```

### 4. Load the extension in Chrome

1. Open `chrome://extensions/`
2. Enable **Developer mode**
3. Click **Load unpacked** → select `extension\dist\`
4. Copy the Extension ID → re-run `install.ps1 -ExtensionId <id>` if not provided earlier

### 5. Restart Chrome

The popup should show "Connected to daemon".

**Configuration** (port, embeddings, etc.): create `%APPDATA%\vbm\env` with one `KEY=value` per line — loaded automatically at daemon startup.

---

## Configuration

### Change the bind address or port

By default the daemon binds to `127.0.0.1` on a random port. Configuration is read from `~/.config/vbm/env` at startup — this file is **not overwritten by `make install`**, so settings survive upgrades.

```bash
mkdir -p ~/.config/vbm
cat >> ~/.config/vbm/env <<'EOF'
# Fixed port (optional — random if unset)
VBM_PORT=7532

# Bind address — only change if running in an isolated Docker network
# NEVER set to 0.0.0.0 on a shared/multi-user host
VBM_BIND=127.0.0.1
EOF

systemctl --user restart vbmd
```

The active port is always readable from `~/.local/share/vbm/session.json`:

```bash
jq -r .port ~/.local/share/vbm/session.json
```

### Change the extension ID after installation

Needed when you reload the unpacked extension (Chrome assigns a new ID) or reinstall Chrome.

```bash
# 1. Get the new ID from chrome://extensions/
NEW_ID=abcdefghijklmnopabcdefghijklmnop

# 2. Update the Native Messaging manifest
sed -i "s|chrome-extension://[^/]*/|chrome-extension://${NEW_ID}/|g" \
  ~/.config/google-chrome/NativeMessagingHosts/com.vbm.daemon.json

# 3. Verify
cat ~/.config/google-chrome/NativeMessagingHosts/com.vbm.daemon.json | jq .allowed_origins

# 4. Restart Chrome (NM host discovery is cached — required)
```

Alternatively, re-run the installer which prompts for the ID interactively:

```bash
cd daemon && EXTENSION_ID=abcdefghijklmnopabcdefghijklmnop bash install/install.sh
```

---

## Uninstall

```bash
# Stop and remove the daemon service
systemctl --user stop vbmd
systemctl --user disable vbmd
rm -f ~/.config/systemd/user/vbmd.service
systemctl --user daemon-reload

# Remove the binary and Native Messaging host
rm -f ~/.local/bin/vbmd
rm -f ~/.config/google-chrome/NativeMessagingHosts/com.vbm.daemon.json
rm -f ~/.config/chromium/NativeMessagingHosts/com.vbm.daemon.json

# Remove config (optional)
rm -rf ~/.config/vbm/

# Remove all indexed data (optional — permanent)
rm -rf ~/.local/share/vbm/
```

Data in `~/.local/share/vbm/` is intentionally left in place by `make uninstall` to avoid accidental loss. Delete it manually when you're sure.

Finish by removing the extension from `chrome://extensions/`.

---

## Development

```bash
# Run daemon in dev mode (hot reload not included yet)
cd daemon && make run

# With custom bind/port (env vars — systemd and ~/.config/vbm/env are not used here)
cd daemon && VBM_PORT=7532 make run
cd daemon && VBM_BIND=127.0.0.1 VBM_PORT=7532 make run

# With semantic search via Ollama
cd daemon && VBM_EMBED_URL=http://127.0.0.1:11434/api/embeddings make run

# Watch extension for changes
cd extension && npm run dev
```

## Verify daemon is running

```bash
systemctl --user status vbmd
curl http://127.0.0.1:$(cat ~/.local/share/vbm/session.json | python3 -c "import sys,json;print(json.load(sys.stdin)['port'])")/healthz
```

## Privacy

- Incognito windows: never captured
- Sensitive domains (banks, `.gov`, auth pages): blocked by default denylist
- Password fields: capture paused when focused
- All data stays on your machine (v0.1 — no sync server)
- Run `@recall forget` from the popup to delete any page/domain

## Project structure

```
/
├── extension/     Chrome MV3 extension (TypeScript + Vite + CRXJS)
├── daemon/        Native Go daemon (server + NM host)
├── proto/         Shared TypeScript type definitions
└── docs/
    └── bootstrap/ PLAN.md + PRD.md
```

## Roadmap

- **v0.1** — Local semantic recall, Linux only (current)
- **v0.2** — Snapshots, LLM via Ollama, rich UI, macOS
- **v0.3** — Cross-device E2EE sync, self-hosted server
