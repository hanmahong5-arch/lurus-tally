package team

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/team"
)

type env struct {
	svc    *Service
	store  *fakeStore
	org    *fakeOrg
	tenant uuid.UUID
	orgID  int64
	now    *time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	tenant, orgID := uuid.New(), int64(77)
	st := newFakeStore(tenant)
	st.orgID = orgID
	org := newFakeOrg(orgID)
	now := time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)
	e := &env{store: st, org: org, tenant: tenant, orgID: orgID, now: &now}
	e.svc = NewService(st, org, fakeAccounts{ids: map[string]int64{"owner": 1, "mgr": 2, "clk": 3, "new": 9, "csh": 4}}, nil)
	e.svc.now = func() time.Time { return *e.now }
	org.members[1] = &OrgMember{AccountID: 1, Role: "owner", ProductRoles: map[string]string{}}
	org.members[2] = &OrgMember{AccountID: 2, Role: "admin", ProductRoles: map[string]string{}}
	org.members[3] = &OrgMember{AccountID: 3, Role: "member", ProductRoles: map[string]string{"tally": "clerk"}}
	st.maps["owner"] = &Mapping{TenantID: tenant, IDPSubject: "owner", Role: "owner", IsOwner: true, AccountID: 1}
	st.maps["mgr"] = &Mapping{TenantID: tenant, IDPSubject: "mgr", Role: "manager", AccountID: 2}
	st.maps["clk"] = &Mapping{TenantID: tenant, IDPSubject: "clk", Role: "clerk", AccountID: 3}
	return e
}

func (e *env) caller(sub string) Caller { return Caller{TenantID: e.tenant, Sub: sub} }

func TestResolveRole_PlatformAndCache(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	r, err := e.svc.ResolveRole(ctx, e.tenant, "mgr")
	if err != nil || r != domain.RoleManager {
		t.Fatalf("got %v %v", r, err)
	}
	_, _ = e.svc.ResolveRole(ctx, e.tenant, "mgr")
	if e.org.getCalls != 1 {
		t.Fatalf("second call must hit cache, platform calls=%d", e.org.getCalls)
	}
	*e.now = e.now.Add(61 * time.Second)
	_, _ = e.svc.ResolveRole(ctx, e.tenant, "mgr")
	if e.org.getCalls != 2 {
		t.Fatalf("cache must expire after 60s, calls=%d", e.org.getCalls)
	}
}

func TestResolveRole_InvalidateOnEvent(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_, _ = e.svc.ResolveRole(ctx, e.tenant, "clk")
	e.org.members[3].ProductRoles = map[string]string{"tally": "cashier"}
	if r, _ := e.svc.ResolveRole(ctx, e.tenant, "clk"); r != domain.RoleClerk {
		t.Fatalf("still cached, got %v", r)
	}
	e.svc.HandleMemberEvent(ctx, MemberEvent{Type: EventRoleChanged, OrgID: e.orgID, AccountID: 3})
	if r, _ := e.svc.ResolveRole(ctx, e.tenant, "clk"); r != domain.RoleCashier {
		t.Fatalf("after event want cashier, got %v", r)
	}
}

func TestResolveRole_NotMember(t *testing.T) {
	e := newEnv(t)
	delete(e.org.members, 3)
	if _, err := e.svc.ResolveRole(context.Background(), e.tenant, "clk"); !errors.Is(err, ErrNotMember) {
		t.Fatalf("want ErrNotMember, got %v", err)
	}
}

func TestResolveRole_PlatformDown(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_, _ = e.svc.ResolveRole(ctx, e.tenant, "mgr")
	e.org.getErr = ErrUnavailable
	*e.now = e.now.Add(5 * time.Minute)
	if r, err := e.svc.ResolveRole(ctx, e.tenant, "mgr"); err != nil || r != domain.RoleManager {
		t.Fatalf("stale: got %v %v", r, err)
	}
	if r, err := e.svc.ResolveRole(ctx, e.tenant, "clk"); err != nil || r != domain.RoleClerk {
		t.Fatalf("projection: got %v %v", r, err)
	}
}

