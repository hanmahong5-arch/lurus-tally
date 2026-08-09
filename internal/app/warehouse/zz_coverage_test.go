package warehouse_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	appwarehouse "github.com/hanmahong5-arch/lurus-tally/internal/app/warehouse"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/warehouse"
)

// zzFakeRepo is a fully programmable fake satisfying appwarehouse.Repository,
// distinct from fakeRepo in warehouse_usecases_test.go (that one is store-driven;
// this one lets every method return an arbitrary canned value+error pair so we
// can hit every error branch in the use cases directly).
type zzFakeRepo struct {
	createCalled bool
	createErr    error

	getByIDResult *domain.Warehouse
	getByIDErr    error

	listResult []*domain.Warehouse
	listTotal  int
	listErr    error
	lastFilter domain.ListFilter

	updateCalled bool
	updateErr    error

	deleteErr error

	restoreResult *domain.Warehouse
	restoreErr    error
}

func (r *zzFakeRepo) Create(_ context.Context, _ *domain.Warehouse) error {
	r.createCalled = true
	return r.createErr
}

func (r *zzFakeRepo) GetByID(_ context.Context, _, _ uuid.UUID) (*domain.Warehouse, error) {
	if r.getByIDErr != nil {
		return nil, r.getByIDErr
	}
	return r.getByIDResult, nil
}

func (r *zzFakeRepo) List(_ context.Context, f domain.ListFilter) ([]*domain.Warehouse, int, error) {
	r.lastFilter = f
	if r.listErr != nil {
		return nil, 0, r.listErr
	}
	return r.listResult, r.listTotal, nil
}

func (r *zzFakeRepo) Update(_ context.Context, _ *domain.Warehouse) error {
	r.updateCalled = true
	return r.updateErr
}

func (r *zzFakeRepo) Delete(_ context.Context, _, _ uuid.UUID) error {
	return r.deleteErr
}

func (r *zzFakeRepo) Restore(_ context.Context, _, _ uuid.UUID) (*domain.Warehouse, error) {
	if r.restoreErr != nil {
		return nil, r.restoreErr
	}
	return r.restoreResult, nil
}

var _ appwarehouse.Repository = (*zzFakeRepo)(nil)

var errRepoOpaque = errors.New("boom: repo backend unavailable")

// --- CreateUseCase ---

func TestZZCreateUseCase_ValidateFailure_RepoNotCalled(t *testing.T) {
	repo := &zzFakeRepo{}
	uc := appwarehouse.NewCreateUseCase(repo)

	// domain.Validate rejects empty Name (see warehouse.go Validate).
	_, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID: uuid.New(),
		Name:     "",
	})
	if err == nil {
		t.Fatal("expected validate error, got nil")
	}
	if repo.createCalled {
		t.Error("repo.Create must NOT be called when domain validation fails")
	}
	wantMsg := "warehouse create validate: name is required"
	if err.Error() != wantMsg {
		t.Errorf("error = %q, want %q", err.Error(), wantMsg)
	}
}

func TestZZCreateUseCase_DuplicateName_ErrorsIsSentinel(t *testing.T) {
	repo := &zzFakeRepo{createErr: appwarehouse.ErrDuplicateName}
	uc := appwarehouse.NewCreateUseCase(repo)

	_, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID: uuid.New(),
		Name:     "仓库A",
	})
	if !errors.Is(err, appwarehouse.ErrDuplicateName) {
		t.Fatalf("expected errors.Is ErrDuplicateName, got %v", err)
	}
	if !repo.createCalled {
		t.Error("repo.Create should have been called")
	}
}

func TestZZCreateUseCase_OpaqueRepoError_Wrapped(t *testing.T) {
	repo := &zzFakeRepo{createErr: errRepoOpaque}
	uc := appwarehouse.NewCreateUseCase(repo)

	_, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID: uuid.New(),
		Name:     "仓库B",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if errors.Is(err, appwarehouse.ErrDuplicateName) {
		t.Error("opaque repo error must not be reported as ErrDuplicateName")
	}
	if !errors.Is(err, errRepoOpaque) {
		t.Errorf("expected wrapped errRepoOpaque, got %v", err)
	}
	wantPrefix := "warehouse create: "
	if got := err.Error(); len(got) < len(wantPrefix) || got[:len(wantPrefix)] != wantPrefix {
		t.Errorf("error = %q, want prefix %q", got, wantPrefix)
	}
}

