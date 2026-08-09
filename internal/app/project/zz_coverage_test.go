package project_test

// Additional coverage tests for internal/app/project, exercising code paths not
// covered by project_usecases_test.go: get/list/delete/restore happy+error paths,
// create validate/generic-error branches, update field-merge/error branches, and
// cross-tenant isolation at the use-case boundary.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	appproject "github.com/hanmahong5-arch/lurus-tally/internal/app/project"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/project"
)

// errBackend is a stand-in for an opaque infrastructure error (e.g. a DB failure)
// that is neither ErrNotFound nor ErrDuplicateCode.
var errBackend = errors.New("backend unavailable")

// memRepo is an in-memory fake implementing appproject.Repository, enforcing
// tenant scoping and per-tenant code uniqueness, keyed by project ID.
type memRepo struct {
	mu    sync.Mutex
	items map[uuid.UUID]*domain.Project
	codes map[string]uuid.UUID // "tenantID|code" -> project ID

	forceCreateErr  error
	forceGetErr     error
	forceListErr    error
	forceUpdateErr  error
	forceDeleteErr  error
	forceRestoreErr error
}

func newMemRepo() *memRepo {
	return &memRepo{
		items: make(map[uuid.UUID]*domain.Project),
		codes: make(map[string]uuid.UUID),
	}
}

func codeKey(tenantID uuid.UUID, code string) string {
	return tenantID.String() + "|" + code
}

func (r *memRepo) Create(_ context.Context, p *domain.Project) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.forceCreateErr != nil {
		return r.forceCreateErr
	}
	key := codeKey(p.TenantID, p.Code)
	if _, exists := r.codes[key]; exists {
		return appproject.ErrDuplicateCode
	}
	clone := *p
	r.items[p.ID] = &clone
	r.codes[key] = p.ID
	return nil
}

func (r *memRepo) GetByID(_ context.Context, tenantID, id uuid.UUID) (*domain.Project, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.forceGetErr != nil {
		return nil, r.forceGetErr
	}
	p, ok := r.items[id]
	if !ok || p.TenantID != tenantID || p.DeletedAt != nil {
		return nil, appproject.ErrNotFound
	}
	clone := *p
	return &clone, nil
}

func (r *memRepo) List(_ context.Context, f domain.ListFilter) ([]*domain.Project, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.forceListErr != nil {
		return nil, 0, r.forceListErr
	}
	var result []*domain.Project
	for _, p := range r.items {
		if p.TenantID == f.TenantID && p.DeletedAt == nil {
			clone := *p
			result = append(result, &clone)
		}
	}
	return result, len(result), nil
}

func (r *memRepo) Update(_ context.Context, p *domain.Project) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.forceUpdateErr != nil {
		return r.forceUpdateErr
	}
	existing, ok := r.items[p.ID]
	if !ok || existing.TenantID != p.TenantID {
		return appproject.ErrNotFound
	}
	clone := *p
	r.items[p.ID] = &clone
	return nil
}

func (r *memRepo) Delete(_ context.Context, tenantID, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.forceDeleteErr != nil {
		return r.forceDeleteErr
	}
	p, ok := r.items[id]
	if !ok || p.TenantID != tenantID || p.DeletedAt != nil {
		return appproject.ErrNotFound
	}
	now := time.Now().UTC()
	p.DeletedAt = &now
	return nil
}

func (r *memRepo) Restore(_ context.Context, tenantID, id uuid.UUID) (*domain.Project, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.forceRestoreErr != nil {
		return nil, r.forceRestoreErr
	}
	p, ok := r.items[id]
	if !ok || p.TenantID != tenantID || p.DeletedAt == nil {
		return nil, appproject.ErrNotFound
	}
	p.DeletedAt = nil
	clone := *p
	return &clone, nil
}

