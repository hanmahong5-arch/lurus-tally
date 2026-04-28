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
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/platformclient"
)

// SubscribeExecutor is the surface satisfied by SubscribeUseCase.
type SubscribeExecutor interface {
	Execute(ctx context.Context, in appbilling.SubscribeInput) (*appbilling.SubscribeOutput, error)
}

// OverviewExecutor is the surface satisfied by OverviewUseCase.
type OverviewExecutor interface {
	Execute(ctx context.Context, zitadelSub string) (*platformclient.AccountOverview, error)
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
// Zitadel sub is present in the gin context.
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
		ZitadelSub:    sub,
		PlanCode:      req.PlanCode,
		BillingCycle:  req.BillingCycle,
		PaymentMethod: req.PaymentMethod,
		ReturnURL:     req.ReturnURL,
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

// callerSub reads the Zitadel sub injected by the AuthMiddleware. In dev
// mode the middleware is bypassed and the X-Zitadel-Sub header is honoured
// so the integration is testable without a real OIDC flow.
func callerSub(c *gin.Context) string {
	if v, ok := c.Get(middleware.CtxKeyZitadelSub); ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return c.GetHeader("X-Zitadel-Sub")
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
	c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "detail": err.Error()})
}
