package tenant_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	appTenant "github.com/hanmahong5-arch/lurus-tally/internal/app/tenant"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/tenant"
)

// TestGetMeUseCase_NoProfile_ReturnsEmptyProfileType verifies that a tenant
// with no profile returns an empty profileType without error.
func TestGetMeUseCase_NoProfile_ReturnsEmptyProfileType(t *testing.T) {
	tenantID := uuid.New()
	repo := &stubProfileRepo{profile: nil}
	uc := appTenant.NewGetMeUseCase(repo)

	out, err := uc.Execute(context.Background(), appTenant.GetMeInput{
		TenantID: tenantID,
		UserSub:  "user-sub-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ProfileType != "" {
		t.Errorf("expected empty profileType, got %q", out.ProfileType)
	}
	if out.TenantID != tenantID.String() {
		t.Errorf("expected tenant_id=%s, got %s", tenantID, out.TenantID)
	}
}

// TestGetMeUseCase_WithProfile_ReturnsProfileType verifies profile type is returned.
func TestGetMeUseCase_WithProfile_ReturnsProfileType(t *testing.T) {
	tenantID := uuid.New()
	profile, _ := domain.NewTenantProfile(tenantID, domain.ProfileTypeCrossBorder)
	repo := &stubProfileRepo{profile: profile}
	uc := appTenant.NewGetMeUseCase(repo)

	out, err := uc.Execute(context.Background(), appTenant.GetMeInput{
		TenantID: tenantID,
		UserSub:  "user-sub-2",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ProfileType != "cross_border" {
		t.Errorf("expected cross_border, got %q", out.ProfileType)
	}
}
