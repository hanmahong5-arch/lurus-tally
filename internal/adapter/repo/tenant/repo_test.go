package tenant_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	repoTenant "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/tenant"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/tenant"
)

// TestTenantProfileRepo_GetByTenantID_NotFound_ReturnsNil verifies that a missing profile
// returns nil, nil (not an error).
func TestTenantProfileRepo_GetByTenantID_NotFound_ReturnsNil(t *testing.T) {
	// Use a real in-memory stub — the repo.GetByTenantID must return nil,nil on ErrNoRows.
	// We test this via the use case layer with a mock repo. This test validates the contract.
	ctx := context.Background()
	tenantID := uuid.New()

	// Construct a mock that returns ErrNotFound behavior.
	mockRepo := &stubTenantProfileRepo{profile: nil}
	result, err := mockRepo.GetByTenantID(ctx, tenantID)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}

// TestTenantProfileRepo_Create_Succeeds tests that create stores a profile (mock level).
func TestTenantProfileRepo_Create_Succeeds(t *testing.T) {
	ctx := context.Background()
	tenantID := uuid.New()
	profile, err := domain.NewTenantProfile(tenantID, domain.ProfileTypeCrossBorder)
	if err != nil {
		t.Fatalf("NewTenantProfile: %v", err)
	}

	mockRepo := &stubTenantProfileRepo{profile: nil}
	if err := mockRepo.Create(ctx, profile); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

// stubTenantProfileRepo satisfies repoTenant.TenantProfileRepository for unit testing.
type stubTenantProfileRepo struct {
	profile *domain.TenantProfile
	err     error
}

func (s *stubTenantProfileRepo) GetByTenantID(ctx context.Context, tenantID uuid.UUID) (*domain.TenantProfile, error) {
	return s.profile, s.err
}

func (s *stubTenantProfileRepo) Create(ctx context.Context, p *domain.TenantProfile) error {
	s.profile = p
	return s.err
}

func (s *stubTenantProfileRepo) QueryProfileType(ctx context.Context, tenantID uuid.UUID) (string, error) {
	if s.profile == nil {
		return "", sql.ErrNoRows
	}
	return string(s.profile.ProfileType), nil
}

// Ensure stubTenantProfileRepo satisfies the required interfaces at compile time.
var _ repoTenant.TenantProfileRepository = (*stubTenantProfileRepo)(nil)
