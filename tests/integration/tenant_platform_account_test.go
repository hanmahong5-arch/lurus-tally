//go:build integration

package integration

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	repotenant "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/tenant"
	"github.com/hanmahong5-arch/lurus-tally/internal/lifecycle"
)

// TestTenantPlatformAccountID_RoundTrip verifies the migration-000051 column
// behaves end-to-end against real PG: the reporter's resolver reads exactly what
// onboarding's SetPlatformAccountID writes, and an unpinned tenant reads back as
// "not provisioned" (the shadow-skip signal) rather than erroring.
func TestTenantPlatformAccountID_RoundTrip(t *testing.T) {
	dsn, cleanup := startPostgres(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := lifecycle.RunMigrations(ctx, dsn, nil); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	store := repotenant.NewSQLBootstrapStore(db)
	repo := repotenant.NewTenantRepo(db)

	tid := uuid.New()
	if err := repo.Create(ctx, tid, "UAT roundtrip co"); err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	// Fresh tenant: column is NULL → not provisioned, no error.
	if id, ok, err := repo.GetPlatformAccountID(ctx, tid); err != nil || ok || id != 0 {
		t.Fatalf("unprovisioned tenant: want (0,false,nil), got (%d,%v,%v)", id, ok, err)
	}

	// Onboarding pins the account id.
	if err := store.SetPlatformAccountID(ctx, tid, 4242); err != nil {
		t.Fatalf("set platform account id: %v", err)
	}

	// Reporter resolves it.
	id, ok, err := repo.GetPlatformAccountID(ctx, tid)
	if err != nil || !ok || id != 4242 {
		t.Fatalf("after pin: want (4242,true,nil), got (%d,%v,%v)", id, ok, err)
	}

	// Unknown tenant: not an error, just not provisioned.
	if id, ok, err := repo.GetPlatformAccountID(ctx, uuid.New()); err != nil || ok || id != 0 {
		t.Fatalf("unknown tenant: want (0,false,nil), got (%d,%v,%v)", id, ok, err)
	}
}
