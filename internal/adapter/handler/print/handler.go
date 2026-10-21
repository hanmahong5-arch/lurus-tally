// Package print serves the cloud receipt-printing endpoints.
package print

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appprint "github.com/hanmahong5-arch/lurus-tally/internal/app/print"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

// UseCase is what the handler needs from the print use case.
type UseCase interface {
	PrintReceipt(ctx context.Context, tenantID, billID uuid.UUID, times int) (string, error)
	PrintTest(ctx context.Context, tenantID uuid.UUID) (string, error)
	Status(ctx context.Context, tenantID uuid.UUID) (appprint.StatusInfo, error)
}

// Handler serves /print/receipts and /tenant/printer/*.
type Handler struct{ uc UseCase }

// New constructs the handler.
func New(uc UseCase) *Handler { return &Handler{uc: uc} }

// RegisterRoutes mounts the routes on rg (the /api/v1 group). The composition
// root hands this to the stock handler as an extra-routes hook, so router.New
// keeps its signature and the routes still pass auth, tenant and permission
// middleware (see permission.RoutePerm).
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/print/receipts", h.PrintReceipt)
	rg.GET("/tenant/printer/status", h.Status)
	rg.POST("/tenant/printer/test", h.Test)
}

type printReq struct {
	BillID string `json:"bill_id"`
	Times  int    `json:"times"`
}

// PrintReceipt handles POST /api/v1/print/receipts.
func (h *Handler) PrintReceipt(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req printReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error", "detail": "请求内容不正确"})
		return
	}
	billID, err := uuid.Parse(strings.TrimSpace(req.BillID))
	if err != nil || billID == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error", "detail": "单据编号不正确"})
		return
	}
	times := req.Times
	if times == 0 {
		times = 1
	}
	if times < 1 || times > 3 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error", "detail": "份数请填 1 到 3"})
		return
	}
	orderID, err := h.uc.PrintReceipt(c.Request.Context(), tenantID, billID, times)
	if err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"order_id": orderID})
}

// Test handles POST /api/v1/tenant/printer/test.
func (h *Handler) Test(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	orderID, err := h.uc.PrintTest(c.Request.Context(), tenantID)
	if err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"order_id": orderID})
}

// Status handles GET /api/v1/tenant/printer/status.
func (h *Handler) Status(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	info, err := h.uc.Status(c.Request.Context(), tenantID)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"configured":            info.Configured,
		"online":                info.Online,
		"status":                info.Status,
		"cloud_print_available": info.CloudPrintAvailable,
	})
}

func writeErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, appprint.ErrPrinterNotConfigured):
		c.JSON(http.StatusConflict, gin.H{"error": "printer_not_configured", "detail": "还没有设置云打印机，请先到“设置 - 打印”里绑定"})
	case errors.Is(err, appprint.ErrPrinterOffline):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "printer_offline", "detail": "打印机不在线或状态异常：" + offlineText(err)})
	case errors.Is(err, appprint.ErrCloudPrintUnavailable):
		c.JSON(http.StatusNotImplemented, gin.H{"error": "cloud_print_unavailable", "detail": "当前环境未开通云打印"})
	case errors.Is(err, appprint.ErrNotSaleBill):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "not_sale_bill", "detail": "只有销售单可以打印小票"})
	case appprint.IsBillNotFound(err):
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "detail": "找不到这张单据"})
	default:
		httperr.WriteInternal(c, err)
	}
}

// offlineText keeps the vendor status sentence that follows the sentinel.
func offlineText(err error) string {
	t := strings.TrimPrefix(err.Error(), appprint.ErrPrinterOffline.Error())
	if t = strings.TrimSpace(strings.TrimPrefix(t, ":")); t != "" {
		return t
	}
	return "请检查打印机电源和网络"
}
