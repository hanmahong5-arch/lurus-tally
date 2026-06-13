package auth

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appauth "github.com/hanmahong5-arch/lurus-tally/internal/app/auth"
	domainauth "github.com/hanmahong5-arch/lurus-tally/internal/domain/auth"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

const (
	maxPATNameLen    = 64
	maxPATsPerTenant = 20
)

// PATHandler exposes CRUD endpoints over Personal Access Tokens. Mount under
// the same auth middleware as the rest of /api/v1 — see ADR-0011.
type PATHandler struct {
	repo appauth.Repository
}

// NewPATHandler wires a PATHandler to the persistence layer.
func NewPATHandler(repo appauth.Repository) *PATHandler {
	return &PATHandler{repo: repo}
}

// RegisterRoutes mounts the PAT CRUD routes onto the given router group.
// Caller is expected to apply AuthMiddleware before this group.
func (h *PATHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/auth/pats", h.Create)
	rg.GET("/auth/pats", h.List)
	rg.DELETE("/auth/pats/:id", h.Revoke)
}

type createPATRequest struct {
	Name      string     `json:"name"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type createPATResponse struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Prefix string    `json:"prefix"`
	// Token is the plaintext value. Returned EXACTLY ONCE at creation time;
	// the server keeps only sha256(prefix||secret). The UI must surface a
	// "copy and store this securely — it won't be shown again" affordance.
	Token     string     `json:"token"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// Create issues a new PAT. Returns 201 with the plaintext token in the body;
// this is the only time the plaintext is visible — subsequent reads only
// expose the prefix.
func (h *PATHandler) Create(c *gin.Context) {
	tenantID := patResolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id required"})
		return
	}

	var req createPATRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error", "detail": err.Error()})
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error", "detail": "name is required"})
		return
	}
	if len(name) > maxPATNameLen {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error", "detail": "name too long"})
		return
	}
	if req.ExpiresAt != nil && !req.ExpiresAt.After(time.Now()) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error", "detail": "expires_at must be in the future"})
		return
	}

	// Soft cap: prevent runaway token creation in a single tenant.
	existing, err := h.repo.ListByTenant(c.Request.Context(), tenantID)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	if len(existing) >= maxPATsPerTenant {
		c.JSON(http.StatusConflict, gin.H{"error": "limit_exceeded", "detail": "active token limit reached; revoke an existing token first"})
		return
	}

	plaintext, prefix, hash, err := domainauth.GenerateToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "detail": "token generation failed"})
		return
	}

	pat := &domainauth.PAT{
		ID:        uuid.New(),
		TenantID:  tenantID,
		Name:      name,
		Prefix:    prefix,
		Hash:      hash,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: req.ExpiresAt,
	}
	if err := h.repo.Create(c.Request.Context(), pat); err != nil {
		httperr.WriteInternal(c, err)
		return
	}

	c.JSON(http.StatusCreated, createPATResponse{
		ID:        pat.ID,
		Name:      pat.Name,
		Prefix:    pat.Prefix,
		Token:     plaintext,
		CreatedAt: pat.CreatedAt,
		ExpiresAt: pat.ExpiresAt,
	})
}

type patSummary struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

// List returns the active (non-revoked) PATs for the caller's tenant. The
// hash and any plaintext fragment are never returned — only the prefix to
// help the user identify which token they're looking at.
func (h *PATHandler) List(c *gin.Context) {
	tenantID := patResolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id required"})
		return
	}

	pats, err := h.repo.ListByTenant(c.Request.Context(), tenantID)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	items := make([]patSummary, 0, len(pats))
	for _, p := range pats {
		items = append(items, patSummary{
			ID: p.ID, Name: p.Name, Prefix: p.Prefix,
			CreatedAt: p.CreatedAt, ExpiresAt: p.ExpiresAt, LastUsedAt: p.LastUsedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// Revoke marks a PAT as revoked. Idempotent: revoking an already-revoked or
// non-existent token returns 204 to avoid leaking which IDs exist.
func (h *PATHandler) Revoke(c *gin.Context) {
	tenantID := patResolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id required"})
		return
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error", "detail": "invalid id"})
		return
	}

	if err := h.repo.Revoke(c.Request.Context(), tenantID, id); err != nil {
		if errors.Is(err, appauth.ErrNotFound) {
			// Treat as success — don't leak token-id existence to other tenants.
			c.Status(http.StatusNoContent)
			return
		}
		httperr.WriteInternal(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// patResolveTenantID returns the tenant UUID injected by AuthMiddleware.
// uuid.Nil → caller MUST return 401. No header fallback (see bill/handler.go).
func patResolveTenantID(c *gin.Context) uuid.UUID {
	return middleware.GetTenantID(c)
}
