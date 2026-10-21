package product_test

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	handlerproduct "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/product"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appproduct "github.com/hanmahong5-arch/lurus-tally/internal/app/product"
	"github.com/hanmahong5-arch/lurus-tally/internal/domain/team"
)

type stubImporter struct {
	req appproduct.ImportRequest
	res *appproduct.ImportResult
	err error
}

func (s *stubImporter) Execute(_ context.Context, req appproduct.ImportRequest) (*appproduct.ImportResult, error) {
	s.req = req
	return s.res, s.err
}

func importRouter(imp handlerproduct.ProductImporter) *gin.Engine {
	h := newPricedHandler(newMockProductRepo(), &recordingSKUWriter{}).WithImporter(imp)
	r := newProductRouter(h)
	h.RegisterImportRoutes(r.Group("/api/v1"))
	return r
}

func upload(t *testing.T, r http.Handler, query, content string, withTenant bool) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if content != "" {
		fw, _ := mw.CreateFormFile("file", "商品.csv")
		_, _ = fw.Write([]byte(content))
	}
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/products/import"+query, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if withTenant {
		req.Header.Set("X-Tenant-ID", testTenantID)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestImport_RouteAbsentWithoutImporter(t *testing.T) {
	h := newPricedHandler(newMockProductRepo(), &recordingSKUWriter{})
	r := newProductRouter(h)
	h.RegisterImportRoutes(r.Group("/api/v1"))
	if w := upload(t, r, "", "x", true); w.Code != http.StatusNotFound {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestImport_NoTenant401(t *testing.T) {
	r := importRouter(&stubImporter{res: &appproduct.ImportResult{}})
	w := upload(t, r, "", "x", false)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "请先登录") || strings.Contains(w.Body.String(), "tenant_id") {
		t.Fatalf("401 must be plain Chinese: %s", w.Body.String())
	}
}

func TestImport_MissingFile400InChinese(t *testing.T) {
	r := importRouter(&stubImporter{res: &appproduct.ImportResult{}})
	w := upload(t, r, "", "", true)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "请选择") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestImport_DryRunFlagAndBytesPassedThrough(t *testing.T) {
	imp := &stubImporter{res: &appproduct.ImportResult{DryRun: true, Total: 1, Valid: 1}}
	r := importRouter(imp)
	w := upload(t, r, "?dry_run=true", "名称*\n螺丝\n", true)
	if w.Code != http.StatusOK || !imp.req.DryRun || string(imp.req.Data) != "名称*\n螺丝\n" {
		t.Fatalf("status=%d req=%+v", w.Code, imp.req)
	}
	imp.req = appproduct.ImportRequest{}
	upload(t, r, "", "名称*\n螺丝\n", true)
	if imp.req.DryRun {
		t.Fatal("without dry_run the import must commit")
	}
}

func TestImport_FileLevelErrorIsPlain400_OthersAre500(t *testing.T) {
	r := importRouter(&stubImporter{err: &appproduct.ImportError{Msg: "表格里没有商品数据"}})
	w := upload(t, r, "", "x", true)
	if w.Code != http.StatusBadRequest || w.Body.String() != `{"error":"表格里没有商品数据"}` {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	r = importRouter(&stubImporter{err: errors.New("db down")})
	w = upload(t, r, "", "x", true)
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "db down") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func importRouterAs(imp handlerproduct.ProductImporter, role team.Role) *gin.Engine {
	h := newPricedHandler(newMockProductRepo(), &recordingSKUWriter{}).WithImporter(imp)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		tid, _ := uuid.Parse(testTenantID)
		c.Set(middleware.CtxKeyTenantID, tid)
		c.Set(middleware.CtxKeyShopRole, role)
	})
	h.RegisterImportRoutes(r.Group("/api/v1"))
	return r
}

// A role without cost.view must not set purchase price or opening cost through
// the import; the sheet is refused up front, in Chinese.
func TestImport_CostColumnsRefusedWithoutCostView(t *testing.T) {
	// The last three are read as headers by the importer too: a space before the
	// star, and a blank (LF or CRLF) first line that encoding/csv skips.
	for _, head := range []string{
		"名称*,进价\n", "名称*,期初数量,期初成本\n",
		"名称 *,进价 *\n", "\n名称,期初数量,期初成本\n", "\r\n名称,期初数量,期初成本\r\n",
	} {
		imp := &stubImporter{res: &appproduct.ImportResult{}}
		w := upload(t, importRouterAs(imp, team.RoleClerk), "", head+"螺丝,5\n", true)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "进价") {
			t.Fatalf("head %q: status=%d body=%s", head, w.Code, w.Body.String())
		}
		if len(imp.req.Data) != 0 {
			t.Fatalf("head %q: importer must not run", head)
		}
	}
	imp := &stubImporter{res: &appproduct.ImportResult{}}
	if w := upload(t, importRouterAs(imp, team.RoleClerk), "", "名称*,零售价\n螺丝,5\n", true); w.Code != http.StatusOK {
		t.Fatalf("sheet without cost columns: status=%d", w.Code)
	}
	imp = &stubImporter{res: &appproduct.ImportResult{}}
	if w := upload(t, importRouterAs(imp, team.RoleOwner), "", "名称*,进价\n螺丝,5\n", true); w.Code != http.StatusOK {
		t.Fatalf("owner: status=%d", w.Code)
	}
}
