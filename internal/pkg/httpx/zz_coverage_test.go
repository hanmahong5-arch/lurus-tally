package httpx

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// capturingRT is like stubRT (defined in httpx_test.go) but also records the
// *http.Request each attempt actually received, so tests can assert that a
// retried, replayable body was rewound via a freshly cloned request.
type capturingRT struct {
	mu    sync.Mutex
	calls int
	reqs  []*http.Request
	step  func(i int) (*http.Response, error)
}

func (c *capturingRT) RoundTrip(req *http.Request) (*http.Response, error) {
	c.mu.Lock()
	i := c.calls
	c.calls++
	c.reqs = append(c.reqs, req)
	c.mu.Unlock()
	return c.step(i)
}

func (c *capturingRT) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// ---- New(): defaulting branches --------------------------------------------

func TestNew_NilBaseFallsBackToDefaultTransport(t *testing.T) {
	tr := New(nil, Config{FailureThreshold: 5, OpenDuration: time.Second})
	if tr.base != http.DefaultTransport {
		t.Errorf("nil base must fall back to http.DefaultTransport, got %v", tr.base)
	}
}

func TestNew_NilRetryMethodsAndStatusesUseDefaults(t *testing.T) {
	tr := New(&stubRT{step: func(int) (*http.Response, error) { return resp(http.StatusOK), nil }}, Config{})
	def := DefaultConfig()
	if !reflect.DeepEqual(tr.cfg.RetryMethods, def.RetryMethods) {
		t.Errorf("nil RetryMethods must default: got %v, want %v", tr.cfg.RetryMethods, def.RetryMethods)
	}
	if !reflect.DeepEqual(tr.cfg.RetryStatuses, def.RetryStatuses) {
		t.Errorf("nil RetryStatuses must default: got %v, want %v", tr.cfg.RetryStatuses, def.RetryStatuses)
	}
	if tr.cfg.now == nil {
		t.Errorf("nil now must default to a usable clock func")
	}
}

// ---- RoundTrip: MaxRetries<=0 edge (attempts computed as 0) ----------------

// A misconfigured negative MaxRetries makes attempts = MaxRetries+1 <= 0, so
// the retry for-loop body never executes and the function falls through to
// its final `return resp, err` with both zero values. This is a real,
// reachable path (not dead code) for any caller that sets MaxRetries < -0.
func TestRoundTrip_NegativeMaxRetries_LoopNeverRunsReturnsNilNil(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxRetries = -1
	rt := &stubRT{step: func(int) (*http.Response, error) {
		t.Fatalf("transport must not be reached when computed attempts is 0")
		return nil, nil
	}}
	tr := New(rt, cfg)

	got, err := tr.RoundTrip(newReq(t, http.MethodGet))
	if got != nil || err != nil {
		t.Errorf("attempts=0 path: got resp=%v err=%v, want nil,nil", got, err)
	}
	if rt.count() != 0 {
		t.Errorf("transport calls: got %d, want 0", rt.count())
	}
}

// ---- RoundTrip: method retry-membership table ------------------------------

func TestRoundTrip_RetryMethodMembership(t *testing.T) {
	cases := []struct {
		method      string
		wantRetries bool
	}{
		{http.MethodGet, true},
		{http.MethodHead, true},
		{http.MethodPut, true},
		{http.MethodDelete, true},
		{http.MethodOptions, true},
		{http.MethodPost, false},
		{http.MethodPatch, false},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.MaxRetries = 2 // attempts = 3 when retryable
			cfg.sleep = func(context.Context, time.Duration) error { return nil }
			rt := &stubRT{step: func(int) (*http.Response, error) {
				return resp(http.StatusServiceUnavailable), nil
			}}
			tr := New(rt, cfg)

			got, err := tr.RoundTrip(newReq(t, tc.method))
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got.StatusCode != http.StatusServiceUnavailable {
				t.Errorf("status: got %d, want 503", got.StatusCode)
			}
			want := 1
			if tc.wantRetries {
				want = 3
			}
			if rt.count() != want {
				t.Errorf("method %s: transport calls got %d, want %d", tc.method, rt.count(), want)
			}
		})
	}
}

