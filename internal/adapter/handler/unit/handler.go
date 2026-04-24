// Package unit implements the Gin HTTP handlers for the unit_def REST API.
package unit

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	repounit "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/unit"
	appunit "github.com/hanmahong5-arch/lurus-tally/internal/app/unit"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/unit"
)

// Handler groups all unit CRUD Gin handlers.
type Handler struct {
	create *appunit.CreateUseCase
	list   *appunit.ListUseCase
	delete *appunit.DeleteUseCase
}

// New creates a Handler wired to the provided use cases.
func New(
	create *appunit.CreateUseCase,
	list *appunit.ListUseCase,
	del *appunit.DeleteUseCase,
) *Handler {
	return &Handler{create: create, list: list, delete: del}
}

// createRequest is the JSON body for POST /api/v1/units.
type createRequest struct {
	Code     string          `json:"code"`
	Name     string          `json:"name"`
	UnitType domain.UnitType `json:"unit_type"`
}

// Create handles POST /api/v1/units.
func (h *Handler) Create(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id required"})
		return
	}

	var req createRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	u, err := h.create.Execute(c.Request.Context(), domain.CreateInput{
		TenantID: tenantID,
		Code:     req.Code,
		Name:     req.Name,
		UnitType: req.UnitType,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, u)
}

// List handles GET /api/v1/units.
// Query param: unit_type (optional filter).
func (h *Handler) List(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id required"})
		return
	}

	filter := domain.ListFilter{
		TenantID: tenantID,
		UnitType: domain.UnitType(c.Query("unit_type")),
	}

	units, err := h.list.Execute(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": units})
}

// Delete handles DELETE /api/v1/units/:id.
// System units (is_system = true) return 403 Forbidden.
func (h *Handler) Delete(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id required"})
		return
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid unit id: must be a UUID"})
		return
	}

	if err := h.delete.Execute(c.Request.Context(), tenantID, id); err != nil {
		switch {
		case errors.Is(err, repounit.ErrNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "unit not found"})
		case isSystemUnitError(err):
			c.JSON(http.StatusForbidden, gin.H{
				"error": "system unit cannot be deleted: only tenant-custom units may be deleted",
			})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	c.Status(http.StatusNoContent)
}

// isSystemUnitError detects the "system unit cannot be deleted" error message.
func isSystemUnitError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, repounit.ErrSystemUnit) ||
		containsString(err.Error(), "system unit")
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && indexString(s, substr) >= 0)
}

func indexString(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// resolveTenantID reads tenant UUID from the Gin context or X-Tenant-ID header.
func resolveTenantID(c *gin.Context) uuid.UUID {
	id := middleware.GetTenantID(c)
	if id != uuid.Nil {
		return id
	}
	if raw := c.GetHeader("X-Tenant-ID"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err == nil {
			return parsed
		}
	}
	return uuid.Nil
}
