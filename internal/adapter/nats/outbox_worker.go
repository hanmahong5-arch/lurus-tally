package nats

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

const (
	outboxPollInterval = 30 * time.Second
	outboxDrainLimit   = 100
	// MaxOutboxAttempts is the inclusive ceiling before a row is skipped on
	// subsequent polls. The row remains in the table for manual inspection.
	// Exported because the SQL repo references it in the Drain WHERE clause —
	// the gate must be enforced server-side so a runaway row cannot saturate
	// the worker each tick.
	MaxOutboxAttempts = 10
)

// OutboxStore is the minimal contract the worker needs from the outbox persistence layer.
// Implemented by internal/adapter/repo/event_outbox.Store; a fake can be injected in tests.
type OutboxStore interface {
	// Drain returns up to limit unpublished rows ordered by created_at ASC.
	Drain(ctx context.Context, limit int) ([]OutboxRow, error)
	// MarkPublished records a successful publish (sets published_at).
	MarkPublished(ctx context.Context, id uuid.UUID) error
	// RecordAttemptError increments attempts and persists the error message.
	RecordAttemptError(ctx context.Context, id uuid.UUID, lastErr string) error
}

// OutboxRow is one pending entry returned by OutboxStore.Drain.
type OutboxRow struct {
	ID      uuid.UUID
	Subject string
	Payload json.RawMessage
}

// OutboxEnqueuer is the write-side contract used by use cases to insert an outbox
// row inside an existing DB transaction. Keeping it narrow (one method) lets the
// use case import just this interface without depending on the full OutboxStore.
type OutboxEnqueuer interface {
	Enqueue(ctx context.Context, tx *sql.Tx, tenantID uuid.UUID, subject string, payload json.RawMessage) error
}

// OutboxWorker drains the event_outbox table into NATS on a fixed tick.
// It runs as a background goroutine started by lifecycle/app.go.
type OutboxWorker struct {
	store OutboxStore
	pub   Publisher
	log   *slog.Logger
}

// NewOutboxWorker constructs an OutboxWorker.
// pub must not be nil; if NATS is unavailable use a no-op publisher (drain will
// mark rows as failed and retry on the next tick once NATS recovers).
func NewOutboxWorker(store OutboxStore, pub Publisher, log *slog.Logger) *OutboxWorker {
	if log == nil {
		log = slog.Default()
	}
	return &OutboxWorker{store: store, pub: pub, log: log}
}

// Run loops until ctx is cancelled, draining the outbox every outboxPollInterval.
// Call this in a goroutine: go worker.Run(ctx).
func (w *OutboxWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(outboxPollInterval)
	defer ticker.Stop()

	// Drain once on startup so a recent backlog is cleared before the first tick.
	w.drainOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.drainOnce(ctx)
		}
	}
}

func (w *OutboxWorker) drainOnce(ctx context.Context) {
	rows, err := w.store.Drain(ctx, outboxDrainLimit)
	if err != nil {
		w.log.Error("outbox: drain failed",
			slog.String("error", err.Error()),
		)
		return
	}

	for _, row := range rows {
		// Skip rows that have already exhausted their attempt budget.
		// They remain in the table so ops can inspect and replay manually.
		w.processRow(ctx, row)
	}
}

func (w *OutboxWorker) processRow(ctx context.Context, row OutboxRow) {
	// Publish the raw JSON payload verbatim to the stored NATS subject.
	// The payload was produced by buildEvent inside the tx, so it is already
	// a fully-formed Event envelope.
	if err := w.pub.Publish(ctx, row.Subject, row.Payload); err != nil {
		errMsg := err.Error()
		w.log.Warn("outbox: publish failed, will retry",
			slog.String("id", row.ID.String()),
			slog.String("subject", row.Subject),
			slog.String("error", errMsg),
		)
		if recErr := w.store.RecordAttemptError(ctx, row.ID, errMsg); recErr != nil {
			w.log.Error("outbox: failed to record attempt error",
				slog.String("id", row.ID.String()),
				slog.String("error", recErr.Error()),
			)
		}
		return
	}

	if err := w.store.MarkPublished(ctx, row.ID); err != nil {
		// The message was published but we failed to mark it. On next poll it
		// will be re-published (duplicate). Downstream consumers should be
		// idempotent on event_id.
		w.log.Error("outbox: published to NATS but failed to mark row as done",
			slog.String("id", row.ID.String()),
			slog.String("error", err.Error()),
		)
	}
}
