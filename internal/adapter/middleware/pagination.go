package middleware

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

// DefaultMaxPageLimit is the hard ceiling applied to ParseLimitQuery when the
// caller does not provide one. Chosen to be generous for legitimate batch UIs
// but well below an OOM-inducing scan.
const DefaultMaxPageLimit = 500

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

// ParseOffsetQuery reads ?{key}=N as a non-negative integer offset. Invalid
// inputs yield 0. There is no upper bound — pagination depth is the caller's
// concern, not ours.
func ParseOffsetQuery(c *gin.Context, key string) int {
	raw := c.Query(key)
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
