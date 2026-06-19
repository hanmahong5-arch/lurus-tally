package onboarding_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	handleronboarding "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/onboarding"
	appob "github.com/hanmahong5-arch/lurus-tally/internal/app/onboarding"
	domainproduct "github.com/hanmahong5-arch/lurus-tally/internal/domain/product"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

func init() {
	gin.SetMode(gin.TestMode)
}

const testTenantID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"

// tenantMiddleware injects tenant_id from the X-Tenant-ID header, mirroring
// what AuthMiddleware does in production. GetTenantID reads c.Get("tenant_id").
func tenantMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if tid := c.GetHeader("X-Tenant-ID"); tid != "" {
			if id, err := uuid.Parse(tid); err == nil {
				c.Set("tenant_id", id)
			}
		}
		c.Next()
	}
}

// buildRouter wires the handler onto a test engine with tenant middleware.
func buildRouter(seed *appob.SeedDemoUseCase, clear *appob.ClearDemoUseCase) *gin.Engine {
	r := gin.New()
	r.Use(tenantMiddleware())
	api := r.Group("/api/v1")
	handleronboarding.NewForTest(seed, clear).RegisterRoutes(api)
	return r
}

// --- stubs ---

type stubProductCreator struct{}

func (s *stubProductCreator) Execute(_ context.Context, in domainproduct.CreateInput) (*domainproduct.Product, error) {
	return &domainproduct.Product{ID: uuid.New(), TenantID: in.TenantID, Code: in.Code, Name: in.Name}, nil
}

type stubStockInitializer struct{}

func (s *stubStockInitializer) Execute(_ context.Context, req appob.StockInitRequest) (*domainstock.Snapshot, error) {
	return &domainstock.Snapshot{
		TenantID:    req.TenantID,
		ProductID:   req.ProductID,
		WarehouseID: req.WarehouseID,
		OnHandQty:   req.Qty,
	}, nil
}

type stubSalesRecorder struct{}

func (s *stubSalesRecorder) RecordSale(_ context.Context, _ appob.DemoSaleRequest) error { return nil }

type stubDemoDeleter struct{ called bool }

func (s *stubDemoDeleter) DeleteDemoProducts(_ context.Context, _ uuid.UUID) error {
	s.called = true
	return nil
}

// --- helpers ---

func seedClear() (*appob.SeedDemoUseCase, *appob.ClearDemoUseCase, *stubDemoDeleter) {
	del := &stubDemoDeleter{}
	return appob.NewSeedDemoUseCase(&stubProductCreator{}, &stubStockInitializer{}, &stubSalesRecorder{}),
		appob.NewClearDemoUseCase(del),
		del
}

func marshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// --- tests ---

func TestSeedDemoHandler_MissingTenant_Returns401(t *testing.T) {
	seed, clear, _ := seedClear()
	r := buildRouter(seed, clear)

	// No X-Tenant-ID header → tenant_id not in context → 401.
	body := marshal(t, map[string]string{"persona": "retail", "warehouse_id": uuid.New().String()})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/seed-demo", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSeedDemoHandler_InvalidPersona_Returns400(t *testing.T) {
	seed, clear, _ := seedClear()
	r := buildRouter(seed, clear)

	body := marshal(t, map[string]string{"persona": "bad_persona", "warehouse_id": uuid.New().String()})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/seed-demo", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSeedDemoHandler_InvalidWarehouseUUID_Returns400(t *testing.T) {
	seed, clear, _ := seedClear()
	r := buildRouter(seed, clear)

	body := marshal(t, map[string]string{"persona": "retail", "warehouse_id": "not-a-uuid"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/seed-demo", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSeedDemoHandler_CrossBorder_Returns200WithCount(t *testing.T) {
	seed, clear, _ := seedClear()
	r := buildRouter(seed, clear)

	body := marshal(t, map[string]string{"persona": "cross_border", "warehouse_id": uuid.New().String()})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/seed-demo", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]int
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if resp["products_created"] != 3 {
		t.Errorf("want products_created=3, got %d", resp["products_created"])
	}
}

func TestSeedDemoHandler_Retail_Returns200WithCount(t *testing.T) {
	seed, clear, _ := seedClear()
	r := buildRouter(seed, clear)

	body := marshal(t, map[string]string{"persona": "retail", "warehouse_id": uuid.New().String()})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/seed-demo", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestClearDemoHandler_Success_Returns204(t *testing.T) {
	seed, clear, del := seedClear()
	r := buildRouter(seed, clear)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/clear-demo", http.NoBody)
	req.Header.Set("X-Tenant-ID", testTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("want 204, got %d: %s", w.Code, w.Body.String())
	}
	if !del.called {
		t.Error("want DeleteDemoProducts called, was not")
	}
}

func TestClearDemoHandler_MissingTenant_Returns401(t *testing.T) {
	seed, clear, _ := seedClear()
	r := buildRouter(seed, clear)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/clear-demo", http.NoBody)
	// No X-Tenant-ID header.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d: %s", w.Code, w.Body.String())
	}
}
