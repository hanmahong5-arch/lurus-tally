package tenant_test

// zz_coverage_test.go adds unit coverage for the branches
// choose_profile_test.go / get_me_test.go don't exercise: store-level error
// injection (GetMappingBySub / GetProfileByTenantID / post-bootstrap fetch),
// the inconsistent-mapping-without-profile sentinel, deriveTenantName's short
// input case, GetMe's profile-getter enrichment fan-out, and
// WithProfileGetter(nil). It reuses stubBootstrapStore / stubUpserter defined
// in choose_profile_test.go (same tenant_test package) and adds its own
// error-injecting store + fake profile getter where the existing stub can't
// express the scenario (e.g. GetMappingBySub returning an error).

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	repoTenant "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/tenant"
	appTenant "github.com/hanmahong5-arch/lurus-tally/internal/app/tenant"
	domainacct "github.com/hanmahong5-arch/lurus-tally/internal/domain/account"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/tenant"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/platformclient"
)

// errInjectingStore is a BootstrapStore whose every method's return value is
// independently configurable. It lets tests reach branches that the
// map-backed stubBootstrapStore (choose_profile_test.go) cannot express,
// such as GetMappingBySub itself failing, or a post-bootstrap re-fetch that
// returns (nil, nil) despite Bootstrap having reported success.
type errInjectingStore struct {
	mapping        *domain.UserIdentityMapping
	mappingErr     error
	profile        *domain.TenantProfile
	profileErr     error
	profileErrOnce bool // when true, profileErr fires only on the 1st GetProfileByTenantID call
	profileCalls   int
	bootstrapErr   error
	// afterBootstrapProfile overrides the profile returned by
	// GetProfileByTenantID once Bootstrap has been called (simulates the
	// post-bootstrap re-fetch finding/not-finding the row).
	afterBootstrapProfile *domain.TenantProfile
	afterBootstrapErr     error
	bootstrapped          bool
	setAcctErr            error
	// autoProfileOnBootstrap makes Bootstrap synthesize a real profile row
	// (via domain.NewTenantProfile) so the post-bootstrap re-fetch succeeds.
	// Off by default so TestChooseProfile_PostBootstrapFetchNil_* can still
	// exercise the "committed but not found" guard with zero-value fields.
	autoProfileOnBootstrap bool
}

func (s *errInjectingStore) GetMappingBySub(_ context.Context, _ string) (*domain.UserIdentityMapping, error) {
	if s.mappingErr != nil {
		return nil, s.mappingErr
	}
	return s.mapping, nil
}

func (s *errInjectingStore) GetProfileByTenantID(_ context.Context, _ uuid.UUID) (*domain.TenantProfile, error) {
	s.profileCalls++
	if s.bootstrapped {
		if s.afterBootstrapErr != nil {
			return nil, s.afterBootstrapErr
		}
		return s.afterBootstrapProfile, nil
	}
	if s.profileErr != nil {
		if s.profileErrOnce && s.profileCalls > 1 {
			return s.profile, nil
		}
		return nil, s.profileErr
	}
	return s.profile, nil
}

func (s *errInjectingStore) Bootstrap(_ context.Context, in repoTenant.BootstrapInput) error {
	if s.bootstrapErr != nil {
		return s.bootstrapErr
	}
	s.bootstrapped = true
	if s.autoProfileOnBootstrap {
		p, err := domain.NewTenantProfile(in.TenantID, in.ProfileType)
		if err != nil {
			return err
		}
		s.afterBootstrapProfile = p
	}
	return nil
}

func (s *errInjectingStore) SetPlatformAccountID(_ context.Context, _ uuid.UUID, _ int64) error {
	return s.setAcctErr
}

// fakeProfileGetter is a minimal ProfileGetter for GetMe enrichment tests.
type fakeProfileGetter struct {
	profile *domainacct.Profile
	err     error
	called  bool
}

func (f *fakeProfileGetter) Execute(_ context.Context, _ uuid.UUID, _ string) (*domainacct.Profile, error) {
	f.called = true
	return f.profile, f.err
}

// ---------------------------------------------------------------------------
// ChooseProfile: store error-branch matrix
// ---------------------------------------------------------------------------

