# Shared local launcher operations. Dot-sourcing this file never starts a process.
function Get-YuiCoreProcess {
    param([string]$Root)
    $binary = Join-Path $Root 'bin\yui-core.exe'
    Get-Process -Name 'yui-core' -ErrorAction SilentlyContinue |
        Where-Object { $_.Path -eq $binary } | Select-Object -First 1
}

function Get-YuiCorePage {
    param([string]$Root, $Process)
    $config = Get-Content -LiteralPath (Join-Path $Root 'yui.config.json') -Raw | ConvertFrom-Json
    $base = "http://$($config.server.local_addr)"
    $state = Join-Path $Root '.cache\check-runtime.json'
    if ($Process -and (Test-Path -LiteralPath $state)) {
        try {
            $run = Get-Content -LiteralPath $state -Raw | ConvertFrom-Json
            if ($run.pid -eq $Process.Id -and $run.token) {
                return "$base/#token=$([Uri]::EscapeDataString($run.token))"
            }
        } catch { } # A running core without saved credentials can still be paired.
    }
    return $base
}

function Start-YuiCore {
    param([string]$Root)
    $existing = Get-YuiCoreProcess -Root $Root
    if ($existing) { return $existing }
    $binary = Join-Path $Root 'bin\yui-core.exe'
    $configPath = Join-Path $Root 'yui.config.json'
    if (-not (Test-Path -LiteralPath $binary)) { throw 'Не найдено ядро. Выполните scripts\build.ps1.' }
    $config = Get-Content -LiteralPath $configPath -Raw | ConvertFrom-Json
    if (-not $config.server.local_addr) { throw 'В конфигурации отсутствует server.local_addr.' }
    $ports = @($config.server.addr, $config.server.local_addr) | ForEach-Object { ([Uri]"http://$_").Port }
    $listeners = [Net.NetworkInformation.IPGlobalProperties]::GetIPGlobalProperties().GetActiveTcpListeners()
    if ($listeners | Where-Object { $_.Port -in $ports }) {
        throw 'Порт ядра уже занят. Проверьте работающий экземпляр Yui.'
    }
    $cache = Join-Path $Root '.cache'
    New-Item -ItemType Directory -Path $cache -Force | Out-Null
    $random = New-Object byte[] 32
    $rng = [Security.Cryptography.RandomNumberGenerator]::Create()
    try { $rng.GetBytes($random) } finally { $rng.Dispose() }
    $token = [Convert]::ToBase64String($random).TrimEnd('=').Replace('+', '-').Replace('/', '_')
    $previousToken = $env:YUI_LOOPBACK_TOKEN
    $previousOpenRouterKey = $env:OPENROUTER_API_KEY
    try {
        $env:YUI_LOOPBACK_TOKEN = $token
        if (-not $env:OPENROUTER_API_KEY) {
            $env:OPENROUTER_API_KEY = [Environment]::GetEnvironmentVariable('OPENROUTER_API_KEY', 'User')
        }
        $process = Start-Process -FilePath $binary -WorkingDirectory $Root -WindowStyle Hidden -PassThru `
            -ArgumentList @('-config', ('"' + $configPath + '"'), '-print-token=false') `
            -RedirectStandardOutput (Join-Path $cache 'core-launcher.log') `
            -RedirectStandardError (Join-Path $cache 'core-launcher-error.log')
        $stateJson = @{ pid = $process.Id; token = $token } | ConvertTo-Json
        [IO.File]::WriteAllText((Join-Path $cache 'check-runtime.json'), $stateJson, (New-Object Text.UTF8Encoding($false)))
        return $process
    } finally {
        $env:YUI_LOOPBACK_TOKEN = $previousToken
        $env:OPENROUTER_API_KEY = $previousOpenRouterKey
    }
}
