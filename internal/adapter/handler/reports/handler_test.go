package reports_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	handlerreports "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/reports"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appreports "github.com/hanmahong5-arch/lurus-tally/internal/app/reports"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// stubRepo satisfies appreports.Repo for handler tests.
type stubRepo struct {
	sales  []appreports.SaleRow
	stocks []appreports.StockRow
}

func (s *stubRepo) ListRecentSaleLines(_ context.Context, _ uuid.UUID, _ int) ([]appreports.SaleRow, error) {
	return s.sales, nil
}

func (s *stubRepo) ListStockSnapshots(_ context.Context, _ uuid.UUID) ([]appreports.StockRow, error) {
	return s.stocks, nil
}

func dec(v string) decimal.Decimal {
	d, _ := decimal.NewFromString(v)
	return d
}

func buildRouter(repo appreports.Repo) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())

	// Inject tenant UUID into context key that GetTenantID reads.
	tid := uuid.New()
	r.Use(func(c *gin.Context) {
		raw := c.GetHeader("X-Tenant-ID")
		if id, err := uuid.Parse(raw); err == nil {
			c.Set(middleware.CtxKeyTenantID, id)
		} else {
			c.Set(middleware.CtxKeyTenantID, tid)
		}
		c.Next()
	})

	uc := appreports.New(repo)
	h := handlerreports.New(uc)
	api := r.Group("/api/v1")
	h.RegisterRoutes(api)
	return r
}

func TestGrossMarginHandler_Returns200(t *testing.T) {
	repo := &stubRepo{
		sales: []appreports.SaleRow{
			{ProductID: uuid.New(), ProductName: "P1", Revenue: dec("100"), Margin: dec("0.4"), SoldAt: time.Now()},
		},
	}
	r := buildRouter(repo)
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/reports/gross-margin?days=30", nil)
	req.Header.Set("X-Tenant-ID", uuid.New().String())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("gross-margin status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp appreports.GrossMarginResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Days != 30 {
		t.Errorf("Days = %d, want 30", resp.Days)
	}
}

func TestABCClassifyHandler_Returns200(t *testing.T) {
	repo := &stubRepo{
		sales: []appreports.SaleRow{
			{ProductID: uuid.New(), ProductName: "Alpha", Revenue: dec("80"), SoldAt: time.Now()},
			{ProductID: uuid.New(), ProductName: "Beta", Revenue: dec("20"), SoldAt: time.Now()},
		},
	}
	r := buildRouter(repo)
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/reports/abc", nil)
	req.Header.Set("X-Tenant-ID", uuid.New().String())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("abc status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp appreports.ABCResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.TotalSKUs != 2 {
		t.Errorf("TotalSKUs = %d, want 2", resp.TotalSKUs)
	}
}

func TestDeadStockHandler_Returns200(t *testing.T) {
	repo := &stubRepo{
		stocks: []appreports.StockRow{
			{ProductID: uuid.New(), ProductName: "Dead", ProductCode: "D1",
				Qty: dec("5"), UnitCost: dec("10"),
				LastMovedAt: time.Now().Add(-120 * 24 * time.Hour)},
		},
	}
	r := buildRouter(repo)
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/reports/dead-stock?days=90", nil)
	req.Header.Set("X-Tenant-ID", uuid.New().String())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("dead-stock status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp appreports.DeadStockResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Count != 1 {
		t.Errorf("Count = %d, want 1", resp.Count)
	}
}

func TestSalesTopHandler_Returns200(t *testing.T) {
	repo := &stubRepo{
		sales: []appreports.SaleRow{
			{ProductID: uuid.New(), ProductName: "Top", Revenue: dec("999"), SoldAt: time.Now()},
		},
	}
	r := buildRouter(repo)
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/reports/sales-top?metric=revenue&days=7&limit=10", nil)
	req.Header.Set("X-Tenant-ID", uuid.New().String())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("sales-top status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp appreports.SalesTopResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.TopProducts) != 1 {
		t.Errorf("TopProducts len = %d, want 1", len(resp.TopProducts))
	}
}

func TestSalesTopHandler_InvalidMetric_Returns400(t *testing.T) {
	r := buildRouter(&stubRepo{})
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/reports/sales-top?metric=invalid", nil)
	req.Header.Set("X-Tenant-ID", uuid.New().String())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid metric status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestReportsHandler_NoTenantID_Returns401(t *testing.T) {
	// Build router without the header-injecting middleware.
	r := gin.New()
	r.Use(gin.Recovery())
	uc := appreports.New(&stubRepo{})
	h := handlerreports.New(uc)
	api := r.Group("/api/v1")
	h.RegisterRoutes(api)

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/reports/gross-margin", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("no tenant status = %d, want 401; body: %s", w.Code, w.Body.String())
	}
}
