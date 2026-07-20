// Package billing implements the Gin HTTP handlers Tally exposes for users to
// view their subscription status and trigger one-click subscription checkouts.
// All real billing work is delegated to lurus-platform via platformclient.
package billing

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appbilling "github.com/hanmahong5-arch/lurus-tally/internal/app/billing"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/platformclient"
)

// SubscribeExecutor is the surface satisfied by SubscribeUseCase.
type SubscribeExecutor interface {
	Execute(ctx context.Context, in appbilling.SubscribeInput) (*appbilling.SubscribeOutput, error)
}

// OverviewExecutor is the surface satisfied by OverviewUseCase.
type OverviewExecutor interface {
	Execute(ctx context.Context, idpSubject string) (*platformclient.AccountOverview, error)
}

// Handler groups Tally's billing-facing HTTP routes.
type Handler struct {
	subscribe SubscribeExecutor
	overview  OverviewExecutor
}

// New constructs a Handler.
func New(subscribe SubscribeExecutor, overview OverviewExecutor) *Handler {
	return &Handler{subscribe: subscribe, overview: overview}
}

// RegisterRoutes mounts billing endpoints onto the given router group.
// The group is expected to already be guarded by AuthMiddleware so the
// IdP subject is present in the gin context.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/billing/overview", h.Overview)
	rg.POST("/billing/subscribe", h.Subscribe)
}

// subscribeRequest is the body of POST /api/v1/billing/subscribe.
// payment_method must be one of: wallet, alipay, wechat, stripe.
type subscribeRequest struct {
	PlanCode      string `json:"plan_code"      binding:"required"`
	BillingCycle  string `json:"billing_cycle"  binding:"required"`
	PaymentMethod string `json:"payment_method" binding:"required"`
	ReturnURL     string `json:"return_url"`
}

// subscribeResponse keeps the client contract small and explicit; the frontend
// branches on which field is non-empty.
type subscribeResponse struct {
	OrderNo      string                               `json:"order_no,omitempty"`
	PayURL       string                               `json:"pay_url,omitempty"`
	Subscription *platformclient.SubscriptionSnapshot `json:"subscription,omitempty"`
}

// Subscribe handles POST /api/v1/billing/subscribe.
func (h *Handler) Subscribe(c *gin.Context) {
	sub := callerSub(c)
	if sub == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "detail": "sign-in required"})
		return
	}

	var req subscribeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request", "detail": err.Error()})
		return
	}

	out, err := h.subscribe.Execute(c.Request.Context(), appbilling.SubscribeInput{
		IDPSubject:     sub,
		PlanCode:       req.PlanCode,
		BillingCycle:   req.BillingCycle,
		PaymentMethod:  req.PaymentMethod,
		ReturnURL:      req.ReturnURL,
		IdempotencyKey: c.GetHeader(middleware.HeaderIdempotencyKey),
	})
	if err != nil {
		writePlatformError(c, err)
		return
	}

	c.JSON(http.StatusOK, subscribeResponse{
		OrderNo:      out.OrderNo,
		PayURL:       out.PayURL,
		Subscription: out.Subscription,
	})
}

// Overview handles GET /api/v1/billing/overview.
func (h *Handler) Overview(c *gin.Context) {
	sub := callerSub(c)
	if sub == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "detail": "sign-in required"})
		return
	}
	ov, err := h.overview.Execute(c.Request.Context(), sub)
	if err != nil {
		writePlatformError(c, err)
		return
	}
	c.JSON(http.StatusOK, ov)
}

// callerSub returns the IdP subject that AuthMiddleware injected into the
// context from a cryptographically-verified OIDC token (CtxKeyIDPSubject).
// It deliberately trusts ONLY the context value and NEVER a request header:
// billing acts on a platform account keyed by this subject, so honouring a
// client-supplied X-IDP-Subject would let any authenticated caller check out /
// read a victim's account. PAT-authenticated requests carry no subject (a PAT
// is tenant-scoped, not identity-scoped) and therefore get "" here → the
// handlers reject them with 401. In dev, the TALLY_DEV_MODE middleware shim
// injects the subject into the context itself (never from prod), so local
// testing keeps working without any header fallback in this path.
func callerSub(c *gin.Context) string {
	return middleware.GetIDPSubject(c)
}

// writePlatformError translates use-case / platform errors into HTTP responses
// with stable error codes the frontend can branch on.
func writePlatformError(c *gin.Context, err error) {
	if errors.Is(err, appbilling.ErrUnauthenticated) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "detail": err.Error()})
		return
	}
	if errors.Is(err, appbilling.ErrInvalidInput) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request", "detail": err.Error()})
		return
	}
	var pe *platformclient.Error
	if errors.As(err, &pe) {
		switch pe.Code {
		case platformclient.ErrCodeInsufficientBalance:
			c.JSON(http.StatusPaymentRequired, gin.H{
				"error":   "insufficient_balance",
				"detail":  "wallet balance is insufficient — top up first or pick another method",
				"message": pe.Message,
			})
			return
		case platformclient.ErrCodeNotFound:
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "detail": pe.Message})
			return
		case platformclient.ErrCodeInvalidParameter:
			c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request", "detail": pe.Message})
			return
		case platformclient.ErrCodeUnauthorized:
			c.JSON(http.StatusBadGateway, gin.H{
				"error":  "platform_auth_failed",
				"detail": "Tally cannot authenticate to billing platform — operator action required",
			})
			return
		case platformclient.ErrCodeUnavailable:
			c.JSON(http.StatusBadGateway, gin.H{
				"error":  "platform_unavailable",
				"detail": "billing platform is unreachable; try again shortly",
			})
			return
		}
	}
	httperr.WriteInternal(c, err)
}
