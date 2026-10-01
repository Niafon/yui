# Storage / retrieval v0.3

## Итоговая схема

Local mode больше не требует PostgreSQL, pgvector, Docker или отдельного vector
server. Единственный authoritative store — `data/yui.db` (SQLite/WAL).

- structured state: обычные SQLite tables/indexes;
- lexical memory: external-content FTS5, relevance via `ORDER BY rank` (BM25);
- semantic memory: embeddings лежат в `memory_items.embedding` как float32 BLOB;
- hot semantic index: rebuildable in-process exact cache;
- cache layout: sequential entries + `id -> position`, normalized document vectors;
- top-k: allocation-light typed min-heap;
- default embedding profile: `Qwen/Qwen3-Embedding-0.6B`, 256 dimensions;
- query/document embedding roles разделены, query использует named query prompt;
- PostgreSQL implementation оставлен только как legacy/export backend.

## Почему exact, а не ANN

Для personal-memory corpus ANN добавляет graph/index maintenance и approximate
recall прежде, чем это становится необходимо. Формат данных от ANN не зависит:
если local benchmark покажет, что exact retrieval занял заметную часть TTFT,
реализацию `MemoryRepo.VectorSearch` можно заменить на Qdrant Edge, Vec1,
HNSW или DiskANN без изменения durable schema.

## Запуск

```powershell
copy yui.config.example.json yui.config.json
$env:YUI_PASSWORD = '...'
scripts\install.ps1
scripts\start.ps1
```

Core автоматически создаёт `data/yui.db` и применяет embedded schema. Сборка
Go должна идти с тегом `sqlite_fts5`; Makefile, CI и Windows scripts уже делают
это автоматически.

## Benchmark ANN threshold

```powershell
cd core
go test -tags sqlite_fts5 ./internal/store/sqlite -run '^$' -bench BenchmarkVectorCacheExact256 -benchmem
```

Сценарии: 1k, 10k, 50k и 100k memories по 256 dimensions. Решение о включении
ANN принимается по p95/CPU/RAM на целевой машине, а не по фиксированному числу
из внешнего benchmark.

## Миграция с v0.2

Старый PostgreSQL backend и SQL migrations не удалены. Он запускается через:

```powershell
make db-up-legacy
```

В v0.3 он предназначен для чтения/экспорта старых установок. Автоматический
one-click importer PostgreSQL -> SQLite пока не добавлен; если существующая
v0.2 база содержит важные данные, её нельзя удалять до экспорта.
