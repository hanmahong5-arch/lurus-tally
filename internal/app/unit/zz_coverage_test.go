package unit

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/unit"
)

// fakeRepo implements Repository for exercising the use cases without a real DB.
// It captures the last *domain.UnitDef passed to Create/Delete calls and returns
// canned results/errors configured per test case.
type fakeRepo struct {
	mu sync.Mutex

	createErr    error
	lastCreated  *domain.UnitDef
	createCalled bool

	listResult []*domain.UnitDef
	listErr    error
	listCalled bool

	getByIDResult *domain.UnitDef
	getByIDErr    error
	getByIDCalled bool

	deleteErr    error
	deleteCalled bool
}

func (f *fakeRepo) Create(_ context.Context, u *domain.UnitDef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalled = true
	f.lastCreated = u
	return f.createErr
}

func (f *fakeRepo) List(_ context.Context, _ domain.ListFilter) ([]*domain.UnitDef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalled = true
	return f.listResult, f.listErr
}

func (f *fakeRepo) GetByID(_ context.Context, _, _ uuid.UUID) (*domain.UnitDef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getByIDCalled = true
	return f.getByIDResult, f.getByIDErr
}

func (f *fakeRepo) Delete(_ context.Context, _, _ uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteCalled = true
	return f.deleteErr
}

var errRepoBoom = errors.New("repo boom")

// --- CreateUseCase ---

func TestCreateUseCase_Execute_ValidationRejectsBeforeRepoCall(t *testing.T) {
	validTenant := uuid.New()

	tests := []struct {
		name    string
		in      domain.CreateInput
		wantErr string
	}{
		{
			name:    "nil tenant id",
			in:      domain.CreateInput{TenantID: uuid.Nil, Code: "PCS", Name: "Piece", UnitType: domain.UnitTypeCount},
			wantErr: "tenant_id is required",
		},
		{
			name:    "empty code",
			in:      domain.CreateInput{TenantID: validTenant, Code: "", Name: "Piece", UnitType: domain.UnitTypeCount},
			wantErr: "code is required",
		},
		{
			name:    "empty name",
			in:      domain.CreateInput{TenantID: validTenant, Code: "PCS", Name: "", UnitType: domain.UnitTypeCount},
			wantErr: "name is required",
		},
		{
			name:    "invalid unit_type",
			in:      domain.CreateInput{TenantID: validTenant, Code: "PCS", Name: "Piece", UnitType: domain.UnitType("bogus")},
			wantErr: `invalid unit_type "bogus"`,
		},
		{
			name:    "empty unit_type also invalid",
			in:      domain.CreateInput{TenantID: validTenant, Code: "PCS", Name: "Piece", UnitType: domain.UnitType("")},
			wantErr: `invalid unit_type ""`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{}
			uc := NewCreateUseCase(repo)

			got, err := uc.Execute(context.Background(), tc.in)

			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tc.wantErr)
			}
			if got != nil {
				t.Fatalf("expected nil result on validation failure, got %+v", got)
			}
			if repo.createCalled {
				t.Fatal("repo.Create must NOT be called when validation fails")
			}
		})
	}
}

func TestCreateUseCase_Execute_AllValidUnitTypesPass(t *testing.T) {
	validTypes := []domain.UnitType{
		domain.UnitTypeCount,
		domain.UnitTypeWeight,
		domain.UnitTypeLength,
		domain.UnitTypeVolume,
		domain.UnitTypeArea,
		domain.UnitTypeTime,
	}
	tenantID := uuid.New()

	for _, ut := range validTypes {
		t.Run(string(ut), func(t *testing.T) {
			repo := &fakeRepo{}
			uc := NewCreateUseCase(repo)

			got, err := uc.Execute(context.Background(), domain.CreateInput{
				TenantID: tenantID,
				Code:     "X",
				Name:     "X unit",
				UnitType: ut,
			})

			if err != nil {
				t.Fatalf("unexpected error for valid unit_type %q: %v", ut, err)
			}
			if !repo.createCalled {
				t.Fatal("repo.Create should have been called for valid input")
			}
			if got == nil {
				t.Fatal("expected non-nil result")
			}
			if got.UnitType != ut {
				t.Fatalf("UnitType = %q, want %q", got.UnitType, ut)
			}
		})
	}
}

