package product

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/product"
)

// errNotFound is a sentinel used by fakeRepo to simulate repository-layer failures
// (analogous to the real repo's ErrNotFound) so we can assert error-wrapping behavior.
var errNotFound = errors.New("sentinel: not found")

// fakeRepo is a hand-written in-memory implementation of Repository. It records the
// *domain.Product pointer passed to Create/Update so tests can assert on exactly what
// the use case built, and lets each test configure canned errors/return values so every
// branch in the use cases can be exercised without a real DB.
type fakeRepo struct {
	createErr error
	createdP  *domain.Product

	getByIDProduct *domain.Product
	getByIDErr     error

	listProducts []*domain.Product
	listTotal    int
	listErr      error
	lastFilter   domain.ListFilter

	updateErr error
	updatedP  *domain.Product

	deleteErr error

	restoreProduct *domain.Product
	restoreErr     error
}

func (f *fakeRepo) Create(_ context.Context, p *domain.Product) error {
	f.createdP = p
	return f.createErr
}

func (f *fakeRepo) GetByID(_ context.Context, _, _ uuid.UUID) (*domain.Product, error) {
	if f.getByIDErr != nil {
		return nil, f.getByIDErr
	}
	return f.getByIDProduct, nil
}

func (f *fakeRepo) List(_ context.Context, filter domain.ListFilter) ([]*domain.Product, int, error) {
	f.lastFilter = filter
	if f.listErr != nil {
		return nil, 0, f.listErr
	}
	return f.listProducts, f.listTotal, nil
}

func (f *fakeRepo) Update(_ context.Context, p *domain.Product) error {
	f.updatedP = p
	return f.updateErr
}

func (f *fakeRepo) Delete(_ context.Context, _, _ uuid.UUID) error {
	return f.deleteErr
}

func (f *fakeRepo) Restore(_ context.Context, _, _ uuid.UUID) (*domain.Product, error) {
	if f.restoreErr != nil {
		return nil, f.restoreErr
	}
	return f.restoreProduct, nil
}

// ---------------------------------------------------------------------------
// CreateUseCase
// ---------------------------------------------------------------------------

func TestCreateUseCase_Execute_ValidationErrors(t *testing.T) {
	tenant := uuid.New()
	cases := []struct {
		name string
		in   domain.CreateInput
		want string
	}{
		{
			name: "missing tenant id",
			in:   domain.CreateInput{TenantID: uuid.Nil, Code: "c1", Name: "n1"},
			want: "create product: tenant_id is required",
		},
		{
			name: "missing code",
			in:   domain.CreateInput{TenantID: tenant, Code: "", Name: "n1"},
			want: "create product: code is required",
		},
		{
			name: "missing name",
			in:   domain.CreateInput{TenantID: tenant, Code: "c1", Name: ""},
			want: "create product: name is required",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{}
			uc := NewCreateUseCase(repo)
			got, err := uc.Execute(context.Background(), tc.in)
			if got != nil {
				t.Fatalf("expected nil product, got %+v", got)
			}
			if err == nil || err.Error() != tc.want {
				t.Fatalf("expected error %q, got %v", tc.want, err)
			}
			if repo.createdP != nil {
				t.Fatalf("repo.Create must not be called on validation failure, got %+v", repo.createdP)
			}
		})
	}
}

func TestCreateUseCase_Execute_InvalidMeasurementStrategy(t *testing.T) {
	repo := &fakeRepo{}
	uc := NewCreateUseCase(repo)
	in := domain.CreateInput{
		TenantID:            uuid.New(),
		Code:                "c1",
		Name:                "n1",
		MeasurementStrategy: domain.MeasurementStrategy("bogus"),
	}
	got, err := uc.Execute(context.Background(), in)
	if got != nil {
		t.Fatalf("expected nil product, got %+v", got)
	}
	wantSubstr := "create product: invalid measurement_strategy"
	if err == nil || len(err.Error()) < len(wantSubstr) || err.Error()[:len(wantSubstr)] != wantSubstr {
		t.Fatalf("expected error prefix %q, got %v", wantSubstr, err)
	}
	if repo.createdP != nil {
		t.Fatalf("repo.Create must not be called when strategy invalid")
	}
}

