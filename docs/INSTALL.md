# Установка на Windows

Целевая конфигурация: Windows 10/11, NVIDIA RTX 3080 10 ГБ. Начиная с
архитектуры ADR-037 PostgreSQL и pgvector для обычной локальной установки не
нужны: Yui Core использует embedded SQLite в режиме WAL, FTS5 и точный
in-process semantic search.

## Шаг 0. Что понадобится

| Компонент | Зачем | Установка |
|---|---|---|
| Go 1.27+ | ядро | `winget install GoLang.Go` |
| GCC / MinGW-w64 | CGO для нативного SQLite | MSYS2 UCRT64, добавить `gcc` в PATH |
| Python 3.11+ | AI-воркеры | `winget install Python.Python.3.12` |
| Node.js 20+ | desktop stage | `winget install OpenJS.NodeJS.LTS` |
| NVIDIA-драйвер с CUDA 12+ | LLM/TTS | NVIDIA App / сайт NVIDIA |
| Ollama или LM Studio | локальная LLM | по выбору |

Отдельный сервер БД, Docker и pgvector больше не являются зависимостями.
Итоговая база — обычный файл `data/yui.db`.

Проверьте окружение:

```powershell
go version
gcc --version
python --version
node --version
```

## Шаг 1. Подготовка

```powershell
git clone <ваш-репозиторий> yui
cd yui
powershell -ExecutionPolicy Bypass -File scripts\install.ps1
```

Скрипт:

- проверяет Go/Python/Node/GCC;
- создаёт `yui.config.json` из примера;
- создаёт `data/`;
- скачивает Go/npm зависимости;
- не устанавливает и не запускает никакую БД.

SQLite schema встроена в бинарник через `go:embed`. При первом открытии Core
создаёт таблицы и FTS5; дальнейшие schema upgrades фиксируются в
`schema_migrations`.

## Шаг 2. Первый запуск на mock-провайдерах

```powershell
powershell -ExecutionPolicy Bypass -File scripts\start.ps1 -Dev
```

`-Dev` оставлен как детерминированный file-backed store для отладки. Для
проверки именно production storage запускайте без `-Dev`.

## Шаг 3. Рабочий SQLite storage + шифрование

```powershell
$env:YUI_PASSWORD = 'длинный пароль, который вы не забудете'
powershell -ExecutionPolicy Bypass -File scripts\start.ps1
```

Core автоматически создаст:

```text
data/
  yui.db
  yui.db-wal       # пока есть активные WAL-транзакции
  yui.db-shm
  yui.key
```

`-wal`/`-shm` — нормальные служебные файлы SQLite. Для резервной копии лучше
закрыть Core или использовать SQLite backup API; не копируйте только `yui.db`
во время активной записи.

При первом запуске шифрования запишите recovery key отдельно от базы.

## Шаг 4. Embeddings

Основной профиль теперь использует:

```text
Qwen/Qwen3-Embedding-0.6B
output dimension: 256
CPU
```

Установка optional ML dependency:

```powershell
cd workers
python -m pip install "sentence-transformers>=5.0.0"
cd ..
```

Запуск одного worker для проверки:

```powershell
cd workers
python -m yui_worker --kind embeddings --port 8801 `
  --model Qwen/Qwen3-Embedding-0.6B --device cpu --embedding-dim 256
```

Worker использует Matryoshka truncation (`truncate_dim=256`) и, если модель
содержит named prompt `query`, применяет его только к query embeddings.
Документные embeddings сохраняются без query-инструкции.

Если модель не установлена или не загрузилась, worker явно сообщает hash
fallback; это не считается production retrieval.

## Шаг 5. Локальная LLM и остальные workers

Настройка LLM/STT/TTS/Vision осталась прежней. После установки нужных ML
зависимостей достаточно:

```powershell
make run-workers
```

или запускать Core: supervisor поднимет workers, перечисленные в
`yui.config.json`.

## Как теперь устроен поиск памяти

```text
query
 ├─ Qwen3 embedding 256d
 │    └─ exact normalized cosine scan in yui-core
 └─ SQLite FTS5 / BM25
      ↓
   union candidates
      ↓
 existing Rank(): semantic + lexical + importance + recency + confidence
      ↓
 permission filtering
```

Векторный cache — производный. Durable source of truth — `memory_items` в
SQLite. После перезапуска cache восстанавливается из BLOB embeddings.

ANN намеренно не используется на малом personal corpus: это сохраняет exact
recall и не требует HNSW/DiskANN graph maintenance. Когда corpus действительно
станет большим, backend можно заменить за интерфейсом `MemoryRepo.VectorSearch`
без миграции остальных сервисов.

## Проверка

```powershell
cd core
go test -tags sqlite_fts5 ./internal/store/sqlite -count=1
go test -tags sqlite_fts5 ./... -race -count=1
```

Проверка всей системы:

```powershell
make test
```

## Частые проблемы

| Ошибка | Что делать |
|---|---|
| `Binary was compiled with 'CGO_ENABLED=0'` / ошибка `go-sqlite3` | установите GCC и убедитесь, что `CGO_ENABLED=1` |
| `no such module: fts5` | собирайте Core с `-tags sqlite_fts5`; Makefile/start.ps1 уже делают это |
| `database is locked` | не запускайте два writer-инстанса Core; WAL рассчитан на один authoritative writer |
| embedding worker пишет `hash fallback` | установите `sentence-transformers` и проверьте доступ к модели |
| старые embeddings другой размерности | они остаются durable, но exact search сравнивает только совпадающие dimensions; переиндексируйте память новой моделью |

## Старый PostgreSQL backend

Код `internal/store/postgres` и старые SQL migrations сохранены только для
экспорта/перехода со старой v0.2 установки. Если он нужен временно:

```bash
make db-up-legacy
```

Новые локальные установки не должны его поднимать.

## Замер порога exact → ANN

Порог ANN специально не захардкожен. На целевом ПК его можно измерить так:

```powershell
cd core
go test -tags sqlite_fts5 ./internal/store/sqlite -run '^$' -bench BenchmarkVectorCacheExact256 -benchmem
```

Бенчмарк прогоняет 256d exact retrieval на 1k/10k/50k/100k memories. Переход
на Qdrant Edge/Vec1 имеет смысл только когда p95/CPU/RAM exact-пути реально
перестанут укладываться в бюджет приложения.
