// Package router wires all HTTP routes onto a Gin engine.
package router

import (
	"net/http"

	"github.com/gin-gonic/gin"
	handlerai "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/ai"
	handlerAuth "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/auth"
	handlerbill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/bill"
	handlerbilling "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/billing"
	handlercurrency "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/currency"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/health"
	handlerpayment "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/payment"
	handlerproduct "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/product"
	handlerstock "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/stock"
	handlerunit "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/unit"
)

// notImplemented is a placeholder handler returned when a handler struct is nil.
// This allows the router to register routes for integration testing even when
// handler dependencies (DB) are not available.
func notImplemented(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "handler not configured"})
}

// New creates and configures a Gin engine with all registered routes.
// All handler pointers may be nil in test environments; routes will still be registered
// and respond with 501 instead of panicking.
// authMW may be nil — in that case the /api/v1 group has no auth middleware
// and handlers will see no sub/tenant_id in context (returns 401).
// The engine mode (release/debug) is controlled by GIN_MODE or gin.SetMode.
//
//nolint:cyclop // router wiring is intentionally long
func New(h *health.Handler, authMW gin.HandlerFunc, ph *handlerproduct.Handler, uh *handlerunit.Handler, ah *handlerAuth.Handler, sh *handlerstock.Handler, bh *handlerbill.Handler, ch *handlercurrency.Handler, saleh *handlerbill.SaleHandler, payh *handlerpayment.Handler, bilh *handlerbilling.Handler, aih *handlerai.Handler) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())

	internal := r.Group("/internal/v1/tally")
	{
		internal.GET("/health", h.Healthz)
		internal.GET("/ready", h.Readyz)
	}

	// API v1 — business endpoints.
	api := r.Group("/api/v1")
	if authMW != nil {
		api.Use(authMW)
	}
	{
		// Auth + tenant profile routes (Story 2.1).
		// Production setup: AuthMiddleware is applied at the lifecycle layer before
		// these routes are reached. In development, handlers accept X-Tenant-ID header.
		if ah != nil {
			ah.RegisterRoutes(api)
		} else {
			api.GET("/me", notImplemented)
			api.POST("/tenant/profile", notImplemented)
			api.POST("/auth/logout", notImplemented)
		}

		products := api.Group("/products")
		{
			products.GET("", productHandler(ph, (*handlerproduct.Handler).List))
			products.POST("", productHandler(ph, (*handlerproduct.Handler).Create))
			products.GET("/:id", productHandler(ph, (*handlerproduct.Handler).GetByID))
			products.PUT("/:id", productHandler(ph, (*handlerproduct.Handler).Update))
			products.DELETE("/:id", productHandler(ph, (*handlerproduct.Handler).Delete))
		}

		units := api.Group("/units")
		{
			units.GET("", unitHandler(uh, (*handlerunit.Handler).List))
			units.POST("", unitHandler(uh, (*handlerunit.Handler).Create))
			units.DELETE("/:id", unitHandler(uh, (*handlerunit.Handler).Delete))
		}

		// Purchase bill routes (Story 6.1).
		if bh != nil {
			bh.RegisterRoutes(api)
		} else {
			// Stub routes so integration tests can verify registration.
			api.POST("/purchase-bills", notImplemented)
			api.PUT("/purchase-bills/:id", notImplemented)
			api.POST("/purchase-bills/:id/approve", notImplemented)
			api.POST("/purchase-bills/:id/cancel", notImplemented)
			api.GET("/purchase-bills", notImplemented)
			api.GET("/purchase-bills/:id", notImplemented)
		}

		// Currency and exchange rate routes (Story 9.1).
		if ch != nil {
			ch.RegisterRoutes(api)
		} else {
			api.GET("/currencies", notImplemented)
			api.GET("/exchange-rates", notImplemented)
			api.POST("/exchange-rates", notImplemented)
			api.GET("/exchange-rates/history", notImplemented)
		}

		// Sale bill routes (Story 7.1).
		if saleh != nil {
			saleh.RegisterRoutes(api)
		} else {
			api.POST("/sale-bills/quick-checkout", notImplemented)
			api.POST("/sale-bills", notImplemented)
			api.PUT("/sale-bills/:id", notImplemented)
			api.POST("/sale-bills/:id/approve", notImplemented)
			api.POST("/sale-bills/:id/cancel", notImplemented)
			api.GET("/sale-bills", notImplemented)
			api.GET("/sale-bills/:id", notImplemented)
		}

		// Payment routes (Story 7.1).
		if payh != nil {
			payh.RegisterRoutes(api)
		} else {
			api.POST("/payments", notImplemented)
			api.GET("/payments", notImplemented)
		}

		// Billing routes — Tally → platform subscription checkout (Story 10.1).
		if bilh != nil {
			bilh.RegisterRoutes(api)
		} else {
			api.GET("/billing/overview", notImplemented)
			api.POST("/billing/subscribe", notImplemented)
		}

		// AI assistant routes (Story 11.1: AI Drawer + ⌘K palette).
		if aih != nil {
			aih.RegisterRoutes(api)
		} else {
			api.POST("/ai/chat", notImplemented)
			api.POST("/ai/plans/:plan_id/confirm", notImplemented)
			api.POST("/ai/plans/:plan_id/cancel", notImplemented)
		}
	}

	return r
}

// productHandler returns a gin.HandlerFunc that delegates to the method on h,
// or notImplemented if h is nil.
func productHandler(h *handlerproduct.Handler, fn func(*handlerproduct.Handler, *gin.Context)) gin.HandlerFunc {
	if h == nil {
		return notImplemented
	}
	return func(c *gin.Context) { fn(h, c) }
}

// unitHandler returns a gin.HandlerFunc that delegates to the method on h,
// or notImplemented if h is nil.
func unitHandler(h *handlerunit.Handler, fn func(*handlerunit.Handler, *gin.Context)) gin.HandlerFunc {
	if h == nil {
		return notImplemented
	}
	return func(c *gin.Context) { fn(h, c) }
}
