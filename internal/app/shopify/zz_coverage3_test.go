package shopify_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	appshopify "github.com/hanmahong5-arch/lurus-tally/internal/app/shopify"
	appwarehouse "github.com/hanmahong5-arch/lurus-tally/internal/app/warehouse"
	domainwarehouse "github.com/hanmahong5-arch/lurus-tally/internal/domain/warehouse"
)

// zz_coverage_test.go and zz_coverage2_test.go already exist in this package
// (both naming slots described by the task brief are taken), so per the
// "add exactly one new file, never touch an existing *_test.go" constraint
// this is a third, non-overlapping addition using distinct fake type names.
//
// It independently re-derives the validateDomain regex table by hand against
// the real regex `^[a-z0-9][a-z0-9\-]{0,61}[a-z0-9]?\.myshopify\.com$` (not by
// reading back the SUT's own output), and separately re-verifies the
// documented validation ORDER and sentinel-identity invariants with a fresh
// set of fakes.

// ---- distinct fakes (avoid name collisions with sibling _test.go files) ----

type zzRepo3 struct {
	createCalls int
	createErr   error
	listItems   []appshopify.ShopMapping
	listErr     error
	deleteErr   error
}

func (r *zzRepo3) Create(_ context.Context, _ *appshopify.ShopMapping) error {
	r.createCalls++
	return r.createErr
}

func (r *zzRepo3) ListByTenant(_ context.Context, _ uuid.UUID) ([]appshopify.ShopMapping, error) {
	return r.listItems, r.listErr
}

func (r *zzRepo3) DeleteByID(_ context.Context, _, _ uuid.UUID) error {
	return r.deleteErr
}

type zzChecker3 struct {
	calls int
	owned bool
	err   error
}

func (c *zzChecker3) BelongsToTenant(_ context.Context, _, _ uuid.UUID) (bool, error) {
	c.calls++
	return c.owned, c.err
}

type zzGetter3 struct {
	wh  *domainwarehouse.Warehouse
	err error
}

func (g *zzGetter3) Execute(_ context.Context, _, _ uuid.UUID) (*domainwarehouse.Warehouse, error) {
	return g.wh, g.err
}

// ---- validateDomain: hand-derived table, independent of any other test file
//
// Regex under test: ^[a-z0-9][a-z0-9\-]{0,61}[a-z0-9]?\.myshopify\.com$
// Hand trace for each case (not derived by running the code first):
//   - "shop.myshopify.com": slug "shop" -> first 's', middle "ho" (2 chars,
//     within {0,61}), optional last 'p' consumed by the optional group -> match.
//   - "my-store.myshopify.com": hyphen is a member of the middle class -> match.
//   - "": empty string can't satisfy the mandatory leading [a-z0-9] -> no match.
//   - "Shop.myshopify.com": 'S' is not in [a-z0-9] (class is lowercase only)
//     -> no match.
//   - "evil.com": no ".myshopify.com" suffix at all -> no match.
//   - "-x.myshopify.com": leading '-' can't satisfy the mandatory leading
//     [a-z0-9] (hyphen is excluded from that first class) -> no match.
//   - "a.myshopify.com.evil.com": the trailing anchor $ requires the string to
//     END at ".myshopify.com"; here it's followed by ".evil.com" -> no match.
type zz3DomainCase struct {
	name    string
	domain  string
	wantErr bool
}

func TestZZ3_ValidateDomain_HandDerivedTable(t *testing.T) {
	cases := []zz3DomainCase{
		{"valid plain", "shop.myshopify.com", false},
		{"valid hyphenated", "my-store.myshopify.com", false},
		{"empty string has no leading alnum", "", true},
		{"uppercase first char excluded from class", "Shop.myshopify.com", true},
		{"wrong tld entirely", "evil.com", true},
		{"leading hyphen excluded from first-char class", "-x.myshopify.com", true},
		{"anchor rejects trailing suffix after .com", "a.myshopify.com.evil.com", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &zzRepo3{}
			checker := &zzChecker3{owned: true}
			uc := appshopify.NewBindShopUseCase(repo, checker)

			_, err := uc.Execute(context.Background(), appshopify.BindInput{
				TenantID:    uuid.New(),
				ShopDomain:  tc.domain,
				WarehouseID: uuid.New(),
				CreatorID:   uuid.New(),
			})

			gotErr := errors.Is(err, appshopify.ErrInvalidDomain)
			if gotErr != tc.wantErr {
				t.Fatalf("domain %q: ErrInvalidDomain=%v (err=%v); want %v", tc.domain, gotErr, err, tc.wantErr)
			}
			// Business invariant: format validation must short-circuit BEFORE
			// the warehouse checker is ever consulted.
			if tc.wantErr && checker.calls != 0 {
				t.Errorf("domain %q: checker.calls=%d; want 0 (must short-circuit before warehouse check)", tc.domain, checker.calls)
			}
		})
	}
}