func TestResolveRole_UnknownPersonAndOtherShop(t *testing.T) {
	e := newEnv(t)
	if _, err := e.svc.ResolveRole(context.Background(), e.tenant, "stranger"); !errors.Is(err, ErrNotMember) {
		t.Fatalf("want ErrNotMember, got %v", err)
	}
	if _, err := e.svc.ResolveRole(context.Background(), uuid.New(), "mgr"); !errors.Is(err, ErrNotMember) {
		t.Fatalf("other shop: want ErrNotMember, got %v", err)
	}
}

func TestResolveRole_NoPlatformClient_UsesProjection(t *testing.T) {
	e := newEnv(t)
	svc := NewService(e.store, nil, nil, nil)
	if r, err := svc.ResolveRole(context.Background(), e.tenant, "owner"); err != nil || r != domain.RoleOwner {
		t.Fatalf("got %v %v", r, err)
	}
	if r, _ := svc.ResolveRole(context.Background(), e.tenant, "clk"); r != domain.RoleClerk {
		t.Fatalf("got %v", r)
	}
}

func TestListMembers_Permissions(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.svc.ListMembers(ctx, e.caller("clk")); !errors.Is(err, ErrForbidden) {
		t.Fatalf("clerk must be forbidden, got %v", err)
	}
	ms, err := e.svc.ListMembers(ctx, e.caller("mgr"))
	if err != nil || len(ms) != 3 {
		t.Fatalf("manager list: %v %v", ms, err)
	}
	selfs := 0
	for _, m := range ms {
		if m.IsSelf {
			selfs++
			if m.AccountID != 2 {
				t.Fatalf("wrong self: %+v", m)
			}
		}
	}
	if selfs != 1 {
		t.Fatalf("exactly one self, got %d", selfs)
	}
}

func TestListMembers_Unavailable(t *testing.T) {
	e := newEnv(t)
	svc := NewService(e.store, nil, nil, nil)
	if _, err := svc.ListMembers(context.Background(), e.caller("owner")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
}

func TestListMembers_LazyBackfillByOwner(t *testing.T) {
	e := newEnv(t)
	e.store.orgID = 0
	e.org.members = map[int64]*OrgMember{}
	if _, err := e.svc.ListMembers(context.Background(), e.caller("mgr")); !errors.Is(err, ErrOrgNotReady) {
		t.Fatalf("manager without org: got %v", err)
	}
	if _, err := e.svc.ListMembers(context.Background(), e.caller("owner")); err != nil {
		t.Fatal(err)
	}
	if e.store.orgID != e.orgID || e.org.ensureCnt != 1 {
		t.Fatalf("org not backfilled: %v cnt=%d", e.store.orgID, e.org.ensureCnt)
	}
}

func TestUpdateMemberRole(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.org.members[5] = &OrgMember{AccountID: 5, Role: "admin", ProductRoles: map[string]string{}}
	if err := e.svc.UpdateMemberRole(ctx, e.caller("mgr"), 5, domain.RoleClerk); !errors.Is(err, ErrForbidden) {
		t.Fatalf("got %v", err)
	}
	if err := e.svc.UpdateMemberRole(ctx, e.caller("mgr"), 3, domain.RoleManager); !errors.Is(err, ErrForbidden) {
		t.Fatalf("got %v", err)
	}
	if err := e.svc.UpdateMemberRole(ctx, e.caller("owner"), 3, domain.RoleOwner); !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("got %v", err)
	}
	if err := e.svc.UpdateMemberRole(ctx, e.caller("mgr"), 3, domain.RoleCashier); err != nil {
		t.Fatal(err)
	}
	u := e.org.updates[len(e.org.updates)-1]
	if u.acct != 3 || u.actor != 2 || u.role != "member" || u.pr["tally"] != "cashier" || len(u.pr) != 1 {
		t.Fatalf("bad update %+v", u)
	}
	if m := e.store.maps["clk"]; m.Role != "cashier" {
		t.Fatalf("projection not synced: %s", m.Role)
	}
	e.org.members[3].ProductRoles = map[string]string{"tally": "cashier", "other": "x"}
	if err := e.svc.UpdateMemberRole(ctx, e.caller("owner"), 3, domain.RoleManager); err != nil {
		t.Fatal(err)
	}
	u = e.org.updates[len(e.org.updates)-1]
	// Only Tally's own key is sent (the platform merges per key); an empty value
	// clears it, and other products' keys are never echoed back.
	if v, ok := u.pr["tally"]; u.role != "admin" || !ok || v != "" || len(u.pr) != 1 {
		t.Fatalf("bad update %+v", u)
	}
	if err := e.svc.UpdateMemberRole(ctx, e.caller("mgr"), 1, domain.RoleClerk); !errors.Is(err, ErrForbidden) {
		t.Fatalf("got %v", err)
	}
	if r, _ := e.svc.ResolveRole(ctx, e.tenant, "clk"); r != domain.RoleManager {
		t.Fatalf("got %v", r)
	}
}

