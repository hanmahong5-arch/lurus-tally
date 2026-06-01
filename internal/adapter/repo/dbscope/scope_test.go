package dbscope

import (
	"context"
	"database/sql"
	"testing"
)

// TestFrom_NoPin_ReturnsFallback verifies that, with nothing stored in context,
// From hands back the caller's fallback pool unchanged — the path every
// un-pinned request and background worker takes.
func TestFrom_NoPin_ReturnsFallback(t *testing.T) {
	db := &sql.DB{} // never used; identity comparison only.
	got := From(context.Background(), db)
	if got != Querier(db) {
		t.Fatalf("From with no pinned conn = %v, want the fallback *sql.DB", got)
	}
}

// TestWith_NilConn_ReturnsSameContext verifies the nil-conn guard: With must not
// wrap a nil connection (which would later be returned as a usable handle).
func TestWith_NilConn_ReturnsSameContext(t *testing.T) {
	ctx := context.Background()
	if With(ctx, nil) != ctx {
		t.Fatal("With(ctx, nil) must return the original context")
	}
	// And From on that context still yields the fallback.
	db := &sql.DB{}
	if got := From(With(ctx, nil), db); got != Querier(db) {
		t.Fatalf("From after With(nil) = %v, want fallback", got)
	}
}
