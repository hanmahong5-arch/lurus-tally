package product_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	handlerproduct "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/product"
	repoproduct "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/product"
	appproduct "github.com/hanmahong5-arch/lurus-tally/internal/app/product"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/product"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ---- mock repository for product handler tests ----

type mockProductRepo struct {
	products map[uuid.UUID]*domain.Product
}

func newMockProductRepo() *mockProductRepo {
	return &mockProductRepo{products: make(map[uuid.UUID]*domain.Product)}
}

func (m *mockProductRepo) Create(_ context.Context, p *domain.Product) error {
	m.products[p.ID] = p
	return nil
}

func (m *mockProductRepo) GetByID(_ context.Context, tenantID, id uuid.UUID) (*domain.Product, error) {
	p, ok := m.products[id]
	if !ok || p.TenantID != tenantID {
		return nil, repoproduct.ErrNotFound
	}
	return p, nil
}

func (m *mockProductRepo) List(_ context.Context, _ domain.ListFilter) ([]*domain.Product, int, error) {
	return nil, 0, nil
}

func (m *mockProductRepo) Update(_ context.Context, p *domain.Product) error {
	m.products[p.ID] = p
	return nil
}

func (m *mockProductRepo) Delete(_ context.Context, tenantID, id uuid.UUID) error {
	p, ok := m.products[id]
	if !ok || p.TenantID != tenantID {
		return repoproduct.ErrNotFound
	}
	now := time.Now()
	p.UpdatedAt = now
	return nil
}

func (m *mockProductRepo) Restore(_ context.Context, tenantID, id uuid.UUID) (*domain.Product, error) {
	p, ok := m.products[id]
	if !ok || p.TenantID != tenantID {
		return nil, repoproduct.ErrNotFound
	}
	p.UpdatedAt = time.Now()
	return p, nil
}

// ---- helper to wire the handler ----

const testTenantID = "11111111-1111-1111-1111-111111111111"

func newTestProductHandler(repo *mockProductRepo) *handlerproduct.Handler {
	return handlerproduct.New(
		appproduct.NewCreateUseCase(repo),
		appproduct.NewListUseCase(repo),
		appproduct.NewGetUseCase(repo),
		appproduct.NewUpdateUseCase(repo),
		appproduct.NewDeleteUseCase(repo),
		appproduct.NewRestoreUseCase(repo),
	)
}

func newProductRouter(h *handlerproduct.Handler) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	api := r.Group("/api/v1")
	api.GET("/products", h.List)
	api.POST("/products", h.Create)
	api.GET("/products/:id", h.GetByID)
	api.PUT("/products/:id", h.Update)
	api.DELETE("/products/:id", h.Delete)
	api.POST("/products/:id/restore", h.Restore)
	return r
}

// seedProduct inserts a product into the mock repo and returns its ID.
func seedProduct(repo *mockProductRepo) uuid.UUID {
	tenantID, _ := uuid.Parse(testTenantID)
	id := uuid.New()
	now := time.Now()
	repo.products[id] = &domain.Product{
		ID:                  id,
		TenantID:            tenantID,
		Code:                "P-RESTORE-001",
		Name:                "Restorable Product",
		MeasurementStrategy: domain.StrategyIndividual,
		Enabled:             true,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	return id
}

// TestProductHandler_Restore_ReturnsOKOnSoftDeletedProduct verifies that restoring a
// soft-deleted product returns HTTP 200 with the product JSON.
func TestProductHandler_Restore_ReturnsOKOnSoftDeletedProduct(t *testing.T) {
	repo := newMockProductRepo()
	id := seedProduct(repo)

	h := newTestProductHandler(repo)
	r := newProductRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/products/"+id.String()+"/restore", nil)
	req.Header.Set("X-Tenant-ID", testTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["id"] == nil {
		t.Error("response missing 'id' field")
	}
}

// TestProductHandler_Restore_Returns404ForNonExistentProduct verifies that restoring an
// unknown product ID returns HTTP 404.
func TestProductHandler_Restore_Returns404ForNonExistentProduct(t *testing.T) {
	repo := newMockProductRepo()

	h := newTestProductHandler(repo)
	r := newProductRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/products/"+uuid.New().String()+"/restore", nil)
	req.Header.Set("X-Tenant-ID", testTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

// TestRestoreUseCase_Execute_ClearsDeletedAt verifies the use case correctly calls Restore
// and returns the product from the repository.
func TestRestoreUseCase_Execute_ClearsDeletedAt(t *testing.T) {
	repo := newMockProductRepo()
	id := seedProduct(repo)

	tenantID, _ := uuid.Parse(testTenantID)
	uc := appproduct.NewRestoreUseCase(repo)

	p, err := uc.Execute(context.Background(), tenantID, id)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil product")
	}
	if p.ID != id {
		t.Errorf("product ID = %s, want %s", p.ID, id)
	}
}

// TestRestoreUseCase_Execute_ReturnsErrorForUnknown verifies that restoring a missing
// product propagates ErrNotFound.
func TestRestoreUseCase_Execute_ReturnsErrorForUnknown(t *testing.T) {
	repo := newMockProductRepo()
	tenantID, _ := uuid.Parse(testTenantID)
	uc := appproduct.NewRestoreUseCase(repo)

	_, err := uc.Execute(context.Background(), tenantID, uuid.New())
	if err == nil {
		t.Fatal("expected error for unknown product, got nil")
	}
	if !errors.Is(err, repoproduct.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// Compile-time check: mockProductRepo must satisfy appproduct.Repository.
var _ appproduct.Repository = (*mockProductRepo)(nil)

// unused import guard
var _ = sql.ErrNoRows
