[CmdletBinding()]
param([switch]$NoBrowser)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$binary = Join-Path $root 'bin\yui-core.exe'
if (-not (Test-Path $binary)) { throw 'Run scripts\build.ps1 first.' }
$state = Join-Path $root '.cache\check-runtime.json'
if (Test-Path $state) {
    $run = Get-Content $state -Raw | ConvertFrom-Json
    $existing = Get-Process -Id $run.pid -ErrorAction SilentlyContinue
    if ($existing -and $existing.Path -eq $binary) {
        $page = "http://127.0.0.1:8766/#token=$($run.token)"
        Write-Host "Yui is already running: $page"
        foreach ($address in (Get-Content (Join-Path $root 'data\tls\addresses.json') | ConvertFrom-Json)) {
            if ($address -match '^192\.168\.|^10\.') { Write-Host "Phone: https://${address}:8765" }
        }
        if (-not $NoBrowser) { Start-Process $page }
        return
    }
}
$random = New-Object byte[] 32
$rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
$rng.GetBytes($random)
$rng.Dispose()
$env:YUI_LOOPBACK_TOKEN = [Convert]::ToBase64String($random).TrimEnd('=').Replace('+','-').Replace('/','_')
$url = "http://127.0.0.1:8766/#token=$env:YUI_LOOPBACK_TOKEN"
Write-Host "PC: $url"
$addresses = Get-Content (Join-Path $root 'data\tls\addresses.json') | ConvertFrom-Json
foreach ($address in $addresses) {
    if ($address -match '^\d+\.' -and $address -notmatch '^127\.') { Write-Host "Phone: https://${address}:8765" }
}
Write-Host 'Phone pairing: open the PC page, press Phone, enter its one-time code on your phone.'
Write-Host 'TLS certificate setup and verification: docs\LOCAL-SETUP.md'
Write-Host 'Keep this window open. Ctrl+C stops the command center.'
if (-not $NoBrowser) {
    $job = Start-Job -ArgumentList $url -ScriptBlock { param($page) Start-Sleep -Seconds 3; Start-Process $page }
}
Push-Location $root
try { & $binary -config (Join-Path $root 'yui.config.json') -print-token=false }
finally {
    Pop-Location
    Remove-Item Env:YUI_LOOPBACK_TOKEN -ErrorAction SilentlyContinue
    if ($job) { Stop-Job $job; Remove-Job $job }
}
