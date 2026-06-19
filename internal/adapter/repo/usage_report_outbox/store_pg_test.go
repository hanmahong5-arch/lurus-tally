// Package usage_report_outbox_test holds the PG integration test for the durable
// usage-retry store. It needs a real PostgreSQL with the tally schema + FORCE RLS
// (migrations applied, incl. 000053) and is skipped when DATABASE_DSN is unset.
// The RLS `SET LOCAL app.tenant_id='service'` drain path cannot be exercised by
// the hermetic SQLite shim, so the SQL round-trip is proven here.
package usage_report_outbox_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	repousageoutbox "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/usage_report_outbox"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/usagereport"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_DSN")
	if dsn == "" {
		t.Skip("DATABASE_DSN not set — skipping integration test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestUsageOutboxPG_RoundTrip exercises every store method against real PG + RLS:
// enqueue (own-tx service pin) → drain (service bypass) → record attempt error
// (RETURNING attempts) → mark sent → no longer drained.
func TestUsageOutboxPG_RoundTrip(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	store := repousageoutbox.New(db)

	id := uuid.New()
	tid := uuid.New()
	occurred := time.Now().UTC().Truncate(time.Second)
	row := usagereport.PendingUsageRow{
		ID: id, TenantID: tid, Model: "deepseek-v4",
		PromptTokens: 12, CompletionTokens: 8, OccurredAt: occurred, Reason: "no_account",
	}

	if err := store.Enqueue(ctx, row); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM tally.usage_report_outbox WHERE id=$1", id)
	})

	// Drain returns our row with fields intact (proves the service-pin bypass).
	got := findRow(t, store, id)
	if got.TenantID != tid || got.Model != "deepseek-v4" ||
		got.PromptTokens != 12 || got.CompletionTokens != 8 || got.Reason != "no_account" {
		t.Errorf("drained row = %+v, want the enqueued values", got)
	}

	// RecordAttemptError increments and returns the new count.
	n, err := store.RecordAttemptError(ctx, id, "platform unreachable")
	if err != nil {
		t.Fatalf("record attempt error: %v", err)
	}
	if n != 1 {
		t.Errorf("attempts after first error = %d, want 1", n)
	}

	// Still pending (attempts < cap) and counted.
	stats, err := store.PendingStats(ctx)
	if err != nil {
		t.Fatalf("pending stats: %v", err)
	}
	if stats.PendingCount < 1 {
		t.Errorf("pending count = %d, want >= 1", stats.PendingCount)
	}
	if drained := findRowOpt(t, store, id); drained == nil {
		t.Error("row should still be drainable after one attempt error")
	}

	// MarkSent → no longer drained.
	if err := store.MarkSent(ctx, id); err != nil {
		t.Fatalf("mark sent: %v", err)
	}
	if drained := findRowOpt(t, store, id); drained != nil {
		t.Error("row should NOT be drained after MarkSent")
	}
}

func findRow(t *testing.T, store *repousageoutbox.Store, id uuid.UUID) usagereport.PendingUsageRow {
	t.Helper()
	r := findRowOpt(t, store, id)
	if r == nil {
		t.Fatalf("row %s not found in drain", id)
	}
	return *r
}

func findRowOpt(t *testing.T, store *repousageoutbox.Store, id uuid.UUID) *usagereport.PendingUsageRow {
	t.Helper()
	rows, err := store.Drain(context.Background(), 1000)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	for i := range rows {
		if rows[i].ID == id {
			return &rows[i]
		}
	}
	return nil
}
