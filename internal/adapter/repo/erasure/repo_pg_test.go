// Package erasure_test holds the PG integration test for the erasure repo. It
// needs a real PostgreSQL with the tally schema + FORCE RLS (migrations applied)
// and is skipped when DATABASE_DSN is not set. The hermetic SQLite shim cannot
// exercise the RLS pin this code depends on, so correctness is proven here.
package erasure_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	repoerasure "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/erasure"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_DSN")
	if dsn == "" {
		t.Skip("DATABASE_DSN not set — skipping integration test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestErasureRepoPG_RedactsOwnerPreservesTenantIdempotent proves the PIPL §47
// cascade against real PG + FORCE RLS: the bootstrap owner's PII is redacted,
// the account is unlinked, the tenant's business row survives, and a replay is a
// zero-affected no-op.
func TestErasureRepoPG_RedactsOwnerPreservesTenantIdempotent(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()

	tenantID := uuid.New().String()
	const accountID = int64(987654321) // far above any real local account id
	sub := "erasure-pg-test-" + tenantID

	// Seed a tenant linked to accountID + its bootstrap-owner identity. uim is
	// FORCE-RLS, but the CASE policy treats an unset app.tenant_id as visible, so
	// these pool inserts are allowed.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO tally.tenant (id, name, platform_account_id) VALUES ($1, 'erasure-pg-test', $2)`,
		tenantID, accountID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM tally.user_identity_mapping WHERE tenant_id=$1`, tenantID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM tally.tenant WHERE id=$1`, tenantID)
	})
	if _, err := db.ExecContext(ctx,
		`INSERT INTO tally.user_identity_mapping (tenant_id, zitadel_sub, email, display_name, is_owner)
		 VALUES ($1, $2, 'owner@real.example', 'Real Owner', true)`,
		tenantID, sub); err != nil {
		t.Fatalf("seed owner identity: %v", err)
	}

	repo := repoerasure.New(db)

	n, err := repo.EraseByPlatformAccount(ctx, accountID)
	if err != nil {
		t.Fatalf("erase: %v", err)
	}
	if n != 1 {
		t.Fatalf("tenants_affected = %d, want 1", n)
	}

	var email, sub2 string
	var display sql.NullString
	var linked sql.NullInt64
	if err := db.QueryRowContext(ctx,
		`SELECT u.email, u.zitadel_sub, u.display_name, t.platform_account_id
		   FROM tally.tenant t
		   JOIN tally.user_identity_mapping u ON u.tenant_id = t.id AND u.is_owner = true
		  WHERE t.id = $1`, tenantID).Scan(&email, &sub2, &display, &linked); err != nil {
		t.Fatalf("read back failed (tenant row must survive): %v", err)
	}
	if email != "erased@tally.invalid" {
		t.Errorf("email = %q, want erased@tally.invalid", email)
	}
	if !strings.HasPrefix(sub2, "erased:") {
		t.Errorf("zitadel_sub = %q, want 'erased:' prefix", sub2)
	}
	if display.Valid {
		t.Errorf("display_name = %q, want NULL", display.String)
	}
	if linked.Valid {
		t.Errorf("platform_account_id = %d, want NULL (unlinked)", linked.Int64)
	}

	n2, err := repo.EraseByPlatformAccount(ctx, accountID)
	if err != nil {
		t.Fatalf("replay erase: %v", err)
	}
	if n2 != 0 {
		t.Errorf("replay tenants_affected = %d, want 0 (idempotent)", n2)
	}
}
