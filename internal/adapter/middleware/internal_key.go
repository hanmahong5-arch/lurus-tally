package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// RequireInternalKey gates a service-to-service endpoint on a shared internal
// key carried as "Authorization: Bearer <key>". It is the symmetric counterpart
// of the key tally itself sends when calling platform's internal API
// (PLATFORM_INTERNAL_KEY): the fleet uses one canonical internal key, so the
// same value authenticates both directions.
//
// Posture is fail-CLOSED, unlike the read-only metrics gate: an empty
// expectedKey returns 503 (endpoint disabled) rather than running open, because
// the endpoints it guards are destructive (PIPL §47 erasure). This mirrors
// tally's existing billing-route contract, which returns 503 when
// PlatformInternalKey is unset. The token comparison is constant-time.
func RequireInternalKey(expectedKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if expectedKey == "" {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "internal_endpoint_disabled"})
			return
		}
		token, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(expectedKey)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}
