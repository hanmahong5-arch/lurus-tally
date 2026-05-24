// Package replenish implements the HTTP handler for weekly replenishment suggestions.
//
// Endpoint:
//
//	GET /api/v1/replenish/suggestions?weeks=2
package replenish

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appreplenish "github.com/hanmahong5-arch/lurus-tally/internal/app/replenish"
)

// ListSuggestionsUseCase is the surface the handler calls.
type ListSuggestionsUseCase interface {
	Execute(ctx context.Context, tenantID uuid.UUID, weeks int) ([]appreplenish.SuggestionRow, error)
}

// Handler groups replenishment HTTP endpoints.
type Handler struct {
	uc ListSuggestionsUseCase
}

// New constructs a Handler.
func New(uc ListSuggestionsUseCase) *Handler {
	return &Handler{uc: uc}
}

// RegisterRoutes mounts replenishment endpoints onto the given router group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/replenish/suggestions", h.GetSuggestions)
}

// suggestionResp is the JSON shape of a single suggestion row.
type suggestionResp struct {
	ProductID     string  `json:"product_id"`
	ProductName   string  `json:"product_name"`
	ProductCode   string  `json:"product_code"`
	AvailableQty  string  `json:"available_qty"`
	SafetyQty     string  `json:"safety_qty"`
	AvgDailySales string  `json:"avg_daily_sales"`
	SuggestedQty  string  `json:"suggested_qty"`
	EstAmountCNY  string  `json:"est_amount_cny"`
	SupplierID    *string `json:"supplier_id,omitempty"`
	SupplierName  string  `json:"supplier_name,omitempty"`
	UrgencyScore  string  `json:"urgency_score"`
}

// GetSuggestions handles GET /api/v1/replenish/suggestions
//
// Query params:
//   - weeks (int, optional, default 2) — coverage period for suggested order qty
func (h *Handler) GetSuggestions(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "detail": "tenant_id required"})
		return
	}

	weeks := 2
	if raw := c.Query("weeks"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			weeks = n
		}
	}

	rows, err := h.uc.Execute(c.Request.Context(), tenantID, weeks)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "detail": err.Error()})
		return
	}

	items := make([]suggestionResp, 0, len(rows))
	for _, r := range rows {
		resp := suggestionResp{
			ProductID:     r.ProductID.String(),
			ProductName:   r.ProductName,
			ProductCode:   r.ProductCode,
			AvailableQty:  r.AvailableQty.String(),
			SafetyQty:     r.SafetyQty.String(),
			AvgDailySales: r.AvgDailySales.String(),
			SuggestedQty:  r.SuggestedQty.String(),
			EstAmountCNY:  r.EstAmountCNY.String(),
			SupplierName:  r.SupplierName,
			UrgencyScore:  r.UrgencyScore.String(),
		}
		if r.SupplierID != nil {
			s := r.SupplierID.String()
			resp.SupplierID = &s
		}
		items = append(items, resp)
	}

	c.JSON(http.StatusOK, gin.H{
		"items": items,
		"count": len(items),
		"weeks": weeks,
	})
}
