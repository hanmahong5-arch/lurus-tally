// Package health implements the liveness and readiness HTTP handlers.
// These endpoints are consumed by Kubernetes probes and must not require authentication.
package health

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const serviceName = "lurus-tally"

// Handler serves the /health and /ready endpoints.
type Handler struct {
	version string
}

// New creates a Handler that reports the given build version in liveness responses.
func New(version string) *Handler {
	return &Handler{version: version}
}

// Healthz handles GET /internal/v1/tally/health (Kubernetes liveness probe).
// Returns 200 with service identity as long as the process is alive.
func (h *Handler) Healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"service": serviceName,
		"version": h.version,
	})
}

// Readyz handles GET /internal/v1/tally/ready (Kubernetes readiness probe).
// In MVP (Story 1.1) this returns ready immediately; Story 1.3 will upgrade
// this to perform a real DB ping before reporting ready.
func (h *Handler) Readyz(c *gin.Context) {
	// TODO(story-1.3): add DB ping here before returning ready.
	c.JSON(http.StatusOK, gin.H{
		"status": "ready",
	})
}
