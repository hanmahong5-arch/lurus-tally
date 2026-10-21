package customer

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	"github.com/hanmahong5-arch/lurus-tally/internal/app/partnerimport"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

// maxImportBytes caps the upload; MaxRows rows of CSV are far below this.
const maxImportBytes = 5 * 1024 * 1024

// Importer is the narrow use-case interface the import endpoint needs.
type Importer interface {
	Execute(ctx context.Context, tenantID uuid.UUID, data []byte) (*partnerimport.Result, error)
}

// WithImporter enables POST /customers/import and the template download.
func (h *Handler) WithImporter(imp Importer) *Handler {
	h.importer = imp
	return h
}

// ImportTemplate handles GET /api/v1/customers/import/template.csv. The file holds no shop data.
func (h *Handler) ImportTemplate(c *gin.Context) {
	c.Header("Content-Disposition", "attachment; filename=\"template.csv\"")
	c.Data(http.StatusOK, "text/csv; charset=utf-8", []byte(partnerimport.Customers.Template()))
}

// Import handles POST /api/v1/customers/import (multipart: file=CSV, UTF-8 or GBK).
// Replies {created, skipped, errors:[{row, message}]}.
func (h *Handler) Import(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "请先登录"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxImportBytes)
	fh, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择要导入的 CSV 文件（文件不能超过 5MB）"})
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
	res, err := h.importer.Execute(c.Request.Context(), tenantID, data)
	if err != nil {
		var ie *partnerimport.ImportError
		if errors.As(err, &ie) {
			c.JSON(http.StatusBadRequest, gin.H{"error": ie.Msg})
			return
		}
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}
