package reports_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	handlerreports "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/reports"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	apprep "github.com/hanmahong5-arch/lurus-tally/internal/app/report"
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

// monthlyStubRepo is a Repo that also serves the month-end figures.
type monthlyStubRepo struct {
	stubRepo
	gotFrom, gotTo time.Time
}

func (m *monthlyStubRepo) MonthlyFigures(_ context.Context, _ uuid.UUID, from, to time.Time) (apprep.Figures, error) {
	m.gotFrom, m.gotTo = from, to
	return apprep.Figures{
		Sales: dec("1000"), SaleReturns: dec("100"), CostOfGoods: dec("500"),
		Purchases: dec("300"), Receivable: dec("42"),
	}, nil
}

func TestMonthlyHandler_ReturnsNetFigures(t *testing.T) {
	repo := &monthlyStubRepo{}
	r := buildRouter(repo)
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/reports/monthly?month=2026-09", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["net_sales"] != "900.00" || got["gross_profit"] != "400.00" || got["receivable"] != "42.00" {
		t.Errorf("body = %v", got)
	}
	if got["date_from"] != "2026-09-01" || got["date_to"] != "2026-09-30" {
		t.Errorf("range = %s..%s", got["date_from"], got["date_to"])
	}
}

func TestMonthlyHandler_BadMonthIs400InChinese(t *testing.T) {
	r := buildRouter(&monthlyStubRepo{})
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/reports/monthly?month=2026-13", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
	var got map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got["message"] == "" || got["message"][0] < 0x80 {
		t.Errorf("message = %q, want Chinese", got["message"])
	}
}

func TestMonthlyCSV_HasBOMAndRows(t *testing.T) {
	r := buildRouter(&monthlyStubRepo{})
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/reports/monthly.csv?month=2026-09", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	b := w.Body.String()
	if len(b) < 3 || b[:3] != "\xef\xbb\xbf" {
		t.Errorf("missing BOM")
	}
	for _, want := range []string{"净销售额,900.00", "毛利,400.00", "月底应收,42.00", "当前库存金额（导出时）"} {
		if !strings.Contains(b, want) {
			t.Errorf("csv lacks %q: %s", want, b)
		}
	}
}
