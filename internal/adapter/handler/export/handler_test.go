package export_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	handlerexport "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/export"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// stubExporter satisfies handlerexport.Exporter.
// It writes a fixed CSV body to w and returns a configurable error.
type stubExporter struct {
	body string
	err  error
}

func (s *stubExporter) Execute(_ context.Context, _ uuid.UUID, w io.Writer) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	_, err := io.WriteString(w, s.body)
	return 1, err
}

// authMW injects tenantID into the Gin context.
func authMW(tenantID uuid.UUID) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(middleware.CtxKeyTenantID, tenantID)
		c.Next()
	}
}

func buildEngine(bills, stock, payments handlerexport.Exporter, tenantID uuid.UUID, withAuth bool) *gin.Engine {
	r := gin.New()
	api := r.Group("/api/v1")
	if withAuth {
		api.Use(authMW(tenantID))
	}
	h := handlerexport.New(bills, stock, payments, nil)
	h.RegisterRoutes(api)
	return r
}

// ----- authed: 200 + text/csv + UTF-8 BOM + Chinese header -----

func TestBills_Authed_ReturnsCSVWithBOMAndHeader(t *testing.T) {
	csvBody := "单号,类型,状态,日期,合作方,仓库,总额,已付,备注\n"
	bills := &stubExporter{body: csvBody}
	stock := &stubExporter{}
	payments := &stubExporter{}

	r := buildEngine(bills, stock, payments, uuid.New(), true)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/exports/bills.csv", nil)
	r.ServeHTTP(w, req)

	assertCSV(t, w, []string{"单号", "类型", "状态", "日期", "合作方", "仓库", "总额", "已付", "备注"})
}

func TestStock_Authed_ReturnsCSVWithBOMAndHeader(t *testing.T) {
	csvBody := "商品编码,商品名,仓库,在库,单位成本\n"
	r := buildEngine(&stubExporter{}, &stubExporter{body: csvBody}, &stubExporter{}, uuid.New(), true)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/exports/stock.csv", nil)
	r.ServeHTTP(w, req)

	assertCSV(t, w, []string{"商品编码", "商品名", "仓库", "在库", "单位成本"})
}

func TestPayments_Authed_ReturnsCSVWithBOMAndHeader(t *testing.T) {
	csvBody := "单号,收付款方,金额,方式,时间\n"
	r := buildEngine(&stubExporter{}, &stubExporter{}, &stubExporter{body: csvBody}, uuid.New(), true)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/exports/payments.csv", nil)
	r.ServeHTTP(w, req)

	assertCSV(t, w, []string{"单号", "收付款方", "金额", "方式", "时间"})
}

// ----- unauthenticated: 401 -----

func TestExport_NoTenant_Returns401(t *testing.T) {
	r := buildEngine(&stubExporter{}, &stubExporter{}, &stubExporter{}, uuid.Nil, false)
	for _, path := range []string{
		"/api/v1/exports/bills.csv",
		"/api/v1/exports/stock.csv",
		"/api/v1/exports/payments.csv",
	} {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, path, nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("path %s: want 401, got %d", path, w.Code)
		}
	}
}

// ----- helpers -----

func assertCSV(t *testing.T, w *httptest.ResponseRecorder, wantCols []string) {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("want Content-Type text/csv, got %q", ct)
	}
	body := w.Body.Bytes()
	if len(body) < 3 || body[0] != 0xEF || body[1] != 0xBB || body[2] != 0xBF {
		t.Errorf("want UTF-8 BOM as first 3 bytes, got %v", body[:minLen(3, len(body))])
	}
	rest := string(body[3:])
	for _, col := range wantCols {
		if !strings.Contains(rest, col) {
			t.Errorf("response missing column %q; body after BOM: %s", col, rest)
		}
	}
}

func minLen(a, b int) int {
	if a < b {
		return a
	}
	return b
}
