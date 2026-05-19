package llmgateway

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
)

// Default knobs for the LLM rate limiter. Tunable via NewRateLimiter args.
const (
	DefaultRateLimit   = 60
	DefaultRateWindow  = time.Minute
	rateLimitKeyPrefix = "tally:rl:llm:"
)

// RedisIncrer is the minimal Redis surface the limiter needs. Implemented by
// *redis.Client; tests pass a fake. INCR returns the post-increment value;
// Expire is best-effort and may be skipped when the key already has a TTL.
type RedisIncrer interface {
	Incr(ctx context.Context, key string) (int64, error)
	Expire(ctx context.Context, key string, ttl time.Duration) error
}

// RateLimiter enforces a fixed-window per-tenant cap on LLM endpoint hits.
// When the store is unhealthy the limiter degrades open — availability of
// /api/v1/ai/* must not depend on Redis being up, even though the budget
// guarantee is then lost.
type RateLimiter struct {
	store  RedisIncrer
	limit  int
	window time.Duration
	clock  func() time.Time
}

// NewRateLimiter constructs a limiter. Pass limit ≤ 0 or window ≤ 0 to use the
// defaults (60 / minute). A nil store yields a permissive limiter that never
// drops — useful in dev and tests that don't want Redis.
func NewRateLimiter(store RedisIncrer, limit int, window time.Duration) *RateLimiter {
	if limit <= 0 {
		limit = DefaultRateLimit
	}
	if window <= 0 {
		window = DefaultRateWindow
	}
	return &RateLimiter{
		store:  store,
		limit:  limit,
		window: window,
		clock:  time.Now,
	}
}

var llmRateLimitDropped = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "tally_llm_rate_limit_dropped_total",
		Help: "LLM requests rejected by the per-tenant rate limiter, by tenant.",
	},
	[]string{"tenant"},
)

func init() {
	prometheus.MustRegister(llmRateLimitDropped)
}

// Allow returns (true, 0, nil) when the call should proceed, or
// (false, retryAfter, nil) when the tenant has exhausted its budget for the
// current window. A non-nil error means the store failed; callers MUST treat
// that as degrade-open (allow the request) to keep AI endpoints available
// during Redis incidents.
func (r *RateLimiter) Allow(ctx context.Context, tenantID uuid.UUID) (bool, time.Duration, error) {
	if r == nil || r.store == nil || tenantID == uuid.Nil {
		return true, 0, nil
	}
	now := r.clock()
	bucket := now.Truncate(r.window).Unix()
	key := fmt.Sprintf("%s%s:%d", rateLimitKeyPrefix, tenantID.String(), bucket)

	count, err := r.store.Incr(ctx, key)
	if err != nil {
		return true, 0, err
	}
	if count == 1 {
		// Best-effort TTL set so the key cannot leak when traffic stops.
		_ = r.store.Expire(ctx, key, r.window+5*time.Second)
	}
	if int(count) > r.limit {
		retryAfter := r.window - time.Duration(now.UnixNano()-now.Truncate(r.window).UnixNano())
		if retryAfter <= 0 || retryAfter > r.window {
			retryAfter = r.window
		}
		return false, retryAfter, nil
	}
	return true, 0, nil
}

// RecordDropped bumps the rate-limit-drop counter for the given tenant. The
// caller (HTTP handler) is responsible for actually returning 429; this is
// just the observability hook.
func RecordDropped(tenantID uuid.UUID) {
	llmRateLimitDropped.WithLabelValues(tenantID.String()).Inc()
}

// Limit returns the configured per-window cap. Exposed for callers that want
// to surface the budget in error responses.
func (r *RateLimiter) Limit() int {
	if r == nil {
		return 0
	}
	return r.limit
}

// Window returns the configured rolling window.
func (r *RateLimiter) Window() time.Duration {
	if r == nil {
		return 0
	}
	return r.window
}
