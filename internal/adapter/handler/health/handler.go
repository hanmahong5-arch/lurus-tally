// Package health implements the liveness and readiness HTTP handlers.
// These endpoints are consumed by Kubernetes probes and must not require authentication.
package health

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	serviceName  = "lurus-tally"
	probeTimeout = 2 * time.Second
)

// Pinger is the minimal contract for a dependency that the readiness probe
// checks. Both *sql.DB.PingContext and *redis.Client.Ping fit this shape via
// trivial adapters (see lifecycle/app.go). Production-critical deps should
// fail readiness when their Ping returns an error; optional deps should not.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Dep describes a single readiness dependency.
//
// Required=true means a Ping failure flips the response to 503; the pod will
// be removed from k8s service endpoints until the probe recovers. Required=false
// means the dep is reported in the response body but does not gate readiness —
// use this for deps with a no-op fallback (e.g. NATS) or deps that the service
// can boot without (e.g. Redis when AI is disabled).
type Dep struct {
	Name     string
	Pinger   Pinger
	Required bool
}

// Handler serves the /health and /ready endpoints.
type Handler struct {
	version string
	deps    []Dep
}

// New creates a Handler that reports the given build version in liveness responses
// and probes each dep in readiness responses. Deps may be empty — Readyz will
// always return ready in that case (matches legacy behaviour for tests that
// don't wire dependencies).
func New(version string, deps ...Dep) *Handler {
	return &Handler{version: version, deps: deps}
}

// Healthz handles GET /internal/v1/tally/health (Kubernetes liveness probe).
// Returns 200 with service identity as long as the process is alive. Liveness
// must be cheap and never depend on external systems — a transient DB blip
// should not cause k8s to kill the pod.
func (h *Handler) Healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"service": serviceName,
		"version": h.version,
	})
}

// Readyz handles GET /internal/v1/tally/ready (Kubernetes readiness probe).
// Returns 200 only when every required dep responds to Ping within probeTimeout;
// otherwise 503. Optional deps surface their status in the response body but
// don't gate readiness. Pings run in parallel so total probe time ≈ slowest dep.
func (h *Handler) Readyz(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), probeTimeout)
	defer cancel()

	results := make([]map[string]any, len(h.deps))
	var wg sync.WaitGroup
	for i := range h.deps {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			d := h.deps[idx]
			entry := map[string]any{
				"name":     d.Name,
				"required": d.Required,
			}
			if err := d.Pinger.Ping(ctx); err != nil {
				entry["status"] = "down"
				entry["error"] = err.Error()
			} else {
				entry["status"] = "ok"
			}
			results[idx] = entry
		}(i)
	}
	wg.Wait()

	overallReady := true
	for _, r := range results {
		if r["status"] == "down" && r["required"].(bool) {
			overallReady = false
			break
		}
	}

	status := "ready"
	httpCode := http.StatusOK
	if !overallReady {
		status = "not_ready"
		httpCode = http.StatusServiceUnavailable
	}

	c.JSON(httpCode, gin.H{
		"status": status,
		"deps":   results,
	})
}