// seedProject directly inserts a project into the repo, bypassing the use case,
// so tests can set up pre-existing state (e.g. for Get/Update/Delete/Restore).
func seedProject(t *testing.T, r *memRepo, tenantID uuid.UUID, code string, deleted bool) *domain.Project {
	t.Helper()
	now := time.Now().UTC()
	p := &domain.Project{
		ID:        uuid.New(),
		TenantID:  tenantID,
		Code:      code,
		Name:      "seed-" + code,
		Status:    domain.StatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if deleted {
		d := now
		p.DeletedAt = &d
	}
	if err := r.Create(context.Background(), p); err != nil {
		t.Fatalf("seedProject: unexpected error: %v", err)
	}
	if deleted {
		r.mu.Lock()
		r.items[p.ID].DeletedAt = p.DeletedAt
		r.mu.Unlock()
	}
	return p
}

// ---------- CreateUseCase ----------

func TestZZCreateUseCase_ValidateFailure_RepoNotCalled(t *testing.T) {
	repo := newMemRepo()
	uc := appproject.NewCreateUseCase(repo)

	// Name is required per domain.Project.Validate; leaving it empty must fail
	// validation before the repo is ever touched.
	_, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID: uuid.New(),
		Code:     "P100",
		Name:     "",
	})
	if err == nil {
		t.Fatal("expected validate error, got nil")
	}
	if !strings.Contains(err.Error(), "project create validate") {
		t.Errorf("expected wrapped 'project create validate' error, got %v", err)
	}
	if len(repo.items) != 0 {
		t.Errorf("expected repo.Create to never be called, but %d items were persisted", len(repo.items))
	}
}

func TestZZCreateUseCase_DuplicateCode_ErrorsIs(t *testing.T) {
	repo := newMemRepo()
	uc := appproject.NewCreateUseCase(repo)
	tenantID := uuid.New()

	seedProject(t, repo, tenantID, "DUP1", false)

	_, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID: tenantID,
		Code:     "DUP1",
		Name:     "another project",
	})
	if err == nil {
		t.Fatal("expected ErrDuplicateCode, got nil")
	}
	if !errors.Is(err, appproject.ErrDuplicateCode) {
		t.Errorf("expected errors.Is(err, ErrDuplicateCode) to be true, got %v", err)
	}
	// create.go returns ErrDuplicateCode unwrapped (not fmt.Errorf-wrapped).
	if err != appproject.ErrDuplicateCode {
		t.Errorf("expected exact sentinel ErrDuplicateCode, got %v", err)
	}
}

func TestZZCreateUseCase_GenericRepoError_Wrapped(t *testing.T) {
	repo := newMemRepo()
	repo.forceCreateErr = errBackend
	uc := appproject.NewCreateUseCase(repo)

	_, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID: uuid.New(),
		Code:     "P200",
		Name:     "some project",
	})
	if err == nil {
		t.Fatal("expected wrapped backend error, got nil")
	}
	if !errors.Is(err, errBackend) {
		t.Errorf("expected errors.Is(err, errBackend) true, got %v", err)
	}
	if !strings.Contains(err.Error(), "project create:") {
		t.Errorf("expected 'project create:' prefix, got %v", err)
	}
}

func TestZZCreateUseCase_CreatedAtEqualsUpdatedAt(t *testing.T) {
	repo := newMemRepo()
	uc := appproject.NewCreateUseCase(repo)

	before := time.Now().UTC()
	p, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID: uuid.New(),
		Code:     "P300",
		Name:     "timestamps project",
	})
	after := time.Now().UTC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !p.CreatedAt.Equal(p.UpdatedAt) {
		t.Errorf("expected CreatedAt == UpdatedAt on create, got CreatedAt=%v UpdatedAt=%v", p.CreatedAt, p.UpdatedAt)
	}
	if p.CreatedAt.Before(before) || p.CreatedAt.After(after) {
		t.Errorf("expected CreatedAt within [%v, %v], got %v", before, after, p.CreatedAt)
	}
}

func TestZZCreateUseCase_DefaultsStatus_TableDriven(t *testing.T) {
	cases := []struct {
		name       string
		inStatus   domain.ProjectStatus
		wantStatus domain.ProjectStatus
	}{
		{"empty status defaults to active", "", domain.StatusActive},
		{"explicit status preserved", domain.StatusPaused, domain.StatusPaused},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newMemRepo()
			uc := appproject.NewCreateUseCase(repo)
			p, err := uc.Execute(context.Background(), domain.CreateInput{
				TenantID: uuid.New(),
				Code:     "PX-" + tc.name,
				Name:     "n",
				Status:   tc.inStatus,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.Status != tc.wantStatus {
				t.Errorf("status = %q, want %q", p.Status, tc.wantStatus)
			}
		})
	}
}

