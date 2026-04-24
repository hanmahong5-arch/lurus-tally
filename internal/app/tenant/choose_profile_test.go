package tenant_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	appTenant "github.com/hanmahong5-arch/lurus-tally/internal/app/tenant"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/tenant"
)

// stubProfileRepo is an in-memory stub for TenantProfileRepository.
type stubProfileRepo struct {
	profile *domain.TenantProfile
	err     error
}

func (s *stubProfileRepo) GetByTenantID(ctx context.Context, tenantID uuid.UUID) (*domain.TenantProfile, error) {
	return s.profile, s.err
}

func (s *stubProfileRepo) Create(ctx context.Context, p *domain.TenantProfile) error {
	if s.err != nil {
		return s.err
	}
	s.profile = p
	return nil
}

func (s *stubProfileRepo) QueryProfileType(ctx context.Context, tenantID uuid.UUID) (string, error) {
	if s.profile == nil {
		return "", sql.ErrNoRows
	}
	return string(s.profile.ProfileType), nil
}

// TestTenantUseCase_ChooseProfile_InvalidType_ReturnsError verifies unknown profile_type → error.
func TestTenantUseCase_ChooseProfile_InvalidType_ReturnsError(t *testing.T) {
	repo := &stubProfileRepo{}
	uc := appTenant.NewChooseProfileUseCase(repo)

	_, err := uc.Execute(context.Background(), appTenant.ChooseProfileInput{
		TenantID:    uuid.New(),
		ProfileType: "invalid_type",
	})
	if err == nil {
		t.Error("expected error for invalid profile type, got nil")
	}
}

// TestTenantUseCase_ChooseProfile_HybridNotAllowed_ReturnsError verifies hybrid is rejected.
func TestTenantUseCase_ChooseProfile_HybridNotAllowed_ReturnsError(t *testing.T) {
	repo := &stubProfileRepo{}
	uc := appTenant.NewChooseProfileUseCase(repo)

	_, err := uc.Execute(context.Background(), appTenant.ChooseProfileInput{
		TenantID:    uuid.New(),
		ProfileType: "hybrid",
	})
	if err == nil {
		t.Error("expected error for hybrid profile type, got nil")
	}
}

// TestTenantUseCase_ChooseProfile_CrossBorder_Succeeds verifies cross_border creates profile.
func TestTenantUseCase_ChooseProfile_CrossBorder_Succeeds(t *testing.T) {
	repo := &stubProfileRepo{}
	uc := appTenant.NewChooseProfileUseCase(repo)

	tenantID := uuid.New()
	p, err := uc.Execute(context.Background(), appTenant.ChooseProfileInput{
		TenantID:    tenantID,
		ProfileType: "cross_border",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.ProfileType != domain.ProfileTypeCrossBorder {
		t.Errorf("expected cross_border, got %s", p.ProfileType)
	}
	if p.InventoryMethod != domain.InventoryMethodFIFO {
		t.Errorf("expected fifo for cross_border, got %s", p.InventoryMethod)
	}
}

// TestTenantUseCase_ChooseProfile_Retail_Succeeds verifies retail creates profile with wac.
func TestTenantUseCase_ChooseProfile_Retail_Succeeds(t *testing.T) {
	repo := &stubProfileRepo{}
	uc := appTenant.NewChooseProfileUseCase(repo)

	p, err := uc.Execute(context.Background(), appTenant.ChooseProfileInput{
		TenantID:    uuid.New(),
		ProfileType: "retail",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.ProfileType != domain.ProfileTypeRetail {
		t.Errorf("expected retail, got %s", p.ProfileType)
	}
	if p.InventoryMethod != domain.InventoryMethodWAC {
		t.Errorf("expected wac for retail, got %s", p.InventoryMethod)
	}
}

// TestTenantUseCase_ChooseProfile_AlreadySet_ReturnsConflict verifies idempotency guard.
func TestTenantUseCase_ChooseProfile_AlreadySet_ReturnsConflict(t *testing.T) {
	tenantID := uuid.New()
	existing, _ := domain.NewTenantProfile(tenantID, domain.ProfileTypeCrossBorder)
	repo := &stubProfileRepo{profile: existing}
	uc := appTenant.NewChooseProfileUseCase(repo)

	_, err := uc.Execute(context.Background(), appTenant.ChooseProfileInput{
		TenantID:    tenantID,
		ProfileType: "retail",
	})
	if err == nil {
		t.Error("expected ErrProfileAlreadySet, got nil")
	}
}
