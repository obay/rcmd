# install-agent.ps1 — install the obcmd-agent Windows service.
#
# Run this from an elevated PowerShell prompt. It expects obcmd-agent.exe
# to be on PATH (e.g. installed via `scoop install obay/obcmd-agent`) or
# present in the current directory.
#
# Usage:
#   .\install-agent.ps1                 # install + start
#   .\install-agent.ps1 -Uninstall      # stop + remove

[CmdletBinding()]
param(
    [switch]$Uninstall
)

$ErrorActionPreference = 'Stop'

# Resolve the binary.
$bin = Get-Command obcmd-agent.exe -ErrorAction SilentlyContinue
if ($null -eq $bin) {
    $local = Join-Path $PSScriptRoot 'obcmd-agent.exe'
    if (Test-Path $local) {
        $binPath = $local
    } else {
        throw "obcmd-agent.exe not found on PATH or in $PSScriptRoot. Install via 'scoop install obay/obcmd-agent' first."
    }
} else {
    $binPath = $bin.Source
}

if ($Uninstall) {
    & $binPath uninstall
    exit $LASTEXITCODE
}

# Make sure %PROGRAMDATA%\obcmd exists and has a config.
$dataDir = Join-Path $env:PROGRAMDATA 'obcmd'
$cfgPath = Join-Path $dataDir 'agent.toml'
if (-not (Test-Path $dataDir)) {
    New-Item -ItemType Directory -Path $dataDir | Out-Null
}
if (-not (Test-Path $cfgPath)) {
    Write-Host "No config at $cfgPath. Creating template — edit it before the agent will work."
    @'
# obcmd-agent config
relay_url   = "https://ai.obay.cloud"
agent_id    = "win-host"
agent_key   = ""   # base64 32B — same as relay's agent_key
payload_key = ""   # base64 32B — same as operator's payload_key
log_file    = "C:\\ProgramData\\obcmd\\agent.log"
default_shell = "cmd"
'@ | Set-Content -Path $cfgPath -Encoding UTF8
    Write-Host "Template config written to $cfgPath"
    Write-Host "Edit it, then re-run this script to register and start the service."
    exit 0
}

& $binPath install --bin-path $binPath
