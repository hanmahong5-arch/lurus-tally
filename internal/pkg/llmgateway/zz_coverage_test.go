package llmgateway

// Additional coverage for branches left untouched by the pre-existing test
// files: the WithTenant("") short-circuit, the exact rate-limit Redis key
// format, the defensive retryAfter clamp (unreachable via the public
// NewRateLimiter constructor — only reachable by direct struct construction
// in a same-package test), RecordDropped, the nil-receiver Limit/Window
// accessors, and a -race proof that the atomic.Pointer sink swap is safe
// across concrete sink types.

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// --- WithTenant("") short-circuit ---

func TestWithTenant_EmptyString_ReturnsContextUnchanged(t *testing.T) {
	ctx := context.Background()

	got := WithTenant(ctx, "")

	if got != ctx {
		t.Fatalf("WithTenant(ctx, \"\") must return the same context unchanged")
	}
	if tenant := TenantFrom(got); tenant != unknownTenant {
		t.Errorf("TenantFrom on untagged ctx = %q, want %q", tenant, unknownTenant)
	}
}

func TestWithTenant_NonEmpty_RoundTrips(t *testing.T) {
	ctx := WithTenant(context.Background(), "acme-corp")
	if got := TenantFrom(ctx); got != "acme-corp" {
		t.Errorf("TenantFrom = %q, want %q", got, "acme-corp")
	}
}

// --- exact Redis key format, fixed clock ---

type zzKeyCapturingIncrer struct {
	lastKey    string
	incrCalls  int
	expireCall int
	expireKey  string
	expireTTL  time.Duration
	count      int64
}

func (f *zzKeyCapturingIncrer) Incr(_ context.Context, key string) (int64, error) {
	f.incrCalls++
	f.lastKey = key
	f.count++
	return f.count, nil
}

func (f *zzKeyCapturingIncrer) Expire(_ context.Context, key string, ttl time.Duration) error {
	f.expireCall++
	f.expireKey = key
	f.expireTTL = ttl
	return nil
}