// ---- validation ORDER: domain -> warehouse ownership -> insert ------------

func TestZZ3_Execute_ValidationOrder_DomainBeforeChecker(t *testing.T) {
	repo := &zzRepo3{}
	checker := &zzChecker3{owned: true}
	uc := appshopify.NewBindShopUseCase(repo, checker)

	_, err := uc.Execute(context.Background(), appshopify.BindInput{
		TenantID:    uuid.New(),
		ShopDomain:  "not-a-valid-domain",
		WarehouseID: uuid.New(),
		CreatorID:   uuid.New(),
	})
	if !errors.Is(err, appshopify.ErrInvalidDomain) {
		t.Fatalf("got %v; want ErrInvalidDomain", err)
	}
	if checker.calls != 0 {
		t.Errorf("checker.calls=%d; want 0 (invalid domain must short-circuit before warehouse check)", checker.calls)
	}
	if repo.createCalls != 0 {
		t.Errorf("repo.createCalls=%d; want 0", repo.createCalls)
	}
}

func TestZZ3_Execute_CheckerErrorPrecedesPersist(t *testing.T) {
	underlying := errors.New("network partition")
	repo := &zzRepo3{}
	checker := &zzChecker3{err: underlying}
	uc := appshopify.NewBindShopUseCase(repo, checker)

	_, err := uc.Execute(context.Background(), appshopify.BindInput{
		TenantID:    uuid.New(),
		ShopDomain:  "abc.myshopify.com",
		WarehouseID: uuid.New(),
		CreatorID:   uuid.New(),
	})
	if !errors.Is(err, underlying) {
		t.Fatalf("expected wrapped underlying error %v, got %v", underlying, err)
	}
	if errors.Is(err, appshopify.ErrWarehouseNotOwned) {
		t.Errorf("a checker error must be distinct from the (false,nil) not-owned case, got %v", err)
	}
	if repo.createCalls != 0 {
		t.Errorf("repo.createCalls=%d; want 0 (checker error must precede persist)", repo.createCalls)
	}
}

func TestZZ3_Execute_NotOwnedPrecedesPersist(t *testing.T) {
	repo := &zzRepo3{}
	checker := &zzChecker3{owned: false}
	uc := appshopify.NewBindShopUseCase(repo, checker)

	_, err := uc.Execute(context.Background(), appshopify.BindInput{
		TenantID:    uuid.New(),
		ShopDomain:  "abc.myshopify.com",
		WarehouseID: uuid.New(),
		CreatorID:   uuid.New(),
	})
	if !errors.Is(err, appshopify.ErrWarehouseNotOwned) {
		t.Fatalf("got %v; want ErrWarehouseNotOwned", err)
	}
	if repo.createCalls != 0 {
		t.Errorf("repo.createCalls=%d; want 0 (cross-tenant guard)", repo.createCalls)
	}
}

func TestZZ3_Execute_DuplicateSentinelSurvives(t *testing.T) {
	repo := &zzRepo3{createErr: appshopify.ErrShopAlreadyBound}
	checker := &zzChecker3{owned: true}
	uc := appshopify.NewBindShopUseCase(repo, checker)

	_, err := uc.Execute(context.Background(), appshopify.BindInput{
		TenantID:    uuid.New(),
		ShopDomain:  "dup.myshopify.com",
		WarehouseID: uuid.New(),
		CreatorID:   uuid.New(),
	})
	if !errors.Is(err, appshopify.ErrShopAlreadyBound) {
		t.Fatalf("got %v; want the exact ErrShopAlreadyBound sentinel (globally-unique shop_domain contract)", err)
	}
}

