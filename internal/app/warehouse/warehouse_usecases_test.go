package warehouse_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	appwarehouse "github.com/hanmahong5-arch/lurus-tally/internal/app/warehouse"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/warehouse"
)

// fakeRepo is an in-memory stub that satisfies appwarehouse.Repository.
type fakeRepo struct {
	store      map[uuid.UUID]*domain.Warehouse
	createErr  error
	getErr     error
	listResult []*domain.Warehouse
	listTotal  int
	updateErr  error
	deleteErr  error
	restoreErr error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{store: make(map[uuid.UUID]*domain.Warehouse)}
}

func (r *fakeRepo) Create(_ context.Context, w *domain.Warehouse) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.store[w.ID] = w
	return nil
}

func (r *fakeRepo) GetByID(_ context.Context, _, id uuid.UUID) (*domain.Warehouse, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	w, ok := r.store[id]
	if !ok {
		return nil, appwarehouse.ErrNotFound
	}
	return w, nil
}

func (r *fakeRepo) List(_ context.Context, _ domain.ListFilter) ([]*domain.Warehouse, int, error) {
	return r.listResult, r.listTotal, nil
}

func (r *fakeRepo) Update(_ context.Context, w *domain.Warehouse) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	r.store[w.ID] = w
	return nil
}

func (r *fakeRepo) Delete(_ context.Context, _, id uuid.UUID) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	delete(r.store, id)
	return nil
}

func (r *fakeRepo) Restore(_ context.Context, _, id uuid.UUID) (*domain.Warehouse, error) {
	if r.restoreErr != nil {
		return nil, r.restoreErr
	}
	w, ok := r.store[id]
	if !ok {
		return nil, appwarehouse.ErrNotFound
	}
	return w, nil
}

// Compile-time check.
var _ appwarehouse.Repository = (*fakeRepo)(nil)

func TestCreateUseCase_Execute_Success(t *testing.T) {
	repo := newFakeRepo()
	uc := appwarehouse.NewCreateUseCase(repo)

	w, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID: uuid.New(),
		Name:     "广州主仓库",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.Name != "广州主仓库" {
		t.Errorf("name = %q, want 广州主仓库", w.Name)
	}
	if w.ID == uuid.Nil {
		t.Error("expected non-nil ID")
	}
}

func TestCreateUseCase_Execute_MissingName(t *testing.T) {
	repo := newFakeRepo()
	uc := appwarehouse.NewCreateUseCase(repo)

	_, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID: uuid.New(),
		Name:     "", // missing
	})
	if err == nil {
		t.Fatal("expected error for missing name, got nil")
	}
}

func TestListUseCase_Execute_ClampsLimit(t *testing.T) {
	repo := newFakeRepo()
	repo.listResult = []*domain.Warehouse{}
	repo.listTotal = 0
	uc := appwarehouse.NewListUseCase(repo)

	_, _, err := uc.Execute(context.Background(), domain.ListFilter{
		TenantID: uuid.New(),
		Limit:    0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateUseCase_Execute_DuplicateName(t *testing.T) {
	repo := newFakeRepo()
	repo.createErr = appwarehouse.ErrDuplicateName
	uc := appwarehouse.NewCreateUseCase(repo)

	_, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID: uuid.New(),
		Name:     "已存在仓库",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != appwarehouse.ErrDuplicateName {
		t.Errorf("expected ErrDuplicateName, got %v", err)
	}
}
