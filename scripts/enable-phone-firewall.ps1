# Run as Administrator only if Windows blocks the phone connection.
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$binary = Join-Path $root 'bin\yui-core.exe'
if (-not (Test-Path $binary)) { throw 'Build yui-core first.' }
if (-not (Get-NetFirewallRule -Name 'Yui-Phone-HTTPS' -ErrorAction SilentlyContinue)) {
    New-NetFirewallRule -Name 'Yui-Phone-HTTPS' -DisplayName 'Yui phone HTTPS (private LAN)' -Direction Inbound -Action Allow -Protocol TCP -LocalPort 8765 -Program $binary -Profile Private -RemoteAddress LocalSubnet | Out-Null
}
Write-Host 'Yui HTTPS is allowed from the local subnet on Private networks.'
