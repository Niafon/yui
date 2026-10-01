module github.com/yui-companion/core

// Go 1.27 (август 2026). Причины апгрейда — ADR-032:
//   - Green Tea GC по умолчанию с 1.26: -10..40% накладных расходов сборщика,
//     что заметно для процесса, живущего неделями;
//   - encoding/json v2 под капотом encoding/json: заметно быстрее разбор,
//     а JSON у нас на горячем пути (кадры WebSocket, протокол воркеров);
//   - профиль goroutineleak: основной режим отказа долгой сессии;
//   - testing/synctest: детерминированные тесты планировщика без sleep;
//   - crypto/tls MLKEM1024: постквантовый обмен ключами для хранилища,
//     которое живёт годами.
go 1.27

require (
	github.com/jackc/pgx/v5 v5.6.0
	github.com/mattn/go-sqlite3 v1.14.49
	golang.org/x/crypto v0.27.0
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20221227161230-091c0ba34f0a // indirect
	github.com/jackc/puddle/v2 v2.2.1 // indirect
	golang.org/x/sync v0.8.0 // indirect
	golang.org/x/sys v0.25.0 // indirect
	golang.org/x/text v0.18.0 // indirect
)

// SQLite — production storage (ADR-037); pgx оставлен для миграции/legacy backend.
// Argon2id используется для key derivation (ADR-024).
