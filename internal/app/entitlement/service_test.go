package entitlement_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/app/entitlement"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/platformclient"
)

type stubResolver struct {
	id  int64
	ok  bool
	err error
}

func (s stubResolver) GetPlatformAccountID(_ context.Context, _ uuid.UUID) (int64, bool, error) {
	return s.id, s.ok, s.err
}

type stubEnts struct {
	ents map[string]string
	err  error
}

func (s stubEnts) GetEntitlements(_ context.Context, _ int64, _ string) (map[string]string, error) {
	return s.ents, s.err
}

func svc(r stubResolver, e stubEnts) *entitlement.Service {
	return entitlement.NewService(r, e, nil)
}

func TestHas_NilTenant_Unauthenticated(t *testing.T) {
	if _, err := svc(stubResolver{}, stubEnts{}).Has(context.Background(), uuid.Nil, "ai_assistant"); !errors.Is(err, entitlement.ErrUnauthenticated) {
		t.Fatalf("want ErrUnauthenticated, got %v", err)
	}
}

func TestHas_ProTenant_GrantsTruthyKey(t *testing.T) {
	s := svc(stubResolver{id: 1, ok: true}, stubEnts{ents: map[string]string{"ai_assistant": "true", "plan_code": "pro"}})
	ok, err := s.Has(context.Background(), uuid.New(), "ai_assistant")
	if err != nil || !ok {
		t.Fatalf("pro tenant should be granted ai_assistant: ok=%v err=%v", ok, err)
	}
}

func TestHas_FreeTenant_DeniesAbsentKey(t *testing.T) {
	s := svc(stubResolver{id: 2, ok: true}, stubEnts{ents: map[string]string{"plan_code": "free"}})
	ok, err := s.Has(context.Background(), uuid.New(), "ai_assistant")
	if err != nil || ok {
		t.Fatalf("free tenant must be denied ai_assistant: ok=%v err=%v", ok, err)
	}
}

func TestHas_NoPinnedAccount_FailsClosed(t *testing.T) {
	// Tenant resolves but has no platform_account_id (NULL) → no plan → deny.
	s := svc(stubResolver{ok: false}, stubEnts{})
	ok, err := s.Has(context.Background(), uuid.New(), "ai_assistant")
	if err != nil || ok {
		t.Fatalf("unprovisioned tenant must be denied (fail-closed): ok=%v err=%v", ok, err)
	}
}

func TestHas_ResolverError_FailsOpen(t *testing.T) {
	s := svc(stubResolver{err: errors.New("db down")}, stubEnts{})
	ok, err := s.Has(context.Background(), uuid.New(), "ai_assistant")
	if err != nil || !ok {
		t.Fatalf("resolver blip must fail OPEN (allow): ok=%v err=%v", ok, err)
	}
}

func TestHas_EntitlementsFetchError_FailsOpen(t *testing.T) {
	s := svc(stubResolver{id: 3, ok: true}, stubEnts{err: errors.New("platform unreachable")})
	ok, err := s.Has(context.Background(), uuid.New(), "ai_assistant")
	if err != nil || !ok {
		t.Fatalf("entitlements fetch blip must fail OPEN: ok=%v err=%v", ok, err)
	}
}

func TestHas_FalsyValue_Denies(t *testing.T) {
	s := svc(stubResolver{id: 4, ok: true}, stubEnts{ents: map[string]string{"ai_assistant": "false"}})
	if ok, _ := s.Has(context.Background(), uuid.New(), "ai_assistant"); ok {
		t.Fatal("falsy entitlement value must deny")
	}
}

// A rejected service key (placeholder secret, rotated or under-scoped key) is a
// standing misconfiguration, not a blip. Failing open on it would make every
// tenant's plan behave like the paid tier until someone read the logs.
func TestHas_PlatformRejectsKey_FailsClosed(t *testing.T) {
	rejected := &platformclient.Error{Code: platformclient.ErrCodeUnauthorized, HTTPStatus: 401}
	s := svc(stubResolver{id: 3, ok: true}, stubEnts{err: rejected})
	ok, err := s.Has(context.Background(), uuid.New(), "ai_assistant")
	if err != nil || ok {
		t.Fatalf("rejected service key must fail CLOSED: ok=%v err=%v", ok, err)
	}
}
