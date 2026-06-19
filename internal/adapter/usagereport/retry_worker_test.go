package usagereport

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// fakeUsageOutbox is an in-memory UsageOutbox for hermetic tests.
type fakeUsageOutbox struct {
	mu        sync.Mutex
	rows      map[uuid.UUID]PendingUsageRow
	order     []uuid.UUID
	sent      map[uuid.UUID]bool
	attempts  map[uuid.UUID]int
	forceCap  int // when >0, RecordAttemptError returns this verbatim (force the exhausted path)
	enqueueCh chan struct{}
}

func newFakeUsageOutbox() *fakeUsageOutbox {
	return &fakeUsageOutbox{
		rows:      map[uuid.UUID]PendingUsageRow{},
		sent:      map[uuid.UUID]bool{},
		attempts:  map[uuid.UUID]int{},
		enqueueCh: make(chan struct{}, 64),
	}
}

func (f *fakeUsageOutbox) Enqueue(_ context.Context, row PendingUsageRow) error {
	f.mu.Lock()
	if _, dup := f.rows[row.ID]; !dup {
		f.order = append(f.order, row.ID)
	}
	f.rows[row.ID] = row
	f.mu.Unlock()
	f.enqueueCh <- struct{}{}
	return nil
}

func (f *fakeUsageOutbox) Drain(_ context.Context, limit int) ([]PendingUsageRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []PendingUsageRow
	for _, id := range f.order {
		if f.sent[id] || f.attempts[id] >= MaxUsageOutboxAttempts {
			continue
		}
		out = append(out, f.rows[id])
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (f *fakeUsageOutbox) MarkSent(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	f.sent[id] = true
	f.mu.Unlock()
	return nil
}

func (f *fakeUsageOutbox) RecordAttemptError(_ context.Context, id uuid.UUID, _ string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.forceCap > 0 {
		return f.forceCap, nil
	}
	f.attempts[id]++
	return f.attempts[id], nil
}

func (f *fakeUsageOutbox) PendingStats(_ context.Context) (UsagePendingStats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for _, id := range f.order {
		if !f.sent[id] && f.attempts[id] < MaxUsageOutboxAttempts {
			n++
		}
	}
	return UsagePendingStats{PendingCount: n}, nil
}

func (f *fakeUsageOutbox) count() int       { f.mu.Lock(); defer f.mu.Unlock(); return len(f.order) }
func (f *fakeUsageOutbox) isSent(id uuid.UUID) bool { f.mu.Lock(); defer f.mu.Unlock(); return f.sent[id] }
func (f *fakeUsageOutbox) attemptsOf(id uuid.UUID) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.attempts[id]
}
func (f *fakeUsageOutbox) first() PendingUsageRow {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rows[f.order[0]]
}

// TestReporter_NoAccount_EnqueuesForRetry: an unprovisioned tenant no longer
// drops the event — it is durably queued (reason=no_account), not posted.
func TestReporter_NoAccount_EnqueuesForRetry(t *testing.T) {
	tid := uuid.New()
	store := newFakeUsageOutbox()
	poster := newFakePoster()
	resolver := &fakeResolver{ids: map[uuid.UUID]int64{}} // tid absent → ok=false
	r := New(poster, resolver, Config{Workers: 1, Store: store})
	r.Start()
	defer r.Stop(context.Background())

	r.Record(ctxWithTenant(tid), "deepseek-v4", 5, 5)

	select {
	case <-store.enqueueCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for durable enqueue")
	}
	if store.count() != 1 {
		t.Fatalf("queued rows = %d, want 1", store.count())
	}
	row := store.first()
	if row.Reason != "no_account" || row.TenantID != tid {
		t.Errorf("row = %+v, want reason=no_account tenant=%s", row, tid)
	}
	if len(poster.calls()) != 0 {
		t.Errorf("unprovisioned tenant must not post, got %d posts", len(poster.calls()))
	}
}

// TestRetryWorker_BackReportsAfterProvisioning is the core proof: a queued
// no_account row stays unsent while the tenant is unprovisioned, then is
// re-resolved + posted once onboarding heals.
func TestRetryWorker_BackReportsAfterProvisioning(t *testing.T) {
	tid := uuid.New()
	id := uuid.New()
	store := newFakeUsageOutbox()
	poster := newFakePoster()
	resolver := &fakeResolver{ids: map[uuid.UUID]int64{}} // initially absent

	_ = store.Enqueue(context.Background(), PendingUsageRow{
		ID: id, TenantID: tid, Model: "m", PromptTokens: 7, CompletionTokens: 3,
		OccurredAt: time.Now().UTC(), Reason: "no_account",
	})
	<-store.enqueueCh

	w := NewRetryWorker(store, poster, resolver, Config{})

	w.drainOnce(context.Background())
	if store.isSent(id) || len(poster.calls()) != 0 {
		t.Fatalf("must not post/send while unprovisioned (sent=%v posts=%d)", store.isSent(id), len(poster.calls()))
	}
	if store.attemptsOf(id) != 1 {
		t.Errorf("attempts = %d, want 1", store.attemptsOf(id))
	}

	// Onboarding heals: the tenant now resolves to an account.
	resolver.mu.Lock()
	resolver.ids[tid] = 777
	resolver.mu.Unlock()

	w.drainOnce(context.Background())
	calls := poster.calls()
	if len(calls) != 1 {
		t.Fatalf("want 1 post after provisioning, got %d", len(calls))
	}
	if calls[0].AccountID != 777 {
		t.Errorf("account_id = %d, want 777 (re-resolved)", calls[0].AccountID)
	}
	if calls[0].Quantity != 10 {
		t.Errorf("quantity = %d, want 10", calls[0].Quantity)
	}
	if calls[0].IdempotencyKey != "tally-usage-"+id.String() {
		t.Errorf("idem key = %q, want tally-usage-%s", calls[0].IdempotencyKey, id)
	}
	if !store.isSent(id) {
		t.Error("row should be marked sent after a successful re-post")
	}
}

// TestRetryWorker_IdempotencyKeyStable: every retry of the same row carries the
// SAME idempotency key, so platform dedups (no double-count on partial success).
func TestRetryWorker_IdempotencyKeyStable(t *testing.T) {
	tid := uuid.New()
	id := uuid.New()
	store := newFakeUsageOutbox()
	poster := newFakePoster()
	poster.err = errors.New("platform down") // every post fails → row keeps retrying
	resolver := &fakeResolver{ids: map[uuid.UUID]int64{tid: 5}}

	_ = store.Enqueue(context.Background(), PendingUsageRow{
		ID: id, TenantID: tid, Model: "m", PromptTokens: 1, CompletionTokens: 1, Reason: "post_error",
	})
	<-store.enqueueCh

	w := NewRetryWorker(store, poster, resolver, Config{})
	w.drainOnce(context.Background())
	w.drainOnce(context.Background())

	calls := poster.calls()
	if len(calls) < 2 {
		t.Fatalf("want >=2 retry posts, got %d", len(calls))
	}
	want := "tally-usage-" + id.String()
	for i, c := range calls {
		if c.IdempotencyKey != want {
			t.Errorf("post %d idem key = %q, want %q (must be stable across retries)", i, c.IdempotencyKey, want)
		}
	}
}

// TestRetryWorker_Exhausted_FiresMetric: when a row crosses the attempts cap the
// exhausted alert metric fires (billable usage at risk).
func TestRetryWorker_Exhausted_FiresMetric(t *testing.T) {
	before := testutil.ToFloat64(metricRetryExhausted)

	store := newFakeUsageOutbox()
	store.forceCap = MaxUsageOutboxAttempts // RecordAttemptError reports the cap
	poster := newFakePoster()
	resolver := &fakeResolver{err: errors.New("resolver down")} // forces recordError

	_ = store.Enqueue(context.Background(), PendingUsageRow{
		ID: uuid.New(), TenantID: uuid.New(), Model: "m", PromptTokens: 1, CompletionTokens: 1, Reason: "resolve_error",
	})
	<-store.enqueueCh

	w := NewRetryWorker(store, poster, resolver, Config{})
	w.drainOnce(context.Background())

	if delta := testutil.ToFloat64(metricRetryExhausted) - before; delta != 1 {
		t.Errorf("retry_exhausted delta = %v, want 1", delta)
	}
}
