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

// zz_coverage_test.go already exists in this package and independently drives
// this package to 100.0% statement coverage. Per task constraints that file
// must not be edited (may contain a concurrent session's uncommitted WIP), so
// this file is a second, non-overlapping addition: distinct fake types (to
// avoid redeclaration) that re-derive the same business invariants from the
// use-case docstrings/brief, so the invariants are enforced from more than
// one independent test author's hand-derived expectations.

// ---- distinct fakes (avoid name collisions with other _test.go files) -----

type zzRepo2 struct {
	createCalls int
	createErr   error
}

func (r *zzRepo2) Create(_ context.Context, _ *appshopify.ShopMapping) error {
	r.createCalls++
	return r.createErr
}

func (r *zzRepo2) ListByTenant(_ context.Context, _ uuid.UUID) ([]appshopify.ShopMapping, error) {
	return nil, nil
}

func (r *zzRepo2) DeleteByID(_ context.Context, _, _ uuid.UUID) error {
	return nil
}

type zzChecker2 struct {
	calls int
	owned bool
	err   error
}

func (c *zzChecker2) BelongsToTenant(_ context.Context, _, _ uuid.UUID) (bool, error) {
	c.calls++
	return c.owned, c.err
}

// ---- validateDomain regex: independently hand-derived table ---------------
//
// Regex under test: ^[a-z0-9][a-z0-9\-]{0,61}[a-z0-9]?\.myshopify\.com$
//
// Hand-derivation notes:
//   - first char class requires a leading alnum -> "-x.myshopify.com" rejected.
//   - the middle class allows hyphens anywhere including immediately before
//     the optional trailing-alnum group, and that trailing group is OPTIONAL
//     ([a-z0-9]?) -- so a slug ending in "-" (e.g. "abc-") DOES match: the
//     regex engine backtracks to let {0,61} consume the final "-" and the
//     optional last-char group matches empty. This is a real accepted case
//     given the CURRENT anchored regex (not a stylistic preference) --
//     verified independently below rather than assumed.
//   - anchors ^...$ reject any suffix after ".com" (evil.com tacked on).
//   - only lowercase a-z0-9- is in the class -> uppercase rejected.

func TestZZ2_ValidateDomain_IndependentTable(t *testing.T) {
	cases := []struct {
		name    string
		domain  string
		wantErr bool
	}{
		{"plain valid", "shop.myshopify.com", false},
		{"hyphen valid", "my-store.myshopify.com", false},
		{"trailing hyphen in slug is accepted by current anchored regex", "abc-.myshopify.com", false},
		{"leading hyphen rejected (first char class has no hyphen)", "-abc.myshopify.com", true},
		{"uppercase rejected", "Shop.myshopify.com", true},
		{"wrong tld rejected", "shop.shopify.com", true},
		{"empty rejected", "", true},
		{"suffix after .com rejected by anchor", "shop.myshopify.com.evil.com", true},
		{"prefix before valid domain rejected by anchor", "evil.comshop.myshopify.com", true},
		{"single char slug valid (min length 1)", "a.myshopify.com", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &zzRepo2{}
			checker := &zzChecker2{owned: true}
			uc := appshopify.NewBindShopUseCase(repo, checker)

			_, err := uc.Execute(context.Background(), appshopify.BindInput{
				TenantID:    uuid.New(),
				ShopDomain:  tc.domain,
				WarehouseID: uuid.New(),
				CreatorID:   uuid.New(),
			})

			gotErr := errors.Is(err, appshopify.ErrInvalidDomain)
			if gotErr != tc.wantErr {
				t.Fatalf("domain %q: ErrInvalidDomain = %v (err=%v); want %v", tc.domain, gotErr, err, tc.wantErr)
			}
			if tc.wantErr && checker.calls != 0 {
				t.Errorf("domain %q: warehouse checker must not be called when domain format is invalid (calls=%d)", tc.domain, checker.calls)
			}
		})
	}
}

// ---- ordering invariant: warehouse-not-owned must precede persistence -----

