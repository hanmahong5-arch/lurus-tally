//go:build integration

// Package integration contains integration tests for lurus-tally.
// These tests require Docker and the pgvector/pgvector:pg16 image which bundles pgvector 0.8.x.
// The stock postgres:16-alpine image does NOT include pgvector — always use pgvector/pgvector:pg16.
//
// Run with:
//
//	go test -v -tags integration -timeout 120s ./tests/integration/...
package integration

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/hanmahong5-arch/lurus-tally/internal/lifecycle"
)

const (
	// pgvectorImage bundles pgvector for pg16; use this instead of stock postgres:16-alpine
	// to ensure CREATE EXTENSION "vector" succeeds.
	pgvectorImage = "pgvector/pgvector:pg16"
	testDB        = "tally_test"
	testUser      = "tally"
	testPassword  = "tally_secret"
)

// startPostgres launches a pgvector-enabled PostgreSQL container and returns the DSN.
func startPostgres(t *testing.T) (string, func()) {
	t.Helper()
	ctx := context.Background()

	pgc, err := tcpostgres.Run(ctx,
		pgvectorImage,
		tcpostgres.WithDatabase(testDB),
		tcpostgres.WithUsername(testUser),
		tcpostgres.WithPassword(testPassword),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}

	dsn, err := pgc.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		pgc.Terminate(ctx) //nolint:errcheck
		t.Fatalf("get connection string: %v", err)
	}

	return dsn, func() {
		pgc.Terminate(ctx) //nolint:errcheck
	}
}

// TestMigration_AllTablesExist verifies that running migrate up on an empty database
// creates all 27 base tables + 1 view (≥27 total) and at least 1 materialized view.
func TestMigration_AllTablesExist(t *testing.T) {
	dsn, cleanup := startPostgres(t)
	defer cleanup()

	ctx := context.Background()
	if err := lifecycle.RunMigrations(ctx, dsn, nil); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	// AC-1: base tables + views count
	var tableCount int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema = 'tally'
		  AND table_type IN ('BASE TABLE', 'VIEW')
	`).Scan(&tableCount)
	if err != nil {
		t.Fatalf("count tables: %v", err)
	}
	t.Logf("tables in tally schema: %d (27 base + 1 view expected)", tableCount)
	if tableCount < 27 {
		t.Errorf("expected ≥27 tables/views in tally schema, got %d", tableCount)
	}

	// AC-1: materialized views
	var mvCount int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM pg_matviews WHERE schemaname = 'tally'
	`).Scan(&mvCount)
	if err != nil {
		t.Fatalf("count materialized views: %v", err)
	}
	t.Logf("materialized views: %d", mvCount)
	if mvCount < 1 {
		t.Errorf("expected ≥1 materialized view in tally schema, got %d", mvCount)
	}
}

// TestMigration_Idempotent verifies that running migrate up twice does not fail
// and does not change the table count.
func TestMigration_Idempotent(t *testing.T) {
	dsn, cleanup := startPostgres(t)
	defer cleanup()

	ctx := context.Background()

	// First run
	if err := lifecycle.RunMigrations(ctx, dsn, nil); err != nil {
		t.Fatalf("first RunMigrations: %v", err)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	var countAfterFirst int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema = 'tally'
		  AND table_type IN ('BASE TABLE', 'VIEW')
	`).Scan(&countAfterFirst)
	if err != nil {
		t.Fatalf("count after first run: %v", err)
	}

	// Second run — must return nil (ErrNoChange treated as success)
	if err := lifecycle.RunMigrations(ctx, dsn, nil); err != nil {
		t.Fatalf("second RunMigrations must return nil (idempotent), got: %v", err)
	}

	var countAfterSecond int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema = 'tally'
		  AND table_type IN ('BASE TABLE', 'VIEW')
	`).Scan(&countAfterSecond)
	if err != nil {
		t.Fatalf("count after second run: %v", err)
	}

	if countAfterFirst != countAfterSecond {
		t.Errorf("table count changed after second run: %d → %d", countAfterFirst, countAfterSecond)
	}
	t.Logf("table count stable after two runs: %d", countAfterSecond)
}

// TestMigration_DownReverses verifies that running migrate down -all removes all base tables
// from the tally schema.
func TestMigration_DownReverses(t *testing.T) {
	dsn, cleanup := startPostgres(t)
	defer cleanup()

	ctx := context.Background()

	if err := lifecycle.RunMigrations(ctx, dsn, nil); err != nil {
		t.Fatalf("RunMigrations up: %v", err)
	}

	// Run down-all via direct migrate call.
	if err := lifecycle.RunMigrationsDown(ctx, dsn); err != nil {
		t.Fatalf("RunMigrationsDown: %v", err)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	// 000001 down drops the entire tally schema; information_schema.tables will show 0 tally rows.
	var remaining int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema = 'tally'
		  AND table_type = 'BASE TABLE'
	`).Scan(&remaining)
	if err != nil {
		t.Fatalf("count after down: %v", err)
	}
	t.Logf("tables after down: %d", remaining)
	if remaining != 0 {
		t.Errorf("expected 0 base tables after down-all, got %d", remaining)
	}
}

// TestMigration_pgvectorAvailable verifies that the vector extension was installed successfully.
func TestMigration_pgvectorAvailable(t *testing.T) {
	dsn, cleanup := startPostgres(t)
	defer cleanup()

	ctx := context.Background()
	if err := lifecycle.RunMigrations(ctx, dsn, nil); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	var extVersion string
	err = db.QueryRowContext(ctx, `
		SELECT extversion FROM pg_extension WHERE extname = 'vector'
	`).Scan(&extVersion)
	if err != nil {
		t.Fatalf("query vector extension: %v", err)
	}
	if extVersion == "" {
		t.Error("vector extension version must not be empty")
	}
	t.Logf("vector extension version: %s", extVersion)
}

// TestMigration_RLSEnabled verifies that all 11 expected tenant-scoped tables
// have a tenant_isolation RLS policy applied.
func TestMigration_RLSEnabled(t *testing.T) {
	dsn, cleanup := startPostgres(t)
	defer cleanup()

	ctx := context.Background()
	if err := lifecycle.RunMigrations(ctx, dsn, nil); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, `
		SELECT tablename FROM pg_policies
		WHERE schemaname = 'tally'
		ORDER BY tablename
	`)
	if err != nil {
		t.Fatalf("query pg_policies: %v", err)
	}
	defer rows.Close()

	var rlsTables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan tablename: %v", err)
		}
		rlsTables = append(rlsTables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows error: %v", err)
	}

	expected := []string{
		"audit_log", "bill_head", "bill_item", "bill_sequence",
		"org_department", "partner", "payment_head",
		"product", "stock_snapshot", "system_config", "warehouse",
	}
	sort.Strings(expected)

	t.Logf("RLS tables: %v", rlsTables)

	for _, want := range expected {
		found := false
		for _, got := range rlsTables {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected RLS policy on tally.%s, not found in: %s",
				want, strings.Join(rlsTables, ", "))
		}
	}

	if len(rlsTables) < len(expected) {
		t.Errorf("expected ≥%d RLS tables, got %d", len(expected), len(rlsTables))
	}
	_ = fmt.Sprintf // suppress import warning
}