func TestChooseProfile_GetMappingBySubError_Propagates(t *testing.T) {
	store := &errInjectingStore{mappingErr: errors.New("conn reset")}
	uc := appTenant.NewChooseProfileUseCase(store, nil, nil)

	_, err := uc.Execute(context.Background(), appTenant.ChooseProfileInput{
		IDPSubject: "sub-map-err", ProfileType: "retail",
	})
	if err == nil {
		t.Fatal("expected error when GetMappingBySub fails, got nil")
	}
}

func TestChooseProfile_GetProfileByTenantIDError_Propagates(t *testing.T) {
	tenantID := uuid.New()
	store := &errInjectingStore{
		mapping:    &domain.UserIdentityMapping{TenantID: tenantID, IDPSubject: "sub-prof-err"},
		profileErr: errors.New("query timeout"),
	}
	uc := appTenant.NewChooseProfileUseCase(store, nil, nil)

	_, err := uc.Execute(context.Background(), appTenant.ChooseProfileInput{
		IDPSubject: "sub-prof-err", ProfileType: "retail",
	})
	if err == nil {
		t.Fatal("expected error when GetProfileByTenantID fails, got nil")
	}
}

// TestChooseProfile_MappingWithoutProfile_ReturnsInconsistentState verifies the
// atomicity-violation sentinel: a mapping exists but its tenant has no
// profile row (Bootstrap was interrupted between the two inserts).
func TestChooseProfile_MappingWithoutProfile_ReturnsInconsistentState(t *testing.T) {
	tenantID := uuid.New()
	store := &errInjectingStore{
		mapping: &domain.UserIdentityMapping{TenantID: tenantID, IDPSubject: "sub-inconsistent"},
		profile: nil, // no profile for this tenant
	}
	uc := appTenant.NewChooseProfileUseCase(store, nil, nil)

	_, err := uc.Execute(context.Background(), appTenant.ChooseProfileInput{
		IDPSubject: "sub-inconsistent", ProfileType: "retail",
	})
	if !errors.Is(err, appTenant.ErrInconsistentTenantState) {
		t.Errorf("expected ErrInconsistentTenantState, got %v", err)
	}
}

func TestChooseProfile_BootstrapError_Propagates(t *testing.T) {
	store := &errInjectingStore{bootstrapErr: errors.New("insert conflict")}
	uc := appTenant.NewChooseProfileUseCase(store, nil, nil)

	_, err := uc.Execute(context.Background(), appTenant.ChooseProfileInput{
		IDPSubject: "sub-boot-err", ProfileType: "retail",
	})
	if err == nil {
		t.Fatal("expected bootstrap error to propagate, got nil")
	}
}

// TestChooseProfile_PostBootstrapFetchError_Propagates verifies the re-fetch
// (after a successful Bootstrap commit) surfaces its own error distinctly.
func TestChooseProfile_PostBootstrapFetchError_Propagates(t *testing.T) {
	store := &errInjectingStore{afterBootstrapErr: errors.New("read replica lag")}
	uc := appTenant.NewChooseProfileUseCase(store, nil, nil)

	_, err := uc.Execute(context.Background(), appTenant.ChooseProfileInput{
		IDPSubject: "sub-postfetch-err", ProfileType: "retail",
	})
	if err == nil {
		t.Fatal("expected post-bootstrap fetch error to propagate, got nil")
	}
}

// TestChooseProfile_PostBootstrapFetchNil_ReturnsSurpriseError verifies the
// "should never happen" guard: Bootstrap reports success but the immediate
// re-fetch finds no row.
func TestChooseProfile_PostBootstrapFetchNil_ReturnsSurpriseError(t *testing.T) {
	store := &errInjectingStore{afterBootstrapProfile: nil, afterBootstrapErr: nil}
	uc := appTenant.NewChooseProfileUseCase(store, nil, nil)

	_, err := uc.Execute(context.Background(), appTenant.ChooseProfileInput{
		IDPSubject: "sub-postfetch-nil", ProfileType: "retail",
	})
	if err == nil {
		t.Fatal("expected error when post-bootstrap fetch returns nil profile, got nil")
	}
}

