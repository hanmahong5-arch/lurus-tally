// Package telemetry receives browser-side product events from the Next.js
// /api/otel-events route and forwards them onto NATS PSI_TELEMETRY.web.*.
// The route is internal-only (mounted under /internal/v1) and gated by the
// shared PLATFORM_INTERNAL_KEY bearer when configured.
package telemetry

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	adapternats "github.com/hanmahong5-arch/lurus-tally/internal/adapter/nats"
)

// Handler forwards web telemetry events into NATS.
type Handler struct {
	publisher     adapternats.Publisher
	expectedKey   string
	defaultTenant string
}

// New builds a telemetry handler.
//
//   - publisher: required; on missing NATS the noopPublisher is acceptable
//     and turns this endpoint into a successful no-op (handy for dev).
//   - expectedKey: bearer-token gate; "" disables auth (dev).
//   - defaultTenant: used when the request omits a tenant id (e.g. when
//     telemetry fires before login completes). Use "anonymous" in prod.
func New(publisher adapternats.Publisher, expectedKey, defaultTenant string) *Handler {
	if defaultTenant == "" {
		defaultTenant = "anonymous"
	}
	return &Handler{publisher: publisher, expectedKey: expectedKey, defaultTenant: defaultTenant}
}

// Register mounts POST /internal/v1/telemetry/web on the given gin engine.
func (h *Handler) Register(r *gin.Engine) {
	r.POST("/internal/v1/telemetry/web", h.serve)
}

type webEvent struct {
	Event    string         `json:"event"`
	TenantID string         `json:"tenant_id,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

func (h *Handler) serve(c *gin.Context) {
	if h.expectedKey != "" {
		const prefix = "Bearer "
		auth := c.GetHeader("Authorization")
		if !strings.HasPrefix(auth, prefix) || auth[len(prefix):] != h.expectedKey {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "telemetry endpoint requires INTERNAL_API_KEY bearer"})
			return
		}
	}

	var body webEvent
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}

	if body.Event == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing event field"})
		return
	}
	if _, ok := adapternats.AllowedWebTelemetryEvents[body.Event]; !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "event not in allow-list", "event": body.Event})
		return
	}

	tenant := body.TenantID
	if tenant == "" {
		tenant = h.defaultTenant
	}

	if err := h.publisher.PublishWebTelemetry(c.Request.Context(), tenant, body.Event, body.Metadata); err != nil {
		// Telemetry must never block the caller. Structured-log the failure
		// and still return 200 so the FE keeps trying future events.
		c.Header("X-Telemetry-Status", "publish-failed")
		c.JSON(http.StatusOK, gin.H{"ok": true, "publish_error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
