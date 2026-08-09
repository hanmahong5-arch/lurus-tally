package supplier_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	appsupp "github.com/hanmahong5-arch/lurus-tally/internal/app/supplier"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/supplier"
)

// errGeneric is a non-sentinel error used to exercise the "wrap with operation
// prefix" branches (as opposed to the ErrNotFound/ErrDuplicateName remap branches).
var errGeneric = errors.New("boom: generic repo failure")

// zzFakeRepo is a fully-configurable stub implementing appsupp.Repository.
// Named distinctly from the sibling test file's fakeRepo to avoid redeclaration
// (both live in package supplier_test).
type zzFakeRepo struct {
	// Create
	createErr    error
	createCalled bool
	lastCreated  *domain.Supplier

	// GetByID
	getErr    error
	getResult *domain.Supplier

	// List
	listErr      error
	listResult   []*domain.Supplier
	listTotal    int
	lastListFilt domain.ListFilter

	// Update
	updateErr   error
	lastUpdated *domain.Supplier

	// Delete
	deleteErr error

	// Restore
	restoreErr    error
	restoreResult *domain.Supplier
}

func (r *zzFakeRepo) Create(_ context.Context, s *domain.Supplier) error {
	r.createCalled = true
	r.lastCreated = s
	if r.createErr != nil {
		return r.createErr
	}
	return nil
}

func (r *zzFakeRepo) GetByID(_ context.Context, _, _ uuid.UUID) (*domain.Supplier, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	return r.getResult, nil
}

func (r *zzFakeRepo) List(_ context.Context, f domain.ListFilter) ([]*domain.Supplier, int, error) {
	r.lastListFilt = f
	if r.listErr != nil {
		return nil, 0, r.listErr
	}
	return r.listResult, r.listTotal, nil
}

func (r *zzFakeRepo) Update(_ context.Context, s *domain.Supplier) error {
	r.lastUpdated = s
	if r.updateErr != nil {
		return r.updateErr
	}
	return nil
}

func (r *zzFakeRepo) Delete(_ context.Context, _, _ uuid.UUID) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	return nil
}

func (r *zzFakeRepo) Restore(_ context.Context, _, _ uuid.UUID) (*domain.Supplier, error) {
	if r.restoreErr != nil {
		return nil, r.restoreErr
	}
	return r.restoreResult, nil
}

var _ appsupp.Repository = (*zzFakeRepo)(nil)

// ---------------------------------------------------------------------------
// CreateUseCase
// ---------------------------------------------------------------------------

func TestZZ_CreateUseCase_Execute_ValidateFailsBeforeRepoWrite(t *testing.T) {
	repo := &zzFakeRepo{}
	uc := appsupp.NewCreateUseCase(repo)

	_, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID: uuid.New(),
		Name:     "", // empty Name -> Validate must reject
	})
	if err == nil {
		t.Fatal("expected error for empty Name, got nil")
	}
	if repo.createCalled {
		t.Error("repo.Create must NOT be called when Validate fails")
	}
	wantPrefix := "supplier create validate:"
	if got := err.Error(); len(got) < len(wantPrefix) || got[:len(wantPrefix)] != wantPrefix {
		t.Errorf("error = %q, want prefix %q", got, wantPrefix)
	}
}

func TestZZ_CreateUseCase_Execute_SetsTimestampsAndFields(t *testing.T) {
	repo := &zzFakeRepo{}
	uc := appsupp.NewCreateUseCase(repo)

	tenantID := uuid.New()
	in := domain.CreateInput{
		TenantID: tenantID,
		Code:     "SUP-001",
		Name:     "供应商甲",
		Contact:  "张三",
		Phone:    "13800000000",
		Email:    "a@b.com",
		Address:  "深圳",
		Remark:   "备注",
	}
	s, err := uc.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.TenantID != tenantID {
		t.Errorf("TenantID = %v, want %v", s.TenantID, tenantID)
	}
	if s.Code != in.Code || s.Contact != in.Contact || s.Phone != in.Phone ||
		s.Email != in.Email || s.Address != in.Address || s.Remark != in.Remark {
		t.Errorf("fields not copied correctly: %+v", s)
	}
	if s.CreatedAt.IsZero() || s.UpdatedAt.IsZero() {
		t.Error("expected CreatedAt/UpdatedAt to be set")
	}
	if !s.CreatedAt.Equal(s.UpdatedAt) {
		t.Errorf("expected CreatedAt == UpdatedAt on create, got %v vs %v", s.CreatedAt, s.UpdatedAt)
	}
	if s.ID == uuid.Nil {
		t.Error("expected non-nil generated ID")
	}
	// Confirm the exact struct passed to repo.Create matches the returned struct
	// (domain.Validate ran on this instance, and it is the one persisted).
	if repo.lastCreated != s {
		t.Error("expected repo.Create to receive the same *domain.Supplier pointer returned to caller")
	}
}