func TestZZ3_Execute_GenericPersistErrorWrapped(t *testing.T) {
	underlying := errors.New("deadlock detected")
	repo := &zzRepo3{createErr: underlying}
	checker := &zzChecker3{owned: true}
	uc := appshopify.NewBindShopUseCase(repo, checker)

	_, err := uc.Execute(context.Background(), appshopify.BindInput{
		TenantID:    uuid.New(),
		ShopDomain:  "abc.myshopify.com",
		WarehouseID: uuid.New(),
		CreatorID:   uuid.New(),
	})
	if errors.Is(err, appshopify.ErrShopAlreadyBound) {
		t.Errorf("generic error must not be surfaced as ErrShopAlreadyBound, got %v", err)
	}
	if !errors.Is(err, underlying) {
		t.Errorf("expected wrapped underlying error %v, got %v", underlying, err)
	}
}

// ---- ListShopsUseCase -------------------------------------------------------

func TestZZ3_ListShops_EmptySliceNilError(t *testing.T) {
	repo := &zzRepo3{listItems: []appshopify.ShopMapping{}}
	uc := appshopify.NewListShopsUseCase(repo)

	got, err := uc.Execute(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("got %v; want an empty (non-nil-or-nil, len 0) slice", got)
	}
}

func TestZZ3_ListShops_RepoErrorWrapped(t *testing.T) {
	underlying := errors.New("connection pool exhausted")
	repo := &zzRepo3{listErr: underlying}
	uc := appshopify.NewListShopsUseCase(repo)

	got, err := uc.Execute(context.Background(), uuid.New())
	if got != nil {
		t.Errorf("expected nil slice on error, got %v", got)
	}
	if !errors.Is(err, underlying) {
		t.Errorf("expected wrapped underlying error %v, got %v", underlying, err)
	}
}

// ---- UnbindShopUseCase: idempotency contract -------------------------------

func TestZZ3_Unbind_IdempotentDeleteIsNotAnError(t *testing.T) {
	repo := &zzRepo3{} // deleteErr is nil: simulates "already deleted" / not-found
	uc := appshopify.NewUnbindShopUseCase(repo)

	if err := uc.Execute(context.Background(), uuid.New(), uuid.New()); err != nil {
		t.Fatalf("expected nil (retry-safe idempotent no-op), got %v", err)
	}
}

func TestZZ3_Unbind_GenuineErrorWrapped(t *testing.T) {
	underlying := errors.New("disk i/o error")
	repo := &zzRepo3{deleteErr: underlying}
	uc := appshopify.NewUnbindShopUseCase(repo)

	err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, underlying) {
		t.Errorf("expected wrapped underlying error %v, got %v", underlying, err)
	}
}

// ---- WarehouseCheckerAdapter: three-way branch coverage --------------------

func TestZZ3_WarehouseAdapter_ErrNotFoundMapsToFalseNil(t *testing.T) {
	getter := &zzGetter3{err: appwarehouse.ErrNotFound}
	adapter := appshopify.NewWarehouseCheckerAdapter(getter)

	owned, err := adapter.BelongsToTenant(context.Background(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("expected nil error for ErrNotFound, got %v", err)
	}
	if owned {
		t.Error("expected owned=false for ErrNotFound (ownership-isolation contract)")
	}
}

func TestZZ3_WarehouseAdapter_OtherErrorPropagatesUnwrapped(t *testing.T) {
	underlying := errors.New("upstream 500")
	getter := &zzGetter3{err: underlying}
	adapter := appshopify.NewWarehouseCheckerAdapter(getter)

	owned, err := adapter.BelongsToTenant(context.Background(), uuid.New(), uuid.New())
	if owned {
		t.Error("expected owned=false on unexpected error")
	}
	if !errors.Is(err, underlying) {
		t.Errorf("expected the exact underlying error to propagate, got %v", err)
	}
}

func TestZZ3_WarehouseAdapter_SuccessMapsToTrueNil(t *testing.T) {
	getter := &zzGetter3{wh: &domainwarehouse.Warehouse{ID: uuid.New()}}
	adapter := appshopify.NewWarehouseCheckerAdapter(getter)

	owned, err := adapter.BelongsToTenant(context.Background(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !owned {
		t.Error("expected owned=true on success")
	}
}
