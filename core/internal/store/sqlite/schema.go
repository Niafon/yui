package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
)

//go:embed schema.sql
var schemaFS embed.FS

func applySchema(ctx context.Context, db *sql.DB) error {
	schema, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		return fmt.Errorf("sqlite: read schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, string(schema)); err != nil {
		return fmt.Errorf("sqlite: apply schema (build with -tags sqlite_fts5): %w", err)
	}
	// Migration 5 introduced the external-content FTS index. Rebuild it once
	// for databases that already contained memory rows; doing this on every
	// startup would make cold-start cost grow with the entire memory corpus.
	var ftsMigrated int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version=5`).Scan(&ftsMigrated); err != nil {
		return fmt.Errorf("sqlite: read FTS5 migration state: %w", err)
	}
	if ftsMigrated == 0 {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("sqlite: begin FTS5 migration: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO memory_fts(memory_fts) VALUES('rebuild')`); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("sqlite: rebuild FTS5 index: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(5)`); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("sqlite: record FTS5 migration: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("sqlite: commit FTS5 migration: %w", err)
		}
	}
	return nil
}
