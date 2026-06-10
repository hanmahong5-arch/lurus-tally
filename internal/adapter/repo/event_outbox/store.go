// Package event_outbox provides the SQL implementation of the nats.OutboxStore interface.
// The outbox pattern guarantees that events are not lost when NATS is temporarily
// unavailable: events are written atomically with the business transaction and drained
// by a background worker once NATS recovers.
package event_outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	adapternats "github.com/hanmahong5-arch/lurus-tally/internal/adapter/nats"
)

// Store implements adapternats.OutboxStore and adapternats.OutboxEnqueuer
// using a PostgreSQL *sql.DB.
type Store struct {
	db *sql.DB
}

// New constructs a Store backed by the given *sql.DB.
func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// Compile-time interface assertions.
var (
	_ adapternats.OutboxStore    = (*Store)(nil)
	_ adapternats.OutboxEnqueuer = (*Store)(nil)
)

// Enqueue inserts one outbox row inside the provided transaction.
// Must be called from within the same *sql.Tx that writes the business row
// so both writes are committed or rolled back atomically.
func (s *Store) Enqueue(ctx context.Context, tx *sql.Tx, tenantID uuid.UUID, subject string, payload json.RawMessage) error {
	const q = `
		INSERT INTO tally.event_outbox (id, tenant_id, subject, payload, created_at)
		VALUES ($1, $2, $3, $4, $5)`
	_, err := tx.ExecContext(ctx, q, uuid.New(), tenantID, subject, []byte(payload), time.Now().UTC())
	if err != nil {
		return fmt.Errorf("event_outbox: enqueue: %w", err)
	}
	return nil
}

// Drain returns up to limit unpublished rows ordered by created_at ASC.
// It executes with SET LOCAL app.tenant_id = 'service' so the RLS policy
// grants access to all tenant rows (the drain worker acts as a service identity).
func (s *Store) Drain(ctx context.Context, limit int) ([]adapternats.OutboxRow, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("event_outbox: drain begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `SET LOCAL app.tenant_id = 'service'`); err != nil {
		return nil, fmt.Errorf("event_outbox: drain set tenant: %w", err)
	}

	// MaxAttempts gates a poison row from being retried forever; the row stays
	// in the table so an operator can inspect last_error and decide manually.
	//
	// FOR UPDATE SKIP LOCKED makes the drain safe to run from more than one
	// replica: each worker locks the rows it claims for the lifetime of this
	// tx and other workers skip them instead of blocking, so no two drains
	// publish the same row in the same cycle.
	const q = `
		SELECT id, subject, payload
		FROM tally.event_outbox
		WHERE published_at IS NULL AND attempts < $2
		ORDER BY created_at
		LIMIT $1
		FOR UPDATE SKIP LOCKED`
	rows, err := tx.QueryContext(ctx, q, limit, adapternats.MaxOutboxAttempts)
	if err != nil {
		return nil, fmt.Errorf("event_outbox: drain query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []adapternats.OutboxRow
	for rows.Next() {
		var r adapternats.OutboxRow
		var rawPayload []byte
		if err := rows.Scan(&r.ID, &r.Subject, &rawPayload); err != nil {
			return nil, fmt.Errorf("event_outbox: drain scan: %w", err)
		}
		r.Payload = json.RawMessage(rawPayload)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("event_outbox: drain rows: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("event_outbox: drain commit: %w", err)
	}
	return out, nil
}

// MarkPublished sets published_at = now() for the given row.
// Uses SET LOCAL app.tenant_id = 'service' so RLS is satisfied.
func (s *Store) MarkPublished(ctx context.Context, id uuid.UUID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("event_outbox: mark published begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `SET LOCAL app.tenant_id = 'service'`); err != nil {
		return fmt.Errorf("event_outbox: mark published set tenant: %w", err)
	}

	const q = `UPDATE tally.event_outbox SET published_at = $1 WHERE id = $2`
	if _, err := tx.ExecContext(ctx, q, time.Now().UTC(), id); err != nil {
		return fmt.Errorf("event_outbox: mark published: %w", err)
	}
	return tx.Commit()
}

// PendingStats returns a lightweight aggregate of the pending outbox queue.
// Uses SET LOCAL app.tenant_id = 'service' so RLS is satisfied across all tenants.
func (s *Store) PendingStats(ctx context.Context) (adapternats.OutboxPendingStats, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return adapternats.OutboxPendingStats{}, fmt.Errorf("event_outbox: pending stats begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `SET LOCAL app.tenant_id = 'service'`); err != nil {
		return adapternats.OutboxPendingStats{}, fmt.Errorf("event_outbox: pending stats set tenant: %w", err)
	}

	const q = `
		SELECT
			COUNT(*),
			COALESCE(EXTRACT(EPOCH FROM (now() - MIN(created_at))), 0)
		FROM tally.event_outbox
		WHERE published_at IS NULL AND attempts < $1`
	var stats adapternats.OutboxPendingStats
	if err := tx.QueryRowContext(ctx, q, adapternats.MaxOutboxAttempts).Scan(
		&stats.PendingCount,
		&stats.OldestAgeSeconds,
	); err != nil {
		return adapternats.OutboxPendingStats{}, fmt.Errorf("event_outbox: pending stats query: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return adapternats.OutboxPendingStats{}, fmt.Errorf("event_outbox: pending stats commit: %w", err)
	}
	return stats, nil
}

// RecordAttemptError increments attempts and persists the error message.
// Uses SET LOCAL app.tenant_id = 'service' so RLS is satisfied.
func (s *Store) RecordAttemptError(ctx context.Context, id uuid.UUID, lastErr string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("event_outbox: record error begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `SET LOCAL app.tenant_id = 'service'`); err != nil {
		return fmt.Errorf("event_outbox: record error set tenant: %w", err)
	}

	const q = `
		UPDATE tally.event_outbox
		SET attempts = attempts + 1, last_error = $1
		WHERE id = $2`
	if _, err := tx.ExecContext(ctx, q, lastErr, id); err != nil {
		return fmt.Errorf("event_outbox: record attempt error: %w", err)
	}
	return tx.Commit()
}
