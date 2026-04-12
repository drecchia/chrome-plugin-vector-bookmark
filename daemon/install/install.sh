#!/usr/bin/env bash
set -e

SCRIPT_DIR="$(dirname "$(readlink -f "$0")")"

# P2-06: prerequisite checks before doing anything.
if [ -n "$1" ] && [ "$1" != "$HOME/.local/bin/vbmd" ]; then
  if [ ! -f "$1" ]; then
    echo "ERROR: binary not found at '$1'" >&2
    exit 1
  fi
elif [ ! -f "$HOME/.local/bin/vbmd" ]; then
  echo "ERROR: vbmd binary not found at \$HOME/.local/bin/vbmd" >&2
  echo "  Build it first: cd daemon && make build && cp bin/vbmd \$HOME/.local/bin/vbmd" >&2
  echo "  Or pass the binary path as first argument: bash install.sh /path/to/vbmd" >&2
  exit 1
fi

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

# Prompt for Chrome extension ID — strictly validate before continuing (P1-14).
# EXTENSION_ID can be pre-set as an env var for non-interactive / CI use.
echo ""
echo "=== Vector Bookmark Installer ==="
echo ""
if [ -z "$EXTENSION_ID" ]; then
  if [ -t 0 ]; then
    echo "To complete setup, you need your Chrome extension ID."
    echo "1. Open Chrome -> chrome://extensions/"
    echo "2. Enable Developer mode"
    echo "3. Load unpacked -> select the 'extension/' folder"
    echo "4. Copy the extension ID shown"
    echo ""
    while true; do
      read -r -p "Extension ID (32 lowercase a-p chars): " EXTENSION_ID
      if echo "$EXTENSION_ID" | grep -qE '^[a-p]{32}$'; then
        break
      fi
      echo "ERROR: '$EXTENSION_ID' is not a valid Chrome extension ID." >&2
      echo "  Expected: exactly 32 lowercase letters a-p" >&2
      echo "  Example:  abcdefghijklmnopabcdefghijklmnop" >&2
    done
  else
    echo "ERROR: EXTENSION_ID env var not set. Run:" >&2
    echo "  EXTENSION_ID=<32-char-id> bash $0 [binary-path]" >&2
    exit 1
  fi
elif ! echo "$EXTENSION_ID" | grep -qE '^[a-p]{32}$'; then
  echo "ERROR: EXTENSION_ID='$EXTENSION_ID' is not a valid Chrome extension ID." >&2
  echo "  Expected: exactly 32 lowercase letters a-p" >&2
  echo "  Example:  abcdefghijklmnopabcdefghijklmnop" >&2
  exit 1
fi

# Install NM manifest for Chrome
CHROME_NM_DIR="$HOME/.config/google-chrome/NativeMessagingHosts"
mkdir -p "$CHROME_NM_DIR"
sed \
  -e "s|BINARY_PATH|$BINARY|g" \
  -e "s|EXTENSION_ID|${EXTENSION_ID}|g" \
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
echo "  1. Restart Chrome so the NM manifest is picked up"
echo "  2. The extension popup should show 'Connected to daemon'"
echo ""
echo "Verify daemon: curl http://127.0.0.1:\$(python3 -c \"import json; d=json.load(open('$HOME/.local/share/vbm/session.json')); print(d['port'])\")/healthz"
