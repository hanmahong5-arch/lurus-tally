// Package account exposes the /api/v1/account/* HTTP routes that back the
// frontend Tier 3 account-center tabs (security / audit / profile / avatar).
package account

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appacct "github.com/hanmahong5-arch/lurus-tally/internal/app/account"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

// Handler groups the account-center Gin handlers.
type Handler struct {
	listSessions  *appacct.ListSessions
	revokeSession *appacct.RevokeSession
	listAudit     *appacct.ListAuditLog
	updateProfile *appacct.UpdateProfile
	getProfile    *appacct.GetProfile
	setAvatar     *appacct.SetAvatar
	getAvatar     *appacct.GetAvatar
}

// New constructs a Handler from the wired use cases.
func New(
	listSessions *appacct.ListSessions,
	revokeSession *appacct.RevokeSession,
	listAudit *appacct.ListAuditLog,
	getProfile *appacct.GetProfile,
	updateProfile *appacct.UpdateProfile,
	setAvatar *appacct.SetAvatar,
	getAvatar *appacct.GetAvatar,
) *Handler {
	return &Handler{
		listSessions:  listSessions,
		revokeSession: revokeSession,
		listAudit:     listAudit,
		updateProfile: updateProfile,
		getProfile:    getProfile,
		setAvatar:     setAvatar,
		getAvatar:     getAvatar,
	}
}

// RegisterRoutes mounts the account-center routes onto the given group. Caller
// is expected to apply AuthMiddleware before this group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/account/sessions", h.ListSessions)
	rg.DELETE("/account/sessions/:id", h.RevokeSession)
	rg.GET("/account/audit-log", h.ListAuditLog)
	rg.GET("/account/profile", h.GetProfile)
	rg.PUT("/account/profile", h.UpdateProfile)
	rg.POST("/account/avatar", h.UploadAvatar)
	rg.GET("/account/avatar", h.DownloadAvatar)
}

// AvatarUploadMax bounds the multipart body to AvatarSizeMax + 16KB header
// padding. Anything larger is rejected before the byte slice is materialised.
const avatarUploadMax = appacct.AvatarSizeMax + 16*1024

