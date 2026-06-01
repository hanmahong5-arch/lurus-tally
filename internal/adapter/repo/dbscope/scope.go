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
)

// Querier is the minimal database/sql surface shared by *sql.DB, *sql.Conn and
// *sql.Tx. Repositories depend on this so they behave identically whether the
// handle is the shared pool or a tenant-pinned connection.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// ctxKey is unexported so only this package can read or write the pinned handle.
type ctxKey struct{}

// With returns a child context carrying conn as the tenant-pinned handle. A nil
// conn returns ctx unchanged so callers don't have to branch.
func With(ctx context.Context, conn *sql.Conn) context.Context {
	if conn == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, conn)
}

// From returns the tenant-pinned handle stored in ctx, or fallback when no
// connection was pinned for this request -- the case for pre-onboarding
// requests (tenant still uuid.Nil), background workers, and repositories not
// yet migrated to dbscope. Passing the repo's own *sql.DB as fallback keeps
// behaviour unchanged when nothing is pinned.
func From(ctx context.Context, fallback *sql.DB) Querier {
	if conn, ok := ctx.Value(ctxKey{}).(*sql.Conn); ok && conn != nil {
		return conn
	}
	return fallback
}

// BeginTx starts a transaction on the tenant-pinned connection when the request
// pinned one, or on the shared pool otherwise. Beginning on the pinned conn is
// what lets a transaction inherit the session-level app.tenant_id (hazard H8):
// the RLS policies then bind every write inside the tx, not just reads. Repos
// route their WithTx through this so the choice is made in one place.
func BeginTx(ctx context.Context, fallback *sql.DB, opts *sql.TxOptions) (*sql.Tx, error) {
	if conn, ok := ctx.Value(ctxKey{}).(*sql.Conn); ok && conn != nil {
		return conn.BeginTx(ctx, opts)
	}
	return fallback.BeginTx(ctx, opts)
}
