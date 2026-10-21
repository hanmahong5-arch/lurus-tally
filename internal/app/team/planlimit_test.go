package team

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/app/entitlement"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/team"
)

type stubGuard struct {
	err   error
	keys  []string
	delta []int
}

func (g *stubGuard) Check(_ context.Context, _ uuid.UUID, key string, delta int) error {
	g.keys = append(g.keys, key)
	g.delta = append(g.delta, delta)
	return g.err
}

func fullErr() error {
	return &entitlement.ErrPlanLimitReached{Key: entitlement.KeyMaxUsers, Used: 3, Limit: 3}
}

func TestCreateInvite_PlanLimitReached(t *testing.T) {
	e := newEnv(t)
	g := &stubGuard{err: fullErr()}
	e.svc.WithPlanGuard(g)
	_, err := e.svc.CreateInvite(context.Background(), e.caller("mgr"), domain.RoleCashier, 0)
	if _, ok := entitlement.IsPlanLimit(err); !ok {
		t.Fatalf("want plan limit error, got %v", err)
	}
	if len(g.keys) != 1 || g.keys[0] != "max_users" || g.delta[0] != 1 {
		t.Fatalf("guard calls: %v %v", g.keys, g.delta)
	}
	if e.org.inviteRole != "" {
		t.Fatal("no invite may be issued when the plan is full")
	}
}

func TestJoin_PlanLimitReached_LeavesOrg(t *testing.T) {
	e := newEnv(t)
	e.org.acceptRes = &AcceptResult{OrgID: e.orgID, Role: "member", ProductRoles: map[string]string{"tally": "cashier"}}
	e.svc.WithPlanGuard(&stubGuard{err: fullErr()})
	_, err := e.svc.Join(context.Background(), JoinInput{Sub: "new", Email: "n@x.cn", Token: "t"})
	if _, ok := entitlement.IsPlanLimit(err); !ok {
		t.Fatalf("want plan limit error, got %v", err)
	}
	if _, ok := e.store.maps["new"]; ok {
		t.Fatal("no mapping may be written")
	}
	if len(e.org.removed) != 1 {
		t.Fatalf("the platform join must be undone, removed=%v", e.org.removed)
	}
}

func TestJoin_GuardPasses(t *testing.T) {
	e := newEnv(t)
	e.org.acceptRes = &AcceptResult{OrgID: e.orgID, Role: "member", ProductRoles: map[string]string{"tally": "cashier"}}
	g := &stubGuard{}
	e.svc.WithPlanGuard(g)
	if _, err := e.svc.Join(context.Background(), JoinInput{Sub: "new", Email: "n@x.cn", Token: "t"}); err != nil {
		t.Fatal(err)
	}
	if len(g.keys) != 1 {
		t.Fatalf("guard calls: %v", g.keys)
	}
}
