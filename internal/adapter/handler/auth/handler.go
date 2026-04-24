// Package auth implements the Gin HTTP handlers for authentication and tenant profile endpoints.
package auth

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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
// The group is expected to already have AuthMiddleware applied by the caller.
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
// Creates the initial profile record. Returns 201 on success, 409 if already set.
func (h *Handler) ChooseProfile(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":  "unauthorized",
			"detail": "tenant_id not available: authenticate first",
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

	p, err := h.chooseProfile.Execute(c.Request.Context(), appTenant.ChooseProfileInput{
		TenantID:    tenantID,
		ProfileType: req.ProfileType,
	})
	if err != nil {
		if errors.Is(err, domain.ErrProfileAlreadySet) {
			c.JSON(http.StatusConflict, gin.H{
				"error":  "conflict",
				"detail": "profile already set for this tenant",
			})
			return
		}
		if errors.Is(err, domain.ErrInvalidProfileType) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":  "bad request",
				"detail": "profile_type must be 'cross_border' or 'retail'",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "internal server error",
			"detail": err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, p)
}

// GetMe handles GET /api/v1/me.
// Returns current user context: userId, tenantId, profileType.
func (h *Handler) GetMe(c *gin.Context) {
	sub, subExists := c.Get(middleware.CtxKeyZitadelSub)
	tenantID := middleware.GetTenantID(c)

	if !subExists && tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":  "unauthorized",
			"detail": "not authenticated",
		})
		return
	}

	subStr, _ := sub.(string)

	out, err := h.getMe.Execute(c.Request.Context(), appTenant.GetMeInput{
		TenantID: tenantID,
		UserSub:  subStr,
	})
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
