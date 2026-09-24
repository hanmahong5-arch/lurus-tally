package tenant_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	appTenant "github.com/hanmahong5-arch/lurus-tally/internal/app/tenant"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/tenant"
)

// TestGetTenantProfile_Found_ReturnsProfile verifies the happy path: store has
// a profile row, use case returns it verbatim.
func TestGetTenantProfile_Found_ReturnsProfile(t *testing.T) {
	store := newStubBootstrapStore()
	tenantID := uuid.New()
	profile, _ := domain.NewTenantProfile(tenantID, domain.ProfileTypeCrossBorder)
	store.profiles[tenantID] = profile

	uc := appTenant.NewGetTenantProfileUseCase(store)
	got, err := uc.Execute(context.Background(), tenantID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.TenantID != tenantID {
		t.Errorf("expected profile for %s, got %+v", tenantID, got)
	}
	if got.ProfileType != domain.ProfileTypeCrossBorder {
		t.Errorf("expected cross_border, got %s", got.ProfileType)
	}
}

// TestGetTenantProfile_NotFound_ReturnsErrProfileNotFound verifies the contract
// for callers: missing profile row → ErrProfileNotFound (not generic error).
func TestGetTenantProfile_NotFound_ReturnsErrProfileNotFound(t *testing.T) {
	store := newStubBootstrapStore()
	uc := appTenant.NewGetTenantProfileUseCase(store)

	_, err := uc.Execute(context.Background(), uuid.New())
	if !errors.Is(err, appTenant.ErrProfileNotFound) {
		t.Errorf("expected ErrProfileNotFound, got %v", err)
	}
}

// TestGetTenantProfile_NilTenantID_RejectedAtBoundary verifies defensive coding:
// uuid.Nil at the boundary means "no auth context" — caller bug, not "not found".
func TestGetTenantProfile_NilTenantID_RejectedAtBoundary(t *testing.T) {
	store := newStubBootstrapStore()
	uc := appTenant.NewGetTenantProfileUseCase(store)

	_, err := uc.Execute(context.Background(), uuid.Nil)
	if err == nil {
		t.Error("expected error for uuid.Nil, got nil")
	}
	if errors.Is(err, appTenant.ErrProfileNotFound) {
		t.Error("uuid.Nil must NOT collapse to ErrProfileNotFound — it is a precondition violation")
	}
}
