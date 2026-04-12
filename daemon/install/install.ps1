# Vector Bookmark — Windows Installer
# Requires: PowerShell 5.1+ (built-in on Windows 10/11), no admin needed.
# Usage:
#   .\install.ps1                         # interactive
#   .\install.ps1 -ExtensionId abc123     # non-interactive

param(
    [string]$ExtensionId = "",
    [string]$BinPath = ""
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# ── Paths ──────────────────────────────────────────────────────────────────────

$VbmDir     = Join-Path $env:LOCALAPPDATA "vbm"
$VbmBin     = Join-Path $VbmDir "vbmd.exe"
$VbmData    = Join-Path $env:APPDATA "vbm"           # session.json, vbm.db, env
$NmHostDir  = Join-Path $env:LOCALAPPDATA "Google\Chrome\User Data\NativeMessagingHosts"
$NmHostDirC = Join-Path $env:LOCALAPPDATA "Chromium\User Data\NativeMessagingHosts"
$NmHostName = "com.vbm.daemon"
$TaskName   = "VectorBookmarkDaemon"

# ── Step 1: Locate the binary ──────────────────────────────────────────────────

if ($BinPath -eq "") {
    # Try relative path from script location (i.e. repo/daemon/bin/vbmd.exe)
    $scriptDir  = Split-Path -Parent $MyInvocation.MyCommand.Path
    $repoBin    = Join-Path $scriptDir "..\bin\vbmd.exe"
    if (Test-Path $repoBin) {
        $BinPath = (Resolve-Path $repoBin).Path
    }
}

if ($BinPath -eq "" -or -not (Test-Path $BinPath)) {
    Write-Error @"
vbmd.exe not found.
Build it first from the daemon directory:
    make build-windows
Or cross-compile from WSL2:
    GOOS=windows GOARCH=amd64 go build -o bin/vbmd.exe ./cmd/vbmd/
Then re-run this script.
"@
    exit 1
}

Write-Host "Binary: $BinPath"

# ── Step 2: Ask for Extension ID ───────────────────────────────────────────────

if ($ExtensionId -eq "") {
    Write-Host ""
    Write-Host "Open chrome://extensions/, enable Developer mode, load extension/dist/."
    Write-Host "Copy the Extension ID (32 lowercase letters)."
    $ExtensionId = Read-Host "Extension ID"
}

$ExtensionId = $ExtensionId.Trim()
if ($ExtensionId -notmatch "^[a-z]{32}$") {
    Write-Error "Invalid Extension ID: must be 32 lowercase letters. Got: $ExtensionId"
    exit 1
}

# ── Step 3: Install binary ─────────────────────────────────────────────────────

New-Item -ItemType Directory -Force -Path $VbmDir  | Out-Null
New-Item -ItemType Directory -Force -Path $VbmData | Out-Null

Copy-Item -Path $BinPath -Destination $VbmBin -Force
Write-Host "Installed: $VbmBin"

# ── Step 4: Install NM host manifest ──────────────────────────────────────────

$manifest = @"
{
  "name": "$NmHostName",
  "description": "Vector Bookmark native daemon bridge",
  "path": "$($VbmBin.Replace('\', '\\'))",
  "type": "stdio",
  "allowed_origins": [
    "chrome-extension://$ExtensionId/"
  ]
}
"@

foreach ($dir in @($NmHostDir, $NmHostDirC)) {
    if (Test-Path (Split-Path -Parent $dir)) {
        New-Item -ItemType Directory -Force -Path $dir | Out-Null
        $manifestPath = Join-Path $dir "$NmHostName.json"
        $manifest | Out-File -FilePath $manifestPath -Encoding ascii
        Write-Host "NM manifest: $manifestPath"
    }
}

# ── Step 5: Register Task Scheduler for auto-start ────────────────────────────

# Remove old task if exists (ignore errors)
schtasks /Delete /TN $TaskName /F 2>$null | Out-Null

$taskXml = @"
<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <Triggers>
    <LogonTrigger><Enabled>true</Enabled></LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <RestartOnFailure>
      <Interval>PT30S</Interval>
      <Count>3</Count>
    </RestartOnFailure>
  </Settings>
  <Actions>
    <Exec>
      <Command>$($VbmBin.Replace('\', '\\'))</Command>
      <Arguments>server</Arguments>
      <WorkingDirectory>$($VbmData.Replace('\', '\\'))</WorkingDirectory>
    </Exec>
  </Actions>
</Task>
"@

$taskFile = Join-Path $env:TEMP "vbmd-task.xml"
$taskXml | Out-File -FilePath $taskFile -Encoding Unicode
schtasks /Create /TN $TaskName /XML $taskFile /F | Out-Null
Remove-Item $taskFile -ErrorAction SilentlyContinue
Write-Host "Task Scheduler: $TaskName (runs at logon, no elevation)"

# ── Step 6: Start the daemon now ──────────────────────────────────────────────

# Stop any existing instance first
Stop-Process -Name "vbmd" -ErrorAction SilentlyContinue

Start-Process -FilePath $VbmBin -ArgumentList "server" -WindowStyle Hidden -WorkingDirectory $VbmData
Start-Sleep -Seconds 1

$sessionFile = Join-Path $VbmData "session.json"
if (Test-Path $sessionFile) {
    $session = Get-Content $sessionFile | ConvertFrom-Json
    Write-Host ""
    Write-Host "Daemon started on port $($session.port)"
    Write-Host "Health check: http://127.0.0.1:$($session.port)/healthz"
} else {
    Write-Warning "Daemon may not have started — check $VbmData for errors."
}

# ── Done ───────────────────────────────────────────────────────────────────────

Write-Host ""
Write-Host "Installation complete."
Write-Host ""
Write-Host "Next steps:"
Write-Host "  1. Open Chrome, go to chrome://extensions/"
Write-Host "  2. Load unpacked → select extension\dist\"
Write-Host "  3. The popup should show 'Connected to daemon'"
Write-Host ""
Write-Host "Configuration: create $VbmData\env to set VBM_PORT, VBM_EMBED_URL, etc."
Write-Host "Uninstall:     run uninstall.ps1 (or see README.md)"
