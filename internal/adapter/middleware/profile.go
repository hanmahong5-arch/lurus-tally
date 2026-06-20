// Package middleware provides Gin middleware for the Tally API.
package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// CtxKeyTenantID is the Gin context key where AuthMiddleware (Story 2.1) injects
// the tenant UUID.
const CtxKeyTenantID = "tenant_id"

// GetTenantID returns the tenant UUID injected by AuthMiddleware (Story 2.1), or
// uuid.Nil if absent.
func GetTenantID(c *gin.Context) uuid.UUID {
	raw, exists := c.Get(CtxKeyTenantID)
	if !exists {
		return uuid.Nil
	}
	id, _ := raw.(uuid.UUID)
	return id
}
