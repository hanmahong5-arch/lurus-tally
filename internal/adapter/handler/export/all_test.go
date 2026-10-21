package export_test

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	handlerexport "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/export"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	"github.com/hanmahong5-arch/lurus-tally/internal/domain/team"
)

type stubAll struct{ gotCost *bool }

func (s *stubAll) Execute(_ context.Context, _ uuid.UUID, showCost bool, _ time.Time, w io.Writer) (int, error) {
	s.gotCost = &showCost
	zw := zip.NewWriter(w)
	f, _ := zw.Create("商品.csv")
	_, _ = f.Write([]byte("\xEF\xBB\xBF编码,名称\n"))
	return 0, zw.Close()
}

func allEngine(tenantID uuid.UUID, role team.Role, a handlerexport.AllExporter) *gin.Engine {
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(func(c *gin.Context) {
		c.Set(middleware.CtxKeyTenantID, tenantID)
		c.Set(middleware.CtxKeyShopRole, role)
		c.Next()
	})
	api.Use(middleware.EnforceRoutePerms())
	h := handlerexport.New(&stubExporter{}, &stubExporter{}, &stubExporter{}, nil)
	if a != nil {
		h.WithAllExporter(a)
	}
	h.RegisterRoutes(api)
	return r
}

func getAll(r *gin.Engine) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/exports/all.zip", nil))
	return w
}

func TestAllZip_ClerkForbidden(t *testing.T) {
	st := &stubAll{}
	w := getAll(allEngine(uuid.New(), team.RoleClerk, st))
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d", w.Code)
	}
	if st.gotCost != nil {
		t.Fatal("exporter ran for a clerk")
	}
}

func TestAllZip_OwnerGetsZip(t *testing.T) {
	st := &stubAll{}
	w := getAll(allEngine(uuid.New(), team.RoleOwner, st))
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("status=%d ct=%s", w.Code, w.Header().Get("Content-Type"))
	}
	if _, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len())); err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	if st.gotCost == nil || !*st.gotCost {
		t.Fatal("owner should see cost")
	}
}

func TestAllZip_NotConfigured501(t *testing.T) {
	w := getAll(allEngine(uuid.New(), team.RoleOwner, nil))
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d", w.Code)
	}
}
