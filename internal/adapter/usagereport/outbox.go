package usagereport

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// MaxUsageOutboxAttempts is the inclusive ceiling before a durably-queued usage
// row stops being retried (it stays in the table for inspection). Mirrors
// nats.MaxOutboxAttempts. Enforced server-side in the store's Drain WHERE clause
// so a poison row cannot saturate the worker every tick.
const MaxUsageOutboxAttempts = 10

// PendingUsageRow is one durably-queued usage event awaiting (re)delivery to the
// platform metering ingest. It carries the tenant (re-resolved to an account at
// drain time, so a tenant provisioned AFTER the drop is back-reported) and the
// token counts — never an account id, which may not have existed when queued.
type PendingUsageRow struct {
	ID               uuid.UUID // stable; also the idempotency seed (usageIdemKey)
	TenantID         uuid.UUID
	Model            string
	PromptTokens     int
	CompletionTokens int
	OccurredAt       time.Time
	Reason           string // why it was queued: no_account | resolve_error | post_error
}

// UsagePendingStats is the observability snapshot the retry worker reads each tick.
type UsagePendingStats struct {
	PendingCount     int64
	OldestAgeSeconds float64
}

// UsageOutbox is the durable retry queue contract. Implemented by
// internal/adapter/repo/usage_report_outbox.Store; a fake is injected in tests.
// Kept in this package (mirroring nats.OutboxStore) so the reporter + retry
// worker depend only on the interface, not the SQL repo (no import cycle).
type UsageOutbox interface {
	// Enqueue durably persists one row. Unlike event_outbox.Enqueue it takes NO
	// caller tx — the reporter worker has no business transaction, so the store
	// opens its own tx + SET LOCAL app.tenant_id='service'.
	Enqueue(ctx context.Context, row PendingUsageRow) error
	// Drain returns up to limit unsent rows (sent_at IS NULL, attempts < cap),
	// FOR UPDATE SKIP LOCKED so multiple replicas don't double-send.
	Drain(ctx context.Context, limit int) ([]PendingUsageRow, error)
	// MarkSent records a successful (re)delivery (sets sent_at).
	MarkSent(ctx context.Context, id uuid.UUID) error
	// RecordAttemptError increments attempts + persists the error, returning the
	// new attempts count so the worker can fire the exhausted signal at the cap.
	RecordAttemptError(ctx context.Context, id uuid.UUID, lastErr string) (int, error)
	// PendingStats returns a lightweight aggregate for the gauges.
	PendingStats(ctx context.Context) (UsagePendingStats, error)
}

// usageIdemKey derives the stable platform idempotency key from a row's id.
// Stable across every retry → platform dedups, so a partial success (POST
// landed, MarkSent failed) never double-counts. Well within platform's key length.
func usageIdemKey(id uuid.UUID) string {
	return "tally-usage-" + id.String()
}
