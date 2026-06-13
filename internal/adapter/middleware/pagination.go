package middleware

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

// DefaultMaxPageLimit is the hard ceiling applied to ParseLimitQuery when the
// caller does not provide one. Chosen to be generous for legitimate batch UIs
// but well below an OOM-inducing scan.
const DefaultMaxPageLimit = 500

// DefaultMaxPageOffset is the hard ceiling applied to ParseOffsetQuery. A deep
// offset forces PostgreSQL to scan and discard every preceding row, so an
// unbounded offset is a cheap resource-exhaustion vector. 100000 is far beyond
// any legitimate UI (200 pages even at the 500-row limit ceiling) while capping
// the worst-case scan. Callers needing deeper traversal should switch to keyset
// pagination rather than raising this.
const DefaultMaxPageOffset = 100000

// ParseLimitQuery reads ?{key}=N and clamps it to [1, max]. Non-numeric, zero,
// negative, or absent values yield def. Values above max are clamped to max
// (not rejected) so existing clients with stale defaults keep working while
// abusive limits are silently bounded.
func ParseLimitQuery(c *gin.Context, key string, def, max int) int {
	if max <= 0 {
		max = DefaultMaxPageLimit
	}
	if def <= 0 {
		def = 20
	}
	if def > max {
		def = max
	}
	raw := c.Query(key)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

// ParseOffsetQuery reads ?{key}=N as a non-negative integer offset and clamps it
// to [0, DefaultMaxPageOffset]. Invalid inputs yield 0; values above the ceiling
// are clamped (not rejected) so existing clients keep working while an abusive
// deep offset is bounded.
func ParseOffsetQuery(c *gin.Context, key string) int {
	raw := c.Query(key)
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0
	}
	if n > DefaultMaxPageOffset {
		return DefaultMaxPageOffset
	}
	return n
}
