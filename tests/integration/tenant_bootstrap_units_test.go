//go:build integration

// Bootstrap seeds the industry default units inside the onboarding
// transaction: a nursery tenant must find 株/棵/丛/袋/平方米 in unit_def on day
// one, and a second Bootstrap attempt for the same subject must fail cleanly
// (unique key) without duplicating or corrupting the seeded units.
//
//	go test -v -tags integration -timeout 180s ./tests/integration/ -run TestBootstrap_SeedsHorticultureUnits
package integration

import (
	"context"
	"testing"

	"github.com/google/uuid"

	repoTenant "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/tenant"
)

func TestBootstrap_SeedsHorticultureUnits(t *testing.T) {
	db, cleanup := sqlRealDB(t)
	defer cleanup()
	ctx := context.Background()
	store := repoTenant.NewSQLBootstrapStore(db)

	in := repoTenant.BootstrapInput{
		TenantID:    uuid.New(),
		TenantName:  "苗圃测试",
		IDPSubject:  "sub-nursery-" + uuid.NewString(),
		Email:       "nursery@example.com",
		DisplayName: "苗圃老板",
		ProfileType: "horticulture",
	}
	if err := store.Bootstrap(ctx, in); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	count := func() int {
		var n int
		if err := db.QueryRowContext(ctx,
			`SELECT count(*) FROM tally.unit_def WHERE tenant_id = $1 AND is_system = false`, in.TenantID).Scan(&n); err != nil {
			t.Fatalf("count units: %v", err)
		}
		return n
	}
	if n := count(); n != 5 {
		t.Fatalf("seeded units = %d, want 5", n)
	}
	var zhu int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM tally.unit_def WHERE tenant_id = $1 AND name = '株'`, in.TenantID).Scan(&zhu); err != nil || zhu != 1 {
		t.Fatalf("unit 株: count=%d err=%v", zhu, err)
	}

	// Same subject again: the mapping unique key rejects it, units untouched.
	in2 := in
	in2.TenantID = uuid.New()
	if err := store.Bootstrap(ctx, in2); err == nil {
		t.Fatal("second Bootstrap for the same subject should fail on the unique key")
	}
	if n := count(); n != 5 {
		t.Fatalf("units after failed re-bootstrap = %d, want 5", n)
	}
}
