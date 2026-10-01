# Run elevated to allow the iPhone on this PC's home LAN.
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$result = Join-Path $root '.cache\iphone-firewall-result.json'
try {
    $ruleName = 'Yui-Phone-HTTPS-Ethernet'
    if (-not (Get-NetFirewallRule -Name $ruleName -ErrorAction SilentlyContinue)) {
        New-NetFirewallRule -Name $ruleName -DisplayName 'Yui iPhone HTTPS on home Ethernet' `
            -Direction Inbound -Action Allow -Protocol TCP -LocalPort 8765 `
            -LocalAddress 192.168.1.15 -RemoteAddress 192.168.1.0/24 `
            -InterfaceAlias 'Ethernet' -Program (Join-Path $root 'bin\yui-core.exe') `
            -Profile Public,Private | Out-Null
    }
    @{ success = $true; rule = $ruleName } | ConvertTo-Json | Set-Content -LiteralPath $result
} catch {
    @{ success = $false; error = $_.Exception.Message } | ConvertTo-Json | Set-Content -LiteralPath $result
    throw
}