func TestCreateUseCase_Execute_ForcesIsSystemFalseAndAssignsUUID(t *testing.T) {
	repo := &fakeRepo{}
	uc := NewCreateUseCase(repo)
	tenantID := uuid.New()

	got, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID: tenantID,
		Code:     "KG",
		Name:     "Kilogram",
		UnitType: domain.UnitTypeWeight,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Business invariant: caller can never make a unit system-owned via Create.
	if repo.lastCreated == nil {
		t.Fatal("repo.Create was not invoked with a unit")
	}
	if repo.lastCreated.IsSystem != false {
		t.Fatalf("IsSystem = %v, want false (tenant-custom units only)", repo.lastCreated.IsSystem)
	}
	if repo.lastCreated.ID == uuid.Nil {
		t.Fatal("expected a non-nil generated UUID for the persisted unit")
	}
	if repo.lastCreated.TenantID != tenantID {
		t.Fatalf("TenantID = %v, want %v", repo.lastCreated.TenantID, tenantID)
	}
	if got.ID != repo.lastCreated.ID {
		t.Fatalf("returned unit ID %v does not match persisted ID %v", got.ID, repo.lastCreated.ID)
	}
}

func TestCreateUseCase_Execute_RepoErrorIsWrapped(t *testing.T) {
	repo := &fakeRepo{createErr: errRepoBoom}
	uc := NewCreateUseCase(repo)

	_, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID: uuid.New(),
		Code:     "PCS",
		Name:     "Piece",
		UnitType: domain.UnitTypeCount,
	})

	if err == nil {
		t.Fatal("expected error from repo failure, got nil")
	}
	if !strings.HasPrefix(err.Error(), "create unit:") {
		t.Fatalf("error = %q, want prefix %q", err.Error(), "create unit:")
	}
	if !errors.Is(err, errRepoBoom) {
		t.Fatalf("error = %v, want wrapped errRepoBoom (errors.Is false)", err)
	}
}

// --- ListUseCase ---

func TestListUseCase_Execute_NilTenantRejected(t *testing.T) {
	repo := &fakeRepo{}
	uc := NewListUseCase(repo)

	got, err := uc.Execute(context.Background(), domain.ListFilter{TenantID: uuid.Nil})

	if err == nil {
		t.Fatal("expected error for nil tenant id")
	}
	if !strings.Contains(err.Error(), "tenant_id is required") {
		t.Fatalf("error = %q, want substring %q", err.Error(), "tenant_id is required")
	}
	if got != nil {
		t.Fatalf("expected nil result, got %+v", got)
	}
	if repo.listCalled {
		t.Fatal("repo.List must NOT be called when tenant_id is nil")
	}
}

func TestListUseCase_Execute_RepoErrorIsWrapped(t *testing.T) {
	repo := &fakeRepo{listErr: errRepoBoom}
	uc := NewListUseCase(repo)

	_, err := uc.Execute(context.Background(), domain.ListFilter{TenantID: uuid.New()})

	if err == nil {
		t.Fatal("expected wrapped error, got nil")
	}
	if !strings.HasPrefix(err.Error(), "list units:") {
		t.Fatalf("error = %q, want prefix %q", err.Error(), "list units:")
	}
	if !errors.Is(err, errRepoBoom) {
		t.Fatalf("error = %v, want wrapped errRepoBoom (errors.Is false)", err)
	}
}

func TestListUseCase_Execute_EmptySliceReturnedAsIs(t *testing.T) {
	tests := []struct {
		name string
		repo *fakeRepo
	}{
		{name: "nil slice passthrough", repo: &fakeRepo{listResult: nil}},
		{name: "empty non-nil slice passthrough", repo: &fakeRepo{listResult: []*domain.UnitDef{}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			uc := NewListUseCase(tc.repo)
			got, err := uc.Execute(context.Background(), domain.ListFilter{TenantID: uuid.New()})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != 0 {
				t.Fatalf("expected zero-length result, got %d items", len(got))
			}
			if tc.repo.listResult == nil && got != nil {
				t.Fatalf("expected nil slice preserved as-is, got non-nil %+v", got)
			}
		})
	}
}

func TestListUseCase_Execute_ReturnsRepoResultUnmodified(t *testing.T) {
	tenantID := uuid.New()
	want := []*domain.UnitDef{
		{ID: uuid.New(), TenantID: tenantID, Code: "PCS", Name: "Piece", UnitType: domain.UnitTypeCount, IsSystem: true},
		{ID: uuid.New(), TenantID: tenantID, Code: "KG", Name: "Kilogram", UnitType: domain.UnitTypeWeight, IsSystem: false},
	}
	repo := &fakeRepo{listResult: want}
	uc := NewListUseCase(repo)

	got, err := uc.Execute(context.Background(), domain.ListFilter{TenantID: tenantID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("item %d pointer mismatch: got %+v want %+v", i, got[i], want[i])
		}
	}
}

// --- DeleteUseCase ---

