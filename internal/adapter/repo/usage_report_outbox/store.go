// Package usage_report_outbox is the SQL implementation of
// usagereport.UsageOutbox: a durable retry queue for LLM usage events that
// could not be reported to the platform metering ingest (unprovisioned tenant,
// resolver error, or platform unreachable). Mirrors the event_outbox pattern
// but targets the HTTP ingest, so its rows are typed usage fields rather than a
// NATS subject/payload, and Enqueue opens its own tx (the reporter worker has
// no surrounding business transaction).
package usage_report_outbox

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/usagereport"
)

// Store persists durable usage-retry rows.
type Store struct {
	db *sql.DB
}

// New constructs a Store over the shared pool.
func New(db *sql.DB) *Store { return &Store{db: db} }

var _ usagereport.UsageOutbox = (*Store)(nil)

// Enqueue inserts one row in its OWN transaction under
// SET LOCAL app.tenant_id='service' — the reporter worker runs on a fresh
// background context with no business tx and no request-pinned tenant, so the
// row would be rejected by RLS without this service pin (the key divergence
// from event_outbox.Enqueue, which inherits the caller's pinned tx).
func (s *Store) Enqueue(ctx context.Context, row usagereport.PendingUsageRow) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("usage_report_outbox: enqueue begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `SET LOCAL app.tenant_id = 'service'`); err != nil {
		return fmt.Errorf("usage_report_outbox: enqueue set tenant: %w", err)
	}

	id := row.ID
	if id == uuid.Nil {
		id = uuid.New()
	}
	const q = `
		INSERT INTO tally.usage_report_outbox
			(id, tenant_id, model, prompt_tokens, completion_tokens, occurred_at, reason, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	if _, err := tx.ExecContext(ctx, q,
		id, row.TenantID, row.Model, row.PromptTokens, row.CompletionTokens,
		row.OccurredAt, row.Reason, time.Now().UTC(),
	); err != nil {
		return fmt.Errorf("usage_report_outbox: enqueue: %w", err)
	}
	return tx.Commit()
}

// Drain returns up to limit unsent rows (attempts below the cap), oldest first.
// FOR UPDATE SKIP LOCKED makes it multi-replica safe; the cap is enforced
// server-side so a poison row cannot saturate the worker each tick.
func (s *Store) Drain(ctx context.Context, limit int) ([]usagereport.PendingUsageRow, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("usage_report_outbox: drain begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `SET LOCAL app.tenant_id = 'service'`); err != nil {
		return nil, fmt.Errorf("usage_report_outbox: drain set tenant: %w", err)
	}

	const q = `
		SELECT id, tenant_id, model, prompt_tokens, completion_tokens, occurred_at, reason
		FROM tally.usage_report_outbox
		WHERE sent_at IS NULL AND attempts < $2
		ORDER BY created_at
		LIMIT $1
		FOR UPDATE SKIP LOCKED`
	rows, err := tx.QueryContext(ctx, q, limit, usagereport.MaxUsageOutboxAttempts)
	if err != nil {
		return nil, fmt.Errorf("usage_report_outbox: drain query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []usagereport.PendingUsageRow
	for rows.Next() {
		var r usagereport.PendingUsageRow
		if err := rows.Scan(&r.ID, &r.TenantID, &r.Model, &r.PromptTokens,
			&r.CompletionTokens, &r.OccurredAt, &r.Reason); err != nil {
			return nil, fmt.Errorf("usage_report_outbox: drain scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("usage_report_outbox: drain rows: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("usage_report_outbox: drain commit: %w", err)
	}
	return out, nil
}

// MarkSent records a successful (re)delivery (sets sent_at).
func (s *Store) MarkSent(ctx context.Context, id uuid.UUID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("usage_report_outbox: mark sent begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `SET LOCAL app.tenant_id = 'service'`); err != nil {
		return fmt.Errorf("usage_report_outbox: mark sent set tenant: %w", err)
	}
	const q = `UPDATE tally.usage_report_outbox SET sent_at = $1 WHERE id = $2`
	if _, err := tx.ExecContext(ctx, q, time.Now().UTC(), id); err != nil {
		return fmt.Errorf("usage_report_outbox: mark sent: %w", err)
	}
	return tx.Commit()
}

// RecordAttemptError increments attempts + persists the error, returning the new
// attempts count so the worker can fire the exhausted signal at the cap.
func (s *Store) RecordAttemptError(ctx context.Context, id uuid.UUID, lastErr string) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("usage_report_outbox: record error begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `SET LOCAL app.tenant_id = 'service'`); err != nil {
		return 0, fmt.Errorf("usage_report_outbox: record error set tenant: %w", err)
	}
	const q = `
		UPDATE tally.usage_report_outbox
		SET attempts = attempts + 1, last_error = $1
		WHERE id = $2
		RETURNING attempts`
	var attempts int
	if err := tx.QueryRowContext(ctx, q, lastErr, id).Scan(&attempts); err != nil {
		return 0, fmt.Errorf("usage_report_outbox: record attempt error: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("usage_report_outbox: record error commit: %w", err)
	}
	return attempts, nil
}

// PendingStats returns the pending-queue aggregate for the gauges.
func (s *Store) PendingStats(ctx context.Context) (usagereport.UsagePendingStats, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return usagereport.UsagePendingStats{}, fmt.Errorf("usage_report_outbox: pending stats begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `SET LOCAL app.tenant_id = 'service'`); err != nil {
		return usagereport.UsagePendingStats{}, fmt.Errorf("usage_report_outbox: pending stats set tenant: %w", err)
	}
	const q = `
		SELECT COUNT(*), COALESCE(EXTRACT(EPOCH FROM (now() - MIN(created_at))), 0)
		FROM tally.usage_report_outbox
		WHERE sent_at IS NULL AND attempts < $1`
	var stats usagereport.UsagePendingStats
	if err := tx.QueryRowContext(ctx, q, usagereport.MaxUsageOutboxAttempts).Scan(
		&stats.PendingCount, &stats.OldestAgeSeconds,
	); err != nil {
		return usagereport.UsagePendingStats{}, fmt.Errorf("usage_report_outbox: pending stats query: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return usagereport.UsagePendingStats{}, fmt.Errorf("usage_report_outbox: pending stats commit: %w", err)
	}
	return stats, nil
}
