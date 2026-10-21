package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/app/subscription"
)

// SubscriptionLockedCode is the stable error code of the 402 body.
const SubscriptionLockedCode = "subscription_expired"

// SubscriptionWriteAllowlist lists the path prefixes that keep accepting writes
// while a shop is locked: sign-in/identity, the plan page itself, exports,
// joining a shop, opening a shop, own profile and leaving cashier mode.
// A prefix matches the path itself or anything below it (segment boundary).
var SubscriptionWriteAllowlist = []string{
	"/api/v1/auth",
	"/api/v1/me",
	"/api/v1/billing",
	"/api/v1/exports",
	"/api/v1/team/join",
	"/api/v1/tenant/profile",
	"/api/v1/account",
	"/api/v1/tenant/cashier-mode/verify",
}

// SubscriptionWriteAllowed reports whether a write to path is exempt from the lock.
func SubscriptionWriteAllowed(path string) bool {
	for _, p := range SubscriptionWriteAllowlist {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

// SubscriptionGuard turns an expired shop read-only: any non-safe request that
// is not allow-listed gets 402 subscription_expired. Reads always pass. It
// fails open: the checker never errors, and a nil checker disables the guard.
// MUST run after auth (needs the tenant).
func SubscriptionGuard(checker subscription.Checker) gin.HandlerFunc {
	return func(c *gin.Context) {
		if checker == nil {
			c.Next()
			return
		}
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			c.Next()
			return
		}
		if SubscriptionWriteAllowed(c.Request.URL.Path) {
			c.Next()
			return
		}
		tid := GetTenantID(c)
		if tid == uuid.Nil {
			c.Next()
			return
		}
		st := checker.State(c.Request.Context(), tid)
		if !st.Locked {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusPaymentRequired, gin.H{
			"code":       SubscriptionLockedCode,
			"message":    subscription.LockedMessage,
			"plan_code":  st.PlanCode,
			"expires_at": st.ExpiresAt,
		})
	}
}
