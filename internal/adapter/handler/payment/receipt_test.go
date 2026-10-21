package payment

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	apppayment "github.com/hanmahong5-arch/lurus-tally/internal/app/payment"
)

func receiptRouter(h *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxKeyTenantID, uuid.New()); c.Next() })
	h.RegisterRoutes(r.Group("/api/v1"))
	return r
}

func TestReceiptRoutes_OnlyWhenEnabled(t *testing.T) {
	plain := receiptRouter(New(nil, nil))
	w := httptest.NewRecorder()
	plain.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/receipts", strings.NewReader("{}")))
	if w.Code != http.StatusNotFound {
		t.Errorf("without receipts wired: code = %d, want 404", w.Code)
	}
	wired := receiptRouter(New(nil, nil).WithReceipts(apppayment.NewReceiptUseCase(nil, nil, nil), apppayment.NewStatementUseCase(nil)))
	w = httptest.NewRecorder()
	wired.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/receipts", strings.NewReader("{}")))
	if w.Code != http.StatusBadRequest {
		t.Errorf("wired, empty body: code = %d, want 400", w.Code)
	}
}

func TestCreateReceipt_RejectsBadInputInChinese(t *testing.T) {
	r := receiptRouter(New(nil, nil).WithReceipts(apppayment.NewReceiptUseCase(nil, nil, nil), apppayment.NewStatementUseCase(nil)))
	p := uuid.NewString()
	cases := map[string]string{
		`{"direction":"x","partner_id":"` + p + `","amount":"1"}`:      "收款还是付款",
		`{"direction":"ar","partner_id":"nope","amount":"1"}`:          "客户或供应商",
		`{"direction":"ar","partner_id":"` + p + `","amount":"0"}`:     "大于 0",
		`{"direction":"ar","partner_id":"` + p + `","amount":"1.234"}`: "两位小数",
		`{"direction":"ar","partner_id":"` + p + `","amount":"1e2"}`:   "大于 0",
	}
	for body, want := range cases {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/receipts", strings.NewReader(body)))
		var out map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		if w.Code != http.StatusBadRequest || !strings.Contains(out["message"], want) {
			t.Errorf("%s: code=%d msg=%q, want 400 containing %q", body, w.Code, out["message"], want)
		}
	}
}

func TestWriteReceiptError_ExceedsDebtIs422(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	writeReceiptError(c, &apppayment.ExceedsPartnerDebtError{Amount: decimal.RequireFromString("700"), Owed: decimal.RequireFromString("600")})
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "该客户只欠 ¥600.00") {
		t.Errorf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCSVText_NeutralisesFormulas(t *testing.T) {
	for in, want := range map[string]string{"=1+1": "'=1+1", "@x": "'@x", "-5": "'-5", "货款": "货款", "": ""} {
		if got := csvText(in); got != want {
			t.Errorf("csvText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStatementDisposition_ChineseName(t *testing.T) {
	st := &apppayment.Statement{PartnerName: "老王/五金", From: time.Date(2026, 10, 1, 0, 0, 0, 0, businessZone), To: time.Date(2026, 10, 31, 0, 0, 0, 0, businessZone)}
	got := statementDisposition(st)
	i := strings.Index(got, "filename*=UTF-8''")
	if i < 0 {
		t.Fatalf("no RFC 5987 name in %q", got)
	}
	name, err := url.PathUnescape(got[i+len("filename*=UTF-8''"):])
	if err != nil || name != "对账单-老王_五金-20261001-20261031.csv" {
		t.Errorf("name = %q (%v)", name, err)
	}
}

func TestWriteStatementCSV_PurchaseReturnOffsetLabel(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	writeStatementCSV(c, &apppayment.Statement{Direction: apppayment.DirectionPayable, PartnerName: "供货商",
		Lines: []apppayment.StatementLine{{Kind: apppayment.LineReturn, BillNo: "PO-1", Delta: decimal.NewFromInt(-5)}}})
	if !strings.Contains(w.Body.String(), "退货冲抵") || strings.Contains(w.Body.String(), "赊账") {
		t.Errorf("csv = %q", w.Body.String())
	}
}
