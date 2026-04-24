// Package router wires all HTTP routes onto a Gin engine.
package router

import (
	"net/http"

	"github.com/gin-gonic/gin"
	handlerAuth "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/auth"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/health"
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
// ph, uh, ah, sh may be nil in test environments; routes will still be registered
// and respond with 501 instead of panicking.
// The engine mode (release/debug) is controlled by the GIN_MODE environment variable
// or by calling gin.SetMode before invoking New.
func New(h *health.Handler, ph *handlerproduct.Handler, uh *handlerunit.Handler, ah *handlerAuth.Handler, sh *handlerstock.Handler) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())

	internal := r.Group("/internal/v1/tally")
	{
		internal.GET("/health", h.Healthz)
		internal.GET("/ready", h.Readyz)
	}

	// API v1 — business endpoints.
	api := r.Group("/api/v1")
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
