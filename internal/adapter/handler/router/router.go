// Package router wires all HTTP routes onto a Gin engine.
// Additional route groups (business API, auth, relay) will be added here in later stories.
package router

import (
	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/health"
)

// New creates and configures a Gin engine with all registered routes.
// The engine mode (release/debug) is controlled by the GIN_MODE environment variable
// or by calling gin.SetMode before invoking New.
func New(h *health.Handler) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())

	internal := r.Group("/internal/v1/tally")
	{
		internal.GET("/health", h.Healthz)
		internal.GET("/ready", h.Readyz)
	}

	return r
}