func TestZZ_CreateUseCase_Execute_DuplicateName_RemappedToBareSentinel(t *testing.T) {
	repo := &zzFakeRepo{createErr: appsupp.ErrDuplicateName}
	uc := appsupp.NewCreateUseCase(repo)

	_, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID: uuid.New(),
		Name:     "重复供应商",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, appsupp.ErrDuplicateName) {
		t.Errorf("errors.Is(err, ErrDuplicateName) = false, err = %v", err)
	}
	// Business invariant: must be the *bare* sentinel, not wrapped (so handler can 409 cleanly).
	if err != appsupp.ErrDuplicateName {
		t.Errorf("expected bare ErrDuplicateName sentinel, got wrapped: %v", err)
	}
}

func TestZZ_CreateUseCase_Execute_GenericRepoError_IsWrapped(t *testing.T) {
	repo := &zzFakeRepo{createErr: errGeneric}
	uc := appsupp.NewCreateUseCase(repo)

	_, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID: uuid.New(),
		Name:     "供应商乙",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if errors.Is(err, appsupp.ErrDuplicateName) {
		t.Error("generic error must not match ErrDuplicateName")
	}
	if !errors.Is(err, errGeneric) {
		t.Errorf("expected wrapped errGeneric, got %v", err)
	}
	wantPrefix := "supplier create:"
	if got := err.Error(); len(got) < len(wantPrefix) || got[:len(wantPrefix)] != wantPrefix {
		t.Errorf("error = %q, want prefix %q", got, wantPrefix)
	}
}

// ---------------------------------------------------------------------------
// UpdateUseCase
// ---------------------------------------------------------------------------

