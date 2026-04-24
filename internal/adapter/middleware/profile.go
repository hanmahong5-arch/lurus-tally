// Package middleware provides Gin middleware for the Tally API.
package middleware

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	// CtxKeyTenantID is the Gin context key where AuthMiddleware (Story 2.1) injects the tenant UUID.
	CtxKeyTenantID = "tenant_id"
	// CtxKeyProfileType is the Gin context key where ProfileMiddleware injects the profile string.
	CtxKeyProfileType = "profile_type"
)

// ProfileQuerier abstracts the one query ProfileMiddleware needs:
// SELECT profile_type FROM tally.tenant_profile WHERE tenant_id = $1.
// This allows unit-testing without a real DB.
type ProfileQuerier interface {
	QueryProfileType(ctx context.Context, tenantID uuid.UUID) (string, error)
}

// ProfileMiddleware reads the tenant_id injected by AuthMiddleware (Story 2.1),
// looks up the profile_type in tally.tenant_profile, and injects it into the Gin context.
//
// STUB BEHAVIOUR (until Story 2.1 implements AuthMiddleware):
//   - If the Gin context has no "tenant_id" key, ProfileMiddleware skips profile injection
//     and calls c.Next() so auth middleware can return 401 appropriately.
//   - If the tenant has no profile record yet (tenant_profile row not found),
//     profile_type is set to "" and the request proceeds — handlers should treat "" as "no profile".
//
// Story 2.1 Wire-up TODO:
//  1. AuthMiddleware must inject tenant_id (as uuid.UUID) into the Gin context before
//     ProfileMiddleware runs.
//  2. Add AuthMiddleware to the router group before ProfileMiddleware in router.go.
func ProfileMiddleware(q ProfileQuerier, log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, exists := c.Get(CtxKeyTenantID)
		if !exists {
			// No tenant_id → auth not done yet; skip and let auth middleware respond.
			c.Next()
			return
		}

		tenantID, ok := raw.(uuid.UUID)
		if !ok {
			log.Error("profile middleware: tenant_id in context is not uuid.UUID")
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error": "internal error: malformed tenant_id in context",
			})
			return
		}

		profileType, err := q.QueryProfileType(c.Request.Context(), tenantID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			log.Error("profile middleware: failed to query profile",
				slog.String("tenant_id", tenantID.String()),
				slog.Any("error", err),
			)
			// Non-fatal: inject empty profile and continue — request can still be served.
			c.Set(CtxKeyProfileType, "")
		} else {
			c.Set(CtxKeyProfileType, profileType)
		}

		c.Next()
	}
}

// GetProfileType returns the profile_type injected by ProfileMiddleware, or "" if not set.
func GetProfileType(c *gin.Context) string {
	raw, exists := c.Get(CtxKeyProfileType)
	if !exists {
		return ""
	}
	s, _ := raw.(string)
	return s
}

// GetTenantID returns the tenant UUID injected by AuthMiddleware (Story 2.1), or uuid.Nil if absent.
func GetTenantID(c *gin.Context) uuid.UUID {
	raw, exists := c.Get(CtxKeyTenantID)
	if !exists {
		return uuid.Nil
	}
	id, _ := raw.(uuid.UUID)
	return id
}
