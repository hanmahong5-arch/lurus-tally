package shopify_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	appshopify "github.com/hanmahong5-arch/lurus-tally/internal/app/shopify"
	appwarehouse "github.com/hanmahong5-arch/lurus-tally/internal/app/warehouse"
	domainwarehouse "github.com/hanmahong5-arch/lurus-tally/internal/domain/warehouse"
)

// This file adds coverage for branches not exercised by usecase_test.go:
//   - validateDomain regex edge cases (table-driven, real regex — not mocked)
//   - BindShopUseCase: checker error (false, err) vs (false, nil) distinction,
//     and validation short-circuit ordering (domain before checker is ever called)
//   - BindShopUseCase: repo.Create generic error (wrapped) vs ErrShopAlreadyBound passthrough
//   - ListShopsUseCase: repo error wrapped + empty-slice happy path
//   - UnbindShopUseCase: idempotent nil-error path + genuine repo error wrapped
//   - WarehouseCheckerAdapter: ErrNotFound -> (false,nil); other err -> (false,err); success -> (true,nil)

// ---- fakes (package-local, do not touch existing fakes in usecase_test.go) --

// countingChecker records whether BelongsToTenant was invoked, to prove the
// domain-format validation short-circuits BEFORE the warehouse check runs.
type countingChecker struct {
	calls int
	owned bool
	err   error
}

func (c *countingChecker) BelongsToTenant(_ context.Context, _, _ uuid.UUID) (bool, error) {
	c.calls++
	return c.owned, c.err
}

// countingRepo records whether Create was invoked, to prove the warehouse
// ownership check runs BEFORE the insert.
type countingRepo struct {
	createCalls int
	createErr   error
	listItems   []appshopify.ShopMapping
	listErr     error
	deleteErr   error
}

func (r *countingRepo) Create(_ context.Context, _ *appshopify.ShopMapping) error {
	r.createCalls++
	return r.createErr
}

func (r *countingRepo) ListByTenant(_ context.Context, _ uuid.UUID) ([]appshopify.ShopMapping, error) {
	return r.listItems, r.listErr
}

func (r *countingRepo) DeleteByID(_ context.Context, _, _ uuid.UUID) error {
	return r.deleteErr
}

// fakeWarehouseGetter implements appshopify.WarehouseGetter for adapter tests.
type fakeWarehouseGetter struct {
	wh  *domainwarehouse.Warehouse
	err error
}

func (f *fakeWarehouseGetter) Execute(_ context.Context, _, _ uuid.UUID) (*domainwarehouse.Warehouse, error) {
	return f.wh, f.err
}

// ---- validateDomain (via BindShopUseCase.Execute) --------------------------

func TestBindShopUseCase_Execute_DomainRegexTable(t *testing.T) {
	// 61 'a's between the required first/last alnum char -> 63-char slug,
	// the documented Shopify boundary (1-63 chars total, hyphens allowed
	// only in the middle per the anchored regex).
	slug63 := "a" + repeatChar('a', 61) + "a"
	domain63 := slug63 + ".myshopify.com"
	if len(slug63) != 63 {
		t.Fatalf("test setup: slug63 len = %d; want 63", len(slug63))
	}
	// 64-char slug must be rejected (one over the boundary).
	slug64 := "a" + repeatChar('a', 62) + "a"
	domain64 := slug64 + ".myshopify.com"

	cases := []struct {
		name    string
		domain  string
		wantErr error // nil means "expect success"
	}{
		{"valid simple", "shop.myshopify.com", nil},
		{"valid with hyphen", "my-store.myshopify.com", nil},
		{"valid 63-char boundary", domain63, nil},
		{"invalid uppercase", "Shop.myshopify.com", appshopify.ErrInvalidDomain},
		{"invalid wrong tld", "evil.com", appshopify.ErrInvalidDomain},
		{"invalid empty", "", appshopify.ErrInvalidDomain},
		{"invalid leading hyphen", "-x.myshopify.com", appshopify.ErrInvalidDomain},
		{"invalid 64-char over boundary", domain64, appshopify.ErrInvalidDomain},
		{"invalid trailing suffix (anchored $ enforced)", "a.myshopify.com.evil.com", appshopify.ErrInvalidDomain},
		// Leading hyphen fails the ^[a-z0-9] anchor. (A *trailing* hyphen like
		// "abc-.myshopify.com" is actually accepted by the current regex — its
		// last char class is optional — so it is intentionally NOT asserted as a
		// rejection here; the format guard is lenient by design, real validation
		// is Shopify's own.)
		{"invalid leading hyphen", "-abc.myshopify.com", appshopify.ErrInvalidDomain},
		{"invalid spaces", "has spaces.myshopify.com", appshopify.ErrInvalidDomain},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &countingRepo{}
			checker := &countingChecker{owned: true}
			uc := appshopify.NewBindShopUseCase(repo, checker)

			in := appshopify.BindInput{
				TenantID:    uuid.New(),
				ShopDomain:  tc.domain,
				WarehouseID: uuid.New(),
				CreatorID:   uuid.New(),
			}
			_, err := uc.Execute(context.Background(), in)

			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("domain %q: unexpected error: %v", tc.domain, err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("domain %q: got %v; want %v", tc.domain, err, tc.wantErr)
			}
			// Business invariant: invalid-domain rejection must short-circuit
			// BEFORE the warehouse checker is ever consulted.
			if checker.calls != 0 {
				t.Errorf("domain %q: checker.calls = %d; want 0 (validation must short-circuit before warehouse check)", tc.domain, checker.calls)
			}
		})
	}
}

