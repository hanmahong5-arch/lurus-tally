package middleware

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

// EntitlementChecker is the app-layer port the gate calls.
// *entitlement.Service satisfies it.
type EntitlementChecker interface {
	Has(ctx context.Context, sub, key string) (bool, error)
}

// RequireEntitlement returns middleware that blocks the request unless the
// caller's subscription plan grants `key`. The caller's Zitadel sub is read
// from context (set by AuthMiddleware), so this MUST run after auth.
//
//   - no sub             → 401 unauthenticated
//   - entitlement absent → 403 feature_not_in_plan
//   - granted / degraded → next (the checker fails open on platform outage)
func RequireEntitlement(checker EntitlementChecker, key string) gin.HandlerFunc {
	return func(c *gin.Context) {
		allowed, err := checker.Has(c.Request.Context(), GetZitadelSub(c), key)
		if err != nil {
			// The checker fails OPEN on platform degrade (returns allowed, nil), so
			// a non-nil error here means the caller could not be authenticated
			// (no Zitadel sub in context) — reject as unauthorized.
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