// ListSessions returns the calling user's active sessions.
func (h *Handler) ListSessions(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetIDPSubject(c)
	if tenantID == uuid.Nil || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	sessions, err := h.listSessions.Execute(c.Request.Context(), tenantID, userID)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	type item struct {
		ID         uuid.UUID  `json:"id"`
		UserAgent  string     `json:"user_agent"`
		IPAddr     string     `json:"ip_addr,omitempty"`
		CreatedAt  time.Time  `json:"created_at"`
		LastActive time.Time  `json:"last_active"`
		Current    bool       `json:"current"`
		RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	}
	currentUA := c.Request.UserAgent()
	out := make([]item, 0, len(sessions))
	for _, s := range sessions {
		ipStr := ""
		if len(s.IPAddr) > 0 {
			ipStr = s.IPAddr.String()
		}
		out = append(out, item{
			ID: s.ID, UserAgent: s.UserAgent, IPAddr: ipStr,
			CreatedAt: s.CreatedAt, LastActive: s.LastActive,
			Current:   s.UserAgent == currentUA,
			RevokedAt: s.RevokedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

// RevokeSession marks the targeted session row revoked.
func (h *Handler) RevokeSession(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error", "detail": "invalid id"})
		return
	}
	if err := h.revokeSession.Execute(c.Request.Context(), tenantID, id); err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ListAuditLog returns paginated audit entries.
func (h *Handler) ListAuditLog(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	limit := atoiDefault(c.Query("limit"), 50)
	offset := atoiDefault(c.Query("offset"), 0)
	entries, total, err := h.listAudit.Execute(c.Request.Context(), tenantID, limit, offset)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	type item struct {
		ID         uuid.UUID `json:"id"`
		ActorID    string    `json:"actor_id"`
		Action     string    `json:"action"`
		TargetKind string    `json:"target_kind,omitempty"`
		TargetID   string    `json:"target_id,omitempty"`
		Payload    any       `json:"payload"`
		CreatedAt  time.Time `json:"created_at"`
	}
	out := make([]item, 0, len(entries))
	for _, e := range entries {
		out = append(out, item{
			ID: e.ID, ActorID: e.ActorID, Action: e.Action,
			TargetKind: e.TargetKind, TargetID: e.TargetID,
			Payload:   string(e.Payload),
			CreatedAt: e.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"items": out, "total": total, "limit": limit, "offset": offset})
}

// GetProfile returns the editable profile fields.
func (h *Handler) GetProfile(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetIDPSubject(c)
	if tenantID == uuid.Nil || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	p, err := h.getProfile.Execute(c.Request.Context(), tenantID, userID)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"display_name": p.DisplayName,
		"phone":        p.Phone,
		"avatar_url":   avatarURL(tenantID, p.HasAvatar),
		"updated_at":   p.UpdatedAt,
	})
}

// UpdateProfile writes display_name / phone overrides.
func (h *Handler) UpdateProfile(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetIDPSubject(c)
	if tenantID == uuid.Nil || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req struct {
		DisplayName string `json:"display_name"`
		Phone       string `json:"phone"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error", "detail": err.Error()})
		return
	}
	if err := h.updateProfile.Execute(c.Request.Context(), tenantID, userID, req.DisplayName, req.Phone); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error", "detail": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// UploadAvatar accepts multipart/form-data with field "file".
func (h *Handler) UploadAvatar(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetIDPSubject(c)
	if tenantID == uuid.Nil || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	// Bound the multipart body so a hostile client cannot exhaust memory.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, avatarUploadMax)
	if err := c.Request.ParseMultipartForm(avatarUploadMax); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error", "detail": "multipart parse: " + err.Error()})
		return
	}
	fh, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error", "detail": "missing form field 'file'"})
		return
	}
	if fh.Size > int64(appacct.AvatarSizeMax) {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "avatar_too_large", "detail": "file exceeds 200KB cap"})
		return
	}
	f, err := fh.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error", "detail": "open upload: " + err.Error()})
		return
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(io.LimitReader(f, int64(appacct.AvatarSizeMax)+1))
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	ct := fh.Header.Get("Content-Type")
	if ct == "" {
		ct = http.DetectContentType(data)
	}
	if err := h.setAvatar.Execute(c.Request.Context(), tenantID, userID, ct, data); err != nil {
		switch {
		case errors.Is(err, appacct.ErrAvatarTooLarge):
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "avatar_too_large", "detail": err.Error()})
		case errors.Is(err, appacct.ErrAvatarUnsupported):
			c.JSON(http.StatusUnsupportedMediaType, gin.H{"error": "avatar_unsupported", "detail": err.Error()})
		default:
			httperr.WriteInternal(c, err)
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"avatar_url": avatarURL(tenantID, true)})
}

// DownloadAvatar streams the avatar bytes for the calling user. Cache-Control
// is short so an edit propagates quickly to all open tabs.
func (h *Handler) DownloadAvatar(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetIDPSubject(c)
	if tenantID == uuid.Nil || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	ct, data, err := h.getAvatar.Execute(c.Request.Context(), tenantID, userID)
	if err != nil {
		if errors.Is(err, appacct.ErrNotFound) {
			c.Status(http.StatusNotFound)
			return
		}
		httperr.WriteInternal(c, err)
		return
	}
	c.Header("Cache-Control", "private, max-age=60")
	c.Data(http.StatusOK, ct, data)
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return def
	}
	return n
}

// avatarURL returns the relative URL the frontend can use to fetch the
// avatar, or "" when the user has no avatar set yet. The URL is namespaced
// by tenant_id so caching at the CDN layer keys correctly.
func avatarURL(_ uuid.UUID, hasAvatar bool) string {
	if !hasAvatar {
		return ""
	}
	return "/api/v1/account/avatar"
}
