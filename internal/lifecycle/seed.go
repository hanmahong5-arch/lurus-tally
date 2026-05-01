package lifecycle

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	migrationdata "github.com/hanmahong5-arch/lurus-tally/migrations/data"
)

// SeedNurseryDict loads the embedded nursery_seed.sql when SEED_NURSERY_DICT=true.
// It is idempotent: the SQL uses ON CONFLICT (tenant_id, name) DO NOTHING.
// Reading from migrations/data embed.FS avoids filesystem dependence (scratch image safe).
func SeedNurseryDict(ctx context.Context, db *sql.DB, log *slog.Logger) error {
	data, err := migrationdata.FS.ReadFile("nursery_seed.sql")
	if err != nil {
		return fmt.Errorf("seed nursery dict: read embedded sql: %w", err)
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
