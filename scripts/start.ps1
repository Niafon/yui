<#
.SYNOPSIS
    Запускает Yui: ядро поднимает воркеры само, сцена открывается отдельно.

.DESCRIPTION
    Пользователь запускает одно приложение, а не пять терминалов (SRS 14.3).
    Воркерами управляет супервизор внутри ядра; этот скрипт лишь запускает
    ядро и, по желанию, десктоп-сцену.
#>

[CmdletBinding()]
param(
    [switch]$Dev,        # in-memory test store вместо SQLite
    [switch]$NoStage     # не открывать сцену
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot

if ($Dev) {
    $env:YUI_DB_DRIVER = "memory"
    Write-Host "Режим разработки: in-memory хранилище, данные не сохраняются между запусками." -ForegroundColor Yellow
} elseif (-not $env:YUI_PASSWORD) {
    Write-Host "YUI_PASSWORD не задан — чувствительные поля останутся закрытыми." -ForegroundColor Yellow
    Write-Host "  `$env:YUI_PASSWORD = 'ваш пароль'   (или запустите с -Dev)`n"
}

Push-Location (Join-Path $root "core")
try {
    if (-not $NoStage) {
        # Сцена запускается следом; токен ядро печатает в лог при старте.
        Start-Job -Name "yui-stage" -ScriptBlock {
            Start-Sleep -Seconds 4
            Set-Location $using:root\desktop
            npm run dev
        } | Out-Null
        Write-Host "Сцена откроется на http://localhost:5273 — добавьте ?token=<токен из лога>`n" -ForegroundColor Cyan
    }
    go run -tags sqlite_fts5 ./cmd/yui-core -config ..\yui.config.json
}
finally {
    Pop-Location
    Get-Job -Name "yui-stage" -ErrorAction SilentlyContinue | Stop-Job -PassThru | Remove-Job
}
