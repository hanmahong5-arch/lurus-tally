package middleware

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
)

// TenantDB pins a single *sql.Conn for the duration of the request, sets
// app.tenant_id on it, and stows it in the request context (via dbscope) so
// RLS-aware repositories run their queries on the connection that carries the
// tenant GUC. The connection is released back to the pool on the way out, with
// app.tenant_id RESET first so a pooled connection never leaks tenant scope to
// the next request.
//
// When the tenant is not yet known (uuid.Nil -- first-time users hitting /me or
// /tenant/profile before onboarding) the middleware is a no-op: no connection
// is pinned and repos fall back to the shared pool. RLS still holds because the
// short-circuit policies treat an unset GUC as "visible, rely on WHERE".
//
// MUST be registered AFTER the auth middleware (which injects tenant_id) and
// BEFORE the idempotency middleware and business handlers.
//
// Leak defence is three-layered (audit hazard H7): the deferred RESET runs even
// on a downstream panic (gin.Recovery is the outermost middleware), RESET
// happens before the connection is returned to the pool, and every migrated
// repository still carries an explicit WHERE tenant_id = $N so a stale GUC
// cannot widen a result set.
func TenantDB(db *sql.DB) gin.HandlerFunc {
	if db == nil {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		tenantID := GetTenantID(c)
		if tenantID == uuid.Nil {
			c.Next()
			return
		}

		ctx := c.Request.Context()
		conn, err := db.Conn(ctx)
		if err != nil {
			// Pool exhausted or DB down -- fail closed rather than serving the
			// request on an unscoped connection (which would lean entirely on
			// hand-written WHERE clauses).
			slog.Error("tenant_db: cannot acquire pinned connection",
				slog.String("tenant_id", tenantID.String()),
				slog.Any("error", err))
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error":  "unavailable",
				"detail": "database connection unavailable, retry shortly",
			})
			return
		}

		// Session-level (is_local=false) so the GUC spans every statement in this
		// request, including any transaction opened later on the same conn
		// (BeginTx on a *sql.Conn inherits the session setting).
		if _, err := conn.ExecContext(ctx,
			"SELECT set_config('app.tenant_id', $1, false)", tenantID.String()); err != nil {
			slog.Error("tenant_db: cannot set app.tenant_id on connection",
				slog.String("tenant_id", tenantID.String()),
				slog.Any("error", err))
			_ = conn.Close()
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error":  "unavailable",
				"detail": "database session setup failed, retry shortly",
			})
			return
		}

		defer func() {
			// Use a detached context so the scrub runs even if the request
			// context was cancelled (client disconnect). On a healthy conn this
			// is instant; on a broken conn ExecContext returns quickly and Close
			// discards it rather than returning a tainted conn to the pool.
			if _, err := conn.ExecContext(context.Background(), "RESET app.tenant_id"); err != nil {
				slog.Warn("tenant_db: RESET app.tenant_id failed; connection will be discarded on close",
					slog.Any("error", err))
			}
			_ = conn.Close()
		}()

		c.Request = c.Request.WithContext(dbscope.With(ctx, conn))
		c.Next()
	}
}
