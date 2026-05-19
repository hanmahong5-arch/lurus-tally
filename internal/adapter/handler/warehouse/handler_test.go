package warehouse_test

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
	handlerwarehouse "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/warehouse"
	appwarehouse "github.com/hanmahong5-arch/lurus-tally/internal/app/warehouse"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/warehouse"
)

func init() {
	gin.SetMode(gin.TestMode)
}

const testTenantID = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"

// fakeRepo is an in-memory stub satisfying appwarehouse.Repository.
type fakeRepo struct {
	createErr  error
	getErr     error
	listItems  []*domain.Warehouse
	listTotal  int
	updateErr  error
	deleteErr  error
	restoreErr error
}

func (f *fakeRepo) Create(_ context.Context, _ *domain.Warehouse) error { return f.createErr }

func (f *fakeRepo) GetByID(_ context.Context, _, _ uuid.UUID) (*domain.Warehouse, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return &domain.Warehouse{
		ID:        uuid.New(),
		TenantID:  uuid.MustParse(testTenantID),
		Name:      "广州主仓库",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}, nil
}

func (f *fakeRepo) List(_ context.Context, _ domain.ListFilter) ([]*domain.Warehouse, int, error) {
	return f.listItems, f.listTotal, nil
}

func (f *fakeRepo) Update(_ context.Context, _ *domain.Warehouse) error { return f.updateErr }

func (f *fakeRepo) Delete(_ context.Context, _, _ uuid.UUID) error { return f.deleteErr }

func (f *fakeRepo) Restore(_ context.Context, _, _ uuid.UUID) (*domain.Warehouse, error) {
	if f.restoreErr != nil {
		return nil, f.restoreErr
	}
	return &domain.Warehouse{
		ID:        uuid.New(),
		TenantID:  uuid.MustParse(testTenantID),
		Name:      "广州主仓库",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}, nil
}

var _ appwarehouse.Repository = (*fakeRepo)(nil)

func makeHandler(repo appwarehouse.Repository) (*handlerwarehouse.Handler, *gin.Engine) {
	h := handlerwarehouse.New(
		appwarehouse.NewCreateUseCase(repo),
		appwarehouse.NewGetByIDUseCase(repo),
		appwarehouse.NewListUseCase(repo),
		appwarehouse.NewUpdateUseCase(repo),
		appwarehouse.NewDeleteUseCase(repo),
		appwarehouse.NewRestoreUseCase(repo),
	)
	r := gin.New()
	r.Use(gin.Recovery())
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

func TestWarehouseHandler_Create_Returns201(t *testing.T) {
	repo := &fakeRepo{}
	_, r := makeHandler(repo)

	body, _ := json.Marshal(map[string]any{"name": "广州主仓库"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/warehouses", bytes.NewReader(body))
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

func TestWarehouseHandler_Create_MissingName_Returns400(t *testing.T) {
	repo := &fakeRepo{}
	_, r := makeHandler(repo)

	body, _ := json.Marshal(map[string]any{"code": "WH001"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/warehouses", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestWarehouseHandler_Create_NoTenant_Returns401(t *testing.T) {
	repo := &fakeRepo{}
	_, r := makeHandler(repo)

	body, _ := json.Marshal(map[string]any{"name": "仓库"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/warehouses", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401; body: %s", w.Code, w.Body.String())
	}
}

func TestWarehouseHandler_List_Returns200(t *testing.T) {
	items := []*domain.Warehouse{
		{ID: uuid.New(), TenantID: uuid.MustParse(testTenantID), Name: "主仓库", CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}
	repo := &fakeRepo{listItems: items, listTotal: 1}
	_, r := makeHandler(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/warehouses", nil)
	req.Header.Set("X-Tenant-ID", testTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp handlerwarehouse.WarehouseListResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 1 {
		t.Errorf("total = %d, want 1", resp.Total)
	}
}

func TestWarehouseHandler_GetByID_NotFound_Returns404(t *testing.T) {
	repo := &fakeRepo{getErr: appwarehouse.ErrNotFound}
	_, r := makeHandler(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/warehouses/"+uuid.New().String(), nil)
	req.Header.Set("X-Tenant-ID", testTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

func TestWarehouseHandler_Delete_Returns204(t *testing.T) {
	repo := &fakeRepo{}
	_, r := makeHandler(repo)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/warehouses/"+uuid.New().String(), nil)
	req.Header.Set("X-Tenant-ID", testTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204; body: %s", w.Code, w.Body.String())
	}
}
