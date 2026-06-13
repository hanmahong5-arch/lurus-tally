package usagereport

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/platform"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmgateway"
)

// fakePoster records posted events and can inject a failure.
type fakePoster struct {
	mu     sync.Mutex
	posts  []platform.UsageEventRequest
	err    error
	postCh chan struct{} // signalled after each post attempt
}

func newFakePoster() *fakePoster { return &fakePoster{postCh: make(chan struct{}, 64)} }

func (f *fakePoster) ReportUsageEvent(_ context.Context, req platform.UsageEventRequest) error {
	f.mu.Lock()
	f.posts = append(f.posts, req)
	err := f.err
	f.mu.Unlock()
	f.postCh <- struct{}{}
	return err
}

func (f *fakePoster) calls() []platform.UsageEventRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]platform.UsageEventRequest(nil), f.posts...)
}

// fakeResolver maps tenant -> account id, counts calls, can inject errors.
type fakeResolver struct {
	mu      sync.Mutex
	ids     map[uuid.UUID]int64
	err     error
	callCnt int
}

func (r *fakeResolver) GetPlatformAccountID(_ context.Context, tid uuid.UUID) (int64, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callCnt++
	if r.err != nil {
		return 0, false, r.err
	}
	id, ok := r.ids[tid]
	return id, ok, nil
}

func (r *fakeResolver) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.callCnt
}

// waitForPosts blocks until n post attempts happen or the deadline elapses.
func waitForPosts(t *testing.T, f *fakePoster, n int) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for i := 0; i < n; i++ {
		select {
		case <-f.postCh:
		case <-deadline:
			t.Fatalf("timed out waiting for post %d/%d", i+1, n)
		}
	}
}

func ctxWithTenant(tid uuid.UUID) context.Context {
	return llmgateway.WithTenant(context.Background(), tid.String())
}

// TestReporter_HappyPath_PostsAttributedEvent verifies a recorded completion
// resolves the tenant's account and posts a correctly-shaped usage event.
func TestReporter_HappyPath_PostsAttributedEvent(t *testing.T) {
	tid := uuid.New()
	poster := newFakePoster()
	resolver := &fakeResolver{ids: map[uuid.UUID]int64{tid: 4242}}
	r := New(poster, resolver, Config{Workers: 1})
	r.Start()
	defer r.Stop(context.Background())

	r.Record(ctxWithTenant(tid), "deepseek-v4", 100, 30)
	waitForPosts(t, poster, 1)

	calls := poster.calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 post, got %d", len(calls))
	}
	got := calls[0]
	if got.AccountID != 4242 {
		t.Errorf("account_id: want 4242, got %d", got.AccountID)
	}
	if got.ProductID != ProductID || got.Metric != MetricLLMTokens {
		t.Errorf("product/metric mismatch: %s / %s", got.ProductID, got.Metric)
	}
	if got.Quantity != 130 {
		t.Errorf("quantity: want 130 (100+30), got %d", got.Quantity)
	}
	if got.IdempotencyKey == "" {
		t.Error("expected a non-empty idempotency key")
	}
	if got.Metadata["model"] != "deepseek-v4" ||
		got.Metadata["prompt_tokens"] != 100 || got.Metadata["completion_tokens"] != 30 {
		t.Errorf("metadata mismatch: %+v", got.Metadata)
	}
}

// TestReporter_UnknownTenant_NeverPosts ensures calls without a tenant tag
// (e.g. mis-wired ctx) are skipped, never attributed to "unknown".
func TestReporter_UnknownTenant_NeverPosts(t *testing.T) {
	poster := newFakePoster()
	resolver := &fakeResolver{ids: map[uuid.UUID]int64{}}
	r := New(poster, resolver, Config{Workers: 1})
	r.Start()
	defer r.Stop(context.Background())

	r.Record(context.Background(), "deepseek-v4", 100, 30) // no WithTenant

	// Give a worker a chance; nothing should be posted or even resolved.
	time.Sleep(50 * time.Millisecond)
	if len(poster.calls()) != 0 {
		t.Errorf("expected no posts for untagged tenant, got %d", len(poster.calls()))
	}
	if resolver.calls() != 0 {
		t.Errorf("expected no resolver calls, got %d", resolver.calls())
	}
}

