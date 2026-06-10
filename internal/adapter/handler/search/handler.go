// Package search implements the Gin HTTP handler for the ⌘K entity search endpoint.
package search

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appsearch "github.com/hanmahong5-arch/lurus-tally/internal/app/search"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

const (
	defaultLimit = 5
	maxLimit     = 20
)

// Handler exposes the entity search REST endpoint.
type Handler struct {
	uc *appsearch.SearchEntitiesUseCase
}

// New constructs the handler. uc must be non-nil.
func New(uc *appsearch.SearchEntitiesUseCase) *Handler {
	return &Handler{uc: uc}
}

// RegisterRoutes mounts the search route onto rg.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/search", h.Search)
}

// Search handles GET /api/v1/search?q=&limit=
//
// Response: { groups: [{ type, items: [{ type, id, label, sublabel }] }] }
// Returns 200 with empty groups when q is blank.
// Returns 401 when the request has no valid tenant context.
func (h *Handler) Search(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id required"})
		return
	}

	q := c.Query("q")
	limit := parseLimit(c.Query("limit"))

	req := appsearch.SearchRequest{
		TenantID: tenantID,
		Q:        q,
		Limit:    limit,
	}

	resp, err := h.uc.Execute(c.Request.Context(), req)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func parseLimit(raw string) int {
	if raw == "" {
		return defaultLimit
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultLimit
	}
	if n > maxLimit {
		return maxLimit
	}
	return n
}
