package account

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appacct "github.com/hanmahong5-arch/lurus-tally/internal/app/account"
	domainauth "github.com/hanmahong5-arch/lurus-tally/internal/domain/auth"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

// WithCashierMode wires the cashier-mode use cases. Without it the
// /tenant/cashier-mode routes are not mounted.
func (h *Handler) WithCashierMode(get *appacct.GetCashierMode, set *appacct.SetCashierMode) *Handler {
	h.getCashier = get
	h.setCashier = set
	return h
}

// GetCashierMode returns whether the shop has set a cashier PIN.
func (h *Handler) GetCashierMode(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	st, err := h.getCashier.Execute(c.Request.Context(), tenantID)
	if err != nil {
		writeCashierErr(c, err)
		return
	}
	c.JSON(http.StatusOK, cashierStateBody{HasPin: st.HasPin, TenantID: tenantID.String()})
}

// cashierStateBody is the client view: whether a PIN exists, plus the shop id the
// device uses to key its own on/off flag. The mode itself is never stored here.
type cashierStateBody struct {
	HasPin   bool   `json:"has_pin"`
	TenantID string `json:"tenant_id"`
}

// cashierModeRequest is the PIN body. A legacy "enabled" field is accepted and ignored.
type cashierModeRequest struct {
	PIN string `json:"pin"`
}

// PutCashierMode stores the shop's cashier PIN (first time) or confirms it.
func (h *Handler) PutCashierMode(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req cashierModeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error", "detail": "请求内容不正确"})
		return
	}
	st, err := h.setCashier.Execute(c.Request.Context(), tenantID, pinActor(c), req.PIN)
	if err != nil {
		writeCashierErr(c, err)
		return
	}
	c.JSON(http.StatusOK, cashierStateBody{HasPin: st.HasPin, TenantID: tenantID.String()})
}

// VerifyCashierPin checks the PIN only; a device uses it to leave cashier mode.
func (h *Handler) VerifyCashierPin(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req cashierModeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error", "detail": "请求内容不正确"})
		return
	}
	if err := h.setCashier.Verify(c.Request.Context(), tenantID, pinActor(c), req.PIN); err != nil {
		writeCashierErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// pinActor names who is guessing, so failures are counted per person: the OIDC
// subject, or for a personal access token (no subject) the token's public id.
func pinActor(c *gin.Context) string {
	if sub := middleware.GetIDPSubject(c); sub != "" {
		return sub
	}
	if tok, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer "); ok {
		if prefix, _, ok := domainauth.ParseBearer(tok); ok {
			return "pat:" + prefix
		}
	}
	return ""
}

func writeCashierErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, appacct.ErrPinInvalid):
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error", "detail": "密码需要是 4 到 6 位数字"})
	case errors.Is(err, appacct.ErrPinWrong):
		c.JSON(http.StatusForbidden, gin.H{"error": "pin_wrong", "detail": "密码不对，请再试一次"})
	case errors.Is(err, appacct.ErrCashierNotRetail):
		c.JSON(http.StatusConflict, gin.H{"error": "cashier_not_retail", "detail": "收银模式只适合零售店使用"})
	case errors.Is(err, appacct.ErrPinNotSet):
		c.JSON(http.StatusConflict, gin.H{"error": "pin_not_set", "detail": "还没有设置收银密码"})
	case errors.Is(err, appacct.ErrPinAlreadySet):
		c.JSON(http.StatusConflict, gin.H{"error": "pin_already_set", "detail": "密码已被设置，请用已设的密码"})
	case errors.Is(err, appacct.ErrPinLocked):
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "pin_locked", "detail": "输错次数太多，请 5 分钟后再试"})
	case errors.Is(err, appacct.ErrNotFound):
		writePrintTenantMissing(c)
	default:
		httperr.WriteInternal(c, err)
	}
}