func TestRemoveMember(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if err := e.svc.RemoveMember(ctx, e.caller("mgr"), 1); !errors.Is(err, ErrForbidden) {
		t.Fatalf("manager removing owner: %v", err)
	}
	if err := e.svc.RemoveMember(ctx, e.caller("owner"), 1); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("owner removing self: %v", err)
	}
	if err := e.svc.RemoveMember(ctx, e.caller("mgr"), 3); err != nil {
		t.Fatal(err)
	}
	if len(e.org.removed) != 1 || e.org.removed[0] != 3 {
		t.Fatalf("removed=%v", e.org.removed)
	}
	if _, ok := e.store.maps["clk"]; ok {
		t.Fatal("local mapping must be removed")
	}
	if err := e.svc.RemoveMember(ctx, e.caller("mgr"), 2); err != nil {
		t.Fatalf("self leave: %v", err)
	}
}

func TestCreateInvite(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.svc.CreateInvite(ctx, e.caller("mgr"), domain.RoleManager, 0); !errors.Is(err, ErrForbidden) {
		t.Fatalf("manager inviting manager: %v", err)
	}
	if _, err := e.svc.CreateInvite(ctx, e.caller("clk"), domain.RoleClerk, 0); !errors.Is(err, ErrForbidden) {
		t.Fatalf("clerk inviting: %v", err)
	}
	if _, err := e.svc.CreateInvite(ctx, e.caller("owner"), domain.RoleOwner, 0); !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("owner invite: %v", err)
	}
	inv, err := e.svc.CreateInvite(ctx, e.caller("mgr"), domain.RoleCashier, 0)
	if err != nil || inv.Token != "tok123" {
		t.Fatalf("got %+v %v", inv, err)
	}
	if e.org.inviteRole != "member" || e.org.invitePR["tally"] != "cashier" {
		t.Fatalf("platform role %q %v", e.org.inviteRole, e.org.invitePR)
	}
}