// ---- RoundTrip: RetryStatuses membership + breaker interaction ------------

func TestRoundTrip_RetryStatusMembership(t *testing.T) {
	cases := []struct {
		status    int
		retriable bool
	}{
		{http.StatusTooManyRequests, true},      // 429
		{http.StatusBadGateway, true},           // 502
		{http.StatusServiceUnavailable, true},   // 503
		{http.StatusGatewayTimeout, true},       // 504
		{http.StatusInternalServerError, false}, // 500 — not whitelisted
		{http.StatusBadRequest, false},          // 400
		{http.StatusOK, false},                  // 200
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.MaxRetries = 1 // attempts = 2 when retriable
			cfg.sleep = func(context.Context, time.Duration) error { return nil }
			rt := &stubRT{step: func(int) (*http.Response, error) {
				return resp(tc.status), nil
			}}
			tr := New(rt, cfg)

			got, err := tr.RoundTrip(newReq(t, http.MethodGet))
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got.StatusCode != tc.status {
				t.Errorf("status: got %d, want %d", got.StatusCode, tc.status)
			}
			want := 1
			if tc.retriable {
				want = 2
			}
			if rt.count() != want {
				t.Errorf("status %d: transport calls got %d, want %d", tc.status, rt.count(), want)
			}
		})
	}
}

// Business invariant: only whitelisted RetryStatuses count as breaker
// failures. A repeated non-whitelisted failing status (e.g. 500) is treated
// as a transport "success" for breaker bookkeeping and must never trip it or
// fast-fail subsequent calls.
func TestRoundTrip_NonWhitelistedStatusNeverTripsBreaker(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxRetries = 0
	cfg.FailureThreshold = 1 // would trip after a single real failure
	cfg.OpenDuration = time.Hour
	cfg.sleep = func(context.Context, time.Duration) error { return nil }

	rt := &stubRT{step: func(int) (*http.Response, error) {
		return resp(http.StatusInternalServerError), nil
	}}
	tr := New(rt, cfg)

	for i := 0; i < 5; i++ {
		got, err := tr.RoundTrip(newReq(t, http.MethodGet))
		if err != nil {
			t.Fatalf("call %d: unexpected err (breaker must not trip on 500): %v", i, err)
		}
		if got.StatusCode != http.StatusInternalServerError {
			t.Errorf("call %d: status got %d, want 500", i, got.StatusCode)
		}
	}
	if rt.count() != 5 {
		t.Errorf("transport calls: got %d, want 5 (breaker never opened)", rt.count())
	}
}

// ---- RoundTrip: breaker opens mid retry-loop -------------------------------

func TestRoundTrip_BreakerOpensMidRetryLoop(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxRetries = 2 // attempts = 3
	cfg.FailureThreshold = 1
	cfg.OpenDuration = time.Hour
	cfg.sleep = func(context.Context, time.Duration) error { return nil }

	rt := &stubRT{step: func(int) (*http.Response, error) {
		return resp(http.StatusServiceUnavailable), nil
	}}
	tr := New(rt, cfg)

	_, err := tr.RoundTrip(newReq(t, http.MethodGet))
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("want ErrCircuitOpen once breaker trips mid-retry, got %v", err)
	}
	if rt.count() != 1 {
		t.Errorf("only the first attempt should reach the transport before the breaker opens: got %d, want 1", rt.count())
	}
}

// ---- RoundTrip: replayable-body gating -------------------------------------

