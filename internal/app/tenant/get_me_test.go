package tenant_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	appTenant "github.com/hanmahong5-arch/lurus-tally/internal/app/tenant"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/tenant"
)

// TestGetMe_FirstTimeUser_NoMapping verifies that when no mapping exists
// (sub never seen before), GetMe returns IsFirstTime=true and empty fields.
// The frontend uses IsFirstTime as the gate to redirect to /setup.
func TestGetMe_FirstTimeUser_NoMapping(t *testing.T) {
	store := newStubBootstrapStore()
	uc := appTenant.NewGetMeUseCase(store)

	out, err := uc.Execute(context.Background(), appTenant.GetMeInput{
		UserSub: "sub-never-seen",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.IsFirstTime {
		t.Error("expected IsFirstTime=true for fresh sub, got false")
	}
	if out.TenantID != "" {
		t.Errorf("expected empty tenant_id for first-time user, got %q", out.TenantID)
	}
	if out.ProfileType != "" {
		t.Errorf("expected empty profile_type, got %q", out.ProfileType)
	}
}

// TestGetMe_ReturningUser_FullPayload verifies that an onboarded user gets a
// fully-populated payload (tenant + profile + role).
func TestGetMe_ReturningUser_FullPayload(t *testing.T) {
	store := newStubBootstrapStore()
	tenantID := uuid.New()
	store.mappings["sub-onboarded"] = &domain.UserIdentityMapping{
		ID:          uuid.New(),
		TenantID:    tenantID,
		ZitadelSub:  "sub-onboarded",
		Email:       "bob@example.com",
		DisplayName: "Bob",
		Role:        "admin",
		IsOwner:     true,
	}
	profile, _ := domain.NewTenantProfile(tenantID, domain.ProfileTypeCrossBorder)
	store.profiles[tenantID] = profile

	uc := appTenant.NewGetMeUseCase(store)
	out, err := uc.Execute(context.Background(), appTenant.GetMeInput{
		UserSub: "sub-onboarded",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.IsFirstTime {
		t.Error("expected IsFirstTime=false for onboarded user, got true")
	}
	if out.TenantID != tenantID.String() {
		t.Errorf("expected tenant_id=%s, got %s", tenantID, out.TenantID)
	}
	if out.ProfileType != "cross_border" {
		t.Errorf("expected cross_border, got %q", out.ProfileType)
	}
	if out.Email != "bob@example.com" {
		t.Errorf("expected email bob@example.com, got %q", out.Email)
	}
	if !out.IsOwner {
		t.Error("expected IsOwner=true")
	}
}

// TestGetMe_MissingSub_Rejected verifies the use case requires a sub.
func TestGetMe_MissingSub_Rejected(t *testing.T) {
	store := newStubBootstrapStore()
	uc := appTenant.NewGetMeUseCase(store)

	_, err := uc.Execute(context.Background(), appTenant.GetMeInput{})
	if err == nil {
		t.Error("expected error for missing sub, got nil")
	}
}
