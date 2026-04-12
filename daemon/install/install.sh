#!/usr/bin/env bash
# Installs vbmd binary + systemd user unit on Linux.
set -e

SCRIPT_DIR="$(dirname "$(readlink -f "$0")")"
BINARY="${1:-$SCRIPT_DIR/../bin/vbmd}"

if [ ! -f "$BINARY" ]; then
  echo "ERROR: binary not found at '$BINARY'" >&2
  echo "  Run: cd daemon && make build" >&2
  exit 1
fi

install -Dm755 "$BINARY" "$HOME/.local/bin/vbmd"
mkdir -p "$HOME/.local/share/vbm"
mkdir -p "$HOME/.config/systemd/user"
cp "$SCRIPT_DIR/vbmd.service" "$HOME/.config/systemd/user/vbmd.service"

if command -v systemctl &>/dev/null; then
  systemctl --user daemon-reload
  systemctl --user enable --now vbmd
  echo "✓ vbmd installed and started"
  echo "  Status:  systemctl --user status vbmd"
  echo "  Health:  curl http://127.0.0.1:7532/healthz"
else
  echo "✓ vbmd installed to ~/.local/bin/vbmd"
  echo "  Start:   vbmd server"
fi
