package lifecycle

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
)

// SeedNurseryDict inserts nursery species from the given SQL file when SEED_NURSERY_DICT=true.
// It is idempotent: rows with the seed tenant_id are skipped on conflict
// (the SQL file uses ON CONFLICT (tenant_id, name) DO NOTHING).
//
// sqlPath is the filesystem path to migrations/data/nursery_seed.sql.
// The function reads the file and executes it within a single transaction.
func SeedNurseryDict(ctx context.Context, db *sql.DB, sqlPath string, log *slog.Logger) error {
	data, err := os.ReadFile(sqlPath)
	if err != nil {
		return fmt.Errorf("seed nursery dict: read sql file %q: %w", sqlPath, err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("seed nursery dict: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, string(data))
	if err != nil {
		return fmt.Errorf("seed nursery dict: exec sql: %w", err)
	}

	n, _ := res.RowsAffected()
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("seed nursery dict: commit: %w", err)
	}

	log.Info("nursery seed: loaded rows", slog.Int64("rows_inserted", n))
	return nil
}
