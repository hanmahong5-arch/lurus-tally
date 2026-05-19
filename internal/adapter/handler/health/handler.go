// Package health implements the liveness and readiness HTTP handlers.
// These endpoints are consumed by Kubernetes probes and must not require authentication.
package health

import (
	"context"
	"log/slog"
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
// Required=true means a Ping failure returns 503 and removes the pod from k8s
// service endpoints until the probe recovers.
// Required=false means the dep is reported in the degraded list but does not
// gate readiness — 200 is returned with status="degraded".
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
//
// Response contract:
//   - All required deps healthy  → 200 {"status":"ok"}
//   - All required deps healthy + ≥1 optional dep down
//     → 200 {"status":"degraded","degraded":["cache",...]}  + slog.Warn
//   - Any required dep down      → 503 {"status":"unhealthy","failures":["db",...]}
//
// Pings run in parallel so total probe time ≈ slowest dep.
// NATS being optional (Required=false) means an outbox-backed service treats NATS
// down as degraded rather than unhealthy — events are queued in the DB until NATS
// recovers.
func (h *Handler) Readyz(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), probeTimeout)
	defer cancel()

	type result struct {
		name     string
		required bool
		down     bool
		errMsg   string
	}
	results := make([]result, len(h.deps))

	var wg sync.WaitGroup
	for i := range h.deps {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			d := h.deps[idx]
			r := result{name: d.Name, required: d.Required}
			if err := d.Pinger.Ping(ctx); err != nil {
				r.down = true
				r.errMsg = err.Error()
			}
			results[idx] = r
		}(i)
	}
	wg.Wait()

	var failures []string
	var degraded []string
	for _, r := range results {
		if !r.down {
			continue
		}
		if r.required {
			failures = append(failures, r.name)
		} else {
			degraded = append(degraded, r.name)
		}
	}

	if len(failures) > 0 {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":   "unhealthy",
			"failures": failures,
		})
		return
	}

	if len(degraded) > 0 {
		slog.Warn("readiness probe: optional deps down",
			slog.Any("degraded", degraded),
		)
		c.JSON(http.StatusOK, gin.H{
			"status":  "degraded",
			"degraded": degraded,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
