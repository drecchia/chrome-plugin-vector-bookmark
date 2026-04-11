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

## Development

```bash
# Run daemon in dev mode (hot reload not included yet)
cd daemon && make run

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
