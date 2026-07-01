// Package demo is the HTTP adapter for the no-OIDC public sandbox. It exposes a
// single PUBLIC endpoint — POST /api/v1/demo/start — that provisions a throwaway,
// write-isolated demo tenant and returns its entry credentials (tenant + a
// short-lived PAT). Because it is reachable WITHOUT authentication, it carries
// two deliberate controls: an explicit enable gate (off → 404, so the endpoint is
// invisible unless a deployment opts in via TALLY_DEMO_MODE) and a rate limiter
// (a public tenant-creating endpoint is a spam/DoS surface).
//
// It depends on the app-layer Provisioner through a narrow interface so the
// gating and rate-limit policy are unit-testable without a database.
package demo

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	demoapp "github.com/hanmahong5-arch/lurus-tally/internal/app/demo"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

// provisioner is the slice of the app-layer use case this handler needs. Kept
// narrow so the handler test can substitute a fake.
type provisioner interface {
	Provision(ctx context.Context) (demoapp.Result, error)
}

// Handler serves POST /api/v1/demo/start.
type Handler struct {
	prov    provisioner
	enabled bool
	lim     *rateLimiter
}

// New constructs the handler. enabled comes from TALLY_DEMO_MODE; when false the
// endpoint answers 404 so a production deployment never exposes it even if the
// route is mounted. startsPerMinute bounds sandbox creation across the process.
func New(prov provisioner, enabled bool, startsPerMinute int) *Handler {
	return NewWithClock(prov, enabled, startsPerMinute, time.Now)
}

// NewWithClock is New with an injectable clock so the rate limiter is testable.
func NewWithClock(prov provisioner, enabled bool, startsPerMinute int, now func() time.Time) *Handler {
	return &Handler{prov: prov, enabled: enabled, lim: newRateLimiter(startsPerMinute, now)}
}

// RegisterRoutes mounts the PUBLIC sandbox route. The caller MUST mount it
// OUTSIDE the authenticated /api/v1 group (e.g. on the root engine), since the
// whole point is to be reachable without a session. gin.IRouter is satisfied by
// both *gin.Engine and *gin.RouterGroup so the caller chooses placement.
func (h *Handler) RegisterRoutes(r gin.IRouter) {
	r.POST("/api/v1/demo/start", h.Start)
}

// Start provisions one isolated demo tenant and returns its entry credentials.
//
//	200 { tenant_id, token, expires_at }  — sandbox ready
//	404                                   — demo mode disabled (endpoint hidden)
//	429                                   — rate limited (Retry-After: 60)
//	500                                   — provisioning failed
func (h *Handler) Start(c *gin.Context) {
	if !h.enabled {
		// Hidden when disabled — do not reveal that the endpoint exists.
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}
	if !h.lim.allow() {
		c.Header("Retry-After", "60")
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "rate_limited", "detail": "demo sandbox is busy, try again shortly"})
		return
	}
	res, err := h.prov.Provision(c.Request.Context())
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// rateLimiter is a minimal token bucket (no external dependency). It bounds the
// process-wide rate of sandbox creation; the clock is injectable for tests.
type rateLimiter struct {
	mu     sync.Mutex
	tokens float64
	max    float64
	rate   float64 // tokens refilled per second
	last   time.Time
	now    func() time.Time
}

func newRateLimiter(perMinute int, now func() time.Time) *rateLimiter {
	if now == nil {
		now = time.Now
	}
	max := float64(perMinute)
	if max < 1 {
		max = 1
	}
	return &rateLimiter{tokens: max, max: max, rate: max / 60.0, last: now(), now: now}
}

// allow consumes one token if available, refilling by elapsed time first.
func (l *rateLimiter) allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	t := l.now()
	if elapsed := t.Sub(l.last).Seconds(); elapsed > 0 {
		l.tokens += elapsed * l.rate
		if l.tokens > l.max {
			l.tokens = l.max
		}
		l.last = t
	}
	if l.tokens >= 1 {
		l.tokens--
		return true
	}
	return false
}
