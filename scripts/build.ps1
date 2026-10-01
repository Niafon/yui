[CmdletBinding()]
param()
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$env:GOCACHE = Join-Path $root '.cache\go-build'
$env:GOMODCACHE = Join-Path $root '.cache\go-mod'
Push-Location (Join-Path $root 'core')
try {
    go test -tags sqlite_fts5 ./...
    if ($LASTEXITCODE -ne 0) { throw 'Core tests failed' }
    go build -tags sqlite_fts5 -o ..\bin\yui-core.exe ./cmd/yui-core
    if ($LASTEXITCODE -ne 0) { throw 'Core build failed' }
} finally { Pop-Location }
Push-Location (Join-Path $root 'workers')
try {
    & (Join-Path $root '.venv\Scripts\python.exe') -m unittest discover -s tests
    if ($LASTEXITCODE -ne 0) { throw 'Worker tests failed' }
} finally { Pop-Location }
Push-Location (Join-Path $root 'desktop')
try {
    npm.cmd run build
    if ($LASTEXITCODE -ne 0) { throw 'Stage build failed' }
} finally { Pop-Location }
$flutter = (Get-Command flutter -ErrorAction SilentlyContinue).Source
if (-not $flutter) {
    $flutterSdk = (Get-Content (Join-Path $root 'mobile\android\local.properties') |
        Where-Object { $_ -match '^flutter\.sdk=(.+)$' } |
        ForEach-Object { $Matches[1] })
    if ($flutterSdk) { $flutter = Join-Path $flutterSdk 'bin\flutter.bat' }
}
if (-not $flutter -or -not (Test-Path $flutter)) {
    Write-Warning 'Flutter SDK not found; mobile checks were skipped.'
} else {
    Push-Location (Join-Path $root 'mobile')
    try {
        & $flutter analyze
        if ($LASTEXITCODE -ne 0) { throw 'Mobile analysis failed' }
        & $flutter test
        if ($LASTEXITCODE -ne 0) { throw 'Mobile tests failed' }
        & $flutter build apk --debug
        if ($LASTEXITCODE -ne 0) { throw 'Mobile APK build failed' }
    } finally { Pop-Location }
}
Write-Host 'Build ready. Run Start-Yui.cmd.'
