// Package middleware provides Gin middleware for the Tally API.
package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

const (
	// CtxKeyZitadelSub is the Gin context key where AuthMiddleware injects the Zitadel sub claim.
	CtxKeyZitadelSub = "zitadel_sub"
	// CtxKeyEmail is the Gin context key where AuthMiddleware injects the email claim.
	CtxKeyEmail = "user_email"
	// CtxKeyDisplayName is the Gin context key where AuthMiddleware injects the display name claim.
	CtxKeyDisplayName = "user_display_name"

	// tallyTenantIDClaim is the JWT custom claim name carrying the tally tenant UUID.
	tallyTenantIDClaim = "tally_tenant_id"

	// jwksCacheTTL controls how frequently the JWKS is re-fetched from the provider.
	jwksCacheTTL = 1 * time.Hour
)

// TenantLookup resolves a Zitadel sub to a tally tenant UUID using the
// user_identity_mapping table. Returns uuid.Nil when the user is not yet
// onboarded (first-time user pre-/setup), so the middleware can let the
// request proceed and only /me + /tenant/profile work without tenant_id.
type TenantLookup func(ctx context.Context, sub string) (uuid.UUID, error)

// AuthMiddleware returns a Gin middleware that validates RS256 JWTs issued by the
// given issuer. It fetches public keys from jwksURL (JWKS endpoint) and caches
// them for jwksCacheTTL.
//
// On success it writes into the Gin context:
//   - CtxKeyZitadelSub  → Zitadel sub claim (string)
//   - CtxKeyTenantID    → tally tenant UUID, resolved from (1) tally_tenant_id
//     custom claim, falling back to (2) tenantLookup(sub) against
//     user_identity_mapping. Skipped when tenantLookup is nil or the user
//     hasn't onboarded yet.
//
// On failure it aborts with 401.
func NewAuthMiddleware(jwksURL, expectedIssuer string, tenantLookup TenantLookup) gin.HandlerFunc {
	cache := jwk.NewCache(context.Background())
	_ = cache.Register(jwksURL, jwk.WithRefreshInterval(jwksCacheTTL))

	return func(c *gin.Context) {
		rawToken := extractBearerToken(c)
		if rawToken == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":  "unauthorized",
				"detail": "Authorization header with Bearer token is required",
			})
			return
		}

		// Fetch (or use cached) JWKS.
		keySet, err := cache.Get(c.Request.Context(), jwksURL)
		if err != nil {
			slog.Error("auth middleware: failed to fetch JWKS",
				slog.String("jwks_url", jwksURL),
				slog.Any("error", err),
			)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":  "unauthorized",
				"detail": "unable to retrieve signing keys",
			})
			return
		}

		// Parse and validate the token. jwx validates exp, nbf, and signature automatically.
		tok, err := jwt.Parse([]byte(rawToken),
			jwt.WithKeySet(keySet),
			jwt.WithIssuer(expectedIssuer),
			jwt.WithValidate(true),
		)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":  "unauthorized",
				"detail": "invalid or expired token",
			})
			return
		}

		// Inject Zitadel sub.
		sub := tok.Subject()
		c.Set(CtxKeyZitadelSub, sub)

		// Inject email + display name when present in standard OIDC claims.
		if v, ok := tok.Get("email"); ok {
			if s, ok := v.(string); ok && s != "" {
				c.Set(CtxKeyEmail, s)
			}
		}
		if v, ok := tok.Get("name"); ok {
			if s, ok := v.(string); ok && s != "" {
				c.Set(CtxKeyDisplayName, s)
			}
		}

		// Inject tenant_id — first try the JWT custom claim (preferred path,
		// avoids a DB hit), then fall back to the user_identity_mapping
		// lookup. Lookup result of uuid.Nil is normal for first-time users
		// pre-/setup; downstream handlers must still allow /me + /tenant/profile.
		var tenantID uuid.UUID
		if rawTenantID, ok := tok.Get(tallyTenantIDClaim); ok {
			if s, ok := rawTenantID.(string); ok {
				if parsed, err := uuid.Parse(s); err == nil {
					tenantID = parsed
				}
			}
		}
		if tenantID == uuid.Nil && tenantLookup != nil && sub != "" {
			id, err := tenantLookup(c.Request.Context(), sub)
			if err != nil {
				slog.Error("auth middleware: tenant lookup failed",
					slog.String("sub", sub),
					slog.Any("error", err),
				)
			} else {
				tenantID = id
			}
		}
		if tenantID != uuid.Nil {
			c.Set(CtxKeyTenantID, tenantID)
		}

		c.Next()
	}
}

// GetZitadelSub returns the Zitadel sub claim injected by AuthMiddleware, or "" if absent.
func GetZitadelSub(c *gin.Context) string {
	if v, ok := c.Get(CtxKeyZitadelSub); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// GetEmail returns the email claim, or "" if absent.
func GetEmail(c *gin.Context) string {
	if v, ok := c.Get(CtxKeyEmail); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// GetDisplayName returns the display name claim, or "" if absent.
func GetDisplayName(c *gin.Context) string {
	if v, ok := c.Get(CtxKeyDisplayName); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// extractBearerToken parses the Authorization header and returns the raw token string,
// or "" if absent or malformed.
func extractBearerToken(c *gin.Context) string {
	auth := c.GetHeader("Authorization")
	if auth == "" {
		return ""
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
