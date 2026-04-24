//go:build integration

// Package integration contains integration tests for lurus-tally.
// Run with: go test -v -tags integration -timeout 120s ./tests/integration/...
package integration

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/hanmahong5-arch/lurus-tally/internal/lifecycle"
)

// setupTestDB starts a postgres container, runs migrations, and returns a connected *sql.DB.
// The returned cleanup function terminates the container.
func setupTestDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	dsn, cleanup := startPostgres(t)

	ctx := context.Background()
	if err := lifecycle.RunMigrations(ctx, dsn, nil); err != nil {
		cleanup()
		t.Fatalf("setupTestDB RunMigrations: %v", err)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		cleanup()
		t.Fatalf("setupTestDB open db: %v", err)
	}

	return db, func() {
		db.Close()
		cleanup()
	}
}

// TestRLS_TenantProfile_CrossTenantInvisible verifies that RLS on tenant_profile
// prevents a user from seeing another tenant's row.
//
// Setup: two tenants (A, B) each have a tenant_profile row.
// When app.tenant_id is set to tenant A, only tenant A's row is visible.
func TestRLS_TenantProfile_CrossTenantInvisible(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create two tenant rows (no app.tenant_id needed for superuser insert).
	tenantA := uuid.New()
	tenantB := uuid.New()

	mustExec(t, db, `INSERT INTO tally.tenant (id, name) VALUES ($1, $2)`, tenantA, "Tenant A")
	mustExec(t, db, `INSERT INTO tally.tenant (id, name) VALUES ($1, $2)`, tenantB, "Tenant B")

	// Insert tenant_profile rows bypassing RLS (superuser connection).
	mustExec(t, db, `
		INSERT INTO tally.tenant_profile (id, tenant_id, profile_type, inventory_method)
		VALUES ($1, $2, 'cross_border', 'fifo')`,
		uuid.New(), tenantA)
	mustExec(t, db, `
		INSERT INTO tally.tenant_profile (id, tenant_id, profile_type, inventory_method)
		VALUES ($1, $2, 'retail', 'wac')`,
		uuid.New(), tenantB)

	// Now query with app.tenant_id set to tenant A in a transaction.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		"SET LOCAL app.tenant_id = '"+tenantA.String()+"'"); err != nil {
		t.Fatalf("set local app.tenant_id: %v", err)
	}

	rows, err := tx.QueryContext(ctx, "SELECT tenant_id FROM tally.tenant_profile")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	var seen []string
	for rows.Next() {
		var tid uuid.UUID
		if err := rows.Scan(&tid); err != nil {
			t.Fatalf("scan: %v", err)
		}
		seen = append(seen, tid.String())
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows error: %v", err)
	}

	if len(seen) != 1 {
		t.Errorf("expected 1 row visible to tenant A, got %d: %v", len(seen), seen)
	}
	if len(seen) > 0 && seen[0] != tenantA.String() {
		t.Errorf("expected tenant A's row, got %s", seen[0])
	}
}

// mustExec is a helper that calls ExecContext and fails the test on error.
func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("mustExec(%q): %v", query, err)
	}
}
