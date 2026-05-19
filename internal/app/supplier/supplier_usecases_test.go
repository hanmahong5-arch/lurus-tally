package supplier_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	appsupp "github.com/hanmahong5-arch/lurus-tally/internal/app/supplier"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/supplier"
)

// fakeRepo is an in-memory stub that satisfies appsupp.Repository.
type fakeRepo struct {
	store      map[uuid.UUID]*domain.Supplier
	createErr  error
	getErr     error
	listResult []*domain.Supplier
	listTotal  int
	updateErr  error
	deleteErr  error
	restoreErr error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{store: make(map[uuid.UUID]*domain.Supplier)}
}

func (r *fakeRepo) Create(_ context.Context, s *domain.Supplier) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.store[s.ID] = s
	return nil
}

func (r *fakeRepo) GetByID(_ context.Context, _, id uuid.UUID) (*domain.Supplier, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	s, ok := r.store[id]
	if !ok {
		return nil, appsupp.ErrNotFound
	}
	return s, nil
}

func (r *fakeRepo) List(_ context.Context, _ domain.ListFilter) ([]*domain.Supplier, int, error) {
	return r.listResult, r.listTotal, nil
}

func (r *fakeRepo) Update(_ context.Context, s *domain.Supplier) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	r.store[s.ID] = s
	return nil
}

func (r *fakeRepo) Delete(_ context.Context, _, id uuid.UUID) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	delete(r.store, id)
	return nil
}

func (r *fakeRepo) Restore(_ context.Context, _, id uuid.UUID) (*domain.Supplier, error) {
	if r.restoreErr != nil {
		return nil, r.restoreErr
	}
	s, ok := r.store[id]
	if !ok {
		return nil, appsupp.ErrNotFound
	}
	return s, nil
}

// Compile-time check.
var _ appsupp.Repository = (*fakeRepo)(nil)

func TestCreateUseCase_Execute_Success(t *testing.T) {
	repo := newFakeRepo()
	uc := appsupp.NewCreateUseCase(repo)

	s, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID: uuid.New(),
		Name:     "深圳供应商A",
		Phone:    "0755-12345678",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Name != "深圳供应商A" {
		t.Errorf("name = %q, want 深圳供应商A", s.Name)
	}
	if s.ID == uuid.Nil {
		t.Error("expected non-nil ID")
	}
}

func TestCreateUseCase_Execute_MissingName(t *testing.T) {
	repo := newFakeRepo()
	uc := appsupp.NewCreateUseCase(repo)

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
	repo.listResult = []*domain.Supplier{}
	repo.listTotal = 0
	uc := appsupp.NewListUseCase(repo)

	// limit=0 should default to 20 (no error).
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
	repo.createErr = appsupp.ErrDuplicateName
	uc := appsupp.NewCreateUseCase(repo)

	_, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID: uuid.New(),
		Name:     "已存在供应商",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != appsupp.ErrDuplicateName {
		t.Errorf("expected ErrDuplicateName, got %v", err)
	}
}