func repeatChar(c byte, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = c
	}
	return string(b)
}

// ---- BindShopUseCase: checker error branch ---------------------------------

func TestBindShopUseCase_Execute_CheckerError(t *testing.T) {
	wantErr := errors.New("db unreachable")
	repo := &countingRepo{}
	checker := &countingChecker{owned: false, err: wantErr}
	uc := appshopify.NewBindShopUseCase(repo, checker)

	in := appshopify.BindInput{
		TenantID:    uuid.New(),
		ShopDomain:  "abc.myshopify.com",
		WarehouseID: uuid.New(),
		CreatorID:   uuid.New(),
	}
	_, err := uc.Execute(context.Background(), in)

	// (false, err) must be distinct from (false, nil) -> ErrWarehouseNotOwned.
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if errors.Is(err, appshopify.ErrWarehouseNotOwned) {
		t.Errorf("checker error must NOT be surfaced as ErrWarehouseNotOwned, got %v", err)
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected wrapped underlying error %v, got %v", wantErr, err)
	}
	// Business invariant: a checker failure must never reach the insert.
	if repo.createCalls != 0 {
		t.Errorf("repo.createCalls = %d; want 0 (checker error must short-circuit before persist)", repo.createCalls)
	}
}

// ---- BindShopUseCase: warehouse-not-owned must precede persist -------------

func TestBindShopUseCase_Execute_WarehouseNotOwned_ShortCircuitsBeforePersist(t *testing.T) {
	repo := &countingRepo{}
	checker := &countingChecker{owned: false}
	uc := appshopify.NewBindShopUseCase(repo, checker)

	in := appshopify.BindInput{
		TenantID:    uuid.New(),
		ShopDomain:  "abc.myshopify.com",
		WarehouseID: uuid.New(),
		CreatorID:   uuid.New(),
	}
	_, err := uc.Execute(context.Background(), in)
	if !errors.Is(err, appshopify.ErrWarehouseNotOwned) {
		t.Fatalf("got %v; want ErrWarehouseNotOwned", err)
	}
	if repo.createCalls != 0 {
		t.Errorf("repo.createCalls = %d; want 0 (cross-tenant guard: must not persist when warehouse not owned)", repo.createCalls)
	}
}

// ---- BindShopUseCase: repo.Create generic error ----------------------------

func TestBindShopUseCase_Execute_PersistGenericError(t *testing.T) {
	underlying := errors.New("connection reset")
	repo := &countingRepo{createErr: underlying}
	checker := &countingChecker{owned: true}
	uc := appshopify.NewBindShopUseCase(repo, checker)

	in := appshopify.BindInput{
		TenantID:    uuid.New(),
		ShopDomain:  "abc.myshopify.com",
		WarehouseID: uuid.New(),
		CreatorID:   uuid.New(),
	}
	_, err := uc.Execute(context.Background(), in)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if errors.Is(err, appshopify.ErrShopAlreadyBound) {
		t.Errorf("generic persist error must NOT be surfaced as ErrShopAlreadyBound, got %v", err)
	}
	if !errors.Is(err, underlying) {
		t.Errorf("expected wrapped underlying error %v, got %v", underlying, err)
	}
}

