package product

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/planlimit"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appproduct "github.com/hanmahong5-arch/lurus-tally/internal/app/product"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

// maxImportBytes caps the upload; MaxImportRows rows of CSV are far below this.
const maxImportBytes = 10 * 1024 * 1024

// ProductImporter is the narrow use-case interface the import endpoint needs.
type ProductImporter interface {
	Execute(ctx context.Context, req appproduct.ImportRequest) (*appproduct.ImportResult, error)
}

// WithImporter enables POST /products/import.
func (h *Handler) WithImporter(imp ProductImporter) *Handler {
	h.importer = imp
	return h
}

// RegisterImportRoutes mounts the import route onto rg (the /api/v1 group). The route
// lives next to, not inside, the /products group built by router.New, so the composition
// root hands this method to the stock handler as an extra-routes hook.
func (h *Handler) RegisterImportRoutes(rg *gin.RouterGroup) {
	if h.importer != nil {
		rg.POST("/products/import", h.Import)
	}
}

// Import handles POST /api/v1/products/import (multipart: file=CSV).
// With dry_run=true (query or form) it only validates and reports, per row, in Chinese;
// otherwise it imports every valid row and reports the rest.
func (h *Handler) Import(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "message": "请先登录"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxImportBytes)
	fh, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择要导入的 CSV 文件（文件不能超过 10MB）"})
		return
	}
	f, err := fh.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件无法打开，请重新选择"})
		return
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(f)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件读取失败，请重新选择"})
		return
	}

	if !middleware.CanViewCost(c) && appproduct.HasCostColumns(data) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "你的角色不能导入“进价”和“期初成本”，请删掉这两列后重新上传"})
		return
	}

	dry := c.Query("dry_run") == "true" || c.PostForm("dry_run") == "true"
	res, err := h.importer.Execute(c.Request.Context(), appproduct.ImportRequest{TenantID: tenantID, Data: data, DryRun: dry})
	if err != nil {
		if planlimit.Write(c, err) {
			return
		}
		var ie *appproduct.ImportError
		if errors.As(err, &ie) {
			c.JSON(http.StatusBadRequest, gin.H{"error": ie.Msg})
			return
		}
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}
