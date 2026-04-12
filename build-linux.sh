#!/usr/bin/env bash
# Build daemon + extension for Linux. Does not start anything.
set -e
cd "$(dirname "$0")"

echo "→ Building daemon..."
(cd daemon && make build -s)

echo "→ Building extension..."
(cd extension && npm install --silent 2>/dev/null && npm run build --silent 2>/dev/null)

echo "✓ Done."
echo "  Daemon:    daemon/bin/vbmd"
echo "  Extension: extension/dist/"
