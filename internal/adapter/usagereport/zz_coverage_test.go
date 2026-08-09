package usagereport

// This file adds coverage for branches not exercised by reporter_test.go /
// retry_worker_test.go: New's default-fallback branches, Start idempotency,
// the bad-tenant skip, the resolve_error retry path, the durable-enqueue
// failure path (event genuinely lost), Stop's ctx-timeout branch, RetryWorker
// Run's startup-drain + cancel lifecycle, and drainOnce/processRow/recordError
// error branches (PendingStats, Drain, MarkSent, RecordAttemptError all
// failing). It reuses the existing fakePoster/fakeResolver/fakeUsageOutbox/
// ctxWithTenant/waitForPosts helpers already declared in this package's other
// _test.go files — it does not redeclare them.

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/platform"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmgateway"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// ---- extra fakes (new types; existing fakes in other _test.go files are reused as-is) ----

// slowPoster sleeps before recording a successful post, so Stop's ctx-timeout
// branch can be exercised deterministically.
type slowPoster struct {
	delay time.Duration
	mu    sync.Mutex
	n     int
}

func (s *slowPoster) ReportUsageEvent(_ context.Context, _ platform.UsageEventRequest) error {
	time.Sleep(s.delay)
	s.mu.Lock()
	s.n++
	s.mu.Unlock()
	return nil
}

// errEnqueueOutbox makes Enqueue fail (both platform AND local DB down) while
// delegating everything else to a real fakeUsageOutbox.
type errEnqueueOutbox struct {
	*fakeUsageOutbox
	err error
}

func (e *errEnqueueOutbox) Enqueue(_ context.Context, _ PendingUsageRow) error { return e.err }

// statsErrOutbox makes PendingStats fail while Drain/Enqueue/etc. still work.
type statsErrOutbox struct {
	*fakeUsageOutbox
	err error
}

func (s *statsErrOutbox) PendingStats(_ context.Context) (UsagePendingStats, error) {
	return UsagePendingStats{}, s.err
}

// drainErrOutbox makes Drain fail so drainOnce must log + return early.
type drainErrOutbox struct {
	*fakeUsageOutbox
	err error
}

func (d *drainErrOutbox) Drain(_ context.Context, _ int) ([]PendingUsageRow, error) {
	return nil, d.err
}

// markSentErrOutbox makes MarkSent fail after a successful post.
type markSentErrOutbox struct {
	*fakeUsageOutbox
	err error
}

func (m *markSentErrOutbox) MarkSent(_ context.Context, _ uuid.UUID) error { return m.err }

// recordAttemptErrOutbox makes RecordAttemptError itself fail.
type recordAttemptErrOutbox struct {
	*fakeUsageOutbox
	err error
}

func (r *recordAttemptErrOutbox) RecordAttemptError(_ context.Context, _ uuid.UUID, _ string) (int, error) {
	return 0, r.err
}

// ---- New: default-fallback branches (reporter.go:102-128) ----

func TestNew_ZeroConfig_AppliesDefaults(t *testing.T) {
	poster := newFakePoster()
	resolver := &fakeResolver{ids: map[uuid.UUID]int64{}}
	r := New(poster, resolver, Config{}) // BufferSize=0, Workers=0, Timeout=0, Logger=nil

	if r.workers != defaultWorkers {
		t.Errorf("workers = %d, want default %d", r.workers, defaultWorkers)
	}
	if r.timeout != defaultTimeout {
		t.Errorf("timeout = %v, want default %v", r.timeout, defaultTimeout)
	}
	if cap(r.ch) != defaultBuffer {
		t.Errorf("buffer cap = %d, want default %d", cap(r.ch), defaultBuffer)
	}
	if r.logger == nil {
		t.Error("logger should default to slog.Default(), got nil")
	}

	// End-to-end sanity: the defaulted reporter still actually works.
	tid := uuid.New()
	resolver.ids[tid] = 1
	r.Start()
	defer r.Stop(context.Background())
	r.Record(ctxWithTenant(tid), "m", 1, 1)
	waitForPosts(t, poster, 1)
}

// ---- Start: idempotency branch (reporter.go:131-134) ----

func TestReporter_Start_SecondCall_SpawnsNoExtraWorkers(t *testing.T) {
	poster := newFakePoster()
	resolver := &fakeResolver{ids: map[uuid.UUID]int64{}}
	r := New(poster, resolver, Config{Workers: 3})
	r.Start()
	defer r.Stop(context.Background())

	before := runtime.NumGoroutine()
	r.Start() // must be a synchronous no-op: CompareAndSwap fails, returns immediately
	after := runtime.NumGoroutine()

	if after-before >= r.workers {
		t.Errorf("second Start looks like it spawned a fresh pool: goroutines before=%d after=%d workers=%d",
			before, after, r.workers)
	}
}

// ---- process: bad_tenant branch (reporter.go:178-181) ----