// ---------------------------------------------------------------------------
// deriveTenantName is unexported; drive it indirectly via Execute's fresh-user
// path and inspect the resulting tenant name propagated to Bootstrap.
// ---------------------------------------------------------------------------

// nameCapturingStore records the TenantName Bootstrap was called with so
// tests can assert deriveTenantName's exact output without exporting it.
type nameCapturingStore struct {
	mapping     *domain.UserIdentityMapping
	profile     *domain.TenantProfile
	gotName     string
	tenantIDArg uuid.UUID
}

func (s *nameCapturingStore) GetMappingBySub(_ context.Context, _ string) (*domain.UserIdentityMapping, error) {
	return s.mapping, nil
}
func (s *nameCapturingStore) GetProfileByTenantID(_ context.Context, _ uuid.UUID) (*domain.TenantProfile, error) {
	return s.profile, nil
}
func (s *nameCapturingStore) Bootstrap(_ context.Context, in repoTenant.BootstrapInput) error {
	s.gotName = in.TenantName
	s.tenantIDArg = in.TenantID
	p, err := domain.NewTenantProfile(in.TenantID, in.ProfileType)
	if err != nil {
		return err
	}
	s.profile = p
	return nil
}
func (s *nameCapturingStore) SetPlatformAccountID(_ context.Context, _ uuid.UUID, _ int64) error {
	return nil
}

