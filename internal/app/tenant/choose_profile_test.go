package tenant_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	repoTenant "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/tenant"
	appTenant "github.com/hanmahong5-arch/lurus-tally/internal/app/tenant"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/tenant"
)

// stubBootstrapStore is an in-memory BootstrapStore for unit testing the use case.
// It mirrors the production *sql.DB-backed implementation but holds state in maps.
type stubBootstrapStore struct {
	mu          sync.Mutex
	mappings    map[string]*domain.UserIdentityMapping // sub → mapping
	profiles    map[uuid.UUID]*domain.TenantProfile    // tenant_id → profile
	bootstrapEr error                                  // injectable for failure cases
}

func newStubBootstrapStore() *stubBootstrapStore {
	return &stubBootstrapStore{
		mappings: make(map[string]*domain.UserIdentityMapping),
		profiles: make(map[uuid.UUID]*domain.TenantProfile),
	}
}

func (s *stubBootstrapStore) GetMappingBySub(_ context.Context, sub string) (*domain.UserIdentityMapping, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mappings[sub], nil
}

func (s *stubBootstrapStore) GetProfileByTenantID(_ context.Context, tenantID uuid.UUID) (*domain.TenantProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.profiles[tenantID], nil
}

func (s *stubBootstrapStore) Bootstrap(_ context.Context, in repoTenant.BootstrapInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bootstrapEr != nil {
		return s.bootstrapEr
	}
	s.mappings[in.ZitadelSub] = &domain.UserIdentityMapping{
		ID:          uuid.New(),
		TenantID:    in.TenantID,
		ZitadelSub:  in.ZitadelSub,
		Email:       in.Email,
		DisplayName: in.DisplayName,
		Role:        "admin",
		IsOwner:     true,
	}
	p, err := domain.NewTenantProfile(in.TenantID, in.ProfileType)
	if err != nil {
		return err
	}
	s.profiles[in.TenantID] = p
	return nil
}

// TestChooseProfile_FreshUser_BootstrapsTenantAndProfile verifies the happy path:
// a brand-new sub triggers atomic creation of tenant + mapping + profile.
func TestChooseProfile_FreshUser_BootstrapsTenantAndProfile(t *testing.T) {
	store := newStubBootstrapStore()
	uc := appTenant.NewChooseProfileUseCase(store)

	p, err := uc.Execute(context.Background(), appTenant.ChooseProfileInput{
		ZitadelSub:  "sub-fresh-001",
		Email:       "alice@example.com",
		DisplayName: "Alice",
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
	if _, ok := store.mappings["sub-fresh-001"]; !ok {
		t.Error("expected mapping to be created")
	}
}

// TestChooseProfile_IdempotentSameType verifies calling twice with the same
// profile_type returns the existing profile without error (no-op).
func TestChooseProfile_IdempotentSameType(t *testing.T) {
	store := newStubBootstrapStore()
	uc := appTenant.NewChooseProfileUseCase(store)
	in := appTenant.ChooseProfileInput{
		ZitadelSub:  "sub-idem-002",
		ProfileType: "retail",
	}

	first, err := uc.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	second, err := uc.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("second call should be idempotent, got: %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("expected same profile id, got %s vs %s", first.ID, second.ID)
	}
}

// TestChooseProfile_DifferentTypeAfterSet_ReturnsConflict verifies that changing
// profile type after first set returns ErrProfileAlreadySet.
func TestChooseProfile_DifferentTypeAfterSet_ReturnsConflict(t *testing.T) {
	store := newStubBootstrapStore()
	uc := appTenant.NewChooseProfileUseCase(store)
	sub := "sub-conflict-003"

	if _, err := uc.Execute(context.Background(), appTenant.ChooseProfileInput{
		ZitadelSub: sub, ProfileType: "retail",
	}); err != nil {
		t.Fatalf("first call failed: %v", err)
	}

	_, err := uc.Execute(context.Background(), appTenant.ChooseProfileInput{
		ZitadelSub: sub, ProfileType: "cross_border",
	})
	if !errors.Is(err, domain.ErrProfileAlreadySet) {
		t.Errorf("expected ErrProfileAlreadySet, got %v", err)
	}
}

// TestChooseProfile_InvalidType_ReturnsInvalidProfileType verifies validation.
func TestChooseProfile_InvalidType_ReturnsInvalidProfileType(t *testing.T) {
	store := newStubBootstrapStore()
	uc := appTenant.NewChooseProfileUseCase(store)

	_, err := uc.Execute(context.Background(), appTenant.ChooseProfileInput{
		ZitadelSub: "sub-bad", ProfileType: "invalid_type",
	})
	if !errors.Is(err, domain.ErrInvalidProfileType) {
		t.Errorf("expected ErrInvalidProfileType, got %v", err)
	}
}

// TestChooseProfile_HybridRejected verifies "hybrid" (admin-only) is rejected.
func TestChooseProfile_HybridRejected(t *testing.T) {
	store := newStubBootstrapStore()
	uc := appTenant.NewChooseProfileUseCase(store)

	_, err := uc.Execute(context.Background(), appTenant.ChooseProfileInput{
		ZitadelSub: "sub-hyb", ProfileType: "hybrid",
	})
	if !errors.Is(err, domain.ErrInvalidProfileType) {
		t.Errorf("expected ErrInvalidProfileType for hybrid, got %v", err)
	}
}

// TestChooseProfile_MissingSub_Rejected verifies the use case requires a sub.
func TestChooseProfile_MissingSub_Rejected(t *testing.T) {
	store := newStubBootstrapStore()
	uc := appTenant.NewChooseProfileUseCase(store)

	_, err := uc.Execute(context.Background(), appTenant.ChooseProfileInput{
		ProfileType: "retail",
	})
	if err == nil {
		t.Error("expected error for missing sub, got nil")
	}
}

// TestChooseProfile_BootstrapFailure_Surfaces verifies underlying errors propagate.
func TestChooseProfile_BootstrapFailure_Surfaces(t *testing.T) {
	store := newStubBootstrapStore()
	store.bootstrapEr = errors.New("db connection lost")
	uc := appTenant.NewChooseProfileUseCase(store)

	_, err := uc.Execute(context.Background(), appTenant.ChooseProfileInput{
		ZitadelSub: "sub-fail", ProfileType: "retail",
	})
	if err == nil {
		t.Error("expected bootstrap error to propagate, got nil")
	}
}