func TestZZCreateUseCase_Success_FieldsCarried(t *testing.T) {
	repo := &zzFakeRepo{}
	uc := appwarehouse.NewCreateUseCase(repo)
	tenantID := uuid.New()

	w, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID:  tenantID,
		Code:      "WH-01",
		Name:      "总仓",
		Address:   "广州市天河区",
		Manager:   "张三",
		IsDefault: true,
		Remark:    "备注",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.TenantID != tenantID || w.Code != "WH-01" || w.Address != "广州市天河区" ||
		w.Manager != "张三" || !w.IsDefault || w.Remark != "备注" {
		t.Errorf("fields not carried through correctly: %+v", w)
	}
	if w.CreatedAt.IsZero() || w.UpdatedAt.IsZero() {
		t.Error("expected CreatedAt/UpdatedAt to be set")
	}
	if !repo.createCalled {
		t.Error("expected repo.Create to be called")
	}
}

// --- GetByIDUseCase ---

func TestZZGetByIDUseCase_NotFound_Sentinel(t *testing.T) {
	repo := &zzFakeRepo{getByIDErr: appwarehouse.ErrNotFound}
	uc := appwarehouse.NewGetByIDUseCase(repo)

	_, err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, appwarehouse.ErrNotFound) {
		t.Fatalf("expected errors.Is ErrNotFound, got %v", err)
	}
	// Must be the bare sentinel, not wrapped opaquely, so handler can 404 directly.
	if err != appwarehouse.ErrNotFound {
		t.Errorf("expected exact sentinel value, got %v (wrapped)", err)
	}
}

func TestZZGetByIDUseCase_OpaqueError_Wrapped(t *testing.T) {
	repo := &zzFakeRepo{getByIDErr: errRepoOpaque}
	uc := appwarehouse.NewGetByIDUseCase(repo)

	_, err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if errors.Is(err, appwarehouse.ErrNotFound) {
		t.Error("opaque error must not present as ErrNotFound")
	}
	if !errors.Is(err, errRepoOpaque) {
		t.Errorf("expected wrapped errRepoOpaque, got %v", err)
	}
}