func TestRoundTrip_GetBodyRewindsPerAttempt(t *testing.T) {
	getBodyCalls := 0
	req := newReq(t, http.MethodGet)
	req.Body = io.NopCloser(strings.NewReader("payload"))
	req.GetBody = func() (io.ReadCloser, error) {
		getBodyCalls++
		return io.NopCloser(strings.NewReader("payload")), nil
	}

	rt := &capturingRT{step: func(i int) (*http.Response, error) {
		if i == 0 {
			return resp(http.StatusServiceUnavailable), nil
		}
		return resp(http.StatusOK), nil
	}}
	cfg := DefaultConfig()
	cfg.MaxRetries = 1 // attempts = 2
	cfg.sleep = func(context.Context, time.Duration) error { return nil }
	tr := New(rt, cfg)

	got, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", got.StatusCode)
	}
	if rt.count() != 2 {
		t.Fatalf("transport calls: got %d, want 2", rt.count())
	}
	if getBodyCalls != 2 {
		t.Errorf("GetBody must be invoked once per attempt: got %d, want 2", getBodyCalls)
	}
	if rt.reqs[0] == req || rt.reqs[1] == req {
		t.Errorf("each outgoing request must be a clone, never the original *http.Request")
	}
	if rt.reqs[0] == rt.reqs[1] {
		t.Errorf("each attempt must receive a distinct cloned request")
	}
}

func TestRoundTrip_GetBodyErrorPropagatesBeforeTransport(t *testing.T) {
	wantErr := errors.New("getbody boom")
	req := newReq(t, http.MethodGet)
	req.Body = io.NopCloser(strings.NewReader("x"))
	req.GetBody = func() (io.ReadCloser, error) { return nil, wantErr }

	rt := &stubRT{step: func(int) (*http.Response, error) {
		t.Fatalf("transport must not be reached when GetBody fails")
		return nil, nil
	}}
	tr := New(rt, DefaultConfig())

	_, err := tr.RoundTrip(req)
	if !errors.Is(err, wantErr) {
		t.Errorf("want GetBody error propagated as-is, got %v", err)
	}
	if rt.count() != 0 {
		t.Errorf("transport calls: got %d, want 0", rt.count())
	}
}

func TestRoundTrip_BodyWithoutGetBody_SingleAttemptOnly(t *testing.T) {
	req := newReq(t, http.MethodGet)
	req.Body = io.NopCloser(strings.NewReader("x"))
	// req.GetBody intentionally left nil: a consumed, non-rewindable body.

	cfg := DefaultConfig()
	cfg.MaxRetries = 2
	cfg.sleep = func(context.Context, time.Duration) error { return nil }
	rt := &stubRT{step: func(int) (*http.Response, error) {
		return resp(http.StatusServiceUnavailable), nil
	}}
	tr := New(rt, cfg)

	got, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want 503", got.StatusCode)
	}
	if rt.count() != 1 {
		t.Errorf("non-rewindable retryable-method body must not be retried: got %d, want 1", rt.count())
	}
}

// Persistent transport error across every attempt: retries are exhausted and
// the last attempt's error is returned (not swallowed, not nil).
func TestRoundTrip_TransportErrorExhaustsRetries(t *testing.T) {
	var delays []time.Duration
	wantErr := errors.New("dial tcp: connection refused")
	rt := &stubRT{step: func(int) (*http.Response, error) {
		return nil, wantErr
	}}
	cfg := testConfig(&delays)
	cfg.MaxRetries = 2 // attempts = 3

	tr := New(rt, cfg)
	got, err := tr.RoundTrip(newReq(t, http.MethodGet))
	if got != nil {
		t.Errorf("resp: got %v, want nil", got)
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("err: got %v, want %v", err, wantErr)
	}
	if rt.count() != 3 {
		t.Errorf("transport calls: got %d, want 3 (1 + 2 retries, all exhausted)", rt.count())
	}
}

// ---- wait(): real ctx-aware timer path (cfg.sleep left nil) ---------------

