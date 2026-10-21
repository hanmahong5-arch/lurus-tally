// Package telemetry receives browser-side product events from the Next.js
// /api/otel-events route and forwards them onto NATS PSI_TELEMETRY.web.*.
// The route is internal-only (mounted under /internal/v1) and gated by the
// shared PLATFORM_INTERNAL_KEY bearer when configured.
package telemetry

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	adapternats "github.com/hanmahong5-arch/lurus-tally/internal/adapter/nats"
)

// DAURecorder records a per-user Daily-Active-User hit for an allow-listed
// event. Implementations are fire-and-forget from the handler's perspective: a
// returned error is surfaced via a response header but never blocks the 200
// (telemetry must never break the caller). nil is a valid value — DAU counting
// is simply skipped when Redis is unavailable.
type DAURecorder interface {
	Record(ctx context.Context, event, userID string) error
}

// Handler forwards web telemetry events into NATS.
type Handler struct {
	publisher     adapternats.Publisher
	expectedKey   string
	defaultTenant string
	dau           DAURecorder

	errMu      sync.Mutex
	errWindows map[string]*errWindow
	dropped    atomic.Int64
	now        func() time.Time
}

const (
	webErrorEvent       = "web_error"
	webErrorPerMinute   = 30
	webErrorMsgMax      = 300
	webErrorWindowPrune = 1000
)

type errWindow struct {
	start time.Time
	count int
}

// DroppedWebErrors returns how many web_error events were discarded by the
// per-tenant rate limit since process start.
func (h *Handler) DroppedWebErrors() int64 { return h.dropped.Load() }

// allowWebError applies a fixed one-minute window of webErrorPerMinute per tenant.
func (h *Handler) allowWebError(tenant string) bool {
	now := h.now()
	h.errMu.Lock()
	defer h.errMu.Unlock()
	if len(h.errWindows) > webErrorWindowPrune {
		for k, w := range h.errWindows {
			if now.Sub(w.start) >= time.Minute {
				delete(h.errWindows, k)
			}
		}
	}
	w := h.errWindows[tenant]
	if w == nil || now.Sub(w.start) >= time.Minute {
		h.errWindows[tenant] = &errWindow{start: now, count: 1}
		return true
	}
	if w.count >= webErrorPerMinute {
		return false
	}
	w.count++
	return true
}

func metaString(m map[string]any, key string, max int) string {
	v, _ := m[key].(string)
	if r := []rune(v); len(r) > max {
		v = string(r[:max])
	}
	return v
}

// New builds a telemetry handler.
//
//   - publisher: required; on missing NATS the noopPublisher is acceptable
//     and turns this endpoint into a successful no-op (handy for dev).
//   - expectedKey: bearer-token gate; "" disables auth (dev).
//   - defaultTenant: used when the request omits a tenant id (e.g. when
//     telemetry fires before login completes). Use "anonymous" in prod.
//   - dau: optional per-user DAU recorder; nil disables DAU counting.
func New(publisher adapternats.Publisher, expectedKey, defaultTenant string, dau DAURecorder) *Handler {
	if defaultTenant == "" {
		defaultTenant = "anonymous"
	}
	return &Handler{
		publisher: publisher, expectedKey: expectedKey, defaultTenant: defaultTenant, dau: dau,
		errWindows: map[string]*errWindow{}, now: time.Now,
	}
}

// Register mounts POST /internal/v1/telemetry/web on the given gin engine.
func (h *Handler) Register(r *gin.Engine) {
	r.POST("/internal/v1/telemetry/web", h.serve)
}

type webEvent struct {
	Event    string         `json:"event"`
	TenantID string         `json:"tenant_id,omitempty"`
	UserID   string         `json:"user_id,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

func (h *Handler) serve(c *gin.Context) {
	if h.expectedKey != "" {
		const prefix = "Bearer "
		auth := c.GetHeader("Authorization")
		token, ok := strings.CutPrefix(auth, prefix)
		if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(h.expectedKey)) != 1 {
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

	// web_error: rate-limit per tenant (anti-flood), then leave one greppable
	// WARN line so support can search the log by tenant when a customer says
	// "it won't open". Stack stays in the NATS payload, not in the log line.
	if body.Event == webErrorEvent {
		if !h.allowWebError(tenant) {
			h.dropped.Add(1)
			// Fixed label: observable at /internal/v1/metrics, no per-tenant cardinality.
			middleware.IncWebTelemetry("web_error_dropped")
			c.JSON(http.StatusOK, gin.H{"ok": true, "dropped": true})
			return
		}
		slog.Warn("web_error",
			"tenant", tenant,
			"route", metaString(body.Metadata, "route", webErrorMsgMax),
			"message", metaString(body.Metadata, "message", webErrorMsgMax),
			"kind", metaString(body.Metadata, "kind", 20),
			"request_id", metaString(body.Metadata, "request_id", 100),
		)
	}

	// Count the event in Prometheus so the activation funnel is scrapable at
	// /internal/v1/metrics even when NATS (the durable sink) is unavailable.
	middleware.IncWebTelemetry(body.Event)

	// KS2 — split the AI-plan decision by outcome so the adoption rate is
	// computable (accepted=1 / sum). The FE sends metadata.accepted ∈ {"1","0"}
	// (PlanCard confirm/cancel); a non-string or missing value normalizes to
	// "unknown" inside IncPlanAccept. Fire-and-forget like the rest of telemetry.
	if body.Event == "plan_accept_rate" {
		accepted, _ := body.Metadata["accepted"].(string)
		middleware.IncPlanAccept(accepted)
	}

	// Per-user DAU. body.UserID is the verified session subject injected by the
	// Next /api/otel-events route — never client-trusted, so a blank id means
	// "no session" and is skipped (an anonymous DAU is not a measurement).
	// Best-effort: a Redis hiccup surfaces via a header but never blocks the 200.
	// web_error is a fault signal, not engagement: do not count it as activity.
	if h.dau != nil && body.UserID != "" && body.Event != webErrorEvent {
		if err := h.dau.Record(c.Request.Context(), body.Event, body.UserID); err != nil {
			c.Header("X-DAU-Status", "record-failed")
		}
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
