package customer_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	appcust "github.com/hanmahong5-arch/lurus-tally/internal/app/customer"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/customer"
)

// fakeRepo is an in-memory stub that satisfies appcust.Repository.
type fakeRepo struct {
	store       map[uuid.UUID]*domain.Customer
	createCalls int
	createErr   error
	getErr      error
	listResult  []*domain.Customer
	listTotal   int
	listErr     error
	lastFilter  domain.ListFilter
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{store: make(map[uuid.UUID]*domain.Customer)}
}

func (r *fakeRepo) Create(_ context.Context, c *domain.Customer) error {
	r.createCalls++
	if r.createErr != nil {
		return r.createErr
	}
	r.store[c.ID] = c
	return nil
}

func (r *fakeRepo) GetByID(_ context.Context, _, id uuid.UUID) (*domain.Customer, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	c, ok := r.store[id]
	if !ok || c.DeletedAt != nil {
		return nil, appcust.ErrNotFound
	}
	return c, nil
}

func (r *fakeRepo) List(_ context.Context, f domain.ListFilter) ([]*domain.Customer, int, error) {
	r.lastFilter = f
	if r.listErr != nil {
		return nil, 0, r.listErr
	}
	return r.listResult, r.listTotal, nil
}

// FindActiveByName mirrors the SQL: case-insensitive on the normalised name.
func (r *fakeRepo) FindActiveByName(_ context.Context, tenantID uuid.UUID, name string) (*domain.Customer, error) {
	for _, c := range r.store {
		if c.DeletedAt != nil {
			continue
		}
		if c.TenantID == tenantID && strings.EqualFold(appcust.NormalizeName(c.Name), name) {
			return c, nil
		}
	}
	return nil, appcust.ErrNotFound
}

func (r *fakeRepo) FindActiveByMobile(_ context.Context, tenantID uuid.UUID, mobile string) (*domain.Customer, error) {
	for _, c := range r.store {
		if c.DeletedAt != nil {
			continue
		}
		if c.TenantID == tenantID && c.Mobile == mobile {
			return c, nil
		}
	}
	return nil, appcust.ErrNotFound
}

func (r *fakeRepo) Update(_ context.Context, c *domain.Customer) error {
	if _, ok := r.store[c.ID]; !ok {
		return appcust.ErrNotFound
	}
	r.store[c.ID] = c
	return nil
}

func (r *fakeRepo) Delete(_ context.Context, _, id uuid.UUID) error {
	c, ok := r.store[id]
	if !ok || c.DeletedAt != nil {
		return appcust.ErrNotFound
	}
	now := time.Now().UTC()
	c.DeletedAt = &now
	return nil
}

func (r *fakeRepo) Restore(_ context.Context, _, id uuid.UUID) (*domain.Customer, error) {
	c, ok := r.store[id]
	if !ok || c.DeletedAt == nil {
		return nil, appcust.ErrNotFound
	}
	c.DeletedAt = nil
	return c, nil
}

// Compile-time check.
var _ appcust.Repository = (*fakeRepo)(nil)

func TestCreateUseCase_Execute_Success(t *testing.T) {
	repo := newFakeRepo()
	uc := appcust.NewCreateUseCase(repo)
	tenant := uuid.New()

	c, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID:    tenant,
		Code:        "C001",
		Name:        "杭州客户甲",
		ContactName: "张三",
		Mobile:      "13800000000",
		Phone:       "0571-12345678",
		Address:     "杭州市西湖区",
		Remark:      "老客户",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.ID == uuid.Nil {
		t.Error("expected non-nil ID")
	}
	if c.TenantID != tenant || c.Code != "C001" || c.Name != "杭州客户甲" ||
		c.ContactName != "张三" || c.Mobile != "13800000000" || c.Phone != "0571-12345678" ||
		c.Address != "杭州市西湖区" || c.Remark != "老客户" {
		t.Errorf("fields not copied from input: %+v", c)
	}
	if c.CreatedAt.IsZero() || !c.CreatedAt.Equal(c.UpdatedAt) {
		t.Errorf("created_at/updated_at = %v/%v, want equal non-zero", c.CreatedAt, c.UpdatedAt)
	}
	if repo.store[c.ID] != c {
		t.Error("repo.Create did not receive the built customer")
	}
}

func TestCreateUseCase_Execute_MissingName(t *testing.T) {
	repo := newFakeRepo()
	uc := appcust.NewCreateUseCase(repo)

	_, err := uc.Execute(context.Background(), domain.CreateInput{
		TenantID: uuid.New(),
		Name:     "", // missing
	})
	if err == nil {
		t.Fatal("expected error for missing name, got nil")
	}
	if !errors.Is(err, appcust.ErrInvalidInput) {
		t.Errorf("err = %v, want it to wrap ErrInvalidInput", err)
	}
	if repo.createCalls != 0 {
		t.Errorf("repo.Create called %d times on validation failure, want 0", repo.createCalls)
	}
}

