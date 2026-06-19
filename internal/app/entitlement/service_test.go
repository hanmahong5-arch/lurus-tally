package entitlement_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hanmahong5-arch/lurus-tally/internal/app/entitlement"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/platformclient"
)

type stubPlatform struct {
	acc     *platformclient.Account
	accErr  error
	ents    map[string]string
	entsErr error
}

func (s *stubPlatform) GetAccountByZitadelSub(_ context.Context, _ string) (*platformclient.Account, error) {
	return s.acc, s.accErr
}

func (s *stubPlatform) GetEntitlements(_ context.Context, _ int64, _ string) (map[string]string, error) {
	return s.ents, s.entsErr
}

func TestHas_EmptySub_Unauthenticated(t *testing.T) {
	svc := entitlement.NewService(&stubPlatform{}, nil)
	if _, err := svc.Has(context.Background(), "", "ai_assistant"); !errors.Is(err, entitlement.ErrUnauthenticated) {
		t.Fatalf("want ErrUnauthenticated, got %v", err)
	}
}

func TestHas_ProAccount_GrantsTruthyKey(t *testing.T) {
	svc := entitlement.NewService(&stubPlatform{
		acc:  &platformclient.Account{ID: 1},
		ents: map[string]string{"ai_assistant": "true", "plan_code": "pro"},
	}, nil)
	ok, err := svc.Has(context.Background(), "sub", "ai_assistant")
	if err != nil || !ok {
		t.Fatalf("pro should be granted ai_assistant: ok=%v err=%v", ok, err)
	}
}

func TestHas_FreeAccount_DeniesAbsentKey(t *testing.T) {
	svc := entitlement.NewService(&stubPlatform{
		acc:  &platformclient.Account{ID: 2},
		ents: map[string]string{"plan_code": "free"}, // no ai_assistant key
	}, nil)
	ok, err := svc.Has(context.Background(), "sub", "ai_assistant")
	if err != nil || ok {
		t.Fatalf("free must be denied ai_assistant: ok=%v err=%v", ok, err)
	}
}

func TestHas_AccountNotFound_FailsClosed(t *testing.T) {
	svc := entitlement.NewService(&stubPlatform{
		accErr: &platformclient.Error{Code: platformclient.ErrCodeNotFound},
	}, nil)
	ok, err := svc.Has(context.Background(), "sub", "ai_assistant")
	if err != nil || ok {
		t.Fatalf("unprovisioned account must be denied (fail-closed): ok=%v err=%v", ok, err)
	}
}

func TestHas_PlatformUnreachable_FailsOpen(t *testing.T) {
	svc := entitlement.NewService(&stubPlatform{
		accErr: &platformclient.Error{Code: platformclient.ErrCodeUnavailable},
	}, nil)
	ok, err := svc.Has(context.Background(), "sub", "ai_assistant")
	if err != nil || !ok {
		t.Fatalf("platform blip must fail OPEN (allow): ok=%v err=%v", ok, err)
	}
}

func TestHas_EntitlementsFetchError_FailsOpen(t *testing.T) {
	svc := entitlement.NewService(&stubPlatform{
		acc:     &platformclient.Account{ID: 3},
		entsErr: &platformclient.Error{Code: platformclient.ErrCodeUnavailable},
	}, nil)
	ok, err := svc.Has(context.Background(), "sub", "ai_assistant")
	if err != nil || !ok {
		t.Fatalf("entitlements fetch blip must fail OPEN: ok=%v err=%v", ok, err)
	}
}

func TestHas_FalsyValue_Denies(t *testing.T) {
	svc := entitlement.NewService(&stubPlatform{
		acc:  &platformclient.Account{ID: 4},
		ents: map[string]string{"ai_assistant": "false"},
	}, nil)
	if ok, _ := svc.Has(context.Background(), "sub", "ai_assistant"); ok {
		t.Fatal("falsy entitlement value must deny")
	}
}
