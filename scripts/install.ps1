<#
.SYNOPSIS
    Проверяет окружение и готовит Yui к первому запуску на Windows.

.DESCRIPTION
    Скрипт ничего не устанавливает молча: он проверяет, чего не хватает, и
    говорит, что именно поставить. Установка одной командой — цель (NFR-007),
    но не ценой скрытых действий на чужой машине.

.EXAMPLE
    powershell -ExecutionPolicy Bypass -File scripts\install.ps1
#>

[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$missing = @()

function Test-Tool {
    param([string]$Name, [string]$Command, [string]$HowTo, [string]$MinVersion)
    $found = Get-Command $Command -ErrorAction SilentlyContinue
    if ($found) {
        Write-Host "  [ок]   $Name" -ForegroundColor Green
        return $true
    }
    Write-Host "  [нет]  $Name — $HowTo" -ForegroundColor Yellow
    $script:missing += $Name
    return $false
}

Write-Host "`nПроверка окружения" -ForegroundColor Cyan
Test-Tool "Go 1.27+"      "go"     "https://go.dev/dl/ (или: winget install GoLang.Go)"          | Out-Null
Test-Tool "Python 3.11+"  "python" "https://python.org (или: winget install Python.Python.3.12)" | Out-Null
Test-Tool "Node.js 20+"   "node"   "https://nodejs.org (или: winget install OpenJS.NodeJS.LTS)"  | Out-Null
Test-Tool "GCC (CGO/SQLite)" "gcc" "MSYS2 UCRT64/MinGW-w64; добавьте gcc в PATH"                     | Out-Null

if ($missing.Count -gt 0) {
    Write-Host "`nНе хватает: $($missing -join ', '). Поставьте и запустите скрипт снова.`n" -ForegroundColor Red
    exit 1
}

# --- шифрование тома -------------------------------------------------------
# BitLocker — первый из двух уровней защиты (ADR-025). Второй уровень
# (шифрование полей и медиа) ещё внедряется поэтапно (ROADMAP п.13), поэтому
# защита тома остаётся обязательной для локальной базы.
try {
    $volume = Get-BitLockerVolume -MountPoint (Split-Path -Qualifier $root) -ErrorAction Stop
    if ($volume.ProtectionStatus -ne "On") {
        Write-Host "`nВНИМАНИЕ: том $($volume.MountPoint) не защищён BitLocker." -ForegroundColor Yellow
        Write-Host "  Шифрование полей ещё внедряется; без BitLocker файл базы доступен с диска (ADR-025).`n"
    } else {
        Write-Host "  [ок]   BitLocker включён" -ForegroundColor Green
    }
} catch {
    Write-Host "  [?]    BitLocker: статус не определён (нужны права администратора)" -ForegroundColor DarkGray
}

# --- конфигурация ----------------------------------------------------------
$config = Join-Path $root "yui.config.json"
if (-not (Test-Path $config)) {
    Copy-Item (Join-Path $root "yui.config.example.json") $config
    Write-Host "`nСоздан yui.config.json из примера." -ForegroundColor Cyan
    Write-Host "  Профиль рассчитан на RTX 3080 10 ГБ; модели см. docs/decisions.md, ADR-017."
} else {
    Write-Host "`nyui.config.json уже существует — не трогаю." -ForegroundColor DarkGray
}

New-Item -ItemType Directory -Force -Path (Join-Path $root "data") | Out-Null

# --- зависимости -----------------------------------------------------------
Write-Host "`nЗависимости ядра" -ForegroundColor Cyan
Push-Location (Join-Path $root "core")
go mod tidy
Pop-Location

Write-Host "`nЗависимости сцены" -ForegroundColor Cyan
Push-Location (Join-Path $root "desktop")
npm install
Pop-Location

# --- storage ----------------------------------------------------------------
Write-Host "`nХранилище" -ForegroundColor Cyan
Write-Host "  SQLite + FTS5 встроены в yui-core; PostgreSQL/pgvector и отдельные миграции не нужны."
Write-Host "  Файл базы будет создан автоматически: data\yui.db"

Write-Host @"

Готово.

Дальше:
  1. Задайте пароль шифрования:   `$env:YUI_PASSWORD = 'ваш пароль'
  2. Запустите:                   scripts\start.ps1
  3. ЗАПИШИТЕ recovery-ключ, который ядро напечатает при первом запуске.
     Он печатается один раз и нигде не сохраняется (ADR-024).

"@ -ForegroundColor Green