func TestCreateUseCase_Execute_RepoErrorKeepsCauseChain(t *testing.T) {
	// The handler classifies a unique violation by walking the error chain, so
	// the use case must wrap (not replace) the repository error.
	cause := errors.New("sentinel: unique violation")
	repo := newFakeRepo()
	repo.createErr = cause
	uc := appcust.NewCreateUseCase(repo)

	_, err := uc.Execute(context.Background(), domain.CreateInput{TenantID: uuid.New(), Name: "N"})
	if !errors.Is(err, cause) {
		t.Fatalf("err = %v, want it to wrap the repo cause", err)
	}
	if errors.Is(err, appcust.ErrInvalidInput) {
		t.Error("repo failure must not be reported as invalid input")
	}
}

func TestGetByIDUseCase_Execute_Found(t *testing.T) {
	repo := newFakeRepo()
	id := uuid.New()
	repo.store[id] = &domain.Customer{ID: id, Name: "杭州客户甲"}
	uc := appcust.NewGetByIDUseCase(repo)

	c, err := uc.Execute(context.Background(), uuid.New(), id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.ID != id {
		t.Errorf("id = %v, want %v", c.ID, id)
	}
}

func TestGetByIDUseCase_Execute_NotFound(t *testing.T) {
	uc := appcust.NewGetByIDUseCase(newFakeRepo())

	_, err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, appcust.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestGetByIDUseCase_Execute_RepoError(t *testing.T) {
	cause := errors.New("sentinel: connection reset")
	repo := newFakeRepo()
	repo.getErr = cause
	uc := appcust.NewGetByIDUseCase(repo)

	_, err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, cause) {
		t.Fatalf("err = %v, want it to wrap the repo cause", err)
	}
	if errors.Is(err, appcust.ErrNotFound) {
		t.Error("repo failure must not be reported as not found")
	}
}

func TestListUseCase_Execute_ClampsLimit(t *testing.T) {
	cases := []struct {
		name  string
		limit int
		want  int
	}{
		{"zero defaults to 20", 0, 20},
		{"negative defaults to 20", -5, 20},
		{"in range kept", 50, 50},
		{"over max clamped to 200", 1000, 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepo()
			uc := appcust.NewListUseCase(repo)
			if _, _, err := uc.Execute(context.Background(), domain.ListFilter{TenantID: uuid.New(), Limit: tc.limit}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if repo.lastFilter.Limit != tc.want {
				t.Errorf("limit = %d, want %d", repo.lastFilter.Limit, tc.want)
			}
		})
	}
}

func TestListUseCase_Execute_PassesQueryAndOffset(t *testing.T) {
	repo := newFakeRepo()
	repo.listResult = []*domain.Customer{{ID: uuid.New(), Name: "A"}}
	repo.listTotal = 7
	uc := appcust.NewListUseCase(repo)

	items, total, err := uc.Execute(context.Background(), domain.ListFilter{TenantID: uuid.New(), Query: "138", Limit: 5, Offset: 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 || total != 7 {
		t.Errorf("items=%d total=%d, want 1/7", len(items), total)
	}
	if repo.lastFilter.Query != "138" || repo.lastFilter.Offset != 5 {
		t.Errorf("filter = %+v, want query=138 offset=5", repo.lastFilter)
	}
}

func TestListUseCase_Execute_RepoError(t *testing.T) {
	cause := errors.New("sentinel: boom")
	repo := newFakeRepo()
	repo.listErr = cause
	uc := appcust.NewListUseCase(repo)

	_, _, err := uc.Execute(context.Background(), domain.ListFilter{TenantID: uuid.New()})
	if !errors.Is(err, cause) {
		t.Fatalf("err = %v, want it to wrap the repo cause", err)
	}
}

func TestCreateUseCase_Execute_DuplicateName(t *testing.T) {
	repo := newFakeRepo()
	uc := appcust.NewCreateUseCase(repo)
	tenant := uuid.New()
	first, err := uc.Execute(context.Background(), domain.CreateInput{TenantID: tenant, Name: "走查 客户王"})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	for _, name := range []string{"走查 客户王", "  走查 客户王 ", "走查   客户王"} {
		_, err := uc.Execute(context.Background(), domain.CreateInput{TenantID: tenant, Name: name})
		var dup *appcust.DuplicateError
		if !errors.Is(err, appcust.ErrDuplicateName) || !errors.As(err, &dup) {
			t.Fatalf("%q: err = %v, want ErrDuplicateName", name, err)
		}
		if dup.ExistingID != first.ID || dup.ExistingName != "走查 客户王" {
			t.Errorf("%q: dup = %+v, want existing %s", name, dup, first.ID)
		}
	}
	// Case differences count as the same name.
	if _, err := uc.Execute(context.Background(), domain.CreateInput{TenantID: tenant, Name: "Abc"}); err != nil {
		t.Fatalf("Abc: %v", err)
	}
	if _, err := uc.Execute(context.Background(), domain.CreateInput{TenantID: tenant, Name: " aBC "}); !errors.Is(err, appcust.ErrDuplicateName) {
		t.Fatalf("aBC: err = %v, want ErrDuplicateName", err)
	}
	// Other tenants are unaffected.
	if _, err := uc.Execute(context.Background(), domain.CreateInput{TenantID: uuid.New(), Name: "走查 客户王"}); err != nil {
		t.Errorf("other tenant: %v", err)
	}
}

func TestCreateUseCase_Execute_DuplicateMobile(t *testing.T) {
	repo := newFakeRepo()
	uc := appcust.NewCreateUseCase(repo)
	tenant := uuid.New()
	first, err := uc.Execute(context.Background(), domain.CreateInput{TenantID: tenant, Name: "甲", Mobile: "13800000000"})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	_, err = uc.Execute(context.Background(), domain.CreateInput{TenantID: tenant, Name: "乙", Mobile: "13800000000"})
	var dup *appcust.DuplicateError
	if !errors.Is(err, appcust.ErrDuplicateMobile) || !errors.As(err, &dup) || dup.ExistingID != first.ID {
		t.Fatalf("err = %v, want ErrDuplicateMobile for %s", err, first.ID)
	}
	// Empty mobile never collides, even with another empty-mobile customer.
	for _, n := range []string{"丙", "丁"} {
		if _, err := uc.Execute(context.Background(), domain.CreateInput{TenantID: tenant, Name: n}); err != nil {
			t.Fatalf("%s: %v", n, err)
		}
	}
}

func TestCreateUseCase_Execute_MobileStoredTrimmed(t *testing.T) {
	repo := newFakeRepo()
	uc := appcust.NewCreateUseCase(repo)
	tenant := uuid.New()
	first, err := uc.Execute(context.Background(), domain.CreateInput{TenantID: tenant, Name: "甲", Mobile: " 13900000000 "})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if first.Mobile != "13900000000" {
		t.Errorf("stored mobile = %q, want trimmed", first.Mobile)
	}
	_, err = uc.Execute(context.Background(), domain.CreateInput{TenantID: tenant, Name: "乙", Mobile: "13900000000"})
	if !errors.Is(err, appcust.ErrDuplicateMobile) {
		t.Fatalf("err = %v, want ErrDuplicateMobile", err)
	}
}

func TestUpdateUseCase_Execute_SuccessAndSelfDuplicateOK(t *testing.T) {
	repo := newFakeRepo()
	create := appcust.NewCreateUseCase(repo)
	tenant := uuid.New()
	existing, err := create.Execute(context.Background(), domain.CreateInput{
		TenantID: tenant, Name: "老客户", Mobile: "13800000001", ContactName: "旧",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	newContact := "新联系人"
	out, err := appcust.NewUpdateUseCase(repo).Execute(context.Background(), tenant, existing.ID, domain.UpdateInput{
		ContactName: &newContact,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if out.ContactName != "新联系人" || out.Name != "老客户" || out.Mobile != "13800000001" {
		t.Errorf("unexpected update result: %+v", out)
	}
	// Keeping the same name must not 409 against self.
	same := "老客户"
	if _, err := appcust.NewUpdateUseCase(repo).Execute(context.Background(), tenant, existing.ID, domain.UpdateInput{Name: &same}); err != nil {
		t.Fatalf("self-same name: %v", err)
	}
}

func TestUpdateUseCase_Execute_DuplicateName(t *testing.T) {
	repo := newFakeRepo()
	create := appcust.NewCreateUseCase(repo)
	tenant := uuid.New()
	_, err := create.Execute(context.Background(), domain.CreateInput{TenantID: tenant, Name: "甲"})
	if err != nil {
		t.Fatalf("甲: %v", err)
	}
	b, err := create.Execute(context.Background(), domain.CreateInput{TenantID: tenant, Name: "乙"})
	if err != nil {
		t.Fatalf("乙: %v", err)
	}
	clash := "甲"
	_, err = appcust.NewUpdateUseCase(repo).Execute(context.Background(), tenant, b.ID, domain.UpdateInput{Name: &clash})
	if !errors.Is(err, appcust.ErrDuplicateName) {
		t.Fatalf("err = %v, want ErrDuplicateName", err)
	}
}

func TestDeleteAndRestoreUseCase(t *testing.T) {
	repo := newFakeRepo()
	create := appcust.NewCreateUseCase(repo)
	tenant := uuid.New()
	c, err := create.Execute(context.Background(), domain.CreateInput{TenantID: tenant, Name: "待删"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := appcust.NewDeleteUseCase(repo).Execute(context.Background(), tenant, c.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := appcust.NewGetByIDUseCase(repo).Execute(context.Background(), tenant, c.ID); !errors.Is(err, appcust.ErrNotFound) {
		t.Fatalf("get after delete: %v", err)
	}
	restored, err := appcust.NewRestoreUseCase(repo).Execute(context.Background(), tenant, c.ID)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.Name != "待删" {
		t.Errorf("restored = %+v", restored)
	}
	if err := appcust.NewDeleteUseCase(repo).Execute(context.Background(), tenant, uuid.New()); !errors.Is(err, appcust.ErrNotFound) {
		t.Fatalf("delete missing: %v", err)
	}
}
