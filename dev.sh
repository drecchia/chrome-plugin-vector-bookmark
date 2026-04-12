#!/usr/bin/env bash
# One-command dev setup for Linux.
# Builds daemon + extension, starts daemon, prints load path.
set -e
cd "$(dirname "$0")"

# --- Embedding config (enables semantic search) ---
# Set VBM_EMBED_API_KEY in your env (e.g. export VBM_EMBED_API_KEY=sk-or-...)
# and the rest defaults to OpenRouter with text-embedding-3-small.
# For local Ollama (CPU, no API key): set VBM_EMBED_URL=http://127.0.0.1:11434/api/embeddings
if [ -n "${VBM_EMBED_API_KEY:-}" ]; then
  export VBM_EMBED_URL="${VBM_EMBED_URL:-https://openrouter.ai/api/v1/embeddings}"
  export VBM_EMBED_FORMAT="${VBM_EMBED_FORMAT:-openai}"
  export VBM_EMBED_MODEL="${VBM_EMBED_MODEL:-openai/text-embedding-3-small}"
fi

echo "→ Building daemon..."
(cd daemon && make build -s)

echo "→ Building extension..."
(cd extension && npm install --silent 2>/dev/null && npm run build --silent 2>/dev/null)

echo "→ Starting daemon..."
pkill -f 'vbmd server' 2>/dev/null || true
sleep 0.2
daemon/bin/vbmd server &
sleep 0.5

if curl -sf http://127.0.0.1:7532/healthz > /dev/null 2>&1; then
  echo "✓ Daemon running at http://127.0.0.1:7532"
else
  echo "⚠ Daemon didn't respond — check: daemon/bin/vbmd server"
fi

echo ""
echo "Load extension in Chrome:"
echo "  chrome://extensions/ → Load unpacked → $(pwd)/extension/dist/"
echo ""
echo "Stop:  pkill -f 'vbmd server'"
echo "Logs:  daemon/bin/vbmd server  (foreground)"