// TestReporter_NoAccountPinned_SkipsInShadow verifies an unprovisioned tenant
// (resolver returns ok=false) is skipped rather than posted with a bogus id.
func TestReporter_NoAccountPinned_SkipsInShadow(t *testing.T) {
	tid := uuid.New()
	poster := newFakePoster()
	resolver := &fakeResolver{ids: map[uuid.UUID]int64{}} // tid absent → ok=false
	r := New(poster, resolver, Config{Workers: 1})
	r.Start()
	defer r.Stop(context.Background())

	r.Record(ctxWithTenant(tid), "deepseek-v4", 10, 5)
	time.Sleep(50 * time.Millisecond)
	if len(poster.calls()) != 0 {
		t.Errorf("expected no post for unprovisioned tenant, got %d", len(poster.calls()))
	}
}

// TestReporter_PostFailure_IsFailOpen verifies a platform error does not panic
// or block — it's logged and dropped (shadow stance).
func TestReporter_PostFailure_IsFailOpen(t *testing.T) {
	tid := uuid.New()
	poster := newFakePoster()
	poster.err = errors.New("platform 502 platform_auth_failed")
	resolver := &fakeResolver{ids: map[uuid.UUID]int64{tid: 7}}
	r := New(poster, resolver, Config{Workers: 1})
	r.Start()
	defer r.Stop(context.Background())

	r.Record(ctxWithTenant(tid), "deepseek-v4", 10, 5)
	waitForPosts(t, poster, 1) // attempt happened despite the error
	if len(poster.calls()) != 1 {
		t.Fatalf("expected the failing post to be attempted once, got %d", len(poster.calls()))
	}
}

// TestReporter_ResolverCached verifies the account lookup is cached: two events
// for the same tenant hit the resolver only once.
func TestReporter_ResolverCached(t *testing.T) {
	tid := uuid.New()
	poster := newFakePoster()
	resolver := &fakeResolver{ids: map[uuid.UUID]int64{tid: 9}}
	r := New(poster, resolver, Config{Workers: 1})
	r.Start()
	defer r.Stop(context.Background())

	r.Record(ctxWithTenant(tid), "deepseek-v4", 1, 1)
	r.Record(ctxWithTenant(tid), "deepseek-v4", 2, 2)
	waitForPosts(t, poster, 2)

	if resolver.calls() != 1 {
		t.Errorf("expected resolver to be hit once (cached), got %d", resolver.calls())
	}
}

// TestReporter_ZeroTokens_NoOp verifies a zero-token completion is ignored.
func TestReporter_ZeroTokens_NoOp(t *testing.T) {
	tid := uuid.New()
	poster := newFakePoster()
	resolver := &fakeResolver{ids: map[uuid.UUID]int64{tid: 1}}
	r := New(poster, resolver, Config{Workers: 1})
	r.Start()
	defer r.Stop(context.Background())

	r.Record(ctxWithTenant(tid), "deepseek-v4", 0, 0)
	time.Sleep(50 * time.Millisecond)
	if len(poster.calls()) != 0 {
		t.Errorf("expected no post for zero-token call, got %d", len(poster.calls()))
	}
}

// TestReporter_RecordNeverBlocks verifies Record returns promptly even when the
// queue is saturated and workers are slow — the hot path must not stall.
func TestReporter_RecordNeverBlocks(t *testing.T) {
	tid := uuid.New()
	poster := newFakePoster()
	resolver := &fakeResolver{ids: map[uuid.UUID]int64{tid: 1}}
	// Buffer 1, zero workers started → the 2nd+ Record must hit the default
	// (drop) branch instead of blocking.
	r := New(poster, resolver, Config{Workers: 1, BufferSize: 1})
	// Intentionally NOT started, so nothing drains.

	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			r.Record(ctxWithTenant(tid), "deepseek-v4", 1, 1)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Record blocked under a full queue — hot path would stall")
	}
}
