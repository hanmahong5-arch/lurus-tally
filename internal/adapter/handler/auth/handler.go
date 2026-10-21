// Package auth implements the Gin HTTP handlers for authentication and tenant profile endpoints.
package auth

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	handlerteam "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/team"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appTenant "github.com/hanmahong5-arch/lurus-tally/internal/app/tenant"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/tenant"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
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
	team          *handlerteam.Handler

	// offlineQueueAvailable is true when the Redis idempotency store is wired, i.e. replayed
	// offline checkouts are deduplicated. Reported to the web in GET /me.
	offlineQueueAvailable bool
}

// WithOfflineQueueAvailable sets the offline_queue_available flag reported by GET /me.
func (h *Handler) WithOfflineQueueAvailable(ok bool) *Handler {
	h.offlineQueueAvailable = ok
	return h
}

// meResponse is GET /me: the account context plus deployment capability flags.
type meResponse struct {
	*appTenant.GetMeOutput
	OfflineQueueAvailable bool `json:"offline_queue_available"`
}

// WithTeam mounts the /team routes (members, invites, join) alongside the auth
// routes, so router.New keeps its signature. nil keeps them answering 501.
func (h *Handler) WithTeam(t *handlerteam.Handler) *Handler {
	h.team = t
	return h
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
	if h.team != nil {
		h.team.RegisterRoutes(rg)
	} else {
		handlerteam.RegisterNotConfigured(rg, func(c *gin.Context) {
			c.JSON(http.StatusNotImplemented, gin.H{"error": "handler not configured"})
		})
	}
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
	sub := middleware.GetIDPSubject(c)
	if sub == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":  "unauthorized",
			"detail": "请先登录",
		})
		return
	}

	var req chooseProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  "bad request",
			"detail": "提交的内容格式不对，请刷新页面后重试",
		})
		return
	}

	in := appTenant.ChooseProfileInput{
		IDPSubject:  sub,
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
				"detail": "请选择一种业务类型",
			})
		case errors.Is(err, domain.ErrProfileAlreadySet):
			c.JSON(http.StatusConflict, gin.H{
				"error":  "conflict",
				"detail": "这个账号已经选过其他业务类型，不能再改",
			})
		case errors.Is(err, appTenant.ErrInconsistentTenantState):
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":  "inconsistent state",
				"detail": "账号信息不完整，请联系客服处理",
			})
		default:
			httperr.Write(c, httperr.Wrap(http.StatusInternalServerError, "internal_error",
				"保存没成功，请稍后再试", "", err))
		}
		return
	}
	c.JSON(http.StatusOK, p)
}

// GetMe handles GET /api/v1/me.
// Returns current user context. For first-time users (no mapping yet),
// IsFirstTime=true and TenantID/ProfileType are empty.
func (h *Handler) GetMe(c *gin.Context) {
	sub := middleware.GetIDPSubject(c)
	if sub == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":  "unauthorized",
			"detail": "请先登录",
		})
		return
	}

	out, err := h.getMe.Execute(c.Request.Context(), appTenant.GetMeInput{UserSub: sub})
	if err != nil {
		httperr.Write(c, httperr.Wrap(http.StatusInternalServerError, "internal_error",
			"账号信息读取失败，请稍后再试", "", err))
		return
	}
	c.JSON(http.StatusOK, meResponse{GetMeOutput: out, OfflineQueueAvailable: h.offlineQueueAvailable})
}

// Logout handles POST /api/v1/auth/logout.
// Session clearing is handled by NextAuth on the frontend; this is a server-side stub.
func (h *Handler) Logout(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "logged out"})
}
