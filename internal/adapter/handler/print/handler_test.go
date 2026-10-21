package print_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	handler "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/print"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appprint "github.com/hanmahong5-arch/lurus-tally/internal/app/print"
)

type fakeUC struct {
	err  error
	info appprint.StatusInfo
}

func (f fakeUC) PrintReceipt(context.Context, uuid.UUID, uuid.UUID, int) (string, error) {
	return "ord-1", f.err
}
func (f fakeUC) PrintTest(context.Context, uuid.UUID) (string, error) { return "ord-2", f.err }
func (f fakeUC) Status(context.Context, uuid.UUID) (appprint.StatusInfo, error) {
	return f.info, f.err
}

func do(t *testing.T, uc fakeUC, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxKeyTenantID, uuid.New()); c.Next() })
	handler.New(uc).RegisterRoutes(r.Group("/api/v1"))
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestPrintReceiptMapping(t *testing.T) {
	body := fmt.Sprintf(`{"bill_id":"%s"}`, uuid.New())
	offline := fmt.Errorf("%w: 离线。", appprint.ErrPrinterOffline)
	cases := []struct {
		err  error
		code int
		key  string
	}{
		{nil, http.StatusOK, `"order_id":"ord-1"`},
		{appprint.ErrPrinterNotConfigured, http.StatusConflict, "printer_not_configured"},
		{offline, http.StatusServiceUnavailable, "printer_offline"},
		{appprint.ErrCloudPrintUnavailable, http.StatusNotImplemented, "cloud_print_unavailable"},
	}
	for _, c := range cases {
		w := do(t, fakeUC{err: c.err}, "POST", "/api/v1/print/receipts", body)
		if w.Code != c.code || !bytes.Contains(w.Body.Bytes(), []byte(c.key)) {
			t.Errorf("err=%v: %d %s", c.err, w.Code, w.Body)
		}
	}
	if w := do(t, fakeUC{err: offline}, "POST", "/api/v1/print/receipts", body); !bytes.Contains(w.Body.Bytes(), []byte("离线。")) {
		t.Errorf("离线响应应带状态文本: %s", w.Body)
	}
}

func TestPrintReceiptValidation(t *testing.T) {
	for _, b := range []string{`{}`, `{"bill_id":"x"}`, fmt.Sprintf(`{"bill_id":"%s","times":4}`, uuid.New()), `not json`} {
		if w := do(t, fakeUC{}, "POST", "/api/v1/print/receipts", b); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", b, w.Code)
		}
	}
}

func TestStatusAndTest(t *testing.T) {
	w := do(t, fakeUC{info: appprint.StatusInfo{Configured: true, Online: true, Status: "在线", CloudPrintAvailable: true}}, "GET", "/api/v1/tenant/printer/status", "")
	for _, k := range []string{`"configured":true`, `"online":true`, `"cloud_print_available":true`, `"status":"在线"`} {
		if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(k)) {
			t.Errorf("%s: %d %s", k, w.Code, w.Body)
		}
	}
	if w := do(t, fakeUC{err: errors.New("x")}, "POST", "/api/v1/tenant/printer/test", ""); w.Code != 500 {
		t.Errorf("test 500: %d", w.Code)
	}
	if w := do(t, fakeUC{err: appprint.ErrPrinterNotConfigured}, "POST", "/api/v1/tenant/printer/test", ""); w.Code != 409 {
		t.Errorf("test 409: %d", w.Code)
	}
}
