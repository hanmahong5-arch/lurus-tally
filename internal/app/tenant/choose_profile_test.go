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
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/platformclient"
)

// stubUpserter records calls and lets tests inject failures. Mirrors the
// production *platformclient.Client surface ChooseProfileUseCase needs.
type stubUpserter struct {
	mu        sync.Mutex
	calls     []platformclient.UpsertAccountRequest
	returnErr error
}

func (s *stubUpserter) UpsertAccount(_ context.Context, req platformclient.UpsertAccountRequest) (*platformclient.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, req)
	if s.returnErr != nil {
		return nil, s.returnErr
	}
	return &platformclient.Account{ID: 42, ZitadelSub: req.ZitadelSub, Email: req.Email}, nil
}

func (s *stubUpserter) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

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
	uc := appTenant.NewChooseProfileUseCase(store, nil, nil)

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
	uc := appTenant.NewChooseProfileUseCase(store, nil, nil)
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
	uc := appTenant.NewChooseProfileUseCase(store, nil, nil)
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
	uc := appTenant.NewChooseProfileUseCase(store, nil, nil)

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
	uc := appTenant.NewChooseProfileUseCase(store, nil, nil)

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
	uc := appTenant.NewChooseProfileUseCase(store, nil, nil)

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
	uc := appTenant.NewChooseProfileUseCase(store, nil, nil)

	_, err := uc.Execute(context.Background(), appTenant.ChooseProfileInput{
		ZitadelSub: "sub-fail", ProfileType: "retail",
	})
	if err == nil {
		t.Error("expected bootstrap error to propagate, got nil")
	}
}

// TestChooseProfile_FreshUser_UpsertsPlatformAccount verifies the happy path
// also provisions an account on lurus-platform so wallet/subscription work
// from the very first login.
func TestChooseProfile_FreshUser_UpsertsPlatformAccount(t *testing.T) {
	store := newStubBootstrapStore()
	upserter := &stubUpserter{}
	uc := appTenant.NewChooseProfileUseCase(store, upserter, nil)

	if _, err := uc.Execute(context.Background(), appTenant.ChooseProfileInput{
		ZitadelSub:  "sub-platform-001",
		Email:       "bob@example.com",
		DisplayName: "Bob",
		ProfileType: "retail",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if upserter.callCount() != 1 {
		t.Fatalf("expected exactly 1 platform upsert call, got %d", upserter.callCount())
	}
	got := upserter.calls[0]
	if got.ZitadelSub != "sub-platform-001" || got.Email != "bob@example.com" || got.DisplayName != "Bob" {
		t.Errorf("upsert payload mismatch: %+v", got)
	}
}

// TestChooseProfile_PlatformUpsertFailure_DoesNotBlockOnboarding verifies that
// a 5xx / network failure on platform side leaves the local tenant intact and
// the use case still returns the new profile to the caller.
func TestChooseProfile_PlatformUpsertFailure_DoesNotBlockOnboarding(t *testing.T) {
	store := newStubBootstrapStore()
	upserter := &stubUpserter{returnErr: errors.New("platform 503 unavailable")}
	uc := appTenant.NewChooseProfileUseCase(store, upserter, nil)

	p, err := uc.Execute(context.Background(), appTenant.ChooseProfileInput{
		ZitadelSub:  "sub-blip-002",
		Email:       "carol@example.com",
		ProfileType: "retail",
	})
	if err != nil {
		t.Fatalf("upsert failure must not propagate: %v", err)
	}
	if p == nil || p.ProfileType != domain.ProfileTypeRetail {
		t.Errorf("expected retail profile despite upsert failure, got %+v", p)
	}
	if upserter.callCount() != 1 {
		t.Errorf("expected upsert to be attempted once, got %d", upserter.callCount())
	}
}

// TestChooseProfile_ReturningUser_HealsByReUpserting verifies that a returning
// user (mapping already exists) still triggers an idempotent upsert so a
// previous platform-side failure self-heals on the next /tenant/profile call.
func TestChooseProfile_ReturningUser_HealsByReUpserting(t *testing.T) {
	store := newStubBootstrapStore()
	// Pre-seed: tenant + mapping + profile already exist (returning user).
	tenantID := uuid.New()
	store.mappings["sub-return-003"] = &domain.UserIdentityMapping{
		ID: uuid.New(), TenantID: tenantID, ZitadelSub: "sub-return-003",
		Email: "dave@example.com", Role: "admin", IsOwner: true,
	}
	prof, _ := domain.NewTenantProfile(tenantID, domain.ProfileTypeCrossBorder)
	store.profiles[tenantID] = prof

	upserter := &stubUpserter{}
	uc := appTenant.NewChooseProfileUseCase(store, upserter, nil)

	if _, err := uc.Execute(context.Background(), appTenant.ChooseProfileInput{
		ZitadelSub:  "sub-return-003",
		Email:       "dave@example.com",
		ProfileType: "cross_border",
	}); err != nil {
		t.Fatalf("idempotent re-call must succeed: %v", err)
	}
	if upserter.callCount() != 1 {
		t.Errorf("expected returning-user heal to call upsert once, got %d", upserter.callCount())
	}
}

// TestChooseProfile_NilUpserter_NoOp verifies the use case still works in
// dev clusters where PLATFORM_INTERNAL_KEY is unset (upserter is nil).
func TestChooseProfile_NilUpserter_NoOp(t *testing.T) {
	store := newStubBootstrapStore()
	uc := appTenant.NewChooseProfileUseCase(store, nil, nil)

	if _, err := uc.Execute(context.Background(), appTenant.ChooseProfileInput{
		ZitadelSub:  "sub-nil-004",
		Email:       "eve@example.com",
		ProfileType: "retail",
	}); err != nil {
		t.Fatalf("nil upserter must be a no-op, not an error: %v", err)
	}
}

// TestChooseProfile_EmptyEmail_SynthesizesPlaceholder verifies that a Zitadel
// user with no email claim (admin / username-only / phone-OTP) still gets a
// platform account upsert with a stable placeholder email so wallet and
// subscription can attach. The placeholder is overwritten on a later call
// once the user adds a real email.
func TestChooseProfile_EmptyEmail_SynthesizesPlaceholder(t *testing.T) {
	store := newStubBootstrapStore()
	upserter := &stubUpserter{}
	uc := appTenant.NewChooseProfileUseCase(store, upserter, nil)

	if _, err := uc.Execute(context.Background(), appTenant.ChooseProfileInput{
		ZitadelSub:  "sub-no-email-005",
		Email:       "",
		ProfileType: "retail",
	}); err != nil {
		t.Fatalf("empty email path must succeed: %v", err)
	}
	if upserter.callCount() != 1 {
		t.Fatalf("expected upsert with synthesized email, got %d calls", upserter.callCount())
	}
	got := upserter.calls[0]
	want := "sub-no-email-005@zitadel.local"
	if got.Email != want {
		t.Errorf("expected synthesized email %q, got %q", want, got.Email)
	}
}