func TestWait_RealTimerHonorsCtxCancellation(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxRetries = 1
	cfg.BaseDelay = 200 * time.Millisecond
	cfg.MaxDelay = 200 * time.Millisecond
	// cfg.sleep left nil on purpose: forces wait() down the real timer path.

	rt := &stubRT{step: func(int) (*http.Response, error) {
		return resp(http.StatusServiceUnavailable), nil
	}}
	tr := New(rt, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled before RoundTrip starts
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://svc/x", nil)
	if err != nil {
		t.Fatalf("newRequest: %v", err)
	}

	_, rtErr := tr.RoundTrip(req)
	if !errors.Is(rtErr, context.Canceled) {
		t.Errorf("want context.Canceled from the real-timer wait path, got %v", rtErr)
	}
	if rt.count() != 1 {
		t.Errorf("only the first attempt should fire before cancellation stops the retry: got %d, want 1", rt.count())
	}
}

func TestWait_RealTimerFiresAndAllowsRetry(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxRetries = 1
	cfg.BaseDelay = 2 * time.Millisecond
	cfg.MaxDelay = 5 * time.Millisecond
	// cfg.sleep left nil: real timer path, no cancellation this time.

	rt := &stubRT{step: func(i int) (*http.Response, error) {
		if i == 0 {
			return resp(http.StatusServiceUnavailable), nil
		}
		return resp(http.StatusOK), nil
	}}
	tr := New(rt, cfg)

	got, err := tr.RoundTrip(newReq(t, http.MethodGet))
	if err != nil {
		t.Fatalf("unexpected err via real-timer path: %v", err)
	}
	if got.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", got.StatusCode)
	}
	if rt.count() != 2 {
		t.Errorf("transport calls: got %d, want 2", rt.count())
	}
}

// ---- backoff(): base/max non-positive edges --------------------------------

func TestBackoff_NonPositiveBaseAlwaysZero(t *testing.T) {
	for _, base := range []time.Duration{0, -1, -5 * time.Millisecond} {
		for attempt := 1; attempt <= 3; attempt++ {
			if d := Backoff(attempt, base, time.Second); d != 0 {
				t.Errorf("base=%v attempt=%d: got %v, want 0", base, attempt, d)
			}
		}
	}
}

func TestBackoff_NonPositiveMaxCollapsesToZero(t *testing.T) {
	for _, max := range []time.Duration{0, -1} {
		for attempt := 1; attempt <= 3; attempt++ {
			if d := Backoff(attempt, 100*time.Millisecond, max); d != 0 {
				t.Errorf("max=%v attempt=%d: got %v, want 0", max, attempt, d)
			}
		}
	}
}

// ---- drain(): nil safety ----------------------------------------------------

func TestDrain_NilSafe(t *testing.T) {
	drain(nil) // must not panic
	drain(&http.Response{Body: nil})
}

// ---- breaker: direct state-machine tests -----------------------------------

func TestBreaker_DisabledWhenThresholdNonPositive(t *testing.T) {
	for _, threshold := range []int{0, -1, -100} {
		b := &breaker{threshold: threshold, cooldown: time.Minute, now: time.Now, state: stateOpen, failures: 7}
		if !b.allow() {
			t.Errorf("threshold=%d: disabled breaker must always allow", threshold)
		}
		b.onFailure()
		if b.failures != 7 || b.state != stateOpen {
			t.Errorf("threshold=%d: onFailure must be a no-op when disabled, got failures=%d state=%v", threshold, b.failures, b.state)
		}
		b.onSuccess()
		if b.failures != 7 || b.state != stateOpen {
			t.Errorf("threshold=%d: onSuccess must be a no-op when disabled, got failures=%d state=%v", threshold, b.failures, b.state)
		}
	}
}

func TestBreaker_OpensExactlyAtThreshold(t *testing.T) {
	fixed := time.Unix(1000, 0)
	b := &breaker{threshold: 3, cooldown: time.Minute, now: func() time.Time { return fixed }}

	b.onFailure() // 1/3
	if b.state != stateClosed {
		t.Fatalf("after 1/3 failures: want closed, got %v", b.state)
	}
	b.onFailure() // 2/3
	if b.state != stateClosed {
		t.Fatalf("after 2/3 failures: want closed, got %v", b.state)
	}
	b.onFailure() // 3/3 -> trips
	if b.state != stateOpen {
		t.Fatalf("after 3/3 failures: want open, got %v", b.state)
	}
	if !b.openedAt.Equal(fixed) {
		t.Errorf("openedAt: got %v, want %v", b.openedAt, fixed)
	}
}

