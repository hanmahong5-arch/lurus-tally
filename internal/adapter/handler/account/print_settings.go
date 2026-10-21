package account

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appacct "github.com/hanmahong5-arch/lurus-tally/internal/app/account"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

// WithPrintSettings wires the tenant print-settings use cases. Without it the
// /tenant/print-settings routes are not mounted.
func (h *Handler) WithPrintSettings(get *appacct.GetPrintSettings, put *appacct.PutPrintSettings) *Handler {
	h.getPrint = get
	h.putPrint = put
	return h
}

// GetPrintSettings returns the shop header / receipt footer for printing.
func (h *Handler) GetPrintSettings(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	s, err := h.getPrint.Execute(c.Request.Context(), tenantID)
	if err != nil {
		if errors.Is(err, appacct.ErrNotFound) {
			writePrintTenantMissing(c)
			return
		}
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, s)
}

// writePrintTenantMissing answers a request whose tenant row does not exist.
func writePrintTenantMissing(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "detail": "找不到当前店铺的资料，请重新登录后再试"})
}

// PutPrintSettings replaces the shop header / receipt footer.
func (h *Handler) PutPrintSettings(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req appacct.PrintSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error", "detail": "请求内容不正确"})
		return
	}
	s, err := h.putPrint.Execute(c.Request.Context(), tenantID, req)
	if err != nil {
		if errors.Is(err, appacct.ErrPrintSettingsInvalid) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error", "detail": "内容不符合要求（过长，或打印机编号、密钥、纸宽填写有误），请检查后再保存"})
			return
		}
		if errors.Is(err, appacct.ErrPrinterBindFailed) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "printer_bind_failed", "detail": "绑定打印机失败，请核对机身标签上的编号和密钥，并确认打印机已联网"})
			return
		}
		if errors.Is(err, appacct.ErrNotFound) {
			writePrintTenantMissing(c)
			return
		}
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, s)
}
