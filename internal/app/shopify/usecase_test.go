package shopify_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	appshopify "github.com/hanmahong5-arch/lurus-tally/internal/app/shopify"
)

// ---- fakes ----------------------------------------------------------------

type fakeShopRepo struct {
	createErr error
	listItems []appshopify.ShopMapping
	listErr   error
	deleteErr error
	created   *appshopify.ShopMapping
	deletedID uuid.UUID
}

func (f *fakeShopRepo) Create(_ context.Context, m *appshopify.ShopMapping) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.created = m
	return nil
}

func (f *fakeShopRepo) ListByTenant(_ context.Context, _ uuid.UUID) ([]appshopify.ShopMapping, error) {
	return f.listItems, f.listErr
}

func (f *fakeShopRepo) DeleteByID(_ context.Context, _, id uuid.UUID) error {
	f.deletedID = id
	return f.deleteErr
}

type fakeChecker struct {
	owned bool
	err   error
}

func (f *fakeChecker) BelongsToTenant(_ context.Context, _, _ uuid.UUID) (bool, error) {
	return f.owned, f.err
}

// ---- BindShopUseCase tests ------------------------------------------------

func TestBindShopUseCase_Execute_Success(t *testing.T) {
	repo := &fakeShopRepo{}
	checker := &fakeChecker{owned: true}
	uc := appshopify.NewBindShopUseCase(repo, checker)

	in := appshopify.BindInput{
		TenantID:    uuid.New(),
		ShopDomain:  "my-store.myshopify.com",
		WarehouseID: uuid.New(),
		CreatorID:   uuid.New(),
	}
	m, err := uc.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil mapping")
	}
	if m.ShopDomain != in.ShopDomain {
		t.Errorf("ShopDomain = %q; want %q", m.ShopDomain, in.ShopDomain)
	}
	if repo.created == nil {
		t.Error("expected repo.Create to be called")
	}
}

func TestBindShopUseCase_Execute_InvalidDomain(t *testing.T) {
	cases := []string{
		"mystore.shopify.com",
		"mystore",
		"mystore.myshopify.com.evil.com",
		"",
		"has spaces.myshopify.com",
		"UPPER.myshopify.com",
	}
	for _, domain := range cases {
		repo := &fakeShopRepo{}
		checker := &fakeChecker{owned: true}
		uc := appshopify.NewBindShopUseCase(repo, checker)

		in := appshopify.BindInput{
			TenantID:    uuid.New(),
			ShopDomain:  domain,
			WarehouseID: uuid.New(),
			CreatorID:   uuid.New(),
		}
		_, err := uc.Execute(context.Background(), in)
		if !errors.Is(err, appshopify.ErrInvalidDomain) {
			t.Errorf("domain %q: got %v; want ErrInvalidDomain", domain, err)
		}
	}
}

func TestBindShopUseCase_Execute_WarehouseNotOwned(t *testing.T) {
	repo := &fakeShopRepo{}
	checker := &fakeChecker{owned: false}
	uc := appshopify.NewBindShopUseCase(repo, checker)

	in := appshopify.BindInput{
		TenantID:    uuid.New(),
		ShopDomain:  "abc.myshopify.com",
		WarehouseID: uuid.New(),
		CreatorID:   uuid.New(),
	}
	_, err := uc.Execute(context.Background(), in)
	if !errors.Is(err, appshopify.ErrWarehouseNotOwned) {
		t.Errorf("got %v; want ErrWarehouseNotOwned", err)
	}
}

func TestBindShopUseCase_Execute_Duplicate(t *testing.T) {
	repo := &fakeShopRepo{createErr: appshopify.ErrShopAlreadyBound}
	checker := &fakeChecker{owned: true}
	uc := appshopify.NewBindShopUseCase(repo, checker)

	in := appshopify.BindInput{
		TenantID:    uuid.New(),
		ShopDomain:  "taken.myshopify.com",
		WarehouseID: uuid.New(),
		CreatorID:   uuid.New(),
	}
	_, err := uc.Execute(context.Background(), in)
	if !errors.Is(err, appshopify.ErrShopAlreadyBound) {
		t.Errorf("got %v; want ErrShopAlreadyBound", err)
	}
}

// ---- ListShopsUseCase tests -----------------------------------------------

func TestListShopsUseCase_Execute_Success(t *testing.T) {
	tenantID := uuid.New()
	items := []appshopify.ShopMapping{
		{ID: uuid.New(), TenantID: tenantID, ShopDomain: "a.myshopify.com"},
		{ID: uuid.New(), TenantID: tenantID, ShopDomain: "b.myshopify.com"},
	}
	repo := &fakeShopRepo{listItems: items}
	uc := appshopify.NewListShopsUseCase(repo)

	got, err := uc.Execute(context.Background(), tenantID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != len(items) {
		t.Errorf("len = %d; want %d", len(got), len(items))
	}
}

// ---- UnbindShopUseCase tests ----------------------------------------------

func TestUnbindShopUseCase_Execute_Success(t *testing.T) {
	repo := &fakeShopRepo{}
	uc := appshopify.NewUnbindShopUseCase(repo)

	tenantID := uuid.New()
	id := uuid.New()
	if err := uc.Execute(context.Background(), tenantID, id); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.deletedID != id {
		t.Errorf("deletedID = %v; want %v", repo.deletedID, id)
	}
}
