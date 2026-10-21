package account_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	handleracct "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/account"
	appacct "github.com/hanmahong5-arch/lurus-tally/internal/app/account"
)

type memPrintRepo struct{ s appacct.PrintSettings }

func (m *memPrintRepo) GetPrint(context.Context, uuid.UUID) (appacct.PrintSettings, error) {
	return m.s, nil
}
func (m *memPrintRepo) PutPrint(_ context.Context, _ uuid.UUID, s appacct.PrintSettings) error {
	m.s = s
	return nil
}

func printRouter(repo *memPrintRepo, tenant uuid.UUID) *gin.Engine {
	h := handleracct.New(nil, nil, nil, nil, nil, nil, nil).
		WithPrintSettings(appacct.NewGetPrintSettings(repo), appacct.NewPutPrintSettings(repo))
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("tenant_id", tenant); c.Next() })
	h.RegisterRoutes(r.Group("/api/v1"))
	return r
}

func TestPrintSettings_PutThenGet(t *testing.T) {
	repo := &memPrintRepo{}
	r := printRouter(repo, uuid.New())

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/tenant/print-settings",
		strings.NewReader(`{"shop_name":"老王五金","phone":"138","address":"街1号","receipt_footer":"谢谢"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("put: %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/tenant/print-settings", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"shop_name":"老王五金"`) {
		t.Fatalf("get: %d %s", w.Code, w.Body.String())
	}
}

func TestPrintSettings_TooLongIs400(t *testing.T) {
	r := printRouter(&memPrintRepo{}, uuid.New())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/tenant/print-settings",
		strings.NewReader(`{"shop_name":"`+strings.Repeat("a", 100)+`"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestPrintSettings_NoTenantIs401(t *testing.T) {
	r := printRouter(&memPrintRepo{}, uuid.Nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/tenant/print-settings", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

type missingTenantRepo struct{}

func (missingTenantRepo) GetPrint(context.Context, uuid.UUID) (appacct.PrintSettings, error) {
	return appacct.PrintSettings{}, appacct.ErrNotFound
}
func (missingTenantRepo) PutPrint(context.Context, uuid.UUID, appacct.PrintSettings) error {
	return appacct.ErrNotFound
}

func TestPrintSettings_UnknownTenantIs404InChinese(t *testing.T) {
	h := handleracct.New(nil, nil, nil, nil, nil, nil, nil).
		WithPrintSettings(appacct.NewGetPrintSettings(missingTenantRepo{}), appacct.NewPutPrintSettings(missingTenantRepo{}))
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("tenant_id", uuid.New()); c.Next() })
	h.RegisterRoutes(r.Group("/api/v1"))

	for _, method := range []string{http.MethodGet, http.MethodPut} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/api/v1/tenant/print-settings", strings.NewReader(`{"shop_name":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "找不到") {
			t.Fatalf("%s: want 404 with Chinese detail, got %d %s", method, w.Code, w.Body.String())
		}
	}
}