func TestJoin(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.org.acceptRes = &AcceptResult{OrgID: e.orgID, Role: "member", ProductRoles: map[string]string{"tally": "cashier"}}
	if _, err := e.svc.Join(ctx, JoinInput{Sub: "clk", Token: "t"}); !errors.Is(err, ErrAlreadyInShop) {
		t.Fatalf("got %v", err)
	}
	if e.org.acceptCnt != 0 {
		t.Fatal("invite consumed despite conflict")
	}
	res, err := e.svc.Join(ctx, JoinInput{Sub: "new", Email: "n@x.cn", Name: "小王", Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Role != domain.RoleCashier || res.TenantID != e.tenant {
		t.Fatalf("got %+v", res)
	}
	m := e.store.maps["new"]
	if m == nil || m.Role != "cashier" || m.IsOwner || m.AccountID != 9 || m.TenantID != e.tenant {
		t.Fatalf("mapping %+v", m)
	}
	e.org.acceptErr = ErrInviteInvalid
	if _, err := e.svc.Join(ctx, JoinInput{Sub: "csh", Token: "bad"}); !errors.Is(err, ErrInviteInvalid) {
		t.Fatalf("got %v", err)
	}
}

func TestJoin_Unavailable(t *testing.T) {
	e := newEnv(t)
	svc := NewService(e.store, nil, nil, nil)
	if _, err := svc.Join(context.Background(), JoinInput{Sub: "new", Token: "t"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func TestMemberEventRemoved(t *testing.T) {
	e := newEnv(t)
	e.svc.HandleMemberEvent(context.Background(), MemberEvent{Type: EventRemoved, OrgID: e.orgID, AccountID: 3})
	if _, ok := e.store.maps["clk"]; ok {
		t.Fatal("mapping must be deleted on member_removed")
	}
}

func TestEnsureTenantOrg(t *testing.T) {
	e := newEnv(t)
	e.store.orgID = 0
	if err := e.svc.EnsureTenantOrg(context.Background(), e.tenant, 1); err != nil {
		t.Fatal(err)
	}
	if e.store.orgID != e.orgID {
		t.Fatal("org not stored")
	}
	if err := e.svc.EnsureTenantOrg(context.Background(), e.tenant, 1); err != nil || e.org.ensureCnt != 1 {
		t.Fatalf("err=%v cnt=%d", err, e.org.ensureCnt)
	}
}

// soloShop adds a person who opened a shop by mistake: sole member and owner of
// a second shop that holds only sample data.
func (e *env) soloShop(sub string, acct int64, empty bool) uuid.UUID {
	t := uuid.New()
	if e.store.shopOrg == nil {
		e.store.shopOrg, e.store.shopEmpty = map[uuid.UUID]int64{}, map[uuid.UUID]bool{}
	}
	e.store.shopOrg[t] = 500
	e.store.shopEmpty[t] = empty
	e.store.maps[sub] = &Mapping{TenantID: t, IDPSubject: sub, Role: "owner", IsOwner: true, AccountID: acct}
	e.org.soloOrgID = 500
	e.org.solo = []OrgMember{{AccountID: acct, Role: "owner", ProductRoles: map[string]string{}}}
	e.svc.accounts = fakeAccounts{ids: map[string]int64{"owner": 1, "mgr": 2, "clk": 3, "new": 9, "csh": 4, sub: acct}}
	return t
}

func TestJoin_AlreadyInShop_ReportsWhetherEmptyShopCanBeAbandoned(t *testing.T) {
	e := newEnv(t)
	e.soloShop("solo", 20, true)
	e.soloShop("busy", 21, false)
	e.org.solo = []OrgMember{{AccountID: 20, Role: "owner"}}
	for sub, want := range map[string]bool{"solo": true, "busy": false, "clk": false} {
		_, err := e.svc.Join(context.Background(), JoinInput{Sub: sub, Token: "t"})
		var as *AlreadyInShopError
		if !errors.Is(err, ErrAlreadyInShop) || !errors.As(err, &as) {
			t.Fatalf("%s: got %v", sub, err)
		}
		if as.CanAbandon != want {
			t.Fatalf("%s: CanAbandon=%v want %v", sub, as.CanAbandon, want)
		}
	}
	if e.org.acceptCnt != 0 {
		t.Fatal("invite consumed despite conflict")
	}
}

func TestJoin_AbandonEmptyShop(t *testing.T) {
	e := newEnv(t)
	old := e.soloShop("solo", 20, true)
	e.org.acceptRes = &AcceptResult{OrgID: e.orgID, Role: "member", ProductRoles: map[string]string{"tally": "clerk"}}
	res, err := e.svc.Join(context.Background(), JoinInput{Sub: "solo", Token: "t", AbandonEmptyShop: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.TenantID != e.tenant || res.Role != domain.RoleClerk || res.ShopName != "测试店" {
		t.Fatalf("got %+v", res)
	}
	if len(e.store.abandoned) != 1 || e.store.abandoned[0] != old {
		t.Fatalf("abandoned %v", e.store.abandoned)
	}
	if len(e.org.dissolved) != 1 || e.org.dissolved[0] != 500 {
		t.Fatalf("old org not dissolved: %v", e.org.dissolved)
	}
	if m := e.store.maps["solo"]; m == nil || m.TenantID != e.tenant || m.IsOwner || m.Role != "clerk" {
		t.Fatalf("mapping %+v", m)
	}
}

func TestJoin_AbandonRefusedWhenShopHasRealData(t *testing.T) {
	e := newEnv(t)
	e.soloShop("busy", 21, false)
	e.org.acceptRes = &AcceptResult{OrgID: e.orgID, Role: "member"}
	_, err := e.svc.Join(context.Background(), JoinInput{Sub: "busy", Token: "t", AbandonEmptyShop: true})
	if !errors.Is(err, ErrShopNotEmpty) {
		t.Fatalf("got %v", err)
	}
	if e.org.acceptCnt != 0 || len(e.store.abandoned) != 0 {
		t.Fatal("must not consume the invite or touch the shop")
	}
}

func TestJoin_AbandonRefusedWhenOthersAreMembers(t *testing.T) {
	e := newEnv(t)
	e.soloShop("solo", 20, true)
	e.org.solo = append(e.org.solo, OrgMember{AccountID: 22, Role: "member"})
	_, err := e.svc.Join(context.Background(), JoinInput{Sub: "solo", Token: "t", AbandonEmptyShop: true})
	if !errors.Is(err, ErrShopNotEmpty) {
		t.Fatalf("got %v", err)
	}
	// A non-owner member of the main shop can never abandon it.
	_, err = e.svc.Join(context.Background(), JoinInput{Sub: "clk", Token: "t", AbandonEmptyShop: true})
	var as *AlreadyInShopError
	if !errors.As(err, &as) || as.IsOwner || as.CanAbandon {
		t.Fatalf("clerk got %v", err)
	}
}

func TestJoin_MappingRaceInOtherShopLeavesPlatformOrg(t *testing.T) {
	e := newEnv(t)
	e.soloShop("solo", 20, false)
	e.store.lookupMiss = map[string]bool{"solo": true}
	e.org.acceptRes = &AcceptResult{OrgID: e.orgID, Role: "member"}
	_, err := e.svc.Join(context.Background(), JoinInput{Sub: "solo", Token: "t"})
	if !errors.Is(err, ErrAlreadyInShop) {
		t.Fatalf("got %v", err)
	}
	if len(e.org.removed) != 1 || e.org.removed[0] != 20 {
		t.Fatalf("person must be removed from the invited org, removed=%v", e.org.removed)
	}
}

func TestJoin_MappingRaceSameShopIsIdempotent(t *testing.T) {
	e := newEnv(t)
	e.store.lookupMiss = map[string]bool{"clk": true}
	e.org.acceptRes = &AcceptResult{OrgID: e.orgID, Role: "member", ProductRoles: map[string]string{"tally": "clerk"}}
	res, err := e.svc.Join(context.Background(), JoinInput{Sub: "clk", Token: "t"})
	if err != nil || res.TenantID != e.tenant {
		t.Fatalf("got %+v %v", res, err)
	}
	if len(e.org.removed) != 0 {
		t.Fatal("same-shop re-join must not leave the org")
	}
}

func TestCreateInvite_ReturnsShopName(t *testing.T) {
	e := newEnv(t)
	res, err := e.svc.CreateInvite(context.Background(), e.caller("owner"), domain.RoleClerk, 0)
	if err != nil || res.ShopName != "测试店" {
		t.Fatalf("got %+v %v", res, err)
	}
}

func TestCreateInvite_PrefersPrintSettingsShopName(t *testing.T) {
	e := newEnv(t)
	e.store.printName = "老张五金"
	res, err := e.svc.CreateInvite(context.Background(), e.caller("owner"), domain.RoleClerk, 0)
	if err != nil || res.ShopName != "老张五金" {
		t.Fatalf("got %+v %v", res, err)
	}
}

func TestJoin_PrefersPrintSettingsShopName(t *testing.T) {
	e := newEnv(t)
	e.store.printName = "老张五金"
	e.org.acceptRes = &AcceptResult{OrgID: e.orgID, Role: "member", ProductRoles: map[string]string{"tally": "cashier"}}
	res, err := e.svc.Join(context.Background(), JoinInput{Sub: "new", Email: "n@x.cn", Name: "小王", Token: "t"})
	if err != nil || res.ShopName != "老张五金" {
		t.Fatalf("got %+v %v", res, err)
	}
}

func TestJoin_AbandonRevokesPendingInvitesOfOldShop(t *testing.T) {
	e := newEnv(t)
	e.soloShop("solo", 20, true)
	inv := OrgInvite{ID: uuid.New()}
	e.org.invites = map[int64][]OrgInvite{500: {inv}}
	e.org.acceptRes = &AcceptResult{OrgID: e.orgID, Role: "member", ProductRoles: map[string]string{"tally": "clerk"}}
	if _, err := e.svc.Join(context.Background(), JoinInput{Sub: "solo", Token: "t", AbandonEmptyShop: true}); err != nil {
		t.Fatal(err)
	}
	if len(e.org.revoked) != 1 || e.org.revoked[0] != inv.ID {
		t.Fatalf("pending invite of the abandoned shop not revoked: %v", e.org.revoked)
	}
}

func TestJoin_AbandonFailureLeavesInvitedOrg(t *testing.T) {
	e := newEnv(t)
	e.soloShop("solo", 20, true)
	e.store.abandonErr = ErrShopNotEmpty
	e.org.acceptRes = &AcceptResult{OrgID: e.orgID, Role: "member"}
	_, err := e.svc.Join(context.Background(), JoinInput{Sub: "solo", Token: "t", AbandonEmptyShop: true})
	if !errors.Is(err, ErrShopNotEmpty) {
		t.Fatalf("got %v", err)
	}
	if len(e.org.removed) != 1 || e.org.removed[0] != 20 {
		t.Fatalf("person must leave the invited org, removed=%v", e.org.removed)
	}
	if len(e.org.dissolved) != 0 {
		t.Fatal("old org must stay when the shop was not abandoned")
	}
}

func TestJoin_PersistFailureLeavesInvitedOrg(t *testing.T) {
	e := newEnv(t)
	e.store.insertErr = errors.New("db down")
	e.org.acceptRes = &AcceptResult{OrgID: e.orgID, Role: "member"}
	if _, err := e.svc.Join(context.Background(), JoinInput{Sub: "new", Token: "t"}); err == nil {
		t.Fatal("want error")
	}
	if len(e.org.removed) != 1 || e.org.removed[0] != 9 {
		t.Fatalf("removed=%v", e.org.removed)
	}
}

func TestJoin_InviteOfDeadShopLeavesOrg(t *testing.T) {
	e := newEnv(t)
	e.store.unlinked = map[int64]bool{e.orgID: true}
	e.org.acceptRes = &AcceptResult{OrgID: e.orgID, Role: "member"}
	_, err := e.svc.Join(context.Background(), JoinInput{Sub: "new", Token: "t"})
	if !errors.Is(err, ErrInviteInvalid) {
		t.Fatalf("got %v", err)
	}
	if len(e.org.removed) != 1 || e.org.removed[0] != 9 {
		t.Fatalf("stale invite must not leave a member behind, removed=%v", e.org.removed)
	}
}

func TestJoin_AlreadyInShopReportsOwnership(t *testing.T) {
	e := newEnv(t)
	for sub, want := range map[string]bool{"clk": false, "owner": true} {
		_, err := e.svc.Join(context.Background(), JoinInput{Sub: sub, Token: "t"})
		var as *AlreadyInShopError
		if !errors.As(err, &as) || as.IsOwner != want {
			t.Fatalf("%s: got %v", sub, err)
		}
	}
}

func TestJoin_AbandonCheckFailureIsUnavailableNotShopNotEmpty(t *testing.T) {
	e := newEnv(t)
	e.soloShop("solo", 20, true)
	e.org.listErr = errors.New("platform down")
	for _, abandon := range []bool{false, true} {
		_, err := e.svc.Join(context.Background(), JoinInput{Sub: "solo", Token: "t", AbandonEmptyShop: abandon})
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("abandon=%v: got %v", abandon, err)
		}
	}
	if e.org.acceptCnt != 0 {
		t.Fatal("invite consumed despite failed check")
	}
}

func TestJoin_AbandonDropsOldShopRoleCache(t *testing.T) {
	e := newEnv(t)
	old := e.soloShop("solo", 20, true)
	e.svc.cache.put(roleKey{old, "solo"}, roleEntry{role: domain.RoleOwner}, *e.now)
	e.org.acceptRes = &AcceptResult{OrgID: e.orgID, Role: "member", ProductRoles: map[string]string{"tally": "clerk"}}
	if _, err := e.svc.Join(context.Background(), JoinInput{Sub: "solo", Token: "t", AbandonEmptyShop: true}); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.svc.cache.get(roleKey{old, "solo"}); ok {
		t.Fatal("abandoned shop keeps a cached owner role")
	}
}

// Two concurrent abandons of one shop: the later one finds the old mapping already
// retired; the platform join is shared with the winner, so it must not be removed
// and the person lands in the invited shop.
func TestJoin_AbandonRacedByAnotherAbandon(t *testing.T) {
	e := newEnv(t)
	e.soloShop("solo", 20, true)
	e.store.abandonErr = ErrShopNotEmpty
	e.store.abandonRetires = true // the winner retires the mapping while we try
	e.org.acceptRes = &AcceptResult{OrgID: e.orgID, Role: "member", ProductRoles: map[string]string{"tally": "clerk"}}
	res, err := e.svc.Join(context.Background(), JoinInput{Sub: "solo", Token: "t", AbandonEmptyShop: true})
	if err != nil || res.TenantID != e.tenant {
		t.Fatalf("got %v, %v", res, err)
	}
	if len(e.org.removed) != 0 {
		t.Fatalf("shared platform membership must stay, removed=%v", e.org.removed)
	}
}

// The same invite submitted twice: the winner already mapped the person into the
// invited shop, so the later request succeeds without removing the membership.
func TestJoin_AbandonSameInviteTwiceIsIdempotent(t *testing.T) {
	e := newEnv(t)
	e.soloShop("solo", 20, true)
	e.store.abandonErr = ErrShopNotEmpty
	e.store.abandonRetires = true
	e.store.abandonWinner = &Mapping{TenantID: e.tenant, IDPSubject: "solo", Role: "clerk", AccountID: 20}
	e.org.acceptRes = &AcceptResult{OrgID: e.orgID, Role: "member", ProductRoles: map[string]string{"tally": "clerk"}}
	res, err := e.svc.Join(context.Background(), JoinInput{Sub: "solo", Token: "t", AbandonEmptyShop: true})
	if err != nil || res.TenantID != e.tenant {
		t.Fatalf("got %v, %v", res, err)
	}
	if len(e.org.removed) != 0 {
		t.Fatalf("membership must stay, removed=%v", e.org.removed)
	}
}

// Two different invites at once: the later one leaves its own organization.
func TestJoin_AbandonDifferentInvitesLaterLeavesItsOrg(t *testing.T) {
	e := newEnv(t)
	e.soloShop("solo", 20, true)
	e.store.abandonErr = ErrShopNotEmpty
	e.store.abandonRetires = true
	e.store.abandonWinner = &Mapping{TenantID: uuid.New(), IDPSubject: "solo", Role: "clerk", AccountID: 20}
	e.org.acceptRes = &AcceptResult{OrgID: e.orgID, Role: "member", ProductRoles: map[string]string{"tally": "clerk"}}
	_, err := e.svc.Join(context.Background(), JoinInput{Sub: "solo", Token: "t", AbandonEmptyShop: true})
	if !errors.Is(err, ErrAlreadyInShop) {
		t.Fatalf("got %v", err)
	}
	if len(e.org.removed) != 1 || e.org.removed[0] != 20 {
		t.Fatalf("later invite's org must be left, removed=%v", e.org.removed)
	}
}

func TestJoin_OwnShopInviteIsAlreadyMemberHere(t *testing.T) {
	e := newEnv(t)
	id := uuid.New()
	e.org.invites = map[int64][]OrgInvite{e.org.orgID: {{ID: id, Role: "member"}}}
	for _, abandon := range []bool{false, true} {
		_, err := e.svc.Join(context.Background(), JoinInput{Sub: "owner", Token: "t", InviteID: id, AbandonEmptyShop: abandon})
		if !errors.Is(err, ErrAlreadyMemberHere) {
			t.Fatalf("abandon=%v: got %v", abandon, err)
		}
	}
	if e.org.acceptCnt != 0 || len(e.org.revoked) != 0 || len(e.store.abandoned) != 0 {
		t.Fatalf("invite consumed or shop touched: accept=%d revoked=%v abandoned=%v", e.org.acceptCnt, e.org.revoked, e.store.abandoned)
	}
}

func TestJoin_OtherShopInviteStaysAlreadyInShop(t *testing.T) {
	e := newEnv(t)
	e.org.invites = map[int64][]OrgInvite{e.org.orgID: {{ID: uuid.New(), Role: "member"}}}
	_, err := e.svc.Join(context.Background(), JoinInput{Sub: "owner", Token: "t", InviteID: uuid.New()})
	if !errors.Is(err, ErrAlreadyInShop) || errors.Is(err, ErrAlreadyMemberHere) {
		t.Fatalf("got %v", err)
	}
}

func TestCashierRole_OnlyForRetailShops(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		profile string
		err     error
		blocked bool
	}{
		{"horticulture", nil, true},
		{"cross_border", nil, true},
		{"retail", nil, false},
		{"hybrid", nil, true},
		{"", nil, false},
		{"horticulture", errors.New("db down"), false},
	}
	for _, c := range cases {
		e := newEnv(t)
		e.store.profileType, e.store.profileErr = c.profile, c.err
		_, ierr := e.svc.CreateInvite(ctx, e.caller("owner"), domain.RoleCashier, 0)
		uerr := e.svc.UpdateMemberRole(ctx, e.caller("owner"), 3, domain.RoleCashier)
		if c.blocked != errors.Is(ierr, ErrRoleNotForShop) || c.blocked != errors.Is(uerr, ErrRoleNotForShop) {
			t.Fatalf("%q/%v: invite=%v update=%v", c.profile, c.err, ierr, uerr)
		}
		if !c.blocked && (ierr != nil || uerr != nil) {
			t.Fatalf("%q: unexpected %v %v", c.profile, ierr, uerr)
		}
		if _, err := e.svc.CreateInvite(ctx, e.caller("owner"), domain.RoleClerk, 0); err != nil {
			t.Fatalf("clerk invite must always work: %v", err)
		}
	}
}

func TestListMembers_HintsCashierInNonRetailShop(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.org.members[4] = &OrgMember{AccountID: 4, Role: "member", ProductRoles: map[string]string{"tally": "cashier"}}
	e.store.profileType = "horticulture"
	ms, err := e.svc.ListMembers(ctx, e.caller("owner"))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range ms {
		want := ""
		if m.AccountID == 4 {
			want = "建议改为店员"
		}
		if m.RoleHint != want {
			t.Fatalf("member %d hint %q want %q", m.AccountID, m.RoleHint, want)
		}
	}
	e.store.profileType = "retail"
	ms, _ = e.svc.ListMembers(ctx, e.caller("owner"))
	for _, m := range ms {
		if m.RoleHint != "" {
			t.Fatalf("retail shop must not hint, got %q", m.RoleHint)
		}
	}
}

func TestJoin_OldLinkOfOwnShopIsAlreadyMemberHere(t *testing.T) {
	e := newEnv(t)
	e.store.shopEmpty = map[uuid.UUID]bool{e.tenant: true}
	delete(e.org.members, 2)
	delete(e.org.members, 3)
	delete(e.store.maps, "mgr")
	delete(e.store.maps, "clk")
	// An old link has no invite id, so the pending-list check cannot tell; the accepted org does.
	e.org.acceptRes = &AcceptResult{OrgID: e.orgID, Role: "member", ProductRoles: map[string]string{"tally": "clerk"}}
	// Without the confirmation the old link is still refused as "already in a shop".
	if _, err := e.svc.Join(context.Background(), JoinInput{Sub: "owner", Token: "t"}); !errors.Is(err, ErrAlreadyInShop) {
		t.Fatalf("got %v", err)
	}
	_, err := e.svc.Join(context.Background(), JoinInput{Sub: "owner", Token: "t", AbandonEmptyShop: true})
	if !errors.Is(err, ErrAlreadyMemberHere) {
		t.Fatalf("got %v", err)
	}
	if len(e.store.abandoned) != 0 || len(e.org.removed) != 0 {
		t.Fatalf("own shop touched: abandoned=%v removed=%v", e.store.abandoned, e.org.removed)
	}
}
