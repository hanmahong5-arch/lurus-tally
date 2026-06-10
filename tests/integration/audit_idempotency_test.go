//go:build integration

// Package integration — audit-log idempotency against at-least-once redelivery.
//
// The audit subscriber consumes PSI_EVENTS with MaxDeliver:5 and Naks on
// transient failure, so the same business event can be delivered more than once.
// Migration 000049 + the repo's ON CONFLICT (event_id) make those redeliveries
// collapse to a single row. This test proves that against a real PostgreSQL
// container — the DB UNIQUE index is the judge, not the application.
//
// Run: go test -v -tags integration -timeout 180s ./tests/integration/ -run Audit
package integration

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"

	repoaccount "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/account"
	appacct "github.com/hanmahong5-arch/lurus-tally/internal/app/account"
	"github.com/hanmahong5-arch/lurus-tally/internal/lifecycle"
)

// TestAudit_EventID_DeduplicatesRedelivery proves: the same event_id delivered N
// times yields exactly one row; distinct event_ids each insert; an empty
// event_id falls back to insert-always (no dedup).
func TestAudit_EventID_DeduplicatesRedelivery(t *testing.T) {
	dsn, cleanup := startPostgres(t)
	defer cleanup()

	ctx := context.Background()
	if err := lifecycle.RunMigrations(ctx, dsn, nil); err != nil {
		t.Fatalf("RunMigrations (must include 000049): %v", err)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()

	tenantID := uuid.New()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO tally.tenant (id, name) VALUES ($1, $2)`, tenantID, "audit-idem-tenant"); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	uc := appacct.NewAppendAuditLog(repoaccount.NewAuditRepo(db))

	// ── 1. same event_id delivered 5 times → exactly 1 row ──────────────────
	eventID := uuid.New().String()
	for i := 0; i < 5; i++ {
		if err := uc.Execute(ctx, appacct.AppendInput{
			TenantID:   tenantID,
			ActorID:    "tally",
			Action:     "bill.approved",
			TargetKind: "bill",
			TargetID:   "bill-1",
			EventID:    eventID,
		}); err != nil {
			t.Fatalf("redelivery #%d failed (ON CONFLICT must not error): %v", i, err)
		}
	}
	if got := countByEventID(t, db, eventID); got != 1 {
		t.Fatalf("same event_id delivered 5x: want exactly 1 row, got %d", got)
	}

	// ── 2. two distinct event_ids → one row each ────────────────────────────
	for _, id := range []string{uuid.New().String(), uuid.New().String()} {
		if err := uc.Execute(ctx, appacct.AppendInput{
			TenantID: tenantID, ActorID: "tally", Action: "bill.approved", EventID: id,
		}); err != nil {
			t.Fatalf("distinct event insert: %v", err)
		}
	}

	// ── 3. empty event_id falls back to insert-always (NULL never conflicts) ─
	for i := 0; i < 3; i++ {
		if err := uc.Execute(ctx, appacct.AppendInput{
			TenantID: tenantID, ActorID: "tally", Action: "pat.created",
		}); err != nil {
			t.Fatalf("empty event_id insert #%d: %v", i, err)
		}
	}

	// Total = 1 (deduped) + 2 (distinct) + 3 (no-dedup NULL) = 6.
	if got := countByTenant(t, db, tenantID); got != 6 {
		t.Fatalf("total rows: want 6, got %d", got)
	}
	if got := countNullEventID(t, db, tenantID); got != 3 {
		t.Fatalf("NULL event_id rows: want 3, got %d", got)
	}
	t.Logf("evidence: 5 redeliveries→1 row, 2 distinct→2 rows, 3 empty→3 rows (total 6)")
}

func countByEventID(t *testing.T, db *sql.DB, eventID string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM tally.account_audit_log WHERE event_id = $1`, eventID).Scan(&n); err != nil {
		t.Fatalf("countByEventID: %v", err)
	}
	return n
}

func countByTenant(t *testing.T, db *sql.DB, tenantID uuid.UUID) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM tally.account_audit_log WHERE tenant_id = $1`, tenantID).Scan(&n); err != nil {
		t.Fatalf("countByTenant: %v", err)
	}
	return n
}

func countNullEventID(t *testing.T, db *sql.DB, tenantID uuid.UUID) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM tally.account_audit_log WHERE tenant_id = $1 AND event_id IS NULL`, tenantID).Scan(&n); err != nil {
		t.Fatalf("countNullEventID: %v", err)
	}
	return n
}
