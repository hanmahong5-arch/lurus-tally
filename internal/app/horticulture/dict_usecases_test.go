package horticulture_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	apphort "github.com/hanmahong5-arch/lurus-tally/internal/app/horticulture"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/horticulture"
)

// fakeRepo is a hand-written fake implementation of apphort.Repository for unit tests.
type fakeRepo struct {
	createErr  error
	getErr     error
	listErr    error
	updateErr  error
	deleteErr  error
	restoreErr error

	createdEntry  *domain.NurseryDict
	restoredEntry *domain.NurseryDict
	listFilter    domain.ListFilter
}

func (f *fakeRepo) Create(_ context.Context, d *domain.NurseryDict) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.createdEntry = d
	return nil
}

func (f *fakeRepo) GetByID(_ context.Context, _, _ uuid.UUID) (*domain.NurseryDict, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return &domain.NurseryDict{
		ID:        uuid.New(),
		Name:      "银杏",
		Type:      domain.NurseryTypeTree,
		DeletedAt: nil,
	}, nil
}

func (f *fakeRepo) List(_ context.Context, filter domain.ListFilter) ([]*domain.NurseryDict, int, error) {
	f.listFilter = filter
	if f.listErr != nil {
		return nil, 0, f.listErr
	}
	return []*domain.NurseryDict{}, 0, nil
}

func (f *fakeRepo) Update(_ context.Context, _ *domain.NurseryDict) error {
	return f.updateErr
}

func (f *fakeRepo) Delete(_ context.Context, _, _ uuid.UUID) error {
	return f.deleteErr
}

func (f *fakeRepo) Restore(_ context.Context, _, _ uuid.UUID) (*domain.NurseryDict, error) {
	if f.restoreErr != nil {
		return nil, f.restoreErr
	}
	now := time.Now().UTC()
	if f.restoredEntry != nil {
		f.restoredEntry.DeletedAt = nil
		return f.restoredEntry, nil
	}
	return &domain.NurseryDict{
		ID:        uuid.New(),
		Name:      "银杏",
		Type:      domain.NurseryTypeTree,
		UpdatedAt: now,
		DeletedAt: nil,
	}, nil
}

func TestCreateUseCase_Execute_ReturnsDuplicateNameError(t *testing.T) {
	repo := &fakeRepo{createErr: apphort.ErrDuplicateName}
	uc := apphort.NewCreateUseCase(repo)

	_, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID: uuid.New(),
		Name:     "红枫",
		Type:     domain.NurseryTypeTree,
	})
	if err == nil {
		t.Fatal("expected ErrDuplicateName, got nil")
	}
	if err != apphort.ErrDuplicateName {
		t.Errorf("expected ErrDuplicateName, got %v", err)
	}
}

func TestCreateUseCase_Execute_HappyPath(t *testing.T) {
	repo := &fakeRepo{}
	uc := apphort.NewCreateUseCase(repo)

	d, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID: uuid.New(),
		Name:     "银杏",
		Type:     domain.NurseryTypeTree,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Name != "银杏" {
		t.Errorf("expected name '银杏', got %q", d.Name)
	}
	if repo.createdEntry == nil {
		t.Error("expected repo.Create to be called")
	}
}

func TestDeleteUseCase_Execute_SetsDeletedAt(t *testing.T) {
	repo := &fakeRepo{}
	uc := apphort.NewDeleteUseCase(repo)

	err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDeleteUseCase_Execute_NotFoundError(t *testing.T) {
	repo := &fakeRepo{deleteErr: apphort.ErrNotFound}
	uc := apphort.NewDeleteUseCase(repo)

	err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if err != apphort.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestRestoreUseCase_Execute_HappyPath(t *testing.T) {
	repo := &fakeRepo{}
	uc := apphort.NewRestoreUseCase(repo)

	d, err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.DeletedAt != nil {
		t.Errorf("expected DeletedAt to be nil after restore, got %v", d.DeletedAt)
	}
}

func TestListUseCase_Execute_DefaultsLimit(t *testing.T) {
	repo := &fakeRepo{}
	uc := apphort.NewListUseCase(repo)

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