// TestDeriveTenantName_TableDriven covers all four branches of the unexported
// deriveTenantName helper: displayName wins, then email, then sub>=8 chars
// (truncated to 8), then sub<8 chars (used verbatim).
func TestDeriveTenantName_TableDriven(t *testing.T) {
	cases := []struct {
		name        string
		displayName string
		email       string
		sub         string
		wantName    string
	}{
		{"display_name_wins", "Alice Co", "alice@example.com", "sub-12345678", "Alice Co 的企业"},
		{"email_used_when_no_display_name", "", "bob@example.com", "sub-12345678", "bob@example.com 的企业"},
		{"long_sub_truncated_to_8", "", "", "abcdefghij", "Tenant abcdefgh"},
		{"short_sub_used_verbatim", "", "", "abc123", "Tenant abc123"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &nameCapturingStore{}
			uc := appTenant.NewChooseProfileUseCase(store, nil, nil)

			_, err := uc.Execute(context.Background(), appTenant.ChooseProfileInput{
				IDPSubject:  tc.sub,
				Email:       tc.email,
				DisplayName: tc.displayName,
				ProfileType: "retail",
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if store.gotName != tc.wantName {
				t.Errorf("deriveTenantName: got %q, want %q", store.gotName, tc.wantName)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// upsertPlatformAccount: SetPlatformAccountID skipped when acc.ID <= 0.
// ---------------------------------------------------------------------------

// zeroIDUpserter always returns an Account with ID 0 (platform accepted the
// upsert but has no numeric id to report, e.g. a stub environment). This must
// NOT trigger a SetPlatformAccountID call (acc.ID > 0 guard).
type zeroIDUpserter struct{ setAcctCalled *bool }

func (u zeroIDUpserter) UpsertAccount(_ context.Context, req platformclient.UpsertAccountRequest) (*platformclient.Account, error) {
	return &platformclient.Account{ID: 0, IDPSubject: req.IDPSubject}, nil
}

// spyStoreForAcctID wraps errInjectingStore just to observe whether
// SetPlatformAccountID was ever invoked.
type spyStoreForAcctID struct {
	errInjectingStore
	setAcctCalled bool
}

func (s *spyStoreForAcctID) SetPlatformAccountID(ctx context.Context, tenantID uuid.UUID, accountID int64) error {
	s.setAcctCalled = true
	return s.errInjectingStore.SetPlatformAccountID(ctx, tenantID, accountID)
}

// TestChooseProfile_ZeroAccountID_SkipsPersist verifies that when the
// upserter returns an Account with ID <= 0, SetPlatformAccountID is never
// called (upsertPlatformAccount's acc.ID > 0 guard).
func TestChooseProfile_ZeroAccountID_SkipsPersist(t *testing.T) {
	store := &spyStoreForAcctID{}
	store.autoProfileOnBootstrap = true
	uc := appTenant.NewChooseProfileUseCase(store, zeroIDUpserter{}, nil)

	if _, err := uc.Execute(context.Background(), appTenant.ChooseProfileInput{
		IDPSubject: "sub-zero-acct", ProfileType: "retail",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.setAcctCalled {
		t.Error("expected SetPlatformAccountID NOT to be called when acc.ID <= 0")
	}
}

// ---------------------------------------------------------------------------
// GetMe: store error + profile-getter fan-out matrix
// ---------------------------------------------------------------------------

func TestGetMe_GetProfileByTenantIDError_Propagates(t *testing.T) {
	tenantID := uuid.New()
	store := &errInjectingStore{
		mapping:    &domain.UserIdentityMapping{TenantID: tenantID, IDPSubject: "sub-getme-err"},
		profileErr: errors.New("query timeout"),
	}
	uc := appTenant.NewGetMeUseCase(store)

	_, err := uc.Execute(context.Background(), appTenant.GetMeInput{UserSub: "sub-getme-err"})
	if err == nil {
		t.Fatal("expected error when GetProfileByTenantID fails, got nil")
	}
}

// TestGetMe_MappingWithoutProfile_LeavesProfileTypeEmpty verifies the
// "mapping but no profile" case does NOT error for /me (unlike ChooseProfile) —
// it just leaves ProfileType empty so the frontend can route to profile setup.
func TestGetMe_MappingWithoutProfile_LeavesProfileTypeEmpty(t *testing.T) {
	tenantID := uuid.New()
	store := &errInjectingStore{
		mapping: &domain.UserIdentityMapping{TenantID: tenantID, IDPSubject: "sub-nopfx", Email: "x@example.com"},
		profile: nil,
	}
	uc := appTenant.NewGetMeUseCase(store)

	out, err := uc.Execute(context.Background(), appTenant.GetMeInput{UserSub: "sub-nopfx"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.IsFirstTime {
		t.Error("expected IsFirstTime=false when a mapping exists")
	}
	if out.ProfileType != "" {
		t.Errorf("expected empty ProfileType when no profile row exists, got %q", out.ProfileType)
	}
}

func TestGetMe_ProfileGetterNil_NoFanOut(t *testing.T) {
	tenantID := uuid.New()
	store := &errInjectingStore{
		mapping: &domain.UserIdentityMapping{TenantID: tenantID, IDPSubject: "sub-nogetter", DisplayName: "Mapping Name"},
		profile: mustProfile(t, tenantID, domain.ProfileTypeRetail),
	}
	uc := appTenant.NewGetMeUseCase(store) // no WithProfileGetter call

	out, err := uc.Execute(context.Background(), appTenant.GetMeInput{UserSub: "sub-nogetter"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.DisplayName != "Mapping Name" {
		t.Errorf("expected mapping display name preserved, got %q", out.DisplayName)
	}
	if out.AvatarURL != "" || out.Phone != "" {
		t.Errorf("expected no enrichment without a profileGetter, got %+v", out)
	}
}

func TestGetMe_WithProfileGetterNil_ReturnsSameUseCase(t *testing.T) {
	store := &errInjectingStore{}
	uc := appTenant.NewGetMeUseCase(store)

	got := uc.WithProfileGetter(nil)
	if got != uc {
		t.Error("WithProfileGetter(nil) should return the original receiver unchanged")
	}
}

func TestGetMe_ProfileGetterError_DoesNotBreakMe(t *testing.T) {
	tenantID := uuid.New()
	store := &errInjectingStore{
		mapping: &domain.UserIdentityMapping{TenantID: tenantID, IDPSubject: "sub-getter-err", DisplayName: "Original"},
		profile: mustProfile(t, tenantID, domain.ProfileTypeRetail),
	}
	getter := &fakeProfileGetter{err: errors.New("account profile db down")}
	uc := appTenant.NewGetMeUseCase(store).WithProfileGetter(getter)

	out, err := uc.Execute(context.Background(), appTenant.GetMeInput{UserSub: "sub-getter-err"})
	if err != nil {
		t.Fatalf("profileGetter failure must not break /me: %v", err)
	}
	if !getter.called {
		t.Error("expected profileGetter to be invoked")
	}
	if out.DisplayName != "Original" {
		t.Errorf("expected mapping display name preserved on getter error, got %q", out.DisplayName)
	}
}

func TestGetMe_ProfileGetterReturnsNilAccount_DoesNotOverride(t *testing.T) {
	tenantID := uuid.New()
	store := &errInjectingStore{
		mapping: &domain.UserIdentityMapping{TenantID: tenantID, IDPSubject: "sub-getter-nil", DisplayName: "Original"},
		profile: mustProfile(t, tenantID, domain.ProfileTypeRetail),
	}
	getter := &fakeProfileGetter{profile: nil, err: nil}
	uc := appTenant.NewGetMeUseCase(store).WithProfileGetter(getter)

	out, err := uc.Execute(context.Background(), appTenant.GetMeInput{UserSub: "sub-getter-nil"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.DisplayName != "Original" {
		t.Errorf("expected mapping display name preserved when getter returns nil, got %q", out.DisplayName)
	}
}

// TestGetMe_ProfileGetterEnriches_FullOverlay covers the happy overlay path:
// non-empty DisplayName override, Phone passthrough, and HasAvatar → the
// fixed avatar URL.
func TestGetMe_ProfileGetterEnriches_FullOverlay(t *testing.T) {
	tenantID := uuid.New()
	store := &errInjectingStore{
		mapping: &domain.UserIdentityMapping{TenantID: tenantID, IDPSubject: "sub-getter-full", DisplayName: "Mapping Name"},
		profile: mustProfile(t, tenantID, domain.ProfileTypeHorticulture),
	}
	getter := &fakeProfileGetter{profile: &domainacct.Profile{
		DisplayName: "Overlay Name",
		Phone:       "+8613800001111",
		HasAvatar:   true,
	}}
	uc := appTenant.NewGetMeUseCase(store).WithProfileGetter(getter)

	out, err := uc.Execute(context.Background(), appTenant.GetMeInput{UserSub: "sub-getter-full"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.DisplayName != "Overlay Name" {
		t.Errorf("expected overlay display name, got %q", out.DisplayName)
	}
	if out.Phone != "+8613800001111" {
		t.Errorf("expected overlay phone, got %q", out.Phone)
	}
	if out.AvatarURL != "/api/v1/account/avatar" {
		t.Errorf("expected fixed avatar URL, got %q", out.AvatarURL)
	}
}

// TestGetMe_ProfileGetterEnriches_EmptyDisplayNameKeepsMapping verifies that
// an overlay profile with an empty DisplayName does not blank out the
// mapping-derived display name (only non-empty overrides win).
func TestGetMe_ProfileGetterEnriches_EmptyDisplayNameKeepsMapping(t *testing.T) {
	tenantID := uuid.New()
	store := &errInjectingStore{
		mapping: &domain.UserIdentityMapping{TenantID: tenantID, IDPSubject: "sub-getter-empty-dn", DisplayName: "Mapping Name"},
		profile: mustProfile(t, tenantID, domain.ProfileTypeRetail),
	}
	getter := &fakeProfileGetter{profile: &domainacct.Profile{
		DisplayName: "",
		Phone:       "+861234",
		HasAvatar:   false,
	}}
	uc := appTenant.NewGetMeUseCase(store).WithProfileGetter(getter)

	out, err := uc.Execute(context.Background(), appTenant.GetMeInput{UserSub: "sub-getter-empty-dn"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.DisplayName != "Mapping Name" {
		t.Errorf("expected mapping display name kept when overlay DisplayName is empty, got %q", out.DisplayName)
	}
	if out.AvatarURL != "" {
		t.Errorf("expected empty avatar URL when HasAvatar=false, got %q", out.AvatarURL)
	}
}

func mustProfile(t *testing.T, tenantID uuid.UUID, pt domain.ProfileType) *domain.TenantProfile {
	t.Helper()
	p, err := domain.NewTenantProfile(tenantID, pt)
	if err != nil {
		t.Fatalf("build test profile: %v", err)
	}
	return p
}
