package supplier_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	handlersupp "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/supplier"
	appsupp "github.com/hanmahong5-arch/lurus-tally/internal/app/supplier"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/supplier"
)

func init() {
	gin.SetMode(gin.TestMode)
}

const testTenantID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"

// fakeRepo is an in-memory stub satisfying appsupp.Repository.
type fakeRepo struct {
	createErr  error
	getErr     error
	listItems  []*domain.Supplier
	listTotal  int
	updateErr  error
	deleteErr  error
	restoreErr error
	created    *domain.Supplier
}

func (f *fakeRepo) Create(_ context.Context, s *domain.Supplier) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.created = s
	return nil
}

func (f *fakeRepo) GetByID(_ context.Context, _, _ uuid.UUID) (*domain.Supplier, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return &domain.Supplier{
		ID:        uuid.New(),
		TenantID:  uuid.MustParse(testTenantID),
		Name:      "深圳供应商A",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}, nil
}

func (f *fakeRepo) List(_ context.Context, _ domain.ListFilter) ([]*domain.Supplier, int, error) {
	return f.listItems, f.listTotal, nil
}

func (f *fakeRepo) Update(_ context.Context, _ *domain.Supplier) error { return f.updateErr }

func (f *fakeRepo) Delete(_ context.Context, _, _ uuid.UUID) error { return f.deleteErr }

func (f *fakeRepo) Restore(_ context.Context, _, _ uuid.UUID) (*domain.Supplier, error) {
	if f.restoreErr != nil {
		return nil, f.restoreErr
	}
	return &domain.Supplier{
		ID:        uuid.New(),
		TenantID:  uuid.MustParse(testTenantID),
		Name:      "深圳供应商A",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}, nil
}

var _ appsupp.Repository = (*fakeRepo)(nil)

func makeHandler(repo appsupp.Repository) (*handlersupp.Handler, *gin.Engine) {
	h := handlersupp.New(
		appsupp.NewCreateUseCase(repo),
		appsupp.NewGetByIDUseCase(repo),
		appsupp.NewListUseCase(repo),
		appsupp.NewUpdateUseCase(repo),
		appsupp.NewDeleteUseCase(repo),
		appsupp.NewRestoreUseCase(repo),
	)
	r := gin.New()
	r.Use(gin.Recovery())
	// Inject tenant_id for tests.
	r.Use(func(c *gin.Context) {
		if tid := c.GetHeader("X-Tenant-ID"); tid != "" {
			if id, err := uuid.Parse(tid); err == nil {
				c.Set("tenant_id", id)
			}
		}
		c.Next()
	})
	api := r.Group("/api/v1")
	h.RegisterRoutes(api)
	return h, r
}

func TestSupplierHandler_Create_Returns201(t *testing.T) {
	repo := &fakeRepo{}
	_, r := makeHandler(repo)

	body, _ := json.Marshal(map[string]any{"name": "深圳供应商A"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/suppliers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc == "" {
		t.Error("missing Location header")
	}
}

func TestSupplierHandler_Create_MissingName_Returns400(t *testing.T) {
	repo := &fakeRepo{}
	_, r := makeHandler(repo)

	body, _ := json.Marshal(map[string]any{"code": "S001"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/suppliers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestSupplierHandler_Create_NoTenant_Returns401(t *testing.T) {
	repo := &fakeRepo{}
	_, r := makeHandler(repo)

	body, _ := json.Marshal(map[string]any{"name": "供应商"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/suppliers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// No X-Tenant-ID
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401; body: %s", w.Code, w.Body.String())
	}
}

func TestSupplierHandler_List_Returns200(t *testing.T) {
	items := []*domain.Supplier{
		{ID: uuid.New(), TenantID: uuid.MustParse(testTenantID), Name: "供应商A", CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}
	repo := &fakeRepo{listItems: items, listTotal: 1}
	_, r := makeHandler(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/suppliers", nil)
	req.Header.Set("X-Tenant-ID", testTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp handlersupp.SupplierListResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 1 {
		t.Errorf("total = %d, want 1", resp.Total)
	}
}

func TestSupplierHandler_GetByID_NotFound_Returns404(t *testing.T) {
	repo := &fakeRepo{getErr: appsupp.ErrNotFound}
	_, r := makeHandler(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/suppliers/"+uuid.New().String(), nil)
	req.Header.Set("X-Tenant-ID", testTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

func TestSupplierHandler_Delete_Returns204(t *testing.T) {
	repo := &fakeRepo{}
	_, r := makeHandler(repo)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/suppliers/"+uuid.New().String(), nil)
	req.Header.Set("X-Tenant-ID", testTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204; body: %s", w.Code, w.Body.String())
	}
}
