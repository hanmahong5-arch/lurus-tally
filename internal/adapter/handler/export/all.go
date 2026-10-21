package export

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	repobill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/bill"
	appexport "github.com/hanmahong5-arch/lurus-tally/internal/app/export"
)

// AllExporter writes every table of one shop into a zip on w. showCost=false
// leaves the purchase price, cost and purchase amount columns out entirely.
type AllExporter interface {
	Execute(ctx context.Context, tenantID uuid.UUID, showCost bool, now time.Time, w io.Writer) (int, error)
}

// WithAllExporter enables GET /exports/all.zip and returns h.
func (h *Handler) WithAllExporter(a AllExporter) *Handler {
	h.all = a
	return h
}

// All handles GET /api/v1/exports/all.zip. The zip is written straight to the
// response on the request goroutine, one page of one table at a time.
func (h *Handler) All(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "message": "请先登录"})
		return
	}
	if h.all == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "not_implemented", "message": "暂不支持导出全部数据"})
		return
	}
	now := time.Now()
	filename := fmt.Sprintf("tally-all-%s.zip", repobill.BillNoDay(now))
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
	reqID := middleware.GetRequestID(c)
	ctx := appexport.WithRequestID(c.Request.Context(), reqID)
	if _, err := h.all.Execute(ctx, tenantID, middleware.CanViewCost(c), now, c.Writer); err != nil {
		// Headers are already sent: the zip carries 导出失败.txt and 完整性：失败.
		h.log.Error("export: all.zip generation error",
			slog.String("tenant_id", tenantID.String()),
			slog.String("request_id", reqID),
			slog.String("error", err.Error()))
	}
}