// ---------- GetByIDUseCase ----------

func TestZZGetByIDUseCase_Execute(t *testing.T) {
	repo := newMemRepo()
	tenantA := uuid.New()
	tenantB := uuid.New()
	seeded := seedProject(t, repo, tenantA, "G001", false)
	uc := appproject.NewGetByIDUseCase(repo)

	t.Run("happy path", func(t *testing.T) {
		p, err := uc.Execute(context.Background(), tenantA, seeded.ID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.ID != seeded.ID {
			t.Errorf("got ID %v, want %v", p.ID, seeded.ID)
		}
	})

	t.Run("not found for missing id", func(t *testing.T) {
		_, err := uc.Execute(context.Background(), tenantA, uuid.New())
		if !errors.Is(err, appproject.ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("cross-tenant isolation", func(t *testing.T) {
		// Project created under tenantA must be invisible to tenantB.
		_, err := uc.Execute(context.Background(), tenantB, seeded.ID)
		if !errors.Is(err, appproject.ErrNotFound) {
			t.Errorf("expected ErrNotFound for cross-tenant access, got %v", err)
		}
	})

	t.Run("generic repo error wrapped", func(t *testing.T) {
		errRepo := newMemRepo()
		errRepo.forceGetErr = errBackend
		errUC := appproject.NewGetByIDUseCase(errRepo)
		_, err := errUC.Execute(context.Background(), tenantA, uuid.New())
		if !errors.Is(err, errBackend) {
			t.Errorf("expected errors.Is(err, errBackend), got %v", err)
		}
		if !strings.Contains(err.Error(), "project get:") {
			t.Errorf("expected 'project get:' prefix, got %v", err)
		}
	})
}

// ---------- ListUseCase ----------

// ListUseCase.Execute forwards the (possibly clamped) filter to repo.List; to
// assert the exact clamped value we use a capturing repo wrapper.
type capturingListRepo struct {
	*memRepo
	captured domain.ListFilter
}

func (c *capturingListRepo) List(ctx context.Context, f domain.ListFilter) ([]*domain.Project, int, error) {
	c.captured = f
	return c.memRepo.List(ctx, f)
}

func TestZZListUseCase_Execute_CapturedLimit(t *testing.T) {
	cases := []struct {
		name      string
		inLimit   int
		wantLimit int
	}{
		{"zero defaults to 20", 0, 20},
		{"negative defaults to 20", -5, 20},
		{"within range unchanged", 50, 50},
		{"above max clamped to 200", 500, 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &capturingListRepo{memRepo: newMemRepo()}
			uc := appproject.NewListUseCase(repo)
			_, _, err := uc.Execute(context.Background(), domain.ListFilter{
				TenantID: uuid.New(),
				Limit:    tc.inLimit,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if repo.captured.Limit != tc.wantLimit {
				t.Errorf("captured Limit = %d, want %d", repo.captured.Limit, tc.wantLimit)
			}
		})
	}
}

func TestZZListUseCase_Execute_RepoErrorWrapped(t *testing.T) {
	repo := newMemRepo()
	repo.forceListErr = errBackend
	uc := appproject.NewListUseCase(repo)

	items, total, err := uc.Execute(context.Background(), domain.ListFilter{TenantID: uuid.New()})
	if items != nil || total != 0 {
		t.Errorf("expected nil items and 0 total on error, got items=%v total=%d", items, total)
	}
	if !errors.Is(err, errBackend) {
		t.Errorf("expected errors.Is(err, errBackend), got %v", err)
	}
	if !strings.Contains(err.Error(), "project list:") {
		t.Errorf("expected 'project list:' prefix, got %v", err)
	}
}

func TestZZListUseCase_Execute_ReturnsSeededItems(t *testing.T) {
	repo := newMemRepo()
	tenantID := uuid.New()
	seedProject(t, repo, tenantID, "L001", false)
	seedProject(t, repo, tenantID, "L002", false)
	seedProject(t, repo, uuid.New(), "L003", false) // other tenant, should not appear

	uc := appproject.NewListUseCase(repo)
	items, total, err := uc.Execute(context.Background(), domain.ListFilter{TenantID: tenantID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Errorf("expected 2 items for tenant, got total=%d len=%d", total, len(items))
	}
}

// ---------- DeleteUseCase ----------

func TestZZDeleteUseCase_Execute(t *testing.T) {
	t.Run("happy path then subsequent get is not found", func(t *testing.T) {
		repo := newMemRepo()
		tenantID := uuid.New()
		p := seedProject(t, repo, tenantID, "D001", false)
		uc := appproject.NewDeleteUseCase(repo)

		if err := uc.Execute(context.Background(), tenantID, p.ID); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		getUC := appproject.NewGetByIDUseCase(repo)
		if _, err := getUC.Execute(context.Background(), tenantID, p.ID); !errors.Is(err, appproject.ErrNotFound) {
			t.Errorf("expected ErrNotFound after delete, got %v", err)
		}
	})

	t.Run("not found for missing id", func(t *testing.T) {
		repo := newMemRepo()
		uc := appproject.NewDeleteUseCase(repo)
		err := uc.Execute(context.Background(), uuid.New(), uuid.New())
		if !errors.Is(err, appproject.ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("cross-tenant delete not found", func(t *testing.T) {
		repo := newMemRepo()
		tenantA := uuid.New()
		tenantB := uuid.New()
		p := seedProject(t, repo, tenantA, "D002", false)
		uc := appproject.NewDeleteUseCase(repo)
		if err := uc.Execute(context.Background(), tenantB, p.ID); !errors.Is(err, appproject.ErrNotFound) {
			t.Errorf("expected ErrNotFound for cross-tenant delete, got %v", err)
		}
	})

	t.Run("generic repo error wrapped", func(t *testing.T) {
		repo := newMemRepo()
		repo.forceDeleteErr = errBackend
		uc := appproject.NewDeleteUseCase(repo)
		err := uc.Execute(context.Background(), uuid.New(), uuid.New())
		if !errors.Is(err, errBackend) {
			t.Errorf("expected errors.Is(err, errBackend), got %v", err)
		}
		if !strings.Contains(err.Error(), "project delete:") {
			t.Errorf("expected 'project delete:' prefix, got %v", err)
		}
	})
}

// ---------- RestoreUseCase ----------

func TestZZRestoreUseCase_Execute(t *testing.T) {
	t.Run("happy path returns restored non-nil project", func(t *testing.T) {
		repo := newMemRepo()
		tenantID := uuid.New()
		p := seedProject(t, repo, tenantID, "R001", true) // seeded already soft-deleted
		uc := appproject.NewRestoreUseCase(repo)

		got, err := uc.Execute(context.Background(), tenantID, p.ID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got == nil {
			t.Fatal("expected non-nil restored project")
		}
		if got.DeletedAt != nil {
			t.Errorf("expected DeletedAt nil after restore, got %v", got.DeletedAt)
		}
		if got.ID != p.ID {
			t.Errorf("got ID %v, want %v", got.ID, p.ID)
		}
	})

	t.Run("not found when not soft-deleted", func(t *testing.T) {
		repo := newMemRepo()
		tenantID := uuid.New()
		p := seedProject(t, repo, tenantID, "R002", false) // not deleted
		uc := appproject.NewRestoreUseCase(repo)
		_, err := uc.Execute(context.Background(), tenantID, p.ID)
		if !errors.Is(err, appproject.ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("not found for missing id", func(t *testing.T) {
		repo := newMemRepo()
		uc := appproject.NewRestoreUseCase(repo)
		_, err := uc.Execute(context.Background(), uuid.New(), uuid.New())
		if !errors.Is(err, appproject.ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("generic repo error wrapped", func(t *testing.T) {
		repo := newMemRepo()
		repo.forceRestoreErr = errBackend
		uc := appproject.NewRestoreUseCase(repo)
		_, err := uc.Execute(context.Background(), uuid.New(), uuid.New())
		if !errors.Is(err, errBackend) {
			t.Errorf("expected errors.Is(err, errBackend), got %v", err)
		}
		if !strings.Contains(err.Error(), "project restore:") {
			t.Errorf("expected 'project restore:' prefix, got %v", err)
		}
	})
}

// ---------- UpdateUseCase ----------

func TestZZUpdateUseCase_FetchNotFound(t *testing.T) {
	repo := newMemRepo()
	uc := appproject.NewUpdateUseCase(repo)
	name := "new name"
	_, err := uc.Execute(context.Background(), uuid.New(), uuid.New(), domain.UpdateInput{Name: &name})
	if !errors.Is(err, appproject.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestZZUpdateUseCase_CrossTenantFetchNotFound(t *testing.T) {
	repo := newMemRepo()
	tenantA := uuid.New()
	tenantB := uuid.New()
	p := seedProject(t, repo, tenantA, "U001", false)
	uc := appproject.NewUpdateUseCase(repo)
	name := "hijack attempt"
	_, err := uc.Execute(context.Background(), tenantB, p.ID, domain.UpdateInput{Name: &name})
	if !errors.Is(err, appproject.ErrNotFound) {
		t.Errorf("expected ErrNotFound for cross-tenant update, got %v", err)
	}
}

func TestZZUpdateUseCase_FetchGenericErrorWrapped(t *testing.T) {
	repo := newMemRepo()
	repo.forceGetErr = errBackend
	uc := appproject.NewUpdateUseCase(repo)
	name := "x"
	_, err := uc.Execute(context.Background(), uuid.New(), uuid.New(), domain.UpdateInput{Name: &name})
	if !errors.Is(err, errBackend) {
		t.Errorf("expected errors.Is(err, errBackend), got %v", err)
	}
	if !strings.Contains(err.Error(), "project update fetch:") {
		t.Errorf("expected 'project update fetch:' prefix, got %v", err)
	}
}

func TestZZUpdateUseCase_MergesAllFields(t *testing.T) {
	repo := newMemRepo()
	tenantID := uuid.New()
	p := seedProject(t, repo, tenantID, "U002", false)
	uc := appproject.NewUpdateUseCase(repo)

	newCode := "U002-NEW"
	newName := "renamed project"
	custID := uuid.New()
	amount := "12345.67"
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	addr := "123 Main St"
	mgr := "Alice"
	remark := "important remark"

	before := time.Now().UTC()
	got, err := uc.Execute(context.Background(), tenantID, p.ID, domain.UpdateInput{
		Code:           &newCode,
		Name:           &newName,
		CustomerID:     &custID,
		ContractAmount: &amount,
		StartDate:      &start,
		EndDate:        &end,
		Address:        &addr,
		Manager:        &mgr,
		Remark:         &remark,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	switch {
	case got.Code != newCode:
		t.Errorf("Code = %q, want %q", got.Code, newCode)
	case got.Name != newName:
		t.Errorf("Name = %q, want %q", got.Name, newName)
	case got.CustomerID == nil || *got.CustomerID != custID:
		t.Errorf("CustomerID = %v, want %v", got.CustomerID, custID)
	case got.ContractAmount == nil || *got.ContractAmount != amount:
		t.Errorf("ContractAmount = %v, want %v", got.ContractAmount, amount)
	case got.StartDate == nil || !got.StartDate.Equal(start):
		t.Errorf("StartDate = %v, want %v", got.StartDate, start)
	case got.EndDate == nil || !got.EndDate.Equal(end):
		t.Errorf("EndDate = %v, want %v", got.EndDate, end)
	case got.Address != addr:
		t.Errorf("Address = %q, want %q", got.Address, addr)
	case got.Manager != mgr:
		t.Errorf("Manager = %q, want %q", got.Manager, mgr)
	case got.Remark != remark:
		t.Errorf("Remark = %q, want %q", got.Remark, remark)
	}
	if got.UpdatedAt.Before(before) {
		t.Errorf("expected UpdatedAt refreshed to now, got %v (before test call: %v)", got.UpdatedAt, before)
	}
}

func TestZZUpdateUseCase_PartialUpdate_PreservesUntouchedFields(t *testing.T) {
	repo := newMemRepo()
	tenantID := uuid.New()
	p := seedProject(t, repo, tenantID, "U003", false)
	originalName := p.Name
	uc := appproject.NewUpdateUseCase(repo)

	newCode := "U003-NEW"
	got, err := uc.Execute(context.Background(), tenantID, p.ID, domain.UpdateInput{Code: &newCode})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Code != newCode {
		t.Errorf("Code = %q, want %q", got.Code, newCode)
	}
	if got.Name != originalName {
		t.Errorf("expected Name to remain %q untouched, got %q", originalName, got.Name)
	}
}

func TestZZUpdateUseCase_ValidateFailureAfterMerge(t *testing.T) {
	repo := newMemRepo()
	tenantID := uuid.New()
	p := seedProject(t, repo, tenantID, "U004", false)
	uc := appproject.NewUpdateUseCase(repo)

	emptyName := ""
	_, err := uc.Execute(context.Background(), tenantID, p.ID, domain.UpdateInput{Name: &emptyName})
	if err == nil {
		t.Fatal("expected validate error for empty name, got nil")
	}
	if !strings.Contains(err.Error(), "project update validate") {
		t.Errorf("expected 'project update validate' prefix, got %v", err)
	}
}

func TestZZUpdateUseCase_IllegalStatusTransitionWrapsDomainError(t *testing.T) {
	repo := newMemRepo()
	tenantID := uuid.New()
	p := seedProject(t, repo, tenantID, "U005", false) // starts StatusActive
	// Move active -> archived first via direct mutation to reach a dead-end state
	// (archived can only go to active), so completed target is illegal.
	repo.mu.Lock()
	repo.items[p.ID].Status = domain.StatusCompleted
	repo.mu.Unlock()

	uc := appproject.NewUpdateUseCase(repo)
	illegal := domain.StatusActive // completed -> active is explicitly illegal
	_, err := uc.Execute(context.Background(), tenantID, p.ID, domain.UpdateInput{Status: &illegal})
	if err == nil {
		t.Fatal("expected illegal transition error, got nil")
	}
	if !errors.Is(err, domain.ErrIllegalProjectStatusTransition) {
		t.Errorf("expected errors.Is(err, ErrIllegalProjectStatusTransition), got %v", err)
	}
	if !strings.Contains(err.Error(), "project update:") {
		t.Errorf("expected 'project update:' prefix, got %v", err)
	}
}

func TestZZUpdateUseCase_RepoUpdateGenericErrorWrapped(t *testing.T) {
	repo := newMemRepo()
	tenantID := uuid.New()
	p := seedProject(t, repo, tenantID, "U006", false)
	repo.forceUpdateErr = errBackend
	uc := appproject.NewUpdateUseCase(repo)

	name := "whatever"
	_, err := uc.Execute(context.Background(), tenantID, p.ID, domain.UpdateInput{Name: &name})
	if !errors.Is(err, errBackend) {
		t.Errorf("expected errors.Is(err, errBackend), got %v", err)
	}
	if !strings.Contains(err.Error(), "project update:") {
		t.Errorf("expected 'project update:' prefix, got %v", err)
	}
}

func TestZZUpdateUseCase_RepoUpdateNotFoundPassthrough(t *testing.T) {
	repo := newMemRepo()
	tenantID := uuid.New()
	p := seedProject(t, repo, tenantID, "U007", false)
	repo.forceUpdateErr = appproject.ErrNotFound
	uc := appproject.NewUpdateUseCase(repo)

	name := "whatever"
	_, err := uc.Execute(context.Background(), tenantID, p.ID, domain.UpdateInput{Name: &name})
	if !errors.Is(err, appproject.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// ---------- sanity: fmt import is used via a trivial format check ----------

func TestZZErrBackendMessage(t *testing.T) {
	if fmt.Sprintf("%v", errBackend) != "backend unavailable" {
		t.Errorf("unexpected errBackend message: %v", errBackend)
	}
}

// Compile-time checks that our fakes satisfy the Repository interface.
var (
	_ appproject.Repository = (*memRepo)(nil)
	_ appproject.Repository = (*capturingListRepo)(nil)
)
