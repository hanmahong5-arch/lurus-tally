package project_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	appproject "github.com/hanmahong5-arch/lurus-tally/internal/app/project"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/project"
)

// fakeRepo is a hand-written fake implementation of appproject.Repository for unit tests.
type fakeRepo struct {
	createErr  error
	getErr     error
	listErr    error
	updateErr  error
	deleteErr  error
	restoreErr error

	createdEntry  *domain.Project
	restoredEntry *domain.Project
	listFilter    domain.ListFilter
}

func (f *fakeRepo) Create(_ context.Context, p *domain.Project) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.createdEntry = p
	return nil
}

func (f *fakeRepo) GetByID(_ context.Context, _, _ uuid.UUID) (*domain.Project, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return &domain.Project{
		ID:        uuid.New(),
		Name:      "河道绿化",
		Code:      "P001",
		Status:    domain.StatusActive,
		DeletedAt: nil,
	}, nil
}

func (f *fakeRepo) List(_ context.Context, filter domain.ListFilter) ([]*domain.Project, int, error) {
	f.listFilter = filter
	if f.listErr != nil {
		return nil, 0, f.listErr
	}
	return []*domain.Project{}, 0, nil
}

func (f *fakeRepo) Update(_ context.Context, _ *domain.Project) error {
	return f.updateErr
}

func (f *fakeRepo) Delete(_ context.Context, _, _ uuid.UUID) error {
	return f.deleteErr
}

func (f *fakeRepo) Restore(_ context.Context, _, _ uuid.UUID) (*domain.Project, error) {
	if f.restoreErr != nil {
		return nil, f.restoreErr
	}
	now := time.Now().UTC()
	if f.restoredEntry != nil {
		f.restoredEntry.DeletedAt = nil
		return f.restoredEntry, nil
	}
	return &domain.Project{
		ID:        uuid.New(),
		Name:      "河道绿化",
		Code:      "P001",
		Status:    domain.StatusActive,
		UpdatedAt: now,
		DeletedAt: nil,
	}, nil
}

func TestProjectCreateUseCase_Execute_HappyPath(t *testing.T) {
	repo := &fakeRepo{}
	uc := appproject.NewCreateUseCase(repo)

	p, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID: uuid.New(),
		Name:     "河道绿化",
		Code:     "P001",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name != "河道绿化" {
		t.Errorf("expected name '河道绿化', got %q", p.Name)
	}
	if repo.createdEntry == nil {
		t.Error("expected repo.Create to be called")
	}
}

func TestProjectCreateUseCase_Execute_ReturnsDuplicateCodeError(t *testing.T) {
	repo := &fakeRepo{createErr: appproject.ErrDuplicateCode}
	uc := appproject.NewCreateUseCase(repo)

	_, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID: uuid.New(),
		Name:     "河道绿化",
		Code:     "P001",
	})
	if err == nil {
		t.Fatal("expected ErrDuplicateCode, got nil")
	}
	if err != appproject.ErrDuplicateCode {
		t.Errorf("expected ErrDuplicateCode, got %v", err)
	}
}

func TestProjectCreateUseCase_Execute_DefaultsStatusToActive(t *testing.T) {
	repo := &fakeRepo{}
	uc := appproject.NewCreateUseCase(repo)

	p, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID: uuid.New(),
		Name:     "河道绿化",
		Code:     "P001",
		Status:   "", // empty — should default to active
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Status != domain.StatusActive {
		t.Errorf("expected status 'active', got %q", p.Status)
	}
}

func TestProjectDeleteUseCase_Execute_NotFoundError(t *testing.T) {
	repo := &fakeRepo{deleteErr: appproject.ErrNotFound}
	uc := appproject.NewDeleteUseCase(repo)

	err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if err != appproject.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestProjectRestoreUseCase_Execute_HappyPath(t *testing.T) {
	repo := &fakeRepo{}
	uc := appproject.NewRestoreUseCase(repo)

	p, err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.DeletedAt != nil {
		t.Errorf("expected DeletedAt to be nil after restore, got %v", p.DeletedAt)
	}
}

func TestProjectListUseCase_Execute_DefaultsLimit(t *testing.T) {
	repo := &fakeRepo{}
	uc := appproject.NewListUseCase(repo)

	_, _, err := uc.Execute(context.Background(), domain.ListFilter{
		TenantID: uuid.New(),
		Limit:    0, // should be defaulted to 20
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.listFilter.Limit != 20 {
		t.Errorf("expected Limit=20, got %d", repo.listFilter.Limit)
	}
}

// Compile-time check.
var _ appproject.Repository = (*fakeRepo)(nil)
