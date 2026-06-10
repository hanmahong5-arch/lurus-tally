// Package reports implements Gin HTTP handlers for the analytics reports REST API.
package reports

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appreports "github.com/hanmahong5-arch/lurus-tally/internal/app/reports"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

// Handler groups all analytics report REST handlers.
type Handler struct {
	uc *appreports.UseCase
}

// New constructs the Handler. uc must be non-nil.
func New(uc *appreports.UseCase) *Handler {
	return &Handler{uc: uc}
}

// RegisterRoutes mounts the four read-only analytics routes onto rg.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/reports/gross-margin", h.GrossMargin)
	rg.GET("/reports/abc", h.ABCClassify)
	rg.GET("/reports/dead-stock", h.DeadStock)
	rg.GET("/reports/sales-top", h.SalesTop)
}

// GrossMargin handles GET /api/v1/reports/gross-margin?days=30
func (h *Handler) GrossMargin(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id required"})
		return
	}
	days := middleware.ParseLimitQuery(c, "days", 30, 365)
	result, err := h.uc.GrossMarginSummary(c.Request.Context(), tenantID, days)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ABCClassify handles GET /api/v1/reports/abc
func (h *Handler) ABCClassify(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id required"})
		return
	}
	result, err := h.uc.ABCClassify(c.Request.Context(), tenantID)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeadStock handles GET /api/v1/reports/dead-stock?days=90
func (h *Handler) DeadStock(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id required"})
		return
	}
	days := middleware.ParseLimitQuery(c, "days", 90, 365)
	result, err := h.uc.DeadStock(c.Request.Context(), tenantID, days)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// SalesTop handles GET /api/v1/reports/sales-top?metric=revenue&days=7&limit=10
func (h *Handler) SalesTop(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id required"})
		return
	}
	metric := c.DefaultQuery("metric", "revenue")
	switch metric {
	case "revenue", "margin", "qty":
		// valid
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "metric must be revenue|margin|qty"})
		return
	}
	days := middleware.ParseLimitQuery(c, "days", 7, 365)
	limit := middleware.ParseLimitQuery(c, "limit", 10, 100)

	result, err := h.uc.SalesTop(c.Request.Context(), tenantID, metric, days, limit)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// resolveTenantID returns the tenant UUID set by AuthMiddleware (uuid.Nil → 401).
func resolveTenantID(c *gin.Context) uuid.UUID {
	return middleware.GetTenantID(c)
}
