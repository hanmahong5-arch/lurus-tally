// Package digest implements the Gin HTTP handler for the weekly summary endpoint.
//
// Endpoint:
//
//	GET /api/v1/weekly-summary
//
// Response:
//
//	{
//	  "replenish":            {"count": N, "amount_cny": "12345.67"},
//	  "oversell":             {"count": M},
//	  "dead_stock":           {"count": K},
//	  "suggestion_scorecard": {"suggested": N, "adopted": M, "missed_stockout": K},
//	  "generated_at":         "2026-05-22T00:00:00Z"
//	}
package digest

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appdigest "github.com/hanmahong5-arch/lurus-tally/internal/app/digest"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

// WeeklySummaryUseCase is the surface the handler calls.
type WeeklySummaryUseCase interface {
	Execute(ctx context.Context, tenantID uuid.UUID) (appdigest.Summary, error)
}

// Handler groups the weekly summary HTTP endpoints.
type Handler struct {
	uc WeeklySummaryUseCase
}

// New constructs a Handler. uc must be non-nil.
func New(uc WeeklySummaryUseCase) *Handler {
	return &Handler{uc: uc}
}

// RegisterRoutes mounts the weekly summary route onto the given router group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/weekly-summary", h.GetWeeklySummary)
}

// replenishResp is the JSON shape for the replenishment section.
type replenishResp struct {
	Count     int    `json:"count"`
	AmountCNY string `json:"amount_cny"`
}

// countResp is the JSON shape for a simple count section.
type countResp struct {
	Count int `json:"count"`
}

// scorecardResp is the JSON shape for last week's suggestion track record.
type scorecardResp struct {
	Suggested      int `json:"suggested"`
	Adopted        int `json:"adopted"`
	MissedStockout int `json:"missed_stockout"`
}

// weeklyResp is the full JSON response body.
type weeklyResp struct {
	Replenish           replenishResp `json:"replenish"`
	Oversell            countResp     `json:"oversell"`
	DeadStock           countResp     `json:"dead_stock"`
	SuggestionScorecard scorecardResp `json:"suggestion_scorecard"`
	GeneratedAt         time.Time     `json:"generated_at"`
}

// GetWeeklySummary handles GET /api/v1/weekly-summary.
// It returns counts and estimated purchase cost for the three key inventory
// signals that form the "Monday card" on the dashboard.
func (h *Handler) GetWeeklySummary(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "detail": "tenant_id required"})
		return
	}

	summary, err := h.uc.Execute(c.Request.Context(), tenantID)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}

	c.JSON(http.StatusOK, weeklyResp{
		Replenish: replenishResp{
			Count:     summary.ReplenishCount,
			AmountCNY: summary.ReplenishAmountCNY.StringFixed(2),
		},
		Oversell:  countResp{Count: summary.OversellCount},
		DeadStock: countResp{Count: summary.DeadStockCount},
		SuggestionScorecard: scorecardResp{
			Suggested:      summary.Suggested,
			Adopted:        summary.Adopted,
			MissedStockout: summary.MissedStockout,
		},
		GeneratedAt: summary.GeneratedAt,
	})
}