func TestReporter_BadTenant_SkippedNeverResolvedOrPosted(t *testing.T) {
	poster := newFakePoster()
	resolver := &fakeResolver{ids: map[uuid.UUID]int64{}}
	r := New(poster, resolver, Config{Workers: 1})
	r.Start()
	defer r.Stop(context.Background())

	before := testutil.ToFloat64(metricSkipped.WithLabelValues("bad_tenant"))
	ctx := llmgateway.WithTenant(context.Background(), "not-a-valid-uuid")
	r.Record(ctx, "m", 1, 1)

	// No post channel signal will ever fire (process returns before posting),
	// so poll the counter instead of waitForPosts.
	deadline := time.After(2 * time.Second)
	for {
		if testutil.ToFloat64(metricSkipped.WithLabelValues("bad_tenant"))-before == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for bad_tenant skip metric")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if resolver.calls() != 0 {
		t.Errorf("resolver must not be called for an unparsable tenant, got %d calls", resolver.calls())
	}
	if len(poster.calls()) != 0 {
		t.Errorf("expected no post for bad tenant, got %d", len(poster.calls()))
	}
}

// ---- resolveAccount: resolve_error branch (reporter.go:268-272), fed into
// enqueueForRetry (business invariant: resolve_error is retried, not dropped) ----

func TestReporter_ResolveError_QueuedForRetry_NotDropped(t *testing.T) {
	tid := uuid.New()
	poster := newFakePoster()
	resolver := &fakeResolver{err: errors.New("platform down")}
	store := newFakeUsageOutbox()
	r := New(poster, resolver, Config{Workers: 1, Store: store})
	r.Start()
	defer r.Stop(context.Background())

	r.Record(ctxWithTenant(tid), "m", 5, 5)

	select {
	case <-store.enqueueCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for durable enqueue")
	}
	if store.count() != 1 {
		t.Fatalf("queued rows = %d, want 1", store.count())
	}
	row := store.first()
	if row.Reason != "resolve_error" {
		t.Errorf("reason = %q, want resolve_error", row.Reason)
	}
	if len(poster.calls()) != 0 {
		t.Errorf("a resolve error must never post, got %d posts", len(poster.calls()))
	}
}

// ---- enqueueForRetry: store.Enqueue error branch (reporter.go:241-248) —
// genuine last-resort loss, must not panic or block the caller ----

func TestReporter_DurableEnqueueFails_EventGenuinelyLost(t *testing.T) {
	tid := uuid.New()
	poster := newFakePoster()
	resolver := &fakeResolver{ids: map[uuid.UUID]int64{}} // no_account
	store := &errEnqueueOutbox{fakeUsageOutbox: newFakeUsageOutbox(), err: errors.New("db unreachable")}
	r := New(poster, resolver, Config{Workers: 1, Store: store})
	r.Start()
	defer r.Stop(context.Background())

	before := testutil.ToFloat64(metricEnqueueFailed)
	r.Record(ctxWithTenant(tid), "m", 1, 1)

	deadline := time.After(2 * time.Second)
	for {
		if testutil.ToFloat64(metricEnqueueFailed)-before == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for enqueue_failed metric")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if len(poster.calls()) != 0 {
		t.Error("must not post when the durable enqueue itself fails")
	}
}

// ---- Stop: ctx-timeout-before-drain branch (reporter.go:289-293) ----

func TestReporter_Stop_ReturnsOnCtxTimeout_NotFullDrain(t *testing.T) {
	tid := uuid.New()
	poster := &slowPoster{delay: 300 * time.Millisecond}
	resolver := &fakeResolver{ids: map[uuid.UUID]int64{tid: 1}}
	r := New(poster, resolver, Config{Workers: 1})
	r.Start()

	r.Record(ctxWithTenant(tid), "m", 1, 1)
	time.Sleep(30 * time.Millisecond) // let the worker enter the slow post

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	start := time.Now()
	r.Stop(ctx)
	elapsed := time.Since(start)

	if elapsed >= poster.delay {
		t.Errorf("Stop should return on ctx timeout well before the slow post finishes; took %v (post delay %v)",
			elapsed, poster.delay)
	}
}

// ---- RetryWorker.Run: startup drain + ctx-cancel lifecycle (retry_worker.go:50-63) ----

func TestRetryWorker_Run_DrainsOnStartup_ReturnsOnCancel(t *testing.T) {
	tid := uuid.New()
	id := uuid.New()
	store := newFakeUsageOutbox()
	poster := newFakePoster()
	resolver := &fakeResolver{ids: map[uuid.UUID]int64{tid: 42}}

	if err := store.Enqueue(context.Background(), PendingUsageRow{
		ID: id, TenantID: tid, Model: "m", PromptTokens: 3, CompletionTokens: 2,
		OccurredAt: time.Now().UTC(), Reason: "no_account",
	}); err != nil {
		t.Fatalf("seed enqueue: %v", err)
	}
	<-store.enqueueCh

	w := NewRetryWorker(store, poster, resolver, Config{})
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(runDone)
	}()

	// usageRetryPollInterval is 60s, so only Run's startup drainOnce (called
	// before the ticker loop) can produce this post.
	waitForPosts(t, poster, 1)

	cancel()
	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancellation")
	}
	if !store.isSent(id) {
		t.Error("seeded row should be marked sent after the startup drain")
	}
}

