package importing_test

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	handlerimporting "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/importing"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appimporting "github.com/hanmahong5-arch/lurus-tally/internal/app/importing"
)

// fakeImportUC lets us drive the handler's error branch without a real DB.
type fakeImportUC struct {
	execErr error
}

func (f *fakeImportUC) Execute(_ context.Context, _ appimporting.ImportRequest) (*appimporting.ImportResult, error) {
	if f.execErr != nil {
		return nil, f.execErr
	}
	return &appimporting.ImportResult{}, nil
}

func (f *fakeImportUC) ListMappings(_ context.Context, _ uuid.UUID, _ string) ([]appimporting.SKUMapping, error) {
	return nil, nil
}

// newRouter mounts the import handler under /api/v1 with the tenant pre-seeded
// in the gin context, mimicking what AuthMiddleware does after JWT resolution.
func newRouter(uc handlerimporting.ImportUseCase, tenant uuid.UUID) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	grp := r.Group("/api/v1")
	grp.Use(func(c *gin.Context) {
		c.Set(middleware.CtxKeyTenantID, tenant)
		c.Next()
	})
	handlerimporting.New(uc, uuid.Nil).RegisterRoutes(grp)
	return r
}

func multipartImport(t *testing.T, platform, warehouse string) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	_ = w.WriteField("platform", platform)
	_ = w.WriteField("warehouse", warehouse)
	fw, err := w.CreateFormFile("file", "orders.csv")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	_, _ = fw.Write([]byte("order_no,sku,qty\nA-1,SKU-1,2\n"))
	if err := w.Close(); err != nil {
		t.Fatalf("multipart close: %v", err)
	}
	return body, w.FormDataContentType()
}

// TestImportOrders_UseCaseError_Returns422 is a regression guard for the
// cross-tenant warehouse defence (Swim A): when the use case rejects the import
// (e.g. WarehouseChecker.BelongsToTenant fails), the handler must surface 422,
// never silently succeed.
func TestImportOrders_UseCaseError_Returns422(t *testing.T) {
	tenant := uuid.New()
	uc := &fakeImportUC{execErr: errors.New("warehouse does not belong to tenant")}
	r := newRouter(uc, tenant)

	body, contentType := multipartImport(t, "amazon", uuid.New().String())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/imports/orders", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

// TestImportOrders_NoTenant_Returns401 confirms the handler refuses an
// unauthenticated request (no tenant in context) before touching the use case.
func TestImportOrders_NoTenant_Returns401(t *testing.T) {
	uc := &fakeImportUC{}
	r := newRouter(uc, uuid.Nil)

	body, contentType := multipartImport(t, "amazon", uuid.New().String())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/imports/orders", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}