func TestDeleteUseCase_Execute_NilTenantShortCircuits(t *testing.T) {
	repo := &fakeRepo{}
	uc := NewDeleteUseCase(repo)

	err := uc.Execute(context.Background(), uuid.Nil, uuid.New())

	if err == nil {
		t.Fatal("expected error for nil tenant id")
	}
	if !strings.Contains(err.Error(), "tenant_id is required") {
		t.Fatalf("error = %q, want substring %q", err.Error(), "tenant_id is required")
	}
	if repo.getByIDCalled {
		t.Fatal("repo.GetByID must NOT be called when tenant_id is nil")
	}
	if repo.deleteCalled {
		t.Fatal("repo.Delete must NOT be called when tenant_id is nil")
	}
}

func TestDeleteUseCase_Execute_GetByIDNotFound(t *testing.T) {
	repo := &fakeRepo{getByIDResult: nil, getByIDErr: nil}
	uc := NewDeleteUseCase(repo)

	err := uc.Execute(context.Background(), uuid.New(), uuid.New())

	if err == nil {
		t.Fatal("expected not-found error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error = %q, want substring %q", err.Error(), "not found")
	}
	if repo.deleteCalled {
		t.Fatal("repo.Delete must NOT be called when unit does not exist")
	}
}

func TestDeleteUseCase_Execute_GetByIDErrorIsWrapped(t *testing.T) {
	repo := &fakeRepo{getByIDErr: errRepoBoom}
	uc := NewDeleteUseCase(repo)

	err := uc.Execute(context.Background(), uuid.New(), uuid.New())

	if err == nil {
		t.Fatal("expected wrapped error, got nil")
	}
	if !strings.HasPrefix(err.Error(), "delete unit:") {
		t.Fatalf("error = %q, want prefix %q", err.Error(), "delete unit:")
	}
	if !errors.Is(err, errRepoBoom) {
		t.Fatalf("error = %v, want wrapped errRepoBoom (errors.Is false)", err)
	}
	if repo.deleteCalled {
		t.Fatal("repo.Delete must NOT be called when GetByID fails")
	}
}

func TestDeleteUseCase_Execute_SystemUnitRefused(t *testing.T) {
	repo := &fakeRepo{
		getByIDResult: &domain.UnitDef{
			ID:       uuid.New(),
			TenantID: uuid.Nil, // system units are shared, tenant_id = nil per domain doc
			Code:     "PCS",
			IsSystem: true,
		},
	}
	uc := NewDeleteUseCase(repo)

	err := uc.Execute(context.Background(), uuid.New(), uuid.New())

	if err == nil {
		t.Fatal("expected refusal error for system unit")
	}
	if !strings.Contains(err.Error(), "system unit") || !strings.Contains(err.Error(), "cannot be deleted") {
		t.Fatalf("error = %q, want substring about system unit refusal", err.Error())
	}
	if !strings.Contains(err.Error(), `"PCS"`) {
		t.Fatalf("error = %q, want the unit code %q quoted in the message", err.Error(), "PCS")
	}
	if repo.deleteCalled {
		t.Fatal("repo.Delete must NEVER be called for a system unit (IsSystem=true)")
	}
}

func TestDeleteUseCase_Execute_TenantCustomHappyPath(t *testing.T) {
	repo := &fakeRepo{
		getByIDResult: &domain.UnitDef{
			ID:       uuid.New(),
			TenantID: uuid.New(),
			Code:     "CUSTOM",
			IsSystem: false,
		},
	}
	uc := NewDeleteUseCase(repo)

	err := uc.Execute(context.Background(), uuid.New(), uuid.New())

	if err != nil {
		t.Fatalf("unexpected error for tenant-custom unit delete: %v", err)
	}
	if !repo.deleteCalled {
		t.Fatal("repo.Delete should have been called exactly once for a tenant-custom unit")
	}
}

func TestDeleteUseCase_Execute_RepoDeleteErrorIsWrapped(t *testing.T) {
	repo := &fakeRepo{
		getByIDResult: &domain.UnitDef{
			ID:       uuid.New(),
			TenantID: uuid.New(),
			Code:     "CUSTOM",
			IsSystem: false,
		},
		deleteErr: errRepoBoom,
	}
	uc := NewDeleteUseCase(repo)

	err := uc.Execute(context.Background(), uuid.New(), uuid.New())

	if err == nil {
		t.Fatal("expected wrapped error from repo.Delete failure")
	}
	if !strings.HasPrefix(err.Error(), "delete unit:") {
		t.Fatalf("error = %q, want prefix %q", err.Error(), "delete unit:")
	}
	if !errors.Is(err, errRepoBoom) {
		t.Fatalf("error = %v, want wrapped errRepoBoom (errors.Is false)", err)
	}
	if !repo.deleteCalled {
		t.Fatal("repo.Delete should have been invoked (and then errored)")
	}
}
