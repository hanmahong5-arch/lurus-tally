// Package httpx is a resilient http.RoundTripper: bounded retry with
// exponential backoff + jitter, plus a per-transport circuit breaker.
//
// Two control loops, per the design framing:
//   - Retry is a negative-feedback damper on transient upstream failures
//     (429 / 502 / 503 / 504 / network errors). It fires ONLY for idempotent
//     methods by default — retrying a POST could double a charge or a posting,
//     so non-idempotent methods get one shot and are never replayed.
//   - The breaker is fast-fail back-pressure: after N consecutive failures it
//     opens and short-circuits calls (no socket touched) until a cooldown lets
//     a single probe through. This stops a dead dependency from being hammered
//     and bounds caller latency.
package httpx

import (
	"context"
	"errors"
	"io"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

// ErrCircuitOpen is returned (as the RoundTrip error) when the breaker is open
// and the request was short-circuited without reaching the network.
var ErrCircuitOpen = errors.New("httpx: circuit breaker open")

// Config tunes the transport. The zero value is not usable — use DefaultConfig
// and override fields.
type Config struct {
	// MaxRetries is the number of EXTRA attempts after the first, for
	// retryable methods only. Total attempts = MaxRetries + 1.
	MaxRetries int
	// BaseDelay / MaxDelay bound the exponential backoff (jitter is applied
	// within [0, computed]).
	BaseDelay time.Duration
	MaxDelay  time.Duration
	// RetryMethods is the set of HTTP methods safe to replay. Defaults to the
	// idempotent methods; POST/PATCH are deliberately excluded.
	RetryMethods map[string]bool
	// RetryStatuses is the set of response codes worth retrying.
	RetryStatuses map[int]bool
	// FailureThreshold is the consecutive-failure count that opens the breaker.
	FailureThreshold int
	// OpenDuration is the cooldown before the breaker admits a probe.
	OpenDuration time.Duration

	// sleep is injectable for tests; nil uses a real ctx-aware timer.
	sleep func(context.Context, time.Duration) error
	// now is injectable for tests; nil uses time.Now.
	now func() time.Time
}

// DefaultConfig returns sane production defaults.
func DefaultConfig() Config {
	return Config{
		MaxRetries: 2,
		BaseDelay:  100 * time.Millisecond,
		MaxDelay:   2 * time.Second,
		RetryMethods: map[string]bool{
			http.MethodGet:     true,
			http.MethodHead:    true,
			http.MethodPut:     true,
			http.MethodDelete:  true,
			http.MethodOptions: true,
		},
		RetryStatuses: map[int]bool{
			http.StatusTooManyRequests:    true, // 429
			http.StatusBadGateway:         true, // 502
			http.StatusServiceUnavailable: true, // 503
			http.StatusGatewayTimeout:     true, // 504
		},
		FailureThreshold: 5,
		OpenDuration:     10 * time.Second,
	}
}

// Transport wraps a base RoundTripper with retry + circuit breaking.
type Transport struct {
	base http.RoundTripper
	cfg  Config
	cb   *breaker
}

// New builds a Transport. base nil falls back to http.DefaultTransport.
func New(base http.RoundTripper, cfg Config) *Transport {
	if base == nil {
		base = http.DefaultTransport
	}
	if cfg.now == nil {
		cfg.now = time.Now
	}
	if cfg.RetryMethods == nil {
		cfg.RetryMethods = DefaultConfig().RetryMethods
	}
	if cfg.RetryStatuses == nil {
		cfg.RetryStatuses = DefaultConfig().RetryStatuses
	}
	return &Transport{
		base: base,
		cfg:  cfg,
		cb: &breaker{
			threshold: cfg.FailureThreshold,
			cooldown:  cfg.OpenDuration,
			now:       cfg.now,
		},
	}
}

// RoundTrip implements http.RoundTripper.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !t.cb.allow() {
		return nil, ErrCircuitOpen
	}

	// A retryable request must be replayable: either no body, or a GetBody to
	// rewind it. A consumed, non-rewindable body is treated as single-shot.
	canRetry := t.cfg.RetryMethods[req.Method] && (req.Body == nil || req.GetBody != nil)
	attempts := 1
	if canRetry {
		attempts = t.cfg.MaxRetries + 1
	}

	var resp *http.Response
	var err error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			if werr := t.wait(req.Context(), backoff(attempt, t.cfg.BaseDelay, t.cfg.MaxDelay)); werr != nil {
				return nil, werr
			}
			if !t.cb.allow() {
				return nil, ErrCircuitOpen
			}
		}

		outReq := req
		if req.Body != nil && req.GetBody != nil {
			body, gerr := req.GetBody()
			if gerr != nil {
				return nil, gerr
			}
			outReq = req.Clone(req.Context())
			outReq.Body = body
		}

		resp, err = t.base.RoundTrip(outReq)
		if err != nil {
			t.cb.onFailure()
			if attempt < attempts-1 {
				continue
			}
			return nil, err
		}
		if t.cfg.RetryStatuses[resp.StatusCode] {
			t.cb.onFailure()
			if attempt < attempts-1 {
				drain(resp)
				continue
			}
			return resp, nil // out of retries: hand the failed response back
		}
		t.cb.onSuccess()
		return resp, nil
	}
	return resp, err
}

func (t *Transport) wait(ctx context.Context, d time.Duration) error {
	if t.cfg.sleep != nil {
		return t.cfg.sleep(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// backoff returns a jittered delay for the given retry attempt (1-based),
// bounded to [0, min(max, base*2^(attempt-1))]. Full jitter de-correlates
// concurrent retriers so they do not stampede the recovering upstream.
func backoff(attempt int, base, max time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	d := base << (attempt - 1)
	if d <= 0 || d > max { // overflow or over cap
		d = max
	}
	if d <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(d) + 1)) //nolint:gosec // jitter, not crypto
}

// Backoff is the exported jittered-backoff helper, so callers that retry at a
// higher layer (e.g. the LLM client, which retries its non-idempotent POST only
// on a classified-retryable error) share the same damping curve.
func Backoff(attempt int, base, max time.Duration) time.Duration {
	return backoff(attempt, base, max)
}

func drain(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
}

// ---- circuit breaker -------------------------------------------------------

type cbState int

const (
	stateClosed cbState = iota
	stateOpen
	stateHalfOpen
)

type breaker struct {
	mu        sync.Mutex
	state     cbState
	failures  int
	openedAt  time.Time
	threshold int
	cooldown  time.Duration
	now       func() time.Time
}

// allow reports whether a request may proceed. Open → deny until the cooldown
// elapses, then admit exactly one half-open probe.
func (b *breaker) allow() bool {
	if b.threshold <= 0 {
		return true // breaker disabled
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case stateOpen:
		if b.now().Sub(b.openedAt) >= b.cooldown {
			b.state = stateHalfOpen
			return true // the single probe
		}
		return false
	case stateHalfOpen:
		return false // probe in flight; everyone else fast-fails
	default:
		return true
	}
}

func (b *breaker) onSuccess() {
	if b.threshold <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
	b.state = stateClosed
}

func (b *breaker) onFailure() {
	if b.threshold <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures++
	switch b.state {
	case stateHalfOpen:
		b.state = stateOpen
		b.openedAt = b.now()
	default:
		if b.failures >= b.threshold {
			b.state = stateOpen
			b.openedAt = b.now()
		}
	}
}
