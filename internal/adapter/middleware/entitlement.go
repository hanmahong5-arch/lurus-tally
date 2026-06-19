package middleware

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// EntitlementChecker is the app-layer port the gate calls.
// *entitlement.Service satisfies it.
type EntitlementChecker interface {
	Has(ctx context.Context, tenantID uuid.UUID, key string) (bool, error)
}

// RequireEntitlement returns middleware that blocks the request unless the
// caller's TENANT plan grants `key`. The tenant is read from context (set by
// AuthMiddleware for BOTH the JWT and PAT paths), so the gate works uniformly
// for owner users, non-owner members, and PAT/automation callers — tally's plan
// is per-tenant, not per-user. MUST run after auth.
//
//   - no tenant          → 401 unauthenticated
//   - entitlement absent → 403 feature_not_in_plan
//   - granted / degraded → next (the checker fails open on platform outage)
func RequireEntitlement(checker EntitlementChecker, key string) gin.HandlerFunc {
	return func(c *gin.Context) {
		allowed, err := checker.Has(c.Request.Context(), GetTenantID(c), key)
		if err != nil {
			// The checker fails OPEN on platform degrade (returns allowed, nil), so
			// a non-nil error here means the caller carried no tenant context
			// (not authenticated) — reject as unauthorized.
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
			return
		}
		if !allowed {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":       "feature_not_in_plan",
				"message":     "this feature requires a higher subscription plan",
				"entitlement": key,
			})
			return
		}
		c.Next()
	}
}
