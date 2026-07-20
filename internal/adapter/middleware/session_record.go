package middleware

import (
	"context"
	"net"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// SessionRecorderFn is the function signature this middleware depends on.
// Implemented by (*app/account.RecordSession).Execute.
type SessionRecorderFn func(ctx context.Context, tenantID uuid.UUID, userID, userAgent string, ip net.IP) error

// SessionRecord returns a Gin middleware that fires fn after AuthMiddleware
// has resolved tenant_id + sub. Errors are dropped (best-effort
// observability — a flaky DB on session_record must not 500 the request).
// A nil fn renders the middleware inert so tests / dev runs don't need to
// wire it.
//
// MUST be installed AFTER AuthMiddleware so the context carries tenant + sub.
func SessionRecord(fn SessionRecorderFn) gin.HandlerFunc {
	if fn == nil {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		tenantID := GetTenantID(c)
		userID := GetIDPSubject(c)
		if tenantID != uuid.Nil && userID != "" {
			_ = fn(c.Request.Context(), tenantID, userID, c.Request.UserAgent(), parseClientIP(c))
		}
		c.Next()
	}
}

// parseClientIP returns the IP the request appears to come from. Honours
// X-Forwarded-For for trusted reverse proxies (k8s ingress); falls back to
// RemoteAddr otherwise. Returns nil when no IP is parseable.
func parseClientIP(c *gin.Context) net.IP {
	if xff := c.GetHeader("X-Forwarded-For"); xff != "" {
		if i := strings.Index(xff, ","); i >= 0 {
			xff = xff[:i]
		}
		if ip := net.ParseIP(strings.TrimSpace(xff)); ip != nil {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		return nil
	}
	return net.ParseIP(host)
}