func TestZZGetByIDUseCase_Success(t *testing.T) {
	want := &domain.Warehouse{ID: uuid.New(), Name: "仓库X"}
	repo := &zzFakeRepo{getByIDResult: want}
	uc := appwarehouse.NewGetByIDUseCase(repo)

	got, err := uc.Execute(context.Background(), uuid.New(), want.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// --- UpdateUseCase ---

func TestZZUpdateUseCase_GetByIDNotFound_Sentinel(t *testing.T) {
	repo := &zzFakeRepo{getByIDErr: appwarehouse.ErrNotFound}
	uc := appwarehouse.NewUpdateUseCase(repo)

	newName := "新名字"
	_, err := uc.Execute(context.Background(), uuid.New(), uuid.New(), domain.UpdateInput{Name: &newName})
	if !errors.Is(err, appwarehouse.ErrNotFound) || err != appwarehouse.ErrNotFound {
		t.Fatalf("expected bare ErrNotFound sentinel, got %v", err)
	}
	if repo.updateCalled {
		t.Error("repo.Update must not be called when fetch fails")
	}
}

func TestZZUpdateUseCase_GetByIDOpaqueError_Wrapped(t *testing.T) {
	repo := &zzFakeRepo{getByIDErr: errRepoOpaque}
	uc := appwarehouse.NewUpdateUseCase(repo)

	_, err := uc.Execute(context.Background(), uuid.New(), uuid.New(), domain.UpdateInput{})
	if errors.Is(err, appwarehouse.ErrNotFound) {
		t.Error("opaque fetch error must not present as ErrNotFound")
	}
	if !errors.Is(err, errRepoOpaque) {
		t.Errorf("expected wrapped errRepoOpaque, got %v", err)
	}
}

func TestZZUpdateUseCase_ValidateFailureAfterMerge(t *testing.T) {
	existing := &domain.Warehouse{ID: uuid.New(), Name: "原名"}
	repo := &zzFakeRepo{getByIDResult: existing}
	uc := appwarehouse.NewUpdateUseCase(repo)

	// Setting Name to empty string via pointer triggers domain.Validate failure post-merge.
	empty := ""
	_, err := uc.Execute(context.Background(), uuid.New(), existing.ID, domain.UpdateInput{Name: &empty})
	if err == nil {
		t.Fatal("expected validate error, got nil")
	}
	wantMsg := "warehouse update validate: name is required"
	if err.Error() != wantMsg {
		t.Errorf("error = %q, want %q", err.Error(), wantMsg)
	}
	if repo.updateCalled {
		t.Error("repo.Update must not be called when post-merge validation fails")
	}
}

func TestZZUpdateUseCase_RepoUpdateNotFound_Sentinel(t *testing.T) {
	existing := &domain.Warehouse{ID: uuid.New(), Name: "原名"}
	repo := &zzFakeRepo{getByIDResult: existing, updateErr: appwarehouse.ErrNotFound}
	uc := appwarehouse.NewUpdateUseCase(repo)

	newName := "改名"
	_, err := uc.Execute(context.Background(), uuid.New(), existing.ID, domain.UpdateInput{Name: &newName})
	if !errors.Is(err, appwarehouse.ErrNotFound) || err != appwarehouse.ErrNotFound {
		t.Fatalf("expected bare ErrNotFound sentinel, got %v", err)
	}
}

func TestZZUpdateUseCase_RepoUpdateOpaqueError_Wrapped(t *testing.T) {
	existing := &domain.Warehouse{ID: uuid.New(), Name: "原名"}
	repo := &zzFakeRepo{getByIDResult: existing, updateErr: errRepoOpaque}
	uc := appwarehouse.NewUpdateUseCase(repo)

	newName := "改名"
	_, err := uc.Execute(context.Background(), uuid.New(), existing.ID, domain.UpdateInput{Name: &newName})
	if errors.Is(err, appwarehouse.ErrNotFound) {
		t.Error("opaque update error must not present as ErrNotFound")
	}
	if !errors.Is(err, errRepoOpaque) {
		t.Errorf("expected wrapped errRepoOpaque, got %v", err)
	}
}

func TestZZUpdateUseCase_PartialUpdate_OnlyNonNilFieldsApplied(t *testing.T) {
	existing := &domain.Warehouse{
		ID:        uuid.New(),
		Code:      "OLD-CODE",
		Name:      "旧名",
		Address:   "旧地址",
		Manager:   "旧经理",
		IsDefault: false,
		Remark:    "旧备注",
	}
	repo := &zzFakeRepo{getByIDResult: existing}
	uc := appwarehouse.NewUpdateUseCase(repo)

	newName := "新名"
	newManager := "新经理"
	got, err := uc.Execute(context.Background(), uuid.New(), existing.ID, domain.UpdateInput{
		Name:    &newName,
		Manager: &newManager,
		// Code, Address, IsDefault, Remark intentionally left nil.
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "新名" {
		t.Errorf("Name = %q, want 新名 (should be updated)", got.Name)
	}
	if got.Manager != "新经理" {
		t.Errorf("Manager = %q, want 新经理 (should be updated)", got.Manager)
	}
	// Untouched fields must remain exactly as they were before the merge.
	if got.Code != "OLD-CODE" {
		t.Errorf("Code = %q, want OLD-CODE (must be untouched)", got.Code)
	}
	if got.Address != "旧地址" {
		t.Errorf("Address = %q, want 旧地址 (must be untouched)", got.Address)
	}
	if got.IsDefault != false {
		t.Errorf("IsDefault = %v, want false (must be untouched)", got.IsDefault)
	}
	if got.Remark != "旧备注" {
		t.Errorf("Remark = %q, want 旧备注 (must be untouched)", got.Remark)
	}
	if !repo.updateCalled {
		t.Error("expected repo.Update to be called")
	}
}

func TestZZUpdateUseCase_AllFieldsNil_NoChange(t *testing.T) {
	existing := &domain.Warehouse{ID: uuid.New(), Name: "保持不变"}
	repo := &zzFakeRepo{getByIDResult: existing}
	uc := appwarehouse.NewUpdateUseCase(repo)

	got, err := uc.Execute(context.Background(), uuid.New(), existing.ID, domain.UpdateInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "保持不变" {
		t.Errorf("Name = %q, want 保持不变", got.Name)
	}
}

func TestZZUpdateUseCase_IsDefaultBoolPointer_FalseIsApplied(t *testing.T) {
	// Regression guard: a *bool pointing to false must still be treated as "set"
	// (non-nil), distinguishing "unset" from "explicitly false".
	existing := &domain.Warehouse{ID: uuid.New(), Name: "仓库", IsDefault: true}
	repo := &zzFakeRepo{getByIDResult: existing}
	uc := appwarehouse.NewUpdateUseCase(repo)

	falseVal := false
	got, err := uc.Execute(context.Background(), uuid.New(), existing.ID, domain.UpdateInput{IsDefault: &falseVal})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.IsDefault != false {
		t.Errorf("IsDefault = %v, want false to be applied via non-nil pointer", got.IsDefault)
	}
}

// --- DeleteUseCase ---

func TestZZDeleteUseCase_NotFound_Sentinel(t *testing.T) {
	repo := &zzFakeRepo{deleteErr: appwarehouse.ErrNotFound}
	uc := appwarehouse.NewDeleteUseCase(repo)

	err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, appwarehouse.ErrNotFound) || err != appwarehouse.ErrNotFound {
		t.Fatalf("expected bare ErrNotFound sentinel, got %v", err)
	}
}

func TestZZDeleteUseCase_OpaqueError_Wrapped(t *testing.T) {
	repo := &zzFakeRepo{deleteErr: errRepoOpaque}
	uc := appwarehouse.NewDeleteUseCase(repo)

	err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if errors.Is(err, appwarehouse.ErrNotFound) {
		t.Error("opaque delete error must not present as ErrNotFound")
	}
	if !errors.Is(err, errRepoOpaque) {
		t.Errorf("expected wrapped errRepoOpaque, got %v", err)
	}
}

func TestZZDeleteUseCase_Success(t *testing.T) {
	repo := &zzFakeRepo{}
	uc := appwarehouse.NewDeleteUseCase(repo)

	if err := uc.Execute(context.Background(), uuid.New(), uuid.New()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- RestoreUseCase ---

func TestZZRestoreUseCase_NotFound_Sentinel(t *testing.T) {
	repo := &zzFakeRepo{restoreErr: appwarehouse.ErrNotFound}
	uc := appwarehouse.NewRestoreUseCase(repo)

	_, err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, appwarehouse.ErrNotFound) || err != appwarehouse.ErrNotFound {
		t.Fatalf("expected bare ErrNotFound sentinel, got %v", err)
	}
}

func TestZZRestoreUseCase_OpaqueError_Wrapped(t *testing.T) {
	repo := &zzFakeRepo{restoreErr: errRepoOpaque}
	uc := appwarehouse.NewRestoreUseCase(repo)

	_, err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if errors.Is(err, appwarehouse.ErrNotFound) {
		t.Error("opaque restore error must not present as ErrNotFound")
	}
	if !errors.Is(err, errRepoOpaque) {
		t.Errorf("expected wrapped errRepoOpaque, got %v", err)
	}
}

func TestZZRestoreUseCase_Success(t *testing.T) {
	want := &domain.Warehouse{ID: uuid.New(), Name: "恢复的仓库"}
	repo := &zzFakeRepo{restoreResult: want}
	uc := appwarehouse.NewRestoreUseCase(repo)

	got, err := uc.Execute(context.Background(), uuid.New(), want.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// --- ListUseCase ---

func TestZZListUseCase_LimitClamping(t *testing.T) {
	tests := []struct {
		name      string
		inLimit   int
		wantLimit int
	}{
		{"zero uses default", 0, 20},
		{"negative uses default", -5, 20},
		{"in-range passes through", 50, 50},
		{"exactly at max passes through", 200, 200},
		{"over max clamps to 200", 500, 200},
		{"exactly at default boundary+1 passes through", 21, 21},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &zzFakeRepo{listResult: []*domain.Warehouse{}, listTotal: 0}
			uc := appwarehouse.NewListUseCase(repo)

			_, _, err := uc.Execute(context.Background(), domain.ListFilter{
				TenantID: uuid.New(),
				Limit:    tc.inLimit,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if repo.lastFilter.Limit != tc.wantLimit {
				t.Errorf("Limit = %d, want %d", repo.lastFilter.Limit, tc.wantLimit)
			}
		})
	}
}

func TestZZListUseCase_RepoError_Wrapped(t *testing.T) {
	repo := &zzFakeRepo{listErr: errRepoOpaque}
	uc := appwarehouse.NewListUseCase(repo)

	items, total, err := uc.Execute(context.Background(), domain.ListFilter{TenantID: uuid.New()})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, errRepoOpaque) {
		t.Errorf("expected wrapped errRepoOpaque, got %v", err)
	}
	if items != nil {
		t.Errorf("expected nil items on error, got %v", items)
	}
	if total != 0 {
		t.Errorf("expected 0 total on error, got %d", total)
	}
}

func TestZZListUseCase_Success_PassesThroughResults(t *testing.T) {
	want := []*domain.Warehouse{
		{ID: uuid.New(), Name: "仓库1"},
		{ID: uuid.New(), Name: "仓库2"},
	}
	repo := &zzFakeRepo{listResult: want, listTotal: 42}
	uc := appwarehouse.NewListUseCase(repo)

	items, total, err := uc.Execute(context.Background(), domain.ListFilter{TenantID: uuid.New(), Limit: 10, Query: "仓"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if total != 42 {
		t.Errorf("total = %d, want 42", total)
	}
	if repo.lastFilter.Query != "仓" {
		t.Errorf("Query = %q, want 仓 (filter must pass through unmodified)", repo.lastFilter.Query)
	}
	if repo.lastFilter.Limit != 10 {
		t.Errorf("Limit = %d, want 10 (in-range, no clamping)", repo.lastFilter.Limit)
	}
}