// ---- drainOnce: PendingStats error branch (retry_worker.go:67-70) — gauges
// are skipped but the drain itself must still proceed ----

func TestRetryWorker_DrainOnce_PendingStatsError_StillProcessesRows(t *testing.T) {
	tid := uuid.New()
	id := uuid.New()
	store := &statsErrOutbox{fakeUsageOutbox: newFakeUsageOutbox(), err: errors.New("stats unavailable")}
	poster := newFakePoster()
	resolver := &fakeResolver{ids: map[uuid.UUID]int64{tid: 9}}

	if err := store.Enqueue(context.Background(), PendingUsageRow{
		ID: id, TenantID: tid, Model: "m", PromptTokens: 1, CompletionTokens: 1, Reason: "no_account",
	}); err != nil {
		t.Fatalf("seed enqueue: %v", err)
	}
	<-store.enqueueCh

	w := NewRetryWorker(store, poster, resolver, Config{})
	w.drainOnce(context.Background())

	if len(poster.calls()) != 1 {
		t.Fatalf("expected the row to still be processed despite a PendingStats error, got %d posts", len(poster.calls()))
	}
}

// ---- drainOnce: Drain error branch (retry_worker.go:72-76) — log + early
// return, nothing processed ----

func TestRetryWorker_DrainOnce_DrainError_NothingProcessed(t *testing.T) {
	store := &drainErrOutbox{fakeUsageOutbox: newFakeUsageOutbox(), err: errors.New("db down")}
	poster := newFakePoster()
	resolver := &fakeResolver{ids: map[uuid.UUID]int64{}}
	w := NewRetryWorker(store, poster, resolver, Config{})

	w.drainOnce(context.Background()) // must not panic

	if len(poster.calls()) != 0 {
		t.Errorf("expected no posts when Drain errors, got %d", len(poster.calls()))
	}
}

// ---- processRow: MarkSent error branch (retry_worker.go:120-126) — posted
// but not marked sent, so the next tick will re-post (platform dedups on the
// stable idempotency key, per business invariant) ----

func TestRetryWorker_ProcessRow_MarkSentError_PostedButNotMarkedSent(t *testing.T) {
	tid := uuid.New()
	id := uuid.New()
	store := &markSentErrOutbox{fakeUsageOutbox: newFakeUsageOutbox(), err: errors.New("marksent unavailable")}
	poster := newFakePoster()
	resolver := &fakeResolver{ids: map[uuid.UUID]int64{tid: 5}}

	if err := store.Enqueue(context.Background(), PendingUsageRow{
		ID: id, TenantID: tid, Model: "m", PromptTokens: 2, CompletionTokens: 2, Reason: "post_error",
	}); err != nil {
		t.Fatalf("seed enqueue: %v", err)
	}
	<-store.enqueueCh

	w := NewRetryWorker(store, poster, resolver, Config{})
	w.drainOnce(context.Background())

	if len(poster.calls()) != 1 {
		t.Fatalf("expected the post attempt to happen despite MarkSent failing, got %d", len(poster.calls()))
	}
	if store.isSent(id) {
		t.Error("row must NOT be marked sent when MarkSent itself errors")
	}
}

// ---- recordError: RecordAttemptError error branch (retry_worker.go:133-137)
// — must log and return WITHOUT evaluating the exhausted-attempts alert ----

func TestRetryWorker_RecordError_StoreRecordFails_NoExhaustedAlert(t *testing.T) {
	tid := uuid.New()
	id := uuid.New()
	store := &recordAttemptErrOutbox{fakeUsageOutbox: newFakeUsageOutbox(), err: errors.New("record store down")}
	poster := newFakePoster()
	resolver := &fakeResolver{err: errors.New("resolver down")} // forces recordError

	if err := store.Enqueue(context.Background(), PendingUsageRow{
		ID: id, TenantID: tid, Model: "m", PromptTokens: 1, CompletionTokens: 1, Reason: "resolve_error",
	}); err != nil {
		t.Fatalf("seed enqueue: %v", err)
	}
	<-store.enqueueCh

	before := testutil.ToFloat64(metricRetryExhausted)
	w := NewRetryWorker(store, poster, resolver, Config{})
	w.drainOnce(context.Background())
	after := testutil.ToFloat64(metricRetryExhausted)

	if after != before {
		t.Errorf("retry_exhausted must not fire when RecordAttemptError itself fails: delta=%v", after-before)
	}
}