func TestBreaker_HalfOpenAdmitsExactlyOneProbe(t *testing.T) {
	fixed := time.Unix(2000, 0)
	b := &breaker{
		threshold: 1,
		cooldown:  time.Minute,
		now:       func() time.Time { return fixed },
		state:     stateOpen,
		openedAt:  fixed.Add(-2 * time.Minute), // cooldown already elapsed
	}

	if !b.allow() {
		t.Fatalf("first call after cooldown must admit the probe")
	}
	if b.state != stateHalfOpen {
		t.Fatalf("state after admitting probe: got %v, want halfOpen", b.state)
	}
	if b.allow() {
		t.Errorf("a second caller during the in-flight half-open probe must fast-fail")
	}
}

func TestBreaker_ProbeFailureReopensWithNewTimestamp(t *testing.T) {
	t1 := time.Unix(3000, 0)
	cur := t1
	b := &breaker{threshold: 1, cooldown: time.Minute, now: func() time.Time { return cur },
		state: stateOpen, openedAt: t1.Add(-2 * time.Minute)}

	if !b.allow() {
		t.Fatalf("expected probe admission")
	}
	if b.state != stateHalfOpen {
		t.Fatalf("expected halfOpen, got %v", b.state)
	}

	t2 := t1.Add(5 * time.Second)
	cur = t2
	b.onFailure()
	if b.state != stateOpen {
		t.Fatalf("failed probe must reopen breaker, got %v", b.state)
	}
	if !b.openedAt.Equal(t2) {
		t.Errorf("openedAt after re-open: got %v, want %v", b.openedAt, t2)
	}
}

func TestBreaker_ProbeSuccessClosesAndResetsFailures(t *testing.T) {
	fixed := time.Unix(4000, 0)
	b := &breaker{threshold: 1, cooldown: time.Minute, now: func() time.Time { return fixed },
		state: stateOpen, openedAt: fixed.Add(-2 * time.Minute), failures: 9}

	if !b.allow() {
		t.Fatalf("expected probe admission")
	}
	b.onSuccess()
	if b.state != stateClosed {
		t.Errorf("successful probe must close breaker, got %v", b.state)
	}
	if b.failures != 0 {
		t.Errorf("successful probe must reset failures, got %d", b.failures)
	}
}

// ---- concurrency / -race --------------------------------------------------

func TestBreaker_ConcurrentAccessIsRaceFree(t *testing.T) {
	b := &breaker{threshold: 5, cooldown: 10 * time.Millisecond, now: time.Now}
	var wg sync.WaitGroup
	const n = 200
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if b.allow() {
				if i%2 == 0 {
					b.onFailure()
				} else {
					b.onSuccess()
				}
			}
		}(i)
	}
	wg.Wait()

	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case stateClosed, stateOpen, stateHalfOpen:
		// any of these is a valid terminal state; the point of this test is
		// that -race finds no data race across allow/onFailure/onSuccess.
	default:
		t.Errorf("unexpected breaker state after concurrency: %v", b.state)
	}
}

func TestTransport_ConcurrentRoundTripIsRaceFree(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxRetries = 0
	cfg.FailureThreshold = 1000 // high enough it won't trip within n calls
	cfg.sleep = func(context.Context, time.Duration) error { return nil }

	rt := &stubRT{step: func(int) (*http.Response, error) {
		return resp(http.StatusOK), nil
	}}
	tr := New(rt, cfg)

	var wg sync.WaitGroup
	const n = 100
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := tr.RoundTrip(newReq(t, http.MethodGet)); err != nil {
				t.Errorf("unexpected error under concurrency: %v", err)
			}
		}()
	}
	wg.Wait()

	if rt.count() != n {
		t.Errorf("transport calls: got %d, want %d", rt.count(), n)
	}
}
