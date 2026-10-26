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

	if _, err := tx.ExecContext(ctx, string(data)); err != nil {
		return fmt.Errorf("seed nursery dict: exec sql: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("seed nursery dict: commit: %w", err)
	}

	// database/sql RowsAffected on a multi-statement script only reflects the
	// last INSERT (~9 rows), which falsely looked like "seed wrote 9 of 199".
	// Count the shared dictionary instead (idempotent ON CONFLICT keeps this stable).
	const sharedTenant = "00000000-0000-0000-0000-000000000000"
	var total int64
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tally.nursery_dict WHERE tenant_id = $1 AND deleted_at IS NULL`,
		sharedTenant,
	).Scan(&total); err != nil {
		log.Warn("nursery seed: applied but count failed", slog.String("error", err.Error()))
		return nil
	}
	log.Info("nursery seed: shared dictionary ready", slog.Int64("rows", total))
	return nil
}
