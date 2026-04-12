# Windows dev helper — run from project root in PowerShell.
# Build extension first from WSL: ./build-windows.sh
param(
    [string]$ExePath = "daemon\bin\vbmd.exe"
)

if (-not (Test-Path $ExePath)) {
    Write-Error "vbmd.exe not found at '$ExePath'. Build from WSL: ./build-windows.sh"
    exit 1
}

# Kill existing instance
Get-Process vbmd -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
Start-Sleep -Milliseconds 200

# Start daemon hidden
Start-Process -FilePath (Resolve-Path $ExePath) -ArgumentList "server" -WindowStyle Hidden
Start-Sleep -Seconds 1

# Health check
try {
    $null = Invoke-RestMethod "http://127.0.0.1:7532/healthz" -ErrorAction Stop
    Write-Host "✓ Daemon running at http://127.0.0.1:7532"
} catch {
    Write-Warning "Daemon didn't respond. Try running manually: $ExePath server"
}

Write-Host ""
Write-Host "Load extension in Chrome:"
Write-Host "  chrome://extensions/ -> Load unpacked -> $PWD\extension\dist\"
Write-Host ""
Write-Host "Stop:  Stop-Process -Name vbmd"
