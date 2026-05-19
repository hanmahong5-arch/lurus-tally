package llmgateway

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeIncrer struct {
	counts   map[string]int64
	incrErr  error
	expireOK bool
}

func newFakeIncrer() *fakeIncrer { return &fakeIncrer{counts: map[string]int64{}} }

func (f *fakeIncrer) Incr(_ context.Context, key string) (int64, error) {
	if f.incrErr != nil {
		return 0, f.incrErr
	}
	if f.counts == nil {
		f.counts = map[string]int64{}
	}
	f.counts[key]++
	return f.counts[key], nil
}

func (f *fakeIncrer) Expire(_ context.Context, _ string, _ time.Duration) error {
	f.expireOK = true
	return nil
}

// total returns the aggregate of all key counters — handy for tests that
// don't care about which key was incremented.
func (f *fakeIncrer) total() int64 {
	var n int64
	for _, v := range f.counts {
		n += v
	}
	return n
}

func TestRateLimiter_AllowsUpToLimit(t *testing.T) {
	store := newFakeIncrer()
	rl := NewRateLimiter(store, 3, time.Minute)
	tenant := uuid.New()

	for i := 0; i < 3; i++ {
		ok, _, err := rl.Allow(context.Background(), tenant)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if !ok {
			t.Errorf("call %d: expected allowed, got blocked", i)
		}
	}
	if !store.expireOK {
		t.Errorf("expected Expire to be set on first call")
	}
}

func TestRateLimiter_BlocksOverLimit(t *testing.T) {
	store := newFakeIncrer()
	rl := NewRateLimiter(store, 2, time.Minute)
	tenant := uuid.New()

	// Two allowed, third blocked.
	_, _, _ = rl.Allow(context.Background(), tenant)
	_, _, _ = rl.Allow(context.Background(), tenant)
	ok, retryAfter, err := rl.Allow(context.Background(), tenant)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Errorf("third call should be blocked (over limit of 2)")
	}
	if retryAfter <= 0 || retryAfter > time.Minute {
		t.Errorf("retryAfter=%s, want positive ≤ 1m", retryAfter)
	}
}

func TestRateLimiter_DegradesOpenOnStoreError(t *testing.T) {
	store := &fakeIncrer{counts: map[string]int64{}, incrErr: errors.New("redis down")}
	rl := NewRateLimiter(store, 1, time.Minute)
	tenant := uuid.New()

	ok, _, err := rl.Allow(context.Background(), tenant)
	if err == nil {
		t.Error("store error should be surfaced to caller")
	}
	if !ok {
		t.Error("store error should yield allow=true so caller can degrade open")
	}
}

func TestRateLimiter_NilStorePermissive(t *testing.T) {
	rl := NewRateLimiter(nil, 1, time.Minute)
	tenant := uuid.New()
	for i := 0; i < 10; i++ {
		ok, _, err := rl.Allow(context.Background(), tenant)
		if !ok || err != nil {
			t.Fatalf("nil store should always allow; got ok=%v err=%v", ok, err)
		}
	}
}

func TestRateLimiter_NilTenantPermissive(t *testing.T) {
	store := newFakeIncrer()
	rl := NewRateLimiter(store, 1, time.Minute)
	ok, _, err := rl.Allow(context.Background(), uuid.Nil)
	if !ok || err != nil {
		t.Errorf("uuid.Nil tenant should bypass limiter; got ok=%v err=%v", ok, err)
	}
	if store.total() != 0 {
		t.Errorf("uuid.Nil should not bump the counter; got count=%d", store.total())
	}
}

func TestRateLimiter_DefaultsWhenZeroArgs(t *testing.T) {
	rl := NewRateLimiter(&fakeIncrer{}, 0, 0)
	if rl.Limit() != DefaultRateLimit {
		t.Errorf("Limit=%d, want default %d", rl.Limit(), DefaultRateLimit)
	}
	if rl.Window() != DefaultRateWindow {
		t.Errorf("Window=%s, want default %s", rl.Window(), DefaultRateWindow)
	}
}

func TestRateLimiter_SeparateBucketsPerTenant(t *testing.T) {
	store := newFakeIncrer()
	rl := NewRateLimiter(store, 1, time.Minute)

	a := uuid.New()
	b := uuid.New()

	// Tenant A's single allowed call.
	okA1, _, _ := rl.Allow(context.Background(), a)
	// Tenant B should also be allowed even though A's bucket is full.
	okB1, _, _ := rl.Allow(context.Background(), b)
	if !okA1 || !okB1 {
		t.Errorf("first call per tenant should be allowed; got A=%v B=%v", okA1, okB1)
	}
}
