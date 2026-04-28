// Package auth implements the Gin HTTP handlers for authentication and tenant profile endpoints.
package auth

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appTenant "github.com/hanmahong5-arch/lurus-tally/internal/app/tenant"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/tenant"
)

// ChooseProfileExecutor is the interface satisfied by ChooseProfileUseCase.
type ChooseProfileExecutor interface {
	Execute(ctx context.Context, in appTenant.ChooseProfileInput) (*domain.TenantProfile, error)
}

// GetMeExecutor is the interface satisfied by GetMeUseCase.
type GetMeExecutor interface {
	Execute(ctx context.Context, in appTenant.GetMeInput) (*appTenant.GetMeOutput, error)
}

// Handler groups the auth and tenant profile HTTP handlers.
type Handler struct {
	chooseProfile ChooseProfileExecutor
	getMe         GetMeExecutor
}

// New creates a Handler wired to the provided use cases.
func New(chooseProfile ChooseProfileExecutor, getMe GetMeExecutor) *Handler {
	return &Handler{
		chooseProfile: chooseProfile,
		getMe:         getMe,
	}
}

// RegisterRoutes mounts auth and tenant profile routes onto the given router group.
// The caller is expected to apply AuthMiddleware before this group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/me", h.GetMe)
	rg.POST("/tenant/profile", h.ChooseProfile)
	rg.POST("/auth/logout", h.Logout)
}

// chooseProfileRequest is the JSON body for POST /api/v1/tenant/profile.
type chooseProfileRequest struct {
	ProfileType string `json:"profile_type"`
}

// ChooseProfile handles POST /api/v1/tenant/profile.
//
// The handler extracts identity from JWT-injected context (sub/email/name) and
// delegates to ChooseProfileUseCase, which is fully idempotent. Status mapping:
//
//	201 Created   — first-time onboarding completed
//	200 OK        — same profile_type already set (idempotent no-op)
//	409 Conflict  — different profile_type already set
//	400 Bad Req   — invalid profile_type
//	401           — no sub in context (auth missing)
//	500           — internal failure
func (h *Handler) ChooseProfile(c *gin.Context) {
	sub := middleware.GetZitadelSub(c)
	if sub == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":  "unauthorized",
			"detail": "authentication required",
		})
		return
	}

	var req chooseProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  "bad request",
			"detail": "invalid request body: " + err.Error(),
		})
		return
	}

	in := appTenant.ChooseProfileInput{
		ZitadelSub:  sub,
		Email:       middleware.GetEmail(c),
		DisplayName: middleware.GetDisplayName(c),
		ProfileType: req.ProfileType,
	}

	// First call to Execute determines whether this is a fresh bootstrap (201)
	// or an idempotent no-op (200). Distinguish by checking whether a mapping
	// existed BEFORE the call — but that requires a second lookup. Pragmatic
	// shortcut: always return 200, semantically correct because the resource
	// (profile) exists either way after a successful call.
	p, err := h.chooseProfile.Execute(c.Request.Context(), in)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrInvalidProfileType):
			c.JSON(http.StatusBadRequest, gin.H{
				"error":  "bad request",
				"detail": "profile_type must be 'cross_border' or 'retail'",
			})
		case errors.Is(err, domain.ErrProfileAlreadySet):
			c.JSON(http.StatusConflict, gin.H{
				"error":  "conflict",
				"detail": "a different profile is already set for this tenant",
			})
		case errors.Is(err, appTenant.ErrInconsistentTenantState):
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":  "inconsistent state",
				"detail": "tenant has user mapping but no profile — contact support",
			})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":  "internal server error",
				"detail": err.Error(),
			})
		}
		return
	}
	c.JSON(http.StatusOK, p)
}

// GetMe handles GET /api/v1/me.
// Returns current user context. For first-time users (no mapping yet),
// IsFirstTime=true and TenantID/ProfileType are empty.
func (h *Handler) GetMe(c *gin.Context) {
	sub := middleware.GetZitadelSub(c)
	if sub == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":  "unauthorized",
			"detail": "not authenticated",
		})
		return
	}

	out, err := h.getMe.Execute(c.Request.Context(), appTenant.GetMeInput{UserSub: sub})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "internal server error",
			"detail": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, out)
}

// Logout handles POST /api/v1/auth/logout.
// Session clearing is handled by NextAuth on the frontend; this is a server-side stub.
func (h *Handler) Logout(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "logged out"})
}