func TestZZ_UpdateUseCase_Execute_GetByID_NotFound_RemappedToBareSentinel(t *testing.T) {
	repo := &zzFakeRepo{getErr: appsupp.ErrNotFound}
	uc := appsupp.NewUpdateUseCase(repo)

	newName := "新名字"
	_, err := uc.Execute(context.Background(), uuid.New(), uuid.New(), domain.UpdateInput{Name: &newName})
	if !errors.Is(err, appsupp.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err != appsupp.ErrNotFound {
		t.Errorf("expected bare ErrNotFound sentinel, got wrapped: %v", err)
	}
}

func TestZZ_UpdateUseCase_Execute_GetByID_GenericError_IsWrapped(t *testing.T) {
	repo := &zzFakeRepo{getErr: errGeneric}
	uc := appsupp.NewUpdateUseCase(repo)

	newName := "新名字"
	_, err := uc.Execute(context.Background(), uuid.New(), uuid.New(), domain.UpdateInput{Name: &newName})
	if errors.Is(err, appsupp.ErrNotFound) {
		t.Error("generic get error must not match ErrNotFound")
	}
	if !errors.Is(err, errGeneric) {
		t.Errorf("expected wrapped errGeneric, got %v", err)
	}
	wantPrefix := "supplier update fetch:"
	if got := err.Error(); len(got) < len(wantPrefix) || got[:len(wantPrefix)] != wantPrefix {
		t.Errorf("error = %q, want prefix %q", got, wantPrefix)
	}
}

func TestZZ_UpdateUseCase_Execute_NilPointer_LeavesFieldUnchanged(t *testing.T) {
	existing := &domain.Supplier{
		ID:      uuid.New(),
		Name:    "原名字",
		Code:    "ORIG-CODE",
		Contact: "原联系人",
	}
	repo := &zzFakeRepo{getResult: existing}
	uc := appsupp.NewUpdateUseCase(repo)

	// Only patch Contact; Name/Code left nil -> must remain untouched.
	newContact := "新联系人"
	out, err := uc.Execute(context.Background(), uuid.New(), existing.ID, domain.UpdateInput{Contact: &newContact})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Name != "原名字" {
		t.Errorf("Name changed despite nil pointer: got %q", out.Name)
	}
	if out.Code != "ORIG-CODE" {
		t.Errorf("Code changed despite nil pointer: got %q", out.Code)
	}
	if out.Contact != newContact {
		t.Errorf("Contact = %q, want %q", out.Contact, newContact)
	}
}

func TestZZ_UpdateUseCase_Execute_EmptyStringPointer_ClearsField(t *testing.T) {
	existing := &domain.Supplier{
		ID:      uuid.New(),
		Name:    "有效名字",
		Contact: "会被清空的联系人",
	}
	repo := &zzFakeRepo{getResult: existing}
	uc := appsupp.NewUpdateUseCase(repo)

	// &"" (non-nil pointer to empty string) must CLEAR Contact, unlike nil which skips.
	empty := ""
	out, err := uc.Execute(context.Background(), uuid.New(), existing.ID, domain.UpdateInput{Contact: &empty})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Contact != "" {
		t.Errorf("expected Contact cleared to empty string, got %q", out.Contact)
	}
	// Name untouched since Name pointer was nil.
	if out.Name != "有效名字" {
		t.Errorf("Name changed unexpectedly: got %q", out.Name)
	}
}

func TestZZ_UpdateUseCase_Execute_BlankingName_RejectedByValidate(t *testing.T) {
	existing := &domain.Supplier{
		ID:   uuid.New(),
		Name: "会被清空的名字",
	}
	repo := &zzFakeRepo{getResult: existing}
	uc := appsupp.NewUpdateUseCase(repo)

	empty := ""
	_, err := uc.Execute(context.Background(), uuid.New(), existing.ID, domain.UpdateInput{Name: &empty})
	if err == nil {
		t.Fatal("expected validation error when Name is blanked, got nil")
	}
	wantPrefix := "supplier update validate:"
	if got := err.Error(); len(got) < len(wantPrefix) || got[:len(wantPrefix)] != wantPrefix {
		t.Errorf("error = %q, want prefix %q", got, wantPrefix)
	}
}

func TestZZ_UpdateUseCase_Execute_AllFieldsSet(t *testing.T) {
	existing := &domain.Supplier{ID: uuid.New(), Name: "旧名"}
	repo := &zzFakeRepo{getResult: existing}
	uc := appsupp.NewUpdateUseCase(repo)

	code, name, contact, phone, email, address, remark :=
		"C1", "新名", "联系人", "电话", "邮箱", "地址", "备注"
	out, err := uc.Execute(context.Background(), uuid.New(), existing.ID, domain.UpdateInput{
		Code: &code, Name: &name, Contact: &contact, Phone: &phone,
		Email: &email, Address: &address, Remark: &remark,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Code != code || out.Name != name || out.Contact != contact ||
		out.Phone != phone || out.Email != email || out.Address != address || out.Remark != remark {
		t.Errorf("not all fields applied: %+v", out)
	}
}

func TestZZ_UpdateUseCase_Execute_RepoUpdate_NotFound_RemappedToBareSentinel(t *testing.T) {
	existing := &domain.Supplier{ID: uuid.New(), Name: "名字"}
	repo := &zzFakeRepo{getResult: existing, updateErr: appsupp.ErrNotFound}
	uc := appsupp.NewUpdateUseCase(repo)

	newName := "换个名"
	_, err := uc.Execute(context.Background(), uuid.New(), existing.ID, domain.UpdateInput{Name: &newName})
	if !errors.Is(err, appsupp.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err != appsupp.ErrNotFound {
		t.Errorf("expected bare ErrNotFound sentinel, got wrapped: %v", err)
	}
}

func TestZZ_UpdateUseCase_Execute_RepoUpdate_GenericError_IsWrapped(t *testing.T) {
	existing := &domain.Supplier{ID: uuid.New(), Name: "名字"}
	repo := &zzFakeRepo{getResult: existing, updateErr: errGeneric}
	uc := appsupp.NewUpdateUseCase(repo)

	newName := "换个名"
	_, err := uc.Execute(context.Background(), uuid.New(), existing.ID, domain.UpdateInput{Name: &newName})
	if errors.Is(err, appsupp.ErrNotFound) {
		t.Error("generic update error must not match ErrNotFound")
	}
	if !errors.Is(err, errGeneric) {
		t.Errorf("expected wrapped errGeneric, got %v", err)
	}
	wantPrefix := "supplier update:"
	if got := err.Error(); len(got) < len(wantPrefix) || got[:len(wantPrefix)] != wantPrefix {
		t.Errorf("error = %q, want prefix %q", got, wantPrefix)
	}
}

// ---------------------------------------------------------------------------
// GetByIDUseCase
// ---------------------------------------------------------------------------

func TestZZ_GetByIDUseCase_Execute_Success(t *testing.T) {
	existing := &domain.Supplier{ID: uuid.New(), Name: "供应商"}
	repo := &zzFakeRepo{getResult: existing}
	uc := appsupp.NewGetByIDUseCase(repo)

	out, err := uc.Execute(context.Background(), uuid.New(), existing.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != existing {
		t.Error("expected returned supplier to be the repo result")
	}
}

func TestZZ_GetByIDUseCase_Execute_NotFound_RemappedToBareSentinel(t *testing.T) {
	repo := &zzFakeRepo{getErr: appsupp.ErrNotFound}
	uc := appsupp.NewGetByIDUseCase(repo)

	_, err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, appsupp.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err != appsupp.ErrNotFound {
		t.Errorf("expected bare ErrNotFound sentinel, got wrapped: %v", err)
	}
}

func TestZZ_GetByIDUseCase_Execute_GenericError_IsWrapped(t *testing.T) {
	repo := &zzFakeRepo{getErr: errGeneric}
	uc := appsupp.NewGetByIDUseCase(repo)

	_, err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if errors.Is(err, appsupp.ErrNotFound) {
		t.Error("generic error must not match ErrNotFound")
	}
	if !errors.Is(err, errGeneric) {
		t.Errorf("expected wrapped errGeneric, got %v", err)
	}
	wantPrefix := "supplier get:"
	if got := err.Error(); len(got) < len(wantPrefix) || got[:len(wantPrefix)] != wantPrefix {
		t.Errorf("error = %q, want prefix %q", got, wantPrefix)
	}
}

// ---------------------------------------------------------------------------
// DeleteUseCase
// ---------------------------------------------------------------------------

func TestZZ_DeleteUseCase_Execute_Success(t *testing.T) {
	repo := &zzFakeRepo{}
	uc := appsupp.NewDeleteUseCase(repo)

	if err := uc.Execute(context.Background(), uuid.New(), uuid.New()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestZZ_DeleteUseCase_Execute_NotFound_RemappedToBareSentinel(t *testing.T) {
	repo := &zzFakeRepo{deleteErr: appsupp.ErrNotFound}
	uc := appsupp.NewDeleteUseCase(repo)

	err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, appsupp.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err != appsupp.ErrNotFound {
		t.Errorf("expected bare ErrNotFound sentinel, got wrapped: %v", err)
	}
}

func TestZZ_DeleteUseCase_Execute_GenericError_IsWrapped(t *testing.T) {
	repo := &zzFakeRepo{deleteErr: errGeneric}
	uc := appsupp.NewDeleteUseCase(repo)

	err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if errors.Is(err, appsupp.ErrNotFound) {
		t.Error("generic error must not match ErrNotFound")
	}
	if !errors.Is(err, errGeneric) {
		t.Errorf("expected wrapped errGeneric, got %v", err)
	}
	wantPrefix := "supplier delete:"
	if got := err.Error(); len(got) < len(wantPrefix) || got[:len(wantPrefix)] != wantPrefix {
		t.Errorf("error = %q, want prefix %q", got, wantPrefix)
	}
}

// ---------------------------------------------------------------------------
// RestoreUseCase
// ---------------------------------------------------------------------------

func TestZZ_RestoreUseCase_Execute_Success(t *testing.T) {
	restored := &domain.Supplier{ID: uuid.New(), Name: "恢复的供应商"}
	repo := &zzFakeRepo{restoreResult: restored}
	uc := appsupp.NewRestoreUseCase(repo)

	out, err := uc.Execute(context.Background(), uuid.New(), restored.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != restored {
		t.Error("expected returned supplier to be the repo result")
	}
}

func TestZZ_RestoreUseCase_Execute_NotSoftDeleted_YieldsNotFound(t *testing.T) {
	// Restoring a row that was never soft-deleted (or absent) -> repo reports ErrNotFound.
	repo := &zzFakeRepo{restoreErr: appsupp.ErrNotFound}
	uc := appsupp.NewRestoreUseCase(repo)

	_, err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, appsupp.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err != appsupp.ErrNotFound {
		t.Errorf("expected bare ErrNotFound sentinel, got wrapped: %v", err)
	}
}

func TestZZ_RestoreUseCase_Execute_GenericError_IsWrapped(t *testing.T) {
	repo := &zzFakeRepo{restoreErr: errGeneric}
	uc := appsupp.NewRestoreUseCase(repo)

	_, err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if errors.Is(err, appsupp.ErrNotFound) {
		t.Error("generic error must not match ErrNotFound")
	}
	if !errors.Is(err, errGeneric) {
		t.Errorf("expected wrapped errGeneric, got %v", err)
	}
	wantPrefix := "supplier restore:"
	if got := err.Error(); len(got) < len(wantPrefix) || got[:len(wantPrefix)] != wantPrefix {
		t.Errorf("error = %q, want prefix %q", got, wantPrefix)
	}
}

// ---------------------------------------------------------------------------
// ListUseCase — Limit clamping boundaries
// ---------------------------------------------------------------------------

func TestZZ_ListUseCase_Execute_LimitClamping(t *testing.T) {
	cases := []struct {
		name      string
		inLimit   int
		wantLimit int
	}{
		{"zero_defaults_to_20", 0, 20},
		{"negative_defaults_to_20", -1, 20},
		{"exactly_20_passes_through", 20, 20},
		{"exactly_200_passes_through", 200, 200},
		{"201_clamped_to_200", 201, 200},
		{"large_value_clamped_to_200", 10_000, 200},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &zzFakeRepo{listResult: []*domain.Supplier{}, listTotal: 0}
			uc := appsupp.NewListUseCase(repo)

			_, _, err := uc.Execute(context.Background(), domain.ListFilter{
				TenantID: uuid.New(),
				Limit:    tc.inLimit,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if repo.lastListFilt.Limit != tc.wantLimit {
				t.Errorf("Limit passed to repo = %d, want %d", repo.lastListFilt.Limit, tc.wantLimit)
			}
		})
	}
}

func TestZZ_ListUseCase_Execute_ReturnsItemsAndTotal(t *testing.T) {
	want := []*domain.Supplier{
		{ID: uuid.New(), Name: "供应商A"},
		{ID: uuid.New(), Name: "供应商B"},
	}
	repo := &zzFakeRepo{listResult: want, listTotal: 42}
	uc := appsupp.NewListUseCase(repo)

	items, total, err := uc.Execute(context.Background(), domain.ListFilter{TenantID: uuid.New(), Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 42 {
		t.Errorf("total = %d, want 42", total)
	}
	if len(items) != 2 || items[0] != want[0] || items[1] != want[1] {
		t.Errorf("items = %+v, want %+v", items, want)
	}
}

func TestZZ_ListUseCase_Execute_RepoError_IsWrapped(t *testing.T) {
	repo := &zzFakeRepo{listErr: errGeneric}
	uc := appsupp.NewListUseCase(repo)

	_, _, err := uc.Execute(context.Background(), domain.ListFilter{TenantID: uuid.New(), Limit: 10})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, errGeneric) {
		t.Errorf("expected wrapped errGeneric, got %v", err)
	}
	wantPrefix := "supplier list:"
	if got := err.Error(); len(got) < len(wantPrefix) || got[:len(wantPrefix)] != wantPrefix {
		t.Errorf("error = %q, want prefix %q", got, wantPrefix)
	}
}

// ---------------------------------------------------------------------------
// Preserved-field passthrough for ListFilter (Query/TenantID/Offset untouched
// by clamping logic).
// ---------------------------------------------------------------------------

func TestZZ_ListUseCase_Execute_PreservesOtherFilterFields(t *testing.T) {
	repo := &zzFakeRepo{listResult: []*domain.Supplier{}, listTotal: 0}
	uc := appsupp.NewListUseCase(repo)

	tenantID := uuid.New()
	_, _, err := uc.Execute(context.Background(), domain.ListFilter{
		TenantID: tenantID,
		Query:    "关键字",
		Limit:    5,
		Offset:   30,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.lastListFilt.TenantID != tenantID {
		t.Errorf("TenantID = %v, want %v", repo.lastListFilt.TenantID, tenantID)
	}
	if repo.lastListFilt.Query != "关键字" {
		t.Errorf("Query = %q, want 关键字", repo.lastListFilt.Query)
	}
	if repo.lastListFilt.Offset != 30 {
		t.Errorf("Offset = %d, want 30", repo.lastListFilt.Offset)
	}
	if repo.lastListFilt.Limit != 5 {
		t.Errorf("Limit = %d, want 5 (within bounds, unclamped)", repo.lastListFilt.Limit)
	}
}
