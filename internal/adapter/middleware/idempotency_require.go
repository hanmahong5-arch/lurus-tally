package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// requireIdempotencyKeyRoutes is the allowlist of routes that MUST carry an
// Idempotency-Key header. These are the financially- or inventory-damaging
// writes where a duplicate request (double-click, client retry, proxy replay)
// would record a second payment, re-approve a bill, or re-run a checkout. The
// header lets the Idempotency middleware dedupe the retry; this guard makes
// supplying it mandatory rather than opt-in.
//
// Keyed by "METHOD FULLPATH" against c.Request.Method + " " + c.FullPath() (the
// registered route template, e.g. "POST /api/v1/purchase-bills/:id/approve").
// Matching the method keeps a read on the same path (GET /api/v1/payments) out
// of the requirement, and matching the exact route template fails closed: a
// renamed route drops out of the set rather than silently matching a prefix.
var requireIdempotencyKeyRoutes = map[string]struct{}{
	"POST /api/v1/payments":                   {},
	"POST /api/v1/purchase-bills/:id/approve": {},
	"POST /api/v1/sale-bills/:id/approve":     {},
	"POST /api/v1/sale-bills/quick-checkout":  {},
	"POST /api/v1/ai/plans/:plan_id/confirm":  {},
	// Subscription checkout debits the wallet / creates a payment order on
	// platform; platform itself mandates an Idempotency-Key on this financial
	// mutation, so a key-less retry would 400 downstream. Make it mandatory at
	// Tally's edge and forward the key (see app/billing SubscribeInput).
	"POST /api/v1/billing/subscribe": {},
}

// RequireIdempotencyKey returns a middleware that rejects requests to the
// high-risk allowlisted routes with 400 when the Idempotency-Key header is
// absent.
//
// Unlike Idempotency (which is a no-op when Redis is unavailable), this guard is
// ALWAYS applied: enforcement of the contract must not depend on cache
// availability, otherwise a Redis outage would silently re-open the duplicate
// window. It runs after the auth middleware, so an unauthenticated request is
// rejected with 401 before reaching here. Non-allowlisted routes, and reads on
// an allowlisted path, pass through untouched.
//
// Intended ordering note: as a group-level guard this fires BEFORE a handler
// parses its path params. For /ai/plans/:plan_id/confirm an invalid plan_id with
// no key therefore returns missing_idempotency_key (not "invalid plan_id") — the
// key is mandatory regardless of plan_id validity. The un-gated /cancel and
// /revert parse the UUID first; that asymmetry is accepted, not a bug.
func RequireIdempotencyKey() gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, ok := requireIdempotencyKeyRoutes[c.Request.Method+" "+c.FullPath()]; !ok {
			c.Next()
			return
		}
		if c.GetHeader(HeaderIdempotencyKey) == "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error":   "missing_idempotency_key",
				"message": "this endpoint requires an Idempotency-Key header so a retry cannot double-apply the operation",
			})
			return
		}
		c.Next()
	}
}