func TestZZ2_Execute_CrossTenantGuard_BlocksPersistBeforeInsert(t *testing.T) {
	repo := &zzRepo2{}
	checker := &zzChecker2{owned: false} // not this tenant's warehouse
	uc := appshopify.NewBindShopUseCase(repo, checker)

	_, err := uc.Execute(context.Background(), appshopify.BindInput{
		TenantID:    uuid.New(),
		ShopDomain:  "valid-shop.myshopify.com",
		WarehouseID: uuid.New(),
		CreatorID:   uuid.New(),
	})

	if !errors.Is(err, appshopify.ErrWarehouseNotOwned) {
		t.Fatalf("got %v; want ErrWarehouseNotOwned", err)
	}
	if checker.calls != 1 {
		t.Errorf("checker.calls = %d; want exactly 1", checker.calls)
	}
	if repo.createCalls != 0 {
		t.Errorf("repo.createCalls = %d; want 0 — a tenant must never be able to bind a shop to another tenant's warehouse", repo.createCalls)
	}
}

// ---- ErrShopAlreadyBound must survive fmt.Errorf wrapping elsewhere in the
// call chain the same way errors.Is expects (sentinel identity, not string
// match) -- guards against a future refactor accidentally re-wrapping it.

func TestZZ2_Execute_DuplicateSentinel_IsExactIdentity(t *testing.T) {
	repo := &zzRepo2{createErr: appshopify.ErrShopAlreadyBound}
	checker := &zzChecker2{owned: true}
	uc := appshopify.NewBindShopUseCase(repo, checker)

	_, err := uc.Execute(context.Background(), appshopify.BindInput{
		TenantID:    uuid.New(),
		ShopDomain:  "dup.myshopify.com",
		WarehouseID: uuid.New(),
		CreatorID:   uuid.New(),
	})

	if !errors.Is(err, appshopify.ErrShopAlreadyBound) {
		t.Fatalf("got %v; want ErrShopAlreadyBound sentinel to survive", err)
	}
	// Must be exactly the sentinel per usecase.go's explicit re-return (not a
	// %w-wrapped copy), matching the documented "surfaced as the exact
	// sentinel" contract.
	if !errors.Is(err, appshopify.ErrShopAlreadyBound) || err.Error() != appshopify.ErrShopAlreadyBound.Error() {
		t.Errorf("expected the bare sentinel string, got %q", err.Error())
	}
}

// ---- WarehouseCheckerAdapter: independently-authored getter fake, distinct
// type name from the sibling coverage file, driving all three branches.

type zzGetter2 struct {
	wh  *domainwarehouse.Warehouse
	err error
}

func (g *zzGetter2) Execute(_ context.Context, _, _ uuid.UUID) (*domainwarehouse.Warehouse, error) {
	return g.wh, g.err
}

func TestZZ2_WarehouseCheckerAdapter_NotFoundMeansFalseNil(t *testing.T) {
	getter := &zzGetter2{err: appwarehouse.ErrNotFound}
	adapter := appshopify.NewWarehouseCheckerAdapter(getter)

	owned, err := adapter.BelongsToTenant(context.Background(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("ErrNotFound must map to nil error, got %v", err)
	}
	if owned {
		t.Error("ErrNotFound must map to owned=false")
	}
}

func TestZZ2_WarehouseCheckerAdapter_OtherErrorPropagates(t *testing.T) {
	underlying := errors.New("upstream timeout")
	getter := &zzGetter2{err: underlying}
	adapter := appshopify.NewWarehouseCheckerAdapter(getter)

	owned, err := adapter.BelongsToTenant(context.Background(), uuid.New(), uuid.New())
	if owned {
		t.Error("unexpected error must map to owned=false")
	}
	if !errors.Is(err, underlying) {
		t.Errorf("expected the exact underlying error to propagate unwrapped, got %v", err)
	}
}

func TestZZ2_WarehouseCheckerAdapter_FoundMeansTrueNil(t *testing.T) {
	getter := &zzGetter2{wh: &domainwarehouse.Warehouse{ID: uuid.New()}}
	adapter := appshopify.NewWarehouseCheckerAdapter(getter)

	owned, err := adapter.BelongsToTenant(context.Background(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !owned {
		t.Error("expected owned=true when warehouse is found")
	}
}
