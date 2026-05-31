// Package metrics hosts the /internal/v1/metrics endpoint: a Prometheus
// collector exposed behind an optional bearer-token gate. Sits outside the
// /api/v1 business surface so ServiceMonitor can scrape it without
// touching tenant routing.
package metrics

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmgateway"
)

// MetricsHandler wires the Prometheus collector behind an optional bearer-token
// gate. expectedKey == "" disables the gate (dev / kind cluster); when set, the
// Authorization header must carry "Bearer <expectedKey>" exactly.
type MetricsHandler struct {
	expectedKey string
}

// NewMetricsHandler builds the handler. Pass the same value as PLATFORM_INTERNAL_KEY
// (or a dedicated metrics key) so that Prometheus ServiceMonitor / internal
// dashboards can scrape with one shared secret.
func NewMetricsHandler(expectedKey string) *MetricsHandler {
	return &MetricsHandler{expectedKey: expectedKey}
}

// Serve is a gin.HandlerFunc that proxies to the promhttp.Handler after
// validating the bearer token.
func (h *MetricsHandler) Serve(c *gin.Context) {
	if h.expectedKey != "" {
		const prefix = "Bearer "
		auth := c.GetHeader("Authorization")
		token, ok := strings.CutPrefix(auth, prefix)
		if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(h.expectedKey)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "metrics endpoint requires INTERNAL_API_KEY bearer"})
			return
		}
	}
	llmgateway.Handler().ServeHTTP(c.Writer, c.Request)
}
