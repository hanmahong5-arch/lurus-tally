package ai

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ErrCodeNotConfigured is the stable machine-readable code returned on every
// /ai/* route when no LLM backend is wired (NEWAPI_API_KEY unset). The frontend
// keys its friendly empty state off this exact string — do not rename it.
const ErrCodeNotConfigured = "ai_not_configured"

// ErrCodeNotEntitled is the stable code of the 403 the entitlement gate returns
// when the tenant's plan does not include the AI assistant. The frontend shows
// the same friendly "not enabled" empty state for it as for ErrCodeNotConfigured.
const ErrCodeNotEntitled = "ai_not_entitled"

// NotConfigured answers any AI route with 503 + {error:"ai_not_configured"}.
// Mounted by the router in place of the real handlers when the AI handler is
// nil, so the client can tell "not enabled" apart from a transient failure.
func NotConfigured(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
		"error":   ErrCodeNotConfigured,
		"message": "AI 助手尚未开通",
	})
}
