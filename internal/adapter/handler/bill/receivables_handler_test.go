package bill_test

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

	handlerbill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/bill"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
)

type stubReceivablesRepo struct {
	rows []appbill.PartnerReceivable
	got  appbill.ReceivablesQuery
}

func (s *stubReceivablesRepo) PartnerReceivables(_ context.Context, q appbill.ReceivablesQuery) ([]appbill.PartnerReceivable, error) {
	s.got = q
	return s.rows, nil
}

func receivablesRouter(h *handlerbill.Handler) *gin.Engine {
	r := gin.New()
	r.Use(injectTenantFromHeader)
	h.RegisterRoutes(r.Group("/api/v1"))
	return r
}

func getReceivables(r *gin.Engine, query string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/receivables"+query, nil)
	req.Header.Set("X-Tenant-ID", devTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestReceivablesHandler_OK(t *testing.T) {
	pid := uuid.New()
	oldest := time.Date(2026, 7, 1, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
	repo := &stubReceivablesRepo{rows: []appbill.PartnerReceivable{{
		PartnerID: &pid, PartnerName: "老客户", Unpaid: decimal.RequireFromString("100.5"),
		B0to30: decimal.RequireFromString("40"), B31to60: decimal.Zero, B61to90: decimal.Zero,
		B90Plus: decimal.RequireFromString("60.5"), OldestBillDate: &oldest,
	}}}
	h := newTestHandler(&mockBillRepo{}).WithReceivables(appbill.NewReceivablesUseCase(repo))
	w := getReceivables(receivablesRouter(h), "?direction=ap&as_of=2026-10-10")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if repo.got.Direction != "ap" || repo.got.AsOf.Format("2006-01-02") != "2026-10-10" {
		t.Fatalf("query = %+v", repo.got)
	}
	var body struct {
		Items []map[string]any `json:"items"`
		Total map[string]any   `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 1 || body.Items[0]["unpaid"] != "100.50" || body.Items[0]["b90p"] != "60.50" ||
		body.Items[0]["oldest_bill_date"] != "2026-07-01" || body.Items[0]["partner_name"] != "老客户" {
		t.Fatalf("items = %v", body.Items)
	}
	if body.Total["unpaid"] != "100.50" {
		t.Fatalf("total = %v", body.Total)
	}
}

func TestReceivablesHandler_Validation(t *testing.T) {
	h := newTestHandler(&mockBillRepo{}).WithReceivables(appbill.NewReceivablesUseCase(&stubReceivablesRepo{}))
	r := receivablesRouter(h)
	for _, q := range []string{"?direction=xx", "?as_of=2026-13-40"} {
		if w := getReceivables(r, q); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d", q, w.Code)
		}
	}
	// default direction is ar and an empty result is an empty list, not null.
	w := getReceivables(r, "")
	if w.Code != http.StatusOK || !json.Valid(w.Body.Bytes()) || !contains(w.Body.String(), `"items":[]`) {
		t.Errorf("default: %d %s", w.Code, w.Body.String())
	}
}

func TestReceivablesHandler_NotWired(t *testing.T) {
	r := receivablesRouter(newTestHandler(&mockBillRepo{}))
	if w := getReceivables(r, ""); w.Code != http.StatusNotImplemented {
		t.Errorf("status %d", w.Code)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestReceivablesHandler_TotalAndWalkInNames(t *testing.T) {
	repo := &stubReceivablesRepo{rows: []appbill.PartnerReceivable{{Unpaid: decimal.RequireFromString("30")}}}
	h := newTestHandler(&mockBillRepo{}).WithReceivables(appbill.NewReceivablesUseCase(repo))
	w := getReceivables(receivablesRouter(h), "")
	var body struct {
		Items []map[string]any `json:"items"`
		Total map[string]any   `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Items[0]["partner_name"] != "未指定往来单位" || body.Total["partner_name"] != "合计" {
		t.Fatalf("items=%v total=%v", body.Items, body.Total)
	}
}
