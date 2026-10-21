//go:build integration

package integration

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/platform"
	repoteam "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/team"
	repotenant "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/tenant"
	appteam "github.com/hanmahong5-arch/lurus-tally/internal/app/team"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/team"
	"github.com/hanmahong5-arch/lurus-tally/internal/lifecycle"
)

// teamTestDB returns a migrated database. TEAM_TEST_DSN (an already running
// local PG) wins over a throwaway container so the suite can run without Docker.
func teamTestDB(t *testing.T) *sql.DB {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	dsn := os.Getenv("TEAM_TEST_DSN")
	if dsn == "" {
		var cleanup func()
		dsn, cleanup = startPostgres(t)
		t.Cleanup(cleanup)
	}
	if err := lifecycle.RunMigrations(ctx, dsn, nil); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newTeamShop(t *testing.T, db *sql.DB, ownerSub string) (uuid.UUID, appteam.Store) {
	t.Helper()
	ctx := context.Background()
	tid := uuid.New()
	if err := repotenant.NewTenantRepo(db).Create(ctx, tid, "团队测试店"); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	store := repoteam.New(db)
	if err := store.InsertMapping(ctx, appteam.Mapping{
		TenantID: tid, IDPSubject: ownerSub, Email: ownerSub + "@t.cn", Role: "owner", IsOwner: true,
	}); err != nil {
		t.Fatalf("insert owner mapping: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM tally.tenant WHERE id = $1`, tid)
	})
	return tid, store
}

func TestTeamStore_RoundTrip(t *testing.T) {
	db := teamTestDB(t)
	ctx := context.Background()
	sub := "owner-" + uuid.NewString()
	tid, store := newTeamShop(t, db, sub)

	name, org, err := store.TenantOrg(ctx, tid)
	if err != nil || name != "团队测试店" || org != 0 {
		t.Fatalf("fresh tenant: %q %d %v", name, org, err)
	}
	orgID := time.Now().UnixNano() % 1_000_000_000
	if err := store.SetTenantOrg(ctx, tid, orgID); err != nil {
		t.Fatal(err)
	}
	// first writer wins
	if err := store.SetTenantOrg(ctx, tid, orgID+1); err != nil {
		t.Fatal(err)
	}
	if _, got, _ := store.TenantOrg(ctx, tid); got != orgID {
		t.Fatalf("org overwritten: %d", got)
	}
	if got, err := store.TenantByOrg(ctx, orgID); err != nil || got != tid {
		t.Fatalf("by org: %v %v", got, err)
	}
	if got, _ := store.TenantByOrg(ctx, orgID+7); got != uuid.Nil {
		t.Fatalf("unknown org: %v", got)
	}
	if _, _, err := store.TenantOrg(ctx, uuid.New()); !errors.Is(err, appteam.ErrTenantNotFound) {
		t.Fatalf("unknown tenant: %v", err)
	}

	// duplicate subject
	err = store.InsertMapping(ctx, appteam.Mapping{TenantID: tid, IDPSubject: sub, Email: "x@t.cn", Role: "clerk"})
	if !errors.Is(err, appteam.ErrMappingExists) {
		t.Fatalf("dup mapping: %v", err)
	}
	// every new role passes the CHECK
	for i, role := range []string{"manager", "clerk", "cashier", "finance"} {
		s := sub + "-m" + string(rune('a'+i))
		if err := store.InsertMapping(ctx, appteam.Mapping{TenantID: tid, IDPSubject: s, Email: s + "@t.cn", Role: role, AccountID: int64(9_000_000_000 + i)}); err != nil {
			t.Fatalf("role %s rejected: %v", role, err)
		}
	}
	if err := store.InsertMapping(ctx, appteam.Mapping{TenantID: tid, IDPSubject: sub + "-bad", Email: "b@t.cn", Role: "boss"}); err == nil {
		t.Fatal("unknown role must violate the CHECK")
	}

	// projection sync: role + account id; the owner row stays owner
	if err := store.SyncMapping(ctx, tid, sub, "clerk", 9_100_000_000); err != nil {
		t.Fatal(err)
	}
	m, _ := store.MappingBySub(ctx, sub)
	if m.Role != "owner" || m.AccountID != 9_100_000_000 || !m.IsOwner {
		t.Fatalf("owner projection: %+v", m)
	}
	byAcct, _ := store.MappingByAccount(ctx, tid, 9_000_000_001)
	if byAcct == nil || byAcct.Role != "clerk" {
		t.Fatalf("by account: %+v", byAcct)
	}
	// deletes remove members but never the owner row
	if err := store.DeleteMappingByAccount(ctx, tid, 9_000_000_001); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.MappingByAccount(ctx, tid, 9_000_000_001); got != nil {
		t.Fatal("member row not deleted")
	}
	if err := store.DeleteMappingByAccount(ctx, tid, 9_100_000_000); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.MappingBySub(ctx, sub); got == nil {
		t.Fatal("owner row must survive delete")
	}
}

func TestTeamService_EndToEnd_DevStandIn(t *testing.T) {
	db := teamTestDB(t)
	ctx := context.Background()
	ownerSub := "owner-" + uuid.NewString()
	tid, store := newTeamShop(t, db, ownerSub)

	dev, err := platform.NewDevOrgClient(db, true)
	if err != nil {
		t.Fatal(err)
	}
	svc := appteam.NewService(store, dev, dev, nil)
	owner := appteam.Caller{TenantID: tid, Sub: ownerSub, Email: ownerSub + "@t.cn"}

	// lazy backfill by the owner
	members, err := svc.ListMembers(ctx, owner)
	if err != nil || len(members) != 1 || members[0].Role != domain.RoleOwner || !members[0].IsSelf {
		t.Fatalf("members: %+v %v", members, err)
	}
	if _, org, _ := store.TenantOrg(ctx, tid); org == 0 {
		t.Fatal("org not pinned on tenant")
	}

	inv, err := svc.CreateInvite(ctx, owner, domain.RoleCashier, 0)
	if err != nil {
		t.Fatal(err)
	}
	cashierSub := "cashier-" + uuid.NewString()
	res, err := svc.Join(ctx, appteam.JoinInput{Sub: cashierSub, Email: cashierSub + "@t.cn", Name: "小李", Token: inv.Token})
	if err != nil || res.Role != domain.RoleCashier || res.TenantID != tid {
		t.Fatalf("join: %+v %v", res, err)
	}
	// single use: a second person with the same token is refused
	if _, err := svc.Join(ctx, appteam.JoinInput{Sub: "other-" + uuid.NewString(), Token: inv.Token}); !errors.Is(err, appteam.ErrInviteInvalid) {
		t.Fatalf("reuse: %v", err)
	}
	// one person, one shop
	inv2, _ := svc.CreateInvite(ctx, owner, domain.RoleClerk, 1)
	if _, err := svc.Join(ctx, appteam.JoinInput{Sub: cashierSub, Token: inv2.Token}); !errors.Is(err, appteam.ErrAlreadyInShop) {
		t.Fatalf("second shop: %v", err)
	}
	ivs, err := svc.ListInvites(ctx, owner)
	if err != nil || len(ivs) != 1 {
		t.Fatalf("pending invites: %+v %v", ivs, err)
	}
	if err := svc.RevokeInvite(ctx, owner, ivs[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Join(ctx, appteam.JoinInput{Sub: "late-" + uuid.NewString(), Token: inv2.Token}); !errors.Is(err, appteam.ErrInviteInvalid) {
		t.Fatalf("revoked: %v", err)
	}

	role, err := svc.ResolveRole(ctx, tid, cashierSub)
	if err != nil || role != domain.RoleCashier {
		t.Fatalf("resolve: %v %v", role, err)
	}
	// cashier cannot manage
	if _, err := svc.ListMembers(ctx, appteam.Caller{TenantID: tid, Sub: cashierSub}); !errors.Is(err, appteam.ErrForbidden) {
		t.Fatalf("cashier list: %v", err)
	}
	members, _ = svc.ListMembers(ctx, owner)
	var cashierAcct int64
	for _, m := range members {
		if m.Role == domain.RoleCashier {
			cashierAcct = m.AccountID
		}
	}
	if cashierAcct == 0 {
		t.Fatalf("cashier not listed: %+v", members)
	}
	if err := svc.UpdateMemberRole(ctx, owner, cashierAcct, domain.RoleFinance); err != nil {
		t.Fatal(err)
	}
	if role, _ := svc.ResolveRole(ctx, tid, cashierSub); role != domain.RoleFinance {
		t.Fatalf("after change: %v", role)
	}
	if err := svc.RemoveMember(ctx, owner, cashierAcct); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResolveRole(ctx, tid, cashierSub); !errors.Is(err, appteam.ErrNotMember) {
		t.Fatalf("after removal: %v", err)
	}
	if m, _ := store.MappingBySub(ctx, cashierSub); m != nil {
		t.Fatal("local mapping must be gone so the person can join elsewhere")
	}
	if err := svc.RemoveMember(ctx, owner, members[0].AccountID); !errors.Is(err, appteam.ErrLastOwner) {
		t.Fatalf("owner removal: %v", err)
	}
}

// TestTeamMigration_DownThenUp checks 000059 reverses and re-applies cleanly.
// Everything runs in one transaction that is rolled back, so the shared
// database is left untouched.
func TestTeamMigration_DownThenUp(t *testing.T) {
	db := teamTestDB(t)
	read := func(name string) string {
		b, err := os.ReadFile("../../migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, f := range []string{
		"000059_tenant_platform_org_and_member_roles.down.sql",
		"000059_tenant_platform_org_and_member_roles.up.sql",
	} {
		if _, err := tx.Exec(read(f)); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
	}
}
