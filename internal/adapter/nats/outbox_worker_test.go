package nats_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	adapternats "github.com/hanmahong5-arch/lurus-tally/internal/adapter/nats"
)

// fakeOutboxStore is an in-memory OutboxStore for worker tests.
type fakeOutboxStore struct {
	mu          sync.Mutex
	rows        []adapternats.OutboxRow
	published   map[uuid.UUID]bool
	attempts    map[uuid.UUID]int
	lastErrors  map[uuid.UUID]string
	drainErr    error
}

func newFakeOutboxStore(rows []adapternats.OutboxRow) *fakeOutboxStore {
	return &fakeOutboxStore{
		rows:       rows,
		published:  make(map[uuid.UUID]bool),
		attempts:   make(map[uuid.UUID]int),
		lastErrors: make(map[uuid.UUID]string),
	}
}

func (s *fakeOutboxStore) Drain(_ context.Context, limit int) ([]adapternats.OutboxRow, error) {
	if s.drainErr != nil {
		return nil, s.drainErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []adapternats.OutboxRow
	for _, r := range s.rows {
		if s.published[r.ID] {
			continue
		}
		out = append(out, r)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *fakeOutboxStore) MarkPublished(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.published[id] = true
	return nil
}

func (s *fakeOutboxStore) RecordAttemptError(_ context.Context, id uuid.UUID, lastErr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts[id]++
	s.lastErrors[id] = lastErr
	return nil
}

// fakePublisher is a controllable Publisher stub.
type fakePublisher struct {
	mu        sync.Mutex
	published []string // subject of each successful publish
	publishErr error   // when set, every Publish call returns this error
}

func (f *fakePublisher) Publish(_ context.Context, subject string, _ any) error {
	if f.publishErr != nil {
		return f.publishErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.published = append(f.published, subject)
	return nil
}

func (f *fakePublisher) PublishStockMovementRecorded(_ context.Context, _ string, _ adapternats.StockMovementRecordedPayload) error {
	return nil
}
func (f *fakePublisher) PublishStockSnapshotUpdated(_ context.Context, _ string, _ adapternats.StockSnapshotUpdatedPayload) error {
	return nil
}
func (f *fakePublisher) PublishBillCreated(_ context.Context, _ string, _ adapternats.BillCreatedPayload) error {
	return nil
}
func (f *fakePublisher) PublishBillApproved(_ context.Context, _ string, _ adapternats.BillApprovedPayload) error {
	return nil
}
func (f *fakePublisher) PublishBillRejected(_ context.Context, _ string, _ adapternats.BillRejectedPayload) error {
	return nil
}
func (f *fakePublisher) PublishLowStockAlert(_ context.Context, _ string, _ adapternats.LowStockAlertPayload) error {
	return nil
}
func (f *fakePublisher) PublishWebTelemetry(_ context.Context, _ string, _ string, _ any) error {
	return nil
}
func (f *fakePublisher) Close() error { return nil }

func makeRow(subject string) adapternats.OutboxRow {
	return adapternats.OutboxRow{
		ID:      uuid.New(),
		Subject: subject,
		Payload: json.RawMessage(`{"event_type":"stock.movement_recorded"}`),
	}
}

// TestOutboxWorker_DrainSuccess verifies that when NATS is up, the worker
// publishes each row and marks it as published in the store.
func TestOutboxWorker_DrainSuccess(t *testing.T) {
	row := makeRow("PSI_EVENTS.stock.movement_recorded")
	store := newFakeOutboxStore([]adapternats.OutboxRow{row})
	pub := &fakePublisher{}

	worker := adapternats.NewOutboxWorker(store, pub, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// drainOnce is unexported; drive through Run with a very short poll interval
	// by cancelling context after the first drain completes.
	// We expose drain indirectly: Run calls drainOnce immediately on startup.
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Run(ctx)
	}()

	// Give the worker time to process the first drain.
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	pub.mu.Lock()
	publishedCount := len(pub.published)
	pub.mu.Unlock()

	if publishedCount != 1 {
		t.Errorf("expected 1 publish, got %d", publishedCount)
	}

	store.mu.Lock()
	wasMarked := store.published[row.ID]
	store.mu.Unlock()

	if !wasMarked {
		t.Error("row should be marked published after successful NATS publish")
	}
}

// TestOutboxWorker_NATSDown_RowNotMarkedPublished verifies that when NATS is down,
// the row is NOT marked as published — it stays in the outbox for retry — and
// RecordAttemptError is called to track the failure.
func TestOutboxWorker_NATSDown_RowNotMarkedPublished(t *testing.T) {
	row := makeRow("PSI_EVENTS.stock.movement_recorded")
	store := newFakeOutboxStore([]adapternats.OutboxRow{row})
	pub := &fakePublisher{publishErr: errors.New("nats: connection refused")}

	worker := adapternats.NewOutboxWorker(store, pub, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Run(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	store.mu.Lock()
	wasMarked := store.published[row.ID]
	attempts := store.attempts[row.ID]
	store.mu.Unlock()

	if wasMarked {
		t.Error("row must NOT be marked published when NATS publish failed")
	}
	if attempts < 1 {
		t.Errorf("expected at least 1 recorded attempt, got %d", attempts)
	}
}

// TestOutboxWorker_EmptyDrain_NoPublishes confirms that an empty outbox
// results in zero publishes and no panics.
func TestOutboxWorker_EmptyDrain_NoPublishes(t *testing.T) {
	store := newFakeOutboxStore(nil)
	pub := &fakePublisher{}

	worker := adapternats.NewOutboxWorker(store, pub, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Run(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	pub.mu.Lock()
	publishedCount := len(pub.published)
	pub.mu.Unlock()

	if publishedCount != 0 {
		t.Errorf("expected 0 publishes for empty outbox, got %d", publishedCount)
	}
}
