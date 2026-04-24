package lifecycle

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/golang-migrate/migrate/v4"
	migratepg "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib" // pgx driver for database/sql
	"github.com/hanmahong5-arch/lurus-tally/migrations"
)

// RunMigrations applies all pending SQL migrations from the embedded migrations directory.
// It is idempotent: already-applied migrations are skipped via the tally.schema_migrations table.
// Returns nil if all migrations are already up-to-date (ErrNoChange is treated as success).
//
// On error the message includes three elements:
//   - what happened (e.g. "migration failed: ping db")
//   - what was expected ("ensure PostgreSQL is reachable at DATABASE_DSN")
//   - what the caller can do (check DSN, pgvector availability, network)
func RunMigrations(ctx context.Context, dsn string, logger *slog.Logger) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("migration failed: cannot parse DATABASE_DSN: %w; "+
			"expected a valid PostgreSQL DSN (e.g. postgres://user:pass@host/db?sslmode=disable); "+
			"check DATABASE_DSN environment variable", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("migration failed: cannot reach PostgreSQL: %w; "+
			"expected the database to be reachable at DATABASE_DSN; "+
			"check network connectivity, credentials, and that the PostgreSQL pod is Running", err)
	}

	if _, err := db.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS tally`); err != nil {
		return fmt.Errorf("migration failed: cannot create tally schema: %w; "+
			"ensure the DATABASE_DSN user has CREATE SCHEMA privilege", err)
	}

	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("migration failed: cannot load embedded SQL files: %w; "+
			"this is a build error — rebuild the binary", err)
	}

	driver, err := migratepg.WithInstance(db, &migratepg.Config{
		MigrationsTable: "schema_migrations",
		SchemaName:      "tally",
	})
	if err != nil {
		return fmt.Errorf("migration failed: cannot create postgres driver: %w; "+
			"ensure the DATABASE_DSN user has CREATE SCHEMA and CREATE TABLE privileges", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, "postgres", driver)
	if err != nil {
		return fmt.Errorf("migration failed: cannot initialize migrate instance: %w; "+
			"check that migration files are valid and the database is accessible", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migration failed: apply up migrations: %w; "+
			"check that pgvector is installed (SELECT * FROM pg_available_extensions WHERE name='vector'), "+
			"DATABASE_DSN is correct, and the target database is accessible", err)
	}

	if logger != nil {
		version, dirty, vErr := m.Version()
		if vErr != nil {
			logger.Info("migration completed", slog.String("version", "unknown"))
		} else {
			logger.Info("migration completed",
				slog.Uint64("version", uint64(version)),
				slog.Bool("dirty", dirty),
			)
		}
	}

	return nil
}

// RunMigrationsDown reverses all applied migrations (down-all).
// Intended only for integration tests and operator tooling; never called during normal startup.
func RunMigrationsDown(ctx context.Context, dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("migration down: cannot parse DSN: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("migration down: cannot reach PostgreSQL: %w", err)
	}

	if _, err := db.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS tally`); err != nil {
		return fmt.Errorf("migration down: cannot create tally schema: %w", err)
	}

	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("migration down: cannot load embedded SQL files: %w", err)
	}

	driver, err := migratepg.WithInstance(db, &migratepg.Config{
		MigrationsTable: "schema_migrations",
		SchemaName:      "tally",
	})
	if err != nil {
		return fmt.Errorf("migration down: cannot create postgres driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, "postgres", driver)
	if err != nil {
		return fmt.Errorf("migration down: cannot initialize migrate instance: %w", err)
	}

	if err := m.Down(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migration down: apply down migrations: %w", err)
	}
	return nil
}