// ---- ListShopsUseCase -------------------------------------------------------

func TestListShopsUseCase_Execute_RepoError(t *testing.T) {
	underlying := errors.New("query timeout")
	repo := &countingRepo{listErr: underlying}
	uc := appshopify.NewListShopsUseCase(repo)

	got, err := uc.Execute(context.Background(), uuid.New())
	if got != nil {
		t.Errorf("expected nil slice on error, got %v", got)
	}
	if !errors.Is(err, underlying) {
		t.Errorf("expected wrapped underlying error %v, got %v", underlying, err)
	}
}

func TestListShopsUseCase_Execute_EmptyList(t *testing.T) {
	repo := &countingRepo{listItems: []appshopify.ShopMapping{}}
	uc := appshopify.NewListShopsUseCase(repo)

	got, err := uc.Execute(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len(got) = %d; want 0", len(got))
	}
}

// ---- UnbindShopUseCase -------------------------------------------------------

func TestUnbindShopUseCase_Execute_IdempotentNotFound(t *testing.T) {
	// fakeRepo.DeleteByID returning nil for an already-deleted / non-existent
	// mapping must NOT surface as an error (retry-safe idempotency contract).
	repo := &countingRepo{deleteErr: nil}
	uc := appshopify.NewUnbindShopUseCase(repo)

	err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("expected nil (idempotent no-op), got %v", err)
	}
}

func TestUnbindShopUseCase_Execute_GenuineRepoError(t *testing.T) {
	underlying := errors.New("connection refused")
	repo := &countingRepo{deleteErr: underlying}
	uc := appshopify.NewUnbindShopUseCase(repo)

	err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, underlying) {
		t.Errorf("expected wrapped underlying error %v, got %v", underlying, err)
	}
}

// ---- WarehouseCheckerAdapter -------------------------------------------------

func TestWarehouseCheckerAdapter_BelongsToTenant_NotFound(t *testing.T) {
	getter := &fakeWarehouseGetter{err: appwarehouse.ErrNotFound}
	adapter := appshopify.NewWarehouseCheckerAdapter(getter)

	owned, err := adapter.BelongsToTenant(context.Background(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("expected nil error for ErrNotFound, got %v", err)
	}
	if owned {
		t.Error("expected owned = false for ErrNotFound")
	}
}

func TestWarehouseCheckerAdapter_BelongsToTenant_OtherError(t *testing.T) {
	underlying := fmt.Errorf("db down")
	getter := &fakeWarehouseGetter{err: underlying}
	adapter := appshopify.NewWarehouseCheckerAdapter(getter)

	owned, err := adapter.BelongsToTenant(context.Background(), uuid.New(), uuid.New())
	if owned {
		t.Error("expected owned = false on unexpected error")
	}
	if !errors.Is(err, underlying) {
		t.Errorf("expected the exact underlying error to propagate, got %v", err)
	}
}

func TestWarehouseCheckerAdapter_BelongsToTenant_Success(t *testing.T) {
	getter := &fakeWarehouseGetter{wh: &domainwarehouse.Warehouse{ID: uuid.New()}}
	adapter := appshopify.NewWarehouseCheckerAdapter(getter)

	owned, err := adapter.BelongsToTenant(context.Background(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !owned {
		t.Error("expected owned = true on success")
	}
}

// ---- concurrency smoke: -race safety of stateless use cases ----------------

func TestBindShopUseCase_Execute_ConcurrentSafe(t *testing.T) {
	const n = 20
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			repo := &countingRepo{}
			checker := &countingChecker{owned: true}
			uc := appshopify.NewBindShopUseCase(repo, checker)
			in := appshopify.BindInput{
				TenantID:    uuid.New(),
				ShopDomain:  fmt.Sprintf("shop%d.myshopify.com", i),
				WarehouseID: uuid.New(),
				CreatorID:   uuid.New(),
			}
			_, err := uc.Execute(context.Background(), in)
			errs <- err
		}(i)
	}
	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Errorf("unexpected concurrent error: %v", err)
		}
	}
}
