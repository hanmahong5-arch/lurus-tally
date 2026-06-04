// Package dbscope carries a per-request, tenant-pinned database handle through
// context so repositories can run their queries on the connection that has had
// `app.tenant_id` set (activating the RLS backstop) without changing a single
// query body.
//
// The flow is: middleware.TenantDB acquires one *sql.Conn for the request,
// sets app.tenant_id on it, and stows it here via With. A repository calls
// From(ctx, r.db) at the top of each method and runs its query on the returned
// Querier -- the pinned connection when one exists, otherwise the shared pool.
package dbscope

import (
	"context"
	"database/sql"
	"log/slog"
	"runtime/debug"
	"sync/atomic"
)

// Querier is the minimal database/sql surface shared by *sql.DB, *sql.Conn and
// *sql.Tx. Repositories depend on this so they behave identically whether the
// handle is the shared pool or a tenant-pinned connection.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// pinnedConn wraps the request-pinned *sql.Conn with a concurrency tripwire.
//
// A *sql.Conn serializes operations internally: issuing two queries on it at the
// same time -- an errgroup/goroutine fan-out within ONE pinned request, or a
// detached goroutine that outlives the handler -- surfaces only as a cryptic
// "driver: bad connection" far from the cause. This wrapper detects overlapping
// Query/Exec CALLS and emits one loud, actionable slog.Error (with a stack)
// naming the hazard, turning that heisenbug into an immediate diagnosis. Two real
// instances each cost real debugging time: the weekly-summary digest's parallel
// aggregates and the CSV export's detached producer.
//
// It deliberately neither panics nor serializes. A panic in an errgroup child or
// a detached goroutine is unrecovered (gin.Recovery only wraps the request
// goroutine) and would crash the process -- the very fan-out shapes this catches.
// Blocking to serialize would deadlock the rows-still-open case. So it is a
// DETECTOR, not an enforcer: the invariant is "per-request DB access is sequential
// on the pinned conn". The detector covers overlapping calls; a query issued while
// a prior *sql.Rows is still open is guarded instead by repos keeping iteration
// sequential and joining any producer goroutine before the conn is released.
type pinnedConn struct {
	conn *sql.Conn
	busy atomic.Bool
}

// mark claims the connection for one operation and returns a release func. When
// the connection is already in use it logs the violation once and returns a no-op
// release so it does not clear the owning caller's claim.
func (p *pinnedConn) mark(op string) func() {
	if !p.busy.CompareAndSwap(false, true) {
		slog.Error("dbscope: concurrent use of a tenant-pinned connection",
			slog.String("op", op),
			slog.String("invariant", "per-request DB access must be sequential on the pinned *sql.Conn: do not fan out repo calls with errgroup/goroutines, and join any detached producer before the handler returns"),
			slog.String("stack", string(debug.Stack())),
		)
		return func() {}
	}
	return func() { p.busy.Store(false) }
}

func (p *pinnedConn) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	defer p.mark("Query")()
	return p.conn.QueryContext(ctx, query, args...)
}

func (p *pinnedConn) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	defer p.mark("QueryRow")()
	return p.conn.QueryRowContext(ctx, query, args...)
}

func (p *pinnedConn) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	defer p.mark("Exec")()
	return p.conn.ExecContext(ctx, query, args...)
}

// ctxKey is unexported so only this package can read or write the pinned handle.
type ctxKey struct{}

// With returns a child context carrying conn as the tenant-pinned handle. A nil
// conn returns ctx unchanged so callers don't have to branch.
func With(ctx context.Context, conn *sql.Conn) context.Context {
	if conn == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, &pinnedConn{conn: conn})
}

// From returns the tenant-pinned handle stored in ctx, or fallback when no
// connection was pinned for this request -- the case for pre-onboarding
// requests (tenant still uuid.Nil), background workers, and repositories not
// yet migrated to dbscope. Passing the repo's own *sql.DB as fallback keeps
// behaviour unchanged when nothing is pinned.
func From(ctx context.Context, fallback *sql.DB) Querier {
	if p, ok := ctx.Value(ctxKey{}).(*pinnedConn); ok && p != nil {
		return p
	}
	return fallback
}

// BeginTx starts a transaction on the tenant-pinned connection when the request
// pinned one, or on the shared pool otherwise. Beginning on the pinned conn is
// what lets a transaction inherit the session-level app.tenant_id (hazard H8):
// the RLS policies then bind every write inside the tx, not just reads. Repos
// route their WithTx through this so the choice is made in one place.
func BeginTx(ctx context.Context, fallback *sql.DB, opts *sql.TxOptions) (*sql.Tx, error) {
	if p, ok := ctx.Value(ctxKey{}).(*pinnedConn); ok && p != nil {
		return p.conn.BeginTx(ctx, opts)
	}
	return fallback.BeginTx(ctx, opts)
}

// WithPinnedConn acquires a connection, sets app.tenant_id on it, runs fn with a
// context carrying that connection (so dbscope.From / BeginTx inside fn use it),
// then RESETs the GUC and releases the connection. It is the non-HTTP analogue
// of middleware.TenantDB: for entry points that resolve their tenant OUTSIDE the
// request-auth middleware but still need the RLS backstop -- e.g. the public
// shopify webhook, which resolves shop->tenant itself before importing.
//
// A nil db or empty tenantID runs fn unpinned (on the shared pool), so callers
// can wire it unconditionally and degrade to WHERE-only behaviour.
func WithPinnedConn(ctx context.Context, db *sql.DB, tenantID string, fn func(context.Context) error) error {
	if db == nil || tenantID == "" {
		return fn(ctx)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() {
		// Detached context so the scrub runs even if ctx was cancelled; a broken
		// conn is discarded by Close rather than returned tainted to the pool.
		_, _ = conn.ExecContext(context.Background(), "RESET app.tenant_id")
		_ = conn.Close()
	}()
	if _, err := conn.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, false)", tenantID); err != nil {
		return err
	}
	return fn(With(ctx, conn))
}
