$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'core-launcher.ps1')
function Assert($Condition, [string]$Message) {
    if (-not $Condition) { throw $Message }
}
$fixture = Join-Path ([IO.Path]::GetTempPath()) ('yui-launcher-test-' + [Guid]::NewGuid())
$previousToken = $env:YUI_LOOPBACK_TOKEN
try {
    New-Item -ItemType Directory -Path (Join-Path $fixture 'bin') -Force | Out-Null
    Set-Content -LiteralPath (Join-Path $fixture 'bin\yui-core.exe') -Value 'test fixture'
    # Port 0 is not used by a listening server; no real core is spawned in these tests.
    @{ server = @{ addr = '127.0.0.1:0'; local_addr = '127.0.0.1:0' } } |
        ConvertTo-Json | Set-Content -LiteralPath (Join-Path $fixture 'yui.config.json')
    $script:existing = $null
    $script:spawnCount = 0
    function Get-YuiCoreProcess { param($Root) return $script:existing }
    function Start-Process {
        param($FilePath, $WorkingDirectory, $WindowStyle, [switch]$PassThru,
            $ArgumentList, $RedirectStandardOutput, $RedirectStandardError)
        $script:spawnCount++
        Assert ($WindowStyle -eq 'Hidden') 'Core must start without a console.'
        Assert ($WorkingDirectory -eq $fixture) 'Wrong working directory.'
        Assert ($ArgumentList[1] -eq ('"' + (Join-Path $fixture 'yui.config.json') + '"')) 'Config path must be quoted.'
        Assert ($env:YUI_LOOPBACK_TOKEN.Length -ge 40) 'Missing random token.'
        return [pscustomobject]@{ Id = 12345 }
    }
    $env:YUI_LOOPBACK_TOKEN = 'preserve-parent-token'
    $process = Start-YuiCore -Root $fixture
    Assert ($process.Id -eq 12345 -and $script:spawnCount -eq 1) 'Core was not launched.'
    Assert ($env:YUI_LOOPBACK_TOKEN -eq 'preserve-parent-token') 'Parent environment was changed.'
    $run = Get-Content -LiteralPath (Join-Path $fixture '.cache\check-runtime.json') -Raw | ConvertFrom-Json
    $bytes = [IO.File]::ReadAllBytes((Join-Path $fixture '.cache\check-runtime.json'))
    Assert ($bytes[0] -eq 123) 'Runtime JSON must be UTF-8 without BOM for Python compatibility.'
    Assert ($run.pid -eq 12345 -and $run.token -ne 'preserve-parent-token') 'Runtime credentials were not saved.'
    Assert ((Get-YuiCorePage -Root $fixture -Process $process) -match '#token=') 'Matching process must use saved token.'
    Assert ((Get-YuiCorePage -Root $fixture -Process ([pscustomobject]@{ Id = 999 })) -notmatch '#token=') 'Stale credentials must not be reused.'
    $script:existing = $process
    $again = Start-YuiCore -Root $fixture
    Assert ($again.Id -eq 12345 -and $script:spawnCount -eq 1) 'Duplicate process was spawned.'
    $script:existing = $null
    function Start-Process { throw 'simulated launch failure' }
    try { Start-YuiCore -Root $fixture; throw 'Expected launch failure.' }
    catch { Assert ($_.Exception.Message -eq 'simulated launch failure') 'Unexpected failure.' }
    Assert ($env:YUI_LOOPBACK_TOKEN -eq 'preserve-parent-token') 'Environment not restored on failure.'
    $listener = New-Object Net.Sockets.TcpListener([Net.IPAddress]::Loopback, 0)
    $listener.Start()
    try {
        @{ server = @{ addr = "127.0.0.1:$($listener.LocalEndpoint.Port)"; local_addr = '127.0.0.1:0' } } |
            ConvertTo-Json | Set-Content -LiteralPath (Join-Path $fixture 'yui.config.json')
        try { Start-YuiCore -Root $fixture; throw 'Expected occupied port failure.' }
        catch { Assert ($_.Exception.Message -match 'Порт ядра уже занят') 'Occupied port was not detected.' }
    } finally { $listener.Stop() }
    foreach ($name in @('launcher.ps1', 'core-launcher.ps1')) {
        $parseErrors = $null
        $null = [Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot $name), [ref]$null, [ref]$parseErrors)
        Assert ($parseErrors.Count -eq 0) "PowerShell syntax errors in $name"
    }
    # Construct the real window and exercise its handlers without showing it or starting a core.
    $ui = Get-Content -LiteralPath (Join-Path $PSScriptRoot 'launcher.ps1') -Raw
    $ui = $ui.Replace('$PSScriptRoot', ("'" + $PSScriptRoot.Replace("'", "''") + "'"))
    $smoke = @'
    $timer.Stop()
    function Get-YuiCoreProcess { param($Root) return $null }
    function Start-YuiCore { param($Root) $script:clicked = $true }
    $flags = [Reflection.BindingFlags]'Instance, NonPublic'
    $timer.GetType().GetMethod('OnTick', $flags).Invoke($timer, @([EventArgs]::Empty)) | Out-Null
    Assert ($start.Enabled -and -not $open.Enabled) 'Offline window controls are incorrect.'
    Assert ($start.Text -eq 'Включить ядро') 'Start button label is incorrect.'
    $script:clicked = $false
    $start.GetType().GetMethod('OnClick', $flags).Invoke($start, @([EventArgs]::Empty)) | Out-Null
    Assert ($script:clicked -and -not $start.Enabled) 'Start button handler did not run.'
'@
    Invoke-Expression ($ui.Replace('[Windows.Forms.Application]::Run($form)', $smoke))
    Write-Host 'PASS: launch, duplicate protection, tokens, environment restoration, occupied port, script syntax, window and button handler.'
} finally {
    $env:YUI_LOOPBACK_TOKEN = $previousToken
    $resolved = [IO.Path]::GetFullPath($fixture)
    $tempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd('\') + '\'
    if ($resolved.StartsWith($tempRoot, [StringComparison]::OrdinalIgnoreCase) -and
        ([IO.Path]::GetFileName($resolved)).StartsWith('yui-launcher-test-')) {
        Remove-Item -LiteralPath $resolved -Recurse -Force
    }
}
