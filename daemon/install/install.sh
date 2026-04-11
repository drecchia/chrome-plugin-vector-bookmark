#!/usr/bin/env bash
set -e

SCRIPT_DIR="$(dirname "$(readlink -f "$0")")"

# Determine binary path
BINARY="${1:-$HOME/.local/bin/vbmd}"

# If $1 is a source path (not the install destination), copy it
if [ -n "$1" ] && [ "$1" != "$HOME/.local/bin/vbmd" ]; then
  install -Dm755 "$1" "$HOME/.local/bin/vbmd"
  BINARY="$HOME/.local/bin/vbmd"
fi

# Create data dir
mkdir -p "$HOME/.local/share/vbm"

# Install systemd user service
mkdir -p "$HOME/.config/systemd/user"
cp "$SCRIPT_DIR/vbmd.service" "$HOME/.config/systemd/user/vbmd.service"

# Prompt for Chrome extension ID
echo ""
echo "=== Vector Bookmark Installer ==="
echo ""
echo "To complete setup, you need your Chrome extension ID."
echo "1. Open Chrome → chrome://extensions/"
echo "2. Enable Developer mode"
echo "3. Load unpacked → select the 'extension/' folder"
echo "4. Copy the extension ID shown"
echo ""
read -p "Extension ID (leave blank to set later): " EXTENSION_ID

# Install NM manifest for Chrome
CHROME_NM_DIR="$HOME/.config/google-chrome/NativeMessagingHosts"
mkdir -p "$CHROME_NM_DIR"
sed \
  -e "s|BINARY_PATH|$BINARY|g" \
  -e "s|EXTENSION_ID|${EXTENSION_ID:-REPLACE_ME}|g" \
  "$SCRIPT_DIR/native-messaging-host.json" \
  > "$CHROME_NM_DIR/com.vbm.daemon.json"

# Also install for Chromium if present
if [ -d "$HOME/.config/chromium" ]; then
  CHROMIUM_NM_DIR="$HOME/.config/chromium/NativeMessagingHosts"
  mkdir -p "$CHROMIUM_NM_DIR"
  cp "$CHROME_NM_DIR/com.vbm.daemon.json" "$CHROMIUM_NM_DIR/"
fi

# Enable and start systemd user unit
if command -v systemctl &>/dev/null; then
  systemctl --user daemon-reload
  systemctl --user enable vbmd
  systemctl --user start vbmd
  echo ""
  echo "vbmd service enabled and started."
  echo "Check status: systemctl --user status vbmd"
else
  echo ""
  echo "WARNING: systemctl not found. systemd user unit was installed to"
  echo "  ~/.config/systemd/user/vbmd.service"
  echo "but could not be enabled automatically. Start the daemon manually:"
  echo "  $BINARY server &"
fi

echo ""
echo "=== Installation complete ==="
echo ""
echo "Next steps:"
if [ -z "$EXTENSION_ID" ] || [ "${EXTENSION_ID:-REPLACE_ME}" = "REPLACE_ME" ]; then
  echo "  1. Load the extension in Chrome (chrome://extensions/ → Load unpacked → extension/dist/)"
  echo "  2. Copy the extension ID and run:"
  echo "       EXTENSION_ID=<your_id> bash $SCRIPT_DIR/install.sh"
  echo "     Or manually edit: $CHROME_NM_DIR/com.vbm.daemon.json"
else
  echo "  1. Restart Chrome so the NM manifest is picked up"
  echo "  2. The extension popup should show 'Connected to daemon'"
fi
echo ""
echo "Verify daemon: curl http://127.0.0.1:\$(python3 -c \"import json; d=json.load(open('$HOME/.local/share/vbm/session.json')); print(d['port'])\")/healthz"
