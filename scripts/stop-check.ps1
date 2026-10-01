$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$state = Join-Path $root '.cache\check-runtime.json'
if (-not (Test-Path $state)) { return }
$run = Get-Content $state -Raw | ConvertFrom-Json
$core = Get-Process -Id $run.pid -ErrorAction SilentlyContinue
if (-not $core) { return }
if ($core.Path -ne (Join-Path $root 'bin\yui-core.exe')) { throw 'Stored PID belongs to another process.' }
$status = Invoke-RestMethod http://127.0.0.1:8766/v1/status -Headers @{Authorization="Bearer $($run.token)"}
$owned = @($core) + @(foreach ($worker in $status.workers) {
    if ($worker.pid -gt 0) {
        $process = Get-Process -Id $worker.pid -ErrorAction SilentlyContinue
        if ($process) {
            if (-not $process.Path.StartsWith($root + '\',[StringComparison]::OrdinalIgnoreCase)) { throw 'Unexpected worker executable.' }
            $process
        }
    }
})
foreach ($process in $owned) { Stop-Process -InputObject $process -Force; $process.WaitForExit(5000) | Out-Null }
Write-Host 'Verification command center stopped.'
