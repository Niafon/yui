#!/usr/bin/env bash
# Запуск на Linux и macOS. Windows — scripts/start.ps1.
#
# Ядро само поднимает воркеры через супервизор, поэтому здесь один процесс.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root/core"

if [[ "${1:-}" == "--dev" ]]; then
    export YUI_DB_DRIVER=memory
    echo "Режим разработки: file-backed хранилище."
elif [[ -z "${YUI_PASSWORD:-}" ]]; then
    echo "YUI_PASSWORD не задан — чувствительные поля останутся закрытыми." >&2
fi

exec go run ./cmd/yui-core -config ../yui.config.json
