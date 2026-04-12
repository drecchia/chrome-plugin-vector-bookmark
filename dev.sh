#!/usr/bin/env bash
# One-command dev setup for Linux.
# Builds daemon + extension, starts daemon, prints load path.
set -e
cd "$(dirname "$0")"

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