func TestCreateUseCase_Execute_DefaultsStrategyToIndividual(t *testing.T) {
	repo := &fakeRepo{}
	uc := NewCreateUseCase(repo)
	tenant := uuid.New()

	before := time.Now().UTC()
	got, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID: tenant,
		Code:     "c1",
		Name:     "n1",
		// MeasurementStrategy intentionally empty
	})
	after := time.Now().UTC()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.MeasurementStrategy != domain.StrategyIndividual {
		t.Fatalf("expected default strategy %q, got %q", domain.StrategyIndividual, got.MeasurementStrategy)
	}
	if !got.Enabled {
		t.Fatalf("Enabled must be forced true on create")
	}
	if got.TenantID != tenant {
		t.Fatalf("TenantID mismatch: got %v want %v", got.TenantID, tenant)
	}
	if got.ID == uuid.Nil {
		t.Fatalf("expected a generated non-nil ID")
	}
	if string(got.Attributes) != "{}" {
		t.Fatalf("expected empty Attributes normalized to '{}', got %q", string(got.Attributes))
	}
	if !got.CreatedAt.Equal(got.UpdatedAt) {
		t.Fatalf("expected CreatedAt == UpdatedAt on create, got %v vs %v", got.CreatedAt, got.UpdatedAt)
	}
	if got.CreatedAt.Before(before) || got.CreatedAt.After(after) {
		t.Fatalf("CreatedAt %v not within [%v, %v]", got.CreatedAt, before, after)
	}
	if got.CreatedAt.Location() != time.UTC {
		t.Fatalf("expected CreatedAt in UTC, got location %v", got.CreatedAt.Location())
	}
	// Assert the use case handed the repo the exact same pointer/entity it returns.
	if repo.createdP != got {
		t.Fatalf("expected repo.Create to be called with the returned product pointer")
	}
}

func TestCreateUseCase_Execute_PreservesExplicitStrategyAndAttributes(t *testing.T) {
	repo := &fakeRepo{}
	uc := NewCreateUseCase(repo)
	tenant := uuid.New()
	rawAttrs := json.RawMessage(`{"hs_code":"1234"}`)

	got, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID:            tenant,
		Code:                "c1",
		Name:                "n1",
		MeasurementStrategy: domain.StrategyWeight,
		Attributes:          rawAttrs,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.MeasurementStrategy != domain.StrategyWeight {
		t.Fatalf("expected explicit strategy preserved, got %q", got.MeasurementStrategy)
	}
	if string(got.Attributes) != string(rawAttrs) {
		t.Fatalf("expected explicit attributes preserved, got %q", string(got.Attributes))
	}
}

func TestCreateUseCase_Execute_RepoErrorWrapped(t *testing.T) {
	repo := &fakeRepo{createErr: errNotFound}
	uc := NewCreateUseCase(repo)
	got, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID: uuid.New(),
		Code:     "c1",
		Name:     "n1",
	})
	if got != nil {
		t.Fatalf("expected nil product on repo error, got %+v", got)
	}
	if !errors.Is(err, errNotFound) {
		t.Fatalf("expected wrapped errNotFound, got %v", err)
	}
	wantPrefix := "create product: "
	if err == nil || len(err.Error()) < len(wantPrefix) || err.Error()[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("expected error prefixed with %q, got %v", wantPrefix, err)
	}
}

// ---------------------------------------------------------------------------
// UpdateUseCase
// ---------------------------------------------------------------------------

