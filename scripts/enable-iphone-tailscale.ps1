# Run elevated for this PC and the paired iPhone's Tailscale addresses.
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$result = Join-Path $root '.cache\iphone-tailscale-firewall-result.json'
try {
    $ruleName = 'Yui-iPhone-Tailscale-HTTPS'
    if (-not (Get-NetFirewallRule -Name $ruleName -ErrorAction SilentlyContinue)) {
        New-NetFirewallRule -Name $ruleName -DisplayName 'Yui HTTPS from iPhone over Tailscale' `
            -Direction Inbound -Action Allow -Protocol TCP -LocalPort 8765 `
            -LocalAddress 100.110.162.100 -RemoteAddress 100.72.64.30 `
            -InterfaceAlias 'Tailscale' -Program (Join-Path $root 'bin\yui-core.exe') `
            -Profile Any | Out-Null
    }
    @{ success = $true; rule = $ruleName } | ConvertTo-Json | Set-Content -LiteralPath $result
} catch {
    @{ success = $false; error = $_.Exception.Message } | ConvertTo-Json | Set-Content -LiteralPath $result
    throw
}