func TestAllow_KeyFormat_FixedClock(t *testing.T) {
	store := &zzKeyCapturingIncrer{}
	rl := NewRateLimiter(store, 5, time.Minute)

	fixed := time.Date(2026, 7, 6, 12, 34, 56, 0, time.UTC)
	rl.clock = func() time.Time { return fixed }

	tenant := uuid.New()
	ok, _, err := rl.Allow(context.Background(), tenant)
	if err != nil || !ok {
		t.Fatalf("Allow: ok=%v err=%v, want ok=true err=nil", ok, err)
	}

	wantBucket := fixed.Truncate(time.Minute).Unix()
	wantKey := fmt.Sprintf("tally:rl:llm:%s:%d", tenant.String(), wantBucket)
	if store.lastKey != wantKey {
		t.Errorf("Redis key = %q, want %q", store.lastKey, wantKey)
	}

	// First hit (count==1) must set the TTL leak-prevention Expire, with
	// window+5s, on the very same key.
	if store.expireCall != 1 {
		t.Fatalf("Expire calls = %d, want exactly 1 on first hit", store.expireCall)
	}
	if store.expireKey != wantKey {
		t.Errorf("Expire key = %q, want %q", store.expireKey, wantKey)
	}
	wantTTL := time.Minute + 5*time.Second
	if store.expireTTL != wantTTL {
		t.Errorf("Expire ttl = %s, want %s", store.expireTTL, wantTTL)
	}

	// Second hit under the same fixed clock (same bucket) must NOT call
	// Expire again — TTL is only set on the very first hit of a bucket.
	ok, _, err = rl.Allow(context.Background(), tenant)
	if err != nil || !ok {
		t.Fatalf("second Allow: ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if store.expireCall != 1 {
		t.Errorf("Expire calls after 2nd hit = %d, want still 1 (no repeat TTL set)", store.expireCall)
	}
}

// --- defensive retryAfter clamp ---
//
// The formula `retryAfter := window - (now - now.Truncate(window))` always
// yields a value in (0, window] for any window > 0, because Truncate rounds
// down to a multiple of window so the elapsed term is always in [0, window).
// NewRateLimiter forces window > 0, so the `<= 0 || > window` clamp branch is
// unreachable via the public constructor. It is still defensive code that
// must not panic/misbehave, so we exercise it directly via same-package
// struct construction with a degenerate window.
func TestAllow_RetryAfterClamp_DefensiveBranch(t *testing.T) {
	store := &zzKeyCapturingIncrer{}
	rl := &RateLimiter{
		store:  store,
		limit:  0, // any hit is over budget
		window: -3 * time.Second,
		clock:  func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	}

	ok, retryAfter, err := rl.Allow(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("count(1) > limit(0) must deny the request")
	}
	// Hand-derived: window<=0 makes time.Time.Truncate return the instant
	// unchanged, so elapsed==0 and the pre-clamp retryAfter == r.window
	// (-3s), which is <=0 and thus clamped to r.window again (-3s).
	if retryAfter != -3*time.Second {
		t.Errorf("retryAfter = %s, want -3s (clamped to the degenerate window)", retryAfter)
	}
}

// --- RecordDropped ---

func TestRecordDropped_IncrementsPerTenantCounter(t *testing.T) {
	llmRateLimitDropped.Reset()
	tenant := uuid.New()

	RecordDropped(tenant)
	RecordDropped(tenant)

	got := testutil.ToFloat64(llmRateLimitDropped.WithLabelValues(tenant.String()))
	if got != 2 {
		t.Errorf("dropped_total = %v, want 2", got)
	}
}

// --- nil-receiver accessors ---

func TestLimitWindow_NilReceiver_ReturnZero(t *testing.T) {
	var rl *RateLimiter
	if got := rl.Limit(); got != 0 {
		t.Errorf("nil.Limit() = %d, want 0", got)
	}
	if got := rl.Window(); got != 0 {
		t.Errorf("nil.Window() = %s, want 0", got)
	}
}

func TestLimitWindow_ConfiguredReceiver(t *testing.T) {
	rl := NewRateLimiter(&zzKeyCapturingIncrer{}, 7, 30*time.Second)
	if got := rl.Limit(); got != 7 {
		t.Errorf("Limit() = %d, want 7", got)
	}
	if got := rl.Window(); got != 30*time.Second {
		t.Errorf("Window() = %s, want 30s", got)
	}
}

// --- race-safety of the atomic.Pointer sink swap across concrete types ---

// zzOtherSink is a second, distinct concrete UsageSink implementation. Swapping
// between it and capturingSink (defined in sink_test.go) under -race proves
// the atomic.Pointer[sinkHolder] indirection avoids the "inconsistently typed
// value" panic that a bare atomic.Value would suffer, and that concurrent
// RecordUsage/SetUsageSink calls are race-free.
type zzOtherSink struct {
	mu    sync.Mutex
	calls int
}

func (s *zzOtherSink) Record(_ context.Context, _ string, _, _ int) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
}

func TestRace_ConcurrentRecordUsageAndSinkSwap(t *testing.T) {
	prev := sink.Load()
	t.Cleanup(func() {
		if prev != nil {
			sink.Store(prev)
		} else {
			sink.Store(&sinkHolder{s: noopSink{}})
		}
	})

	a := &capturingSink{}
	b := &zzOtherSink{}
	SetUsageSink(a)

	var wg sync.WaitGroup
	const n = 50

	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			RecordUsage(WithTenant(context.Background(), "race-tenant"), "deepseek-v4", 1, 1)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			SetUsageSink(a)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			SetUsageSink(b)
		}
	}()
	wg.Wait()

	// No assertion on which sink "won" the race — the invariant under test
	// is the absence of a data race / panic, verified by `go test -race`.
}