func baseProduct(tenant uuid.UUID) *domain.Product {
	return &domain.Product{
		ID:                  uuid.New(),
		TenantID:            tenant,
		Code:                "orig-code",
		Name:                "orig-name",
		Manufacturer:        "orig-mfg",
		Model:               "orig-model",
		Spec:                "orig-spec",
		Brand:               "orig-brand",
		Mnemonic:            "orig-mnemonic",
		Color:               "orig-color",
		ShelfPosition:       "orig-shelf",
		Remark:              "orig-remark",
		Enabled:             true,
		EnableSerialNo:      false,
		EnableLotNo:         false,
		MeasurementStrategy: domain.StrategyIndividual,
		Attributes:          json.RawMessage(`{"orig":true}`),
		CreatedAt:           time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:           time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestUpdateUseCase_Execute_MissingTenantID(t *testing.T) {
	repo := &fakeRepo{}
	uc := NewUpdateUseCase(repo)
	got, err := uc.Execute(context.Background(), uuid.Nil, uuid.New(), domain.UpdateInput{})
	if got != nil {
		t.Fatalf("expected nil product, got %+v", got)
	}
	want := "update product: tenant_id is required"
	if err == nil || err.Error() != want {
		t.Fatalf("expected error %q, got %v", want, err)
	}
}

func TestUpdateUseCase_Execute_GetByIDErrorPropagated(t *testing.T) {
	repo := &fakeRepo{getByIDErr: errNotFound}
	uc := NewUpdateUseCase(repo)
	tenant := uuid.New()
	got, err := uc.Execute(context.Background(), tenant, uuid.New(), domain.UpdateInput{Name: "new"})
	if got != nil {
		t.Fatalf("expected nil product on GetByID error, got %+v", got)
	}
	if !errors.Is(err, errNotFound) {
		t.Fatalf("expected wrapped errNotFound, got %v", err)
	}
	if repo.updatedP != nil {
		t.Fatalf("repo.Update must not be called when GetByID fails (no silent cross-tenant update)")
	}
}

func TestUpdateUseCase_Execute_PartialUpdateOnlyOverwritesNonZeroFields(t *testing.T) {
	tenant := uuid.New()
	orig := baseProduct(tenant)
	repo := &fakeRepo{getByIDProduct: orig}
	uc := NewUpdateUseCase(repo)

	// Only Name is set; everything else should remain untouched.
	got, err := uc.Execute(context.Background(), tenant, orig.ID, domain.UpdateInput{Name: "new-name"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "new-name" {
		t.Fatalf("expected Name updated to 'new-name', got %q", got.Name)
	}
	// Untouched fields must retain their original values.
	if got.Code != "orig-code" {
		t.Fatalf("Code must be untouched, got %q", got.Code)
	}
	if got.Manufacturer != "orig-mfg" {
		t.Fatalf("Manufacturer must be untouched, got %q", got.Manufacturer)
	}
	if got.MeasurementStrategy != domain.StrategyIndividual {
		t.Fatalf("MeasurementStrategy must be untouched, got %q", got.MeasurementStrategy)
	}
	if string(got.Attributes) != `{"orig":true}` {
		t.Fatalf("Attributes must be untouched, got %q", string(got.Attributes))
	}
	if !got.Enabled {
		t.Fatalf("Enabled must be untouched (stays true), got %v", got.Enabled)
	}
	if got.CategoryID != nil {
		t.Fatalf("CategoryID must remain nil (untouched), got %+v", got.CategoryID)
	}
	if got.ExpiryDays != nil {
		t.Fatalf("ExpiryDays must remain nil (untouched), got %+v", got.ExpiryDays)
	}
	if got.DefaultUnitID != nil {
		t.Fatalf("DefaultUnitID must remain nil (untouched), got %+v", got.DefaultUnitID)
	}
	if !got.UpdatedAt.After(orig.CreatedAt) {
		t.Fatalf("expected UpdatedAt refreshed to now, got %v", got.UpdatedAt)
	}
	if repo.updatedP != got {
		t.Fatalf("expected repo.Update called with the same product pointer returned")
	}
}

func TestUpdateUseCase_Execute_EnabledFalsePointerDeref(t *testing.T) {
	// Guards the *bool vs value trap: Enabled must be set from *in.Enabled even
	// when the target value is false (a zero-value bool would be skipped by a
	// naive `if in.Enabled {}` check, but this is a pointer so false must apply).
	tenant := uuid.New()
	orig := baseProduct(tenant)
	orig.Enabled = true
	repo := &fakeRepo{getByIDProduct: orig}
	uc := NewUpdateUseCase(repo)

	f := false
	got, err := uc.Execute(context.Background(), tenant, orig.ID, domain.UpdateInput{Enabled: &f})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Enabled {
		t.Fatalf("expected Enabled=false to be applied via pointer deref, got true")
	}
}

func TestUpdateUseCase_Execute_AllFieldsAndPointerFields(t *testing.T) {
	tenant := uuid.New()
	orig := baseProduct(tenant)
	repo := &fakeRepo{getByIDProduct: orig}
	uc := NewUpdateUseCase(repo)

	catID := uuid.New()
	unitID := uuid.New()
	expiry := 30
	weight := "12.5"
	tru := true

	in := domain.UpdateInput{
		CategoryID:          &catID,
		Name:                "n2",
		Manufacturer:        "m2",
		Model:               "mo2",
		Spec:                "s2",
		Brand:               "b2",
		Mnemonic:            "mn2",
		Color:               "c2",
		ExpiryDays:          &expiry,
		WeightKg:            &weight,
		Enabled:             &tru,
		EnableSerialNo:      &tru,
		EnableLotNo:         &tru,
		ShelfPosition:       "sp2",
		ImgURLs:             []string{"http://a", "http://b"},
		Remark:              "r2",
		MeasurementStrategy: domain.StrategyBatch,
		DefaultUnitID:       &unitID,
		Attributes:          json.RawMessage(`{"new":true}`),
	}

	got, err := uc.Execute(context.Background(), tenant, orig.ID, in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.CategoryID != &catID {
		t.Fatalf("CategoryID pointer not applied")
	}
	if got.Name != "n2" || got.Manufacturer != "m2" || got.Model != "mo2" || got.Spec != "s2" ||
		got.Brand != "b2" || got.Mnemonic != "mn2" || got.Color != "c2" || got.ShelfPosition != "sp2" || got.Remark != "r2" {
		t.Fatalf("string fields not fully applied: %+v", got)
	}
	if got.ExpiryDays != &expiry {
		t.Fatalf("ExpiryDays pointer not applied")
	}
	if got.WeightKg != &weight {
		t.Fatalf("WeightKg pointer not applied")
	}
	if !got.Enabled || !got.EnableSerialNo || !got.EnableLotNo {
		t.Fatalf("bool pointer fields not applied: %+v", got)
	}
	if len(got.ImgURLs) != 2 || got.ImgURLs[0] != "http://a" {
		t.Fatalf("ImgURLs not applied: %+v", got.ImgURLs)
	}
	if got.MeasurementStrategy != domain.StrategyBatch {
		t.Fatalf("MeasurementStrategy not applied, got %q", got.MeasurementStrategy)
	}
	if got.DefaultUnitID != &unitID {
		t.Fatalf("DefaultUnitID pointer not applied")
	}
	if string(got.Attributes) != `{"new":true}` {
		t.Fatalf("Attributes not applied, got %q", string(got.Attributes))
	}
}

func TestUpdateUseCase_Execute_InvalidMeasurementStrategyRejectedWithoutPersisting(t *testing.T) {
	tenant := uuid.New()
	orig := baseProduct(tenant)
	repo := &fakeRepo{getByIDProduct: orig}
	uc := NewUpdateUseCase(repo)

	got, err := uc.Execute(context.Background(), tenant, orig.ID, domain.UpdateInput{
		MeasurementStrategy: domain.MeasurementStrategy("nope"),
	})
	if got != nil {
		t.Fatalf("expected nil product on invalid strategy, got %+v", got)
	}
	if err == nil {
		t.Fatalf("expected error for invalid strategy")
	}
	wantPrefix := "update product: invalid measurement_strategy"
	if len(err.Error()) < len(wantPrefix) || err.Error()[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("expected error prefix %q, got %v", wantPrefix, err)
	}
	if repo.updatedP != nil {
		t.Fatalf("repo.Update must not be called when strategy invalid, got %+v", repo.updatedP)
	}
	// Original product's strategy must remain untouched since we mutate the same pointer
	// returned by GetByID only after validation succeeds.
	if orig.MeasurementStrategy != domain.StrategyIndividual {
		t.Fatalf("original product strategy must be untouched, got %q", orig.MeasurementStrategy)
	}
}

func TestUpdateUseCase_Execute_RepoUpdateErrorWrapped(t *testing.T) {
	tenant := uuid.New()
	orig := baseProduct(tenant)
	repo := &fakeRepo{getByIDProduct: orig, updateErr: errNotFound}
	uc := NewUpdateUseCase(repo)

	got, err := uc.Execute(context.Background(), tenant, orig.ID, domain.UpdateInput{Name: "x"})
	if got != nil {
		t.Fatalf("expected nil product on repo.Update error, got %+v", got)
	}
	if !errors.Is(err, errNotFound) {
		t.Fatalf("expected wrapped errNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// ListUseCase
// ---------------------------------------------------------------------------

func TestListUseCase_Execute_MissingTenantID(t *testing.T) {
	repo := &fakeRepo{}
	uc := NewListUseCase(repo)
	got, err := uc.Execute(context.Background(), domain.ListFilter{TenantID: uuid.Nil})
	if got != nil {
		t.Fatalf("expected nil output, got %+v", got)
	}
	want := "list products: tenant_id is required"
	if err == nil || err.Error() != want {
		t.Fatalf("expected error %q, got %v", want, err)
	}
}

func TestListUseCase_Execute_LimitClamping(t *testing.T) {
	tenant := uuid.New()
	cases := []struct {
		name      string
		limitIn   int
		wantLimit int
	}{
		{"zero clamps to default 20", 0, 20},
		{"negative clamps to default 20", -1, 20},
		{"20 passes through unchanged", 20, 20},
		{"200 passes through unchanged (upper boundary)", 200, 200},
		{"201 clamps down to 200", 201, 200},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{}
			uc := NewListUseCase(repo)
			_, err := uc.Execute(context.Background(), domain.ListFilter{TenantID: tenant, Limit: tc.limitIn})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if repo.lastFilter.Limit != tc.wantLimit {
				t.Fatalf("limit %d: expected clamped limit %d, got %d", tc.limitIn, tc.wantLimit, repo.lastFilter.Limit)
			}
		})
	}
}

func TestListUseCase_Execute_ReturnsItemsAndTotal(t *testing.T) {
	tenant := uuid.New()
	items := []*domain.Product{{ID: uuid.New(), TenantID: tenant}, {ID: uuid.New(), TenantID: tenant}}
	repo := &fakeRepo{listProducts: items, listTotal: 42}
	uc := NewListUseCase(repo)

	out, err := uc.Execute(context.Background(), domain.ListFilter{TenantID: tenant, Limit: 10, Offset: 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Total != 42 {
		t.Fatalf("expected Total=42, got %d", out.Total)
	}
	if len(out.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(out.Items))
	}
	if repo.lastFilter.Offset != 5 {
		t.Fatalf("expected Offset passed through unchanged, got %d", repo.lastFilter.Offset)
	}
}

func TestListUseCase_Execute_RepoErrorWrapped(t *testing.T) {
	repo := &fakeRepo{listErr: errNotFound}
	uc := NewListUseCase(repo)
	got, err := uc.Execute(context.Background(), domain.ListFilter{TenantID: uuid.New()})
	if got != nil {
		t.Fatalf("expected nil output on repo error, got %+v", got)
	}
	if !errors.Is(err, errNotFound) {
		t.Fatalf("expected wrapped errNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// GetUseCase
// ---------------------------------------------------------------------------

func TestGetUseCase_Execute_MissingTenantID(t *testing.T) {
	repo := &fakeRepo{}
	uc := NewGetUseCase(repo)
	got, err := uc.Execute(context.Background(), uuid.Nil, uuid.New())
	if got != nil {
		t.Fatalf("expected nil product, got %+v", got)
	}
	want := "get product: tenant_id is required"
	if err == nil || err.Error() != want {
		t.Fatalf("expected error %q, got %v", want, err)
	}
}

func TestGetUseCase_Execute_Success(t *testing.T) {
	tenant := uuid.New()
	want := &domain.Product{ID: uuid.New(), TenantID: tenant, Name: "n1"}
	repo := &fakeRepo{getByIDProduct: want}
	uc := NewGetUseCase(repo)

	got, err := uc.Execute(context.Background(), tenant, want.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Fatalf("expected same product pointer returned, got %+v want %+v", got, want)
	}
}

func TestGetUseCase_Execute_RepoErrorWrapped(t *testing.T) {
	repo := &fakeRepo{getByIDErr: errNotFound}
	uc := NewGetUseCase(repo)
	got, err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if got != nil {
		t.Fatalf("expected nil product on repo error, got %+v", got)
	}
	if !errors.Is(err, errNotFound) {
		t.Fatalf("expected wrapped errNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// DeleteUseCase
// ---------------------------------------------------------------------------

func TestDeleteUseCase_Execute_MissingTenantID(t *testing.T) {
	repo := &fakeRepo{}
	uc := NewDeleteUseCase(repo)
	err := uc.Execute(context.Background(), uuid.Nil, uuid.New())
	want := "delete product: tenant_id is required"
	if err == nil || err.Error() != want {
		t.Fatalf("expected error %q, got %v", want, err)
	}
}

func TestDeleteUseCase_Execute_Success(t *testing.T) {
	repo := &fakeRepo{}
	uc := NewDeleteUseCase(repo)
	if err := uc.Execute(context.Background(), uuid.New(), uuid.New()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteUseCase_Execute_RepoErrorWrapped(t *testing.T) {
	repo := &fakeRepo{deleteErr: errNotFound}
	uc := NewDeleteUseCase(repo)
	err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, errNotFound) {
		t.Fatalf("expected wrapped errNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// RestoreUseCase
// ---------------------------------------------------------------------------

func TestRestoreUseCase_Execute_MissingTenantID(t *testing.T) {
	repo := &fakeRepo{}
	uc := NewRestoreUseCase(repo)
	got, err := uc.Execute(context.Background(), uuid.Nil, uuid.New())
	if got != nil {
		t.Fatalf("expected nil product, got %+v", got)
	}
	want := "restore product: tenant_id is required"
	if err == nil || err.Error() != want {
		t.Fatalf("expected error %q, got %v", want, err)
	}
}

func TestRestoreUseCase_Execute_MissingProductID(t *testing.T) {
	repo := &fakeRepo{}
	uc := NewRestoreUseCase(repo)
	got, err := uc.Execute(context.Background(), uuid.New(), uuid.Nil)
	if got != nil {
		t.Fatalf("expected nil product, got %+v", got)
	}
	want := "restore product: product_id is required"
	if err == nil || err.Error() != want {
		t.Fatalf("expected error %q, got %v", want, err)
	}
}

func TestRestoreUseCase_Execute_Success(t *testing.T) {
	tenant := uuid.New()
	id := uuid.New()
	want := &domain.Product{ID: id, TenantID: tenant}
	repo := &fakeRepo{restoreProduct: want}
	uc := NewRestoreUseCase(repo)

	got, err := uc.Execute(context.Background(), tenant, id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Fatalf("expected same product pointer returned, got %+v want %+v", got, want)
	}
}

func TestRestoreUseCase_Execute_RepoErrorWrapped(t *testing.T) {
	repo := &fakeRepo{restoreErr: errNotFound}
	uc := NewRestoreUseCase(repo)
	got, err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if got != nil {
		t.Fatalf("expected nil product on repo error, got %+v", got)
	}
	if !errors.Is(err, errNotFound) {
		t.Fatalf("expected wrapped errNotFound, got %v", err)
	}
}
