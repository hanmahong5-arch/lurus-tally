package dbscope

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
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

// TestPinnedConn_ConcurrencyTripwire verifies the detector fires exactly when the
// pinned connection is used while already busy, and stays silent for the normal
// sequential pattern. mark() never touches the underlying *sql.Conn, so a nil conn
// is fine here.
func TestPinnedConn_ConcurrencyTripwire(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError})))
	defer slog.SetDefault(prev)

	p := &pinnedConn{}

	// Sequential use: acquire+release, then acquire again → no log.
	p.mark("Query")()
	p.mark("Exec")()
	if strings.Contains(buf.String(), "concurrent use") {
		t.Fatalf("sequential use tripped the detector: %s", buf.String())
	}

	// Overlapping use: hold the first claim, then mark again while still busy.
	release := p.mark("Query")
	buf.Reset()
	p.mark("QueryRow")() // busy → must log once
	if !strings.Contains(buf.String(), "concurrent use of a tenant-pinned connection") {
		t.Errorf("overlapping use did not trip the detector; log=%q", buf.String())
	}
	if !strings.Contains(buf.String(), "QueryRow") {
		t.Errorf("detector log missing the operation name; log=%q", buf.String())
	}
	release()

	// After release the flag is clear again → silent.
	buf.Reset()
	p.mark("Query")()
	if strings.Contains(buf.String(), "concurrent use") {
		t.Errorf("detector false-positive after release; log=%q", buf.String())
	}
}

func TestInTx_JoinsRepoCallsAndCommits(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	mock.ExpectBegin()
	mock.ExpectExec("INSERT a").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT b").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err = InTx(context.Background(), db, func(ctx context.Context) error {
		if TxFrom(ctx) == nil {
			t.Fatal("tx must be visible inside fn")
		}
		if _, err := From(ctx, db).ExecContext(ctx, "INSERT a"); err != nil {
			return err
		}
		// A nested InTx joins the same tx (no second Begin expected).
		return InTx(ctx, db, func(ctx context.Context) error {
			_, err := From(ctx, db).ExecContext(ctx, "INSERT b")
			return err
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestInTx_RollsBackOnError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	mock.ExpectBegin()
	mock.ExpectRollback()
	boom := errors.New("boom")
	if err := InTx(context.Background(), db, func(context.Context) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
