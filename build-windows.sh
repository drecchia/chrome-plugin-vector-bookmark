#!/usr/bin/env bash
# Cross-compile vbmd.exe from Linux/WSL.
set -e
cd "$(dirname "$0")/daemon"
make build-windows -s
echo "✓ daemon/bin/vbmd.exe ready"
echo ""
echo "Copy to Windows and run:  vbmd.exe server"
echo "Or use the dev.ps1 script from the project root."
