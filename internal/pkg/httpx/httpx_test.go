package httpx

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubRT is a scripted RoundTripper: the i-th call (0-based) returns step(i).
type stubRT struct {
	mu    sync.Mutex
	calls int
	step  func(i int) (*http.Response, error)
}

func (s *stubRT) RoundTrip(_ *http.Request) (*http.Response, error) {
	s.mu.Lock()
	i := s.calls
	s.calls++
	s.mu.Unlock()
	return s.step(i)
}

func (s *stubRT) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func resp(code int) *http.Response {
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader("body"))}
}

func newReq(t *testing.T, method string) *http.Request {
	t.Helper()
	r, err := http.NewRequestWithContext(context.Background(), method, "http://svc/x", nil)
	if err != nil {
		t.Fatalf("newReq: %v", err)
	}
	return r
}

// testConfig returns a config whose sleeps are instant and recorded, so the
// tests neither block on real timers nor depend on a wall clock.
func testConfig(delays *[]time.Duration) Config {
	cfg := DefaultConfig()
	cfg.BaseDelay = 10 * time.Millisecond
	cfg.MaxDelay = 100 * time.Millisecond
	cfg.sleep = func(_ context.Context, d time.Duration) error {
		*delays = append(*delays, d)
		return nil
	}
	return cfg
}

// TestRoundTrip_RetriesIdempotentUntilSuccess: GET sees 503,503,200 → final
// 200 after exactly 3 transport calls (2 retries occurred).
func TestRoundTrip_RetriesIdempotentUntilSuccess(t *testing.T) {
	var delays []time.Duration
	rt := &stubRT{step: func(i int) (*http.Response, error) {
		if i < 2 {
			return resp(http.StatusServiceUnavailable), nil
		}
		return resp(http.StatusOK), nil
	}}
	tr := New(rt, testConfig(&delays))

	got, err := tr.RoundTrip(newReq(t, http.MethodGet))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", got.StatusCode)
	}
	if rt.count() != 3 {
		t.Errorf("transport calls: got %d, want 3 (1 + 2 retries)", rt.count())
	}
	if len(delays) != 2 {
		t.Errorf("backoff sleeps: got %d, want 2", len(delays))
	}
	for _, d := range delays {
		if d < 0 || d > 100*time.Millisecond {
			t.Errorf("backoff %v out of [0, MaxDelay]", d)
		}
	}
}

// TestRoundTrip_DoesNotRetryNonIdempotent: a POST that 503s is NOT replayed —
// exactly one transport call, and the failed response is returned to the caller.
func TestRoundTrip_DoesNotRetryNonIdempotent(t *testing.T) {
	var delays []time.Duration
	rt := &stubRT{step: func(int) (*http.Response, error) {
		return resp(http.StatusServiceUnavailable), nil
	}}
	tr := New(rt, testConfig(&delays))

	got, err := tr.RoundTrip(newReq(t, http.MethodPost))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want 503", got.StatusCode)
	}
	if rt.count() != 1 {
		t.Errorf("POST must not retry: transport calls got %d, want 1", rt.count())
	}
}

// TestRoundTrip_CircuitOpensAndFastFails: with no retries and threshold 2, two
// failing GETs trip the breaker; the third short-circuits WITHOUT touching the
// transport (call count frozen at 2) and returns ErrCircuitOpen.
func TestRoundTrip_CircuitOpensAndFastFails(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxRetries = 0
	cfg.FailureThreshold = 2
	cfg.OpenDuration = time.Hour // never cools down during the test
	cfg.sleep = func(context.Context, time.Duration) error { return nil }

	rt := &stubRT{step: func(int) (*http.Response, error) {
		return resp(http.StatusServiceUnavailable), nil
	}}
	tr := New(rt, cfg)

	for i := 0; i < 2; i++ {
		if _, err := tr.RoundTrip(newReq(t, http.MethodGet)); err != nil {
			t.Fatalf("call %d unexpected err: %v", i, err)
		}
	}
	if rt.count() != 2 {
		t.Fatalf("setup: want 2 transport calls, got %d", rt.count())
	}

	_, err := tr.RoundTrip(newReq(t, http.MethodGet))
	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("want ErrCircuitOpen, got %v", err)
	}
	if rt.count() != 2 {
		t.Errorf("open breaker still hit transport: calls went to %d, want frozen at 2", rt.count())
	}
}

// TestRoundTrip_HalfOpenProbeRecovers: after the cooldown the breaker admits one
// probe; a success closes it again.
func TestRoundTrip_HalfOpenProbeRecovers(t *testing.T) {
	now := time.Unix(0, 0)
	cfg := DefaultConfig()
	cfg.MaxRetries = 0
	cfg.FailureThreshold = 1
	cfg.OpenDuration = time.Minute
	cfg.now = func() time.Time { return now }
	cfg.sleep = func(context.Context, time.Duration) error { return nil }

	healthy := false
	rt := &stubRT{step: func(int) (*http.Response, error) {
		if healthy {
			return resp(http.StatusOK), nil
		}
		return resp(http.StatusServiceUnavailable), nil
	}}
	tr := New(rt, cfg)

	// Trip it open.
	_, _ = tr.RoundTrip(newReq(t, http.MethodGet))
	if _, err := tr.RoundTrip(newReq(t, http.MethodGet)); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("breaker should be open, got %v", err)
	}

	// Advance past the cooldown and let the upstream recover; the probe closes it.
	now = now.Add(2 * time.Minute)
	healthy = true
	if _, err := tr.RoundTrip(newReq(t, http.MethodGet)); err != nil {
		t.Fatalf("probe should pass once cooled down + healthy: %v", err)
	}
	// Closed again: a follow-up call proceeds normally.
	if _, err := tr.RoundTrip(newReq(t, http.MethodGet)); err != nil {
		t.Fatalf("breaker should be closed after a successful probe: %v", err)
	}
}

// TestRoundTrip_RetriesTransportError: a network error on an idempotent method
// is retried; success on the next attempt yields no error.
func TestRoundTrip_RetriesTransportError(t *testing.T) {
	var delays []time.Duration
	rt := &stubRT{step: func(i int) (*http.Response, error) {
		if i == 0 {
			return nil, errors.New("dial tcp: connection refused")
		}
		return resp(http.StatusOK), nil
	}}
	tr := New(rt, testConfig(&delays))

	got, err := tr.RoundTrip(newReq(t, http.MethodGet))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", got.StatusCode)
	}
	if rt.count() != 2 {
		t.Errorf("transport calls: got %d, want 2", rt.count())
	}
}

// TestBackoff_Bounds: jittered backoff stays within [0, cap] and never exceeds
// MaxDelay regardless of attempt.
func TestBackoff_Bounds(t *testing.T) {
	base := 50 * time.Millisecond
	max := 400 * time.Millisecond
	for attempt := 1; attempt <= 8; attempt++ {
		for i := 0; i < 50; i++ {
			d := Backoff(attempt, base, max)
			if d < 0 || d > max {
				t.Fatalf("attempt %d: backoff %v outside [0, %v]", attempt, d, max)
			}
		}
	}
}
