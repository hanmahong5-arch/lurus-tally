// Package replenish implements the HTTP handlers for replenishment endpoints.
//
// Endpoints:
//
//	GET  /api/v1/replenish/suggestions?weeks=2
//	POST /api/v1/replenish/draft-batch
//	GET  /api/v1/replenish/scorecard
package replenish

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appreplenish "github.com/hanmahong5-arch/lurus-tally/internal/app/replenish"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/decimalutil"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

// ListSuggestionsUseCase is the surface the handler calls for GET suggestions.
type ListSuggestionsUseCase interface {
	Execute(ctx context.Context, tenantID uuid.UUID, weeks int) ([]appreplenish.SuggestionRow, error)
}

// CreateDraftBatchUseCase is the surface the handler calls for POST draft-batch.
type CreateDraftBatchUseCase interface {
	Execute(ctx context.Context, req appreplenish.DraftBatchRequest) (*appreplenish.DraftBatchOutput, error)
}

// GetScorecardUseCase is the surface the handler calls for GET scorecard.
type GetScorecardUseCase interface {
	Execute(ctx context.Context, tenantID uuid.UUID) (*appreplenish.Scorecard, error)
}

// Handler groups replenishment HTTP endpoints.
type Handler struct {
	uc        ListSuggestionsUseCase
	batch     CreateDraftBatchUseCase // nil when not wired (returns 501)
	scorecard GetScorecardUseCase     // nil when not wired (returns 501)
}

// New constructs a Handler with only the suggestions use case.
// Use NewWithBatch to also wire the draft-batch endpoint.
func New(uc ListSuggestionsUseCase) *Handler {
	return &Handler{uc: uc}
}

// NewWithBatch constructs a Handler with both use cases.
func NewWithBatch(uc ListSuggestionsUseCase, batch CreateDraftBatchUseCase) *Handler {
	return &Handler{uc: uc, batch: batch}
}

// WithScorecard wires the scorecard use case (F3). nil keeps the route at 501.
func (h *Handler) WithScorecard(sc GetScorecardUseCase) *Handler {
	h.scorecard = sc
	return h
}

// RegisterRoutes mounts replenishment endpoints onto the given router group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/replenish/suggestions", h.GetSuggestions)
	rg.POST("/replenish/draft-batch", h.PostDraftBatch)
	rg.GET("/replenish/scorecard", h.GetScorecard)
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
	// Forecast fields — the web client declared these long before the handler
	// serialized them; keep the JSON names aligned with web/lib/api/replenish.ts.
	LeadTimeDays int    `json:"lead_time_days"`
	InTransit    string `json:"in_transit"`
	ROP          string `json:"rop"`
	SafetyStock  string `json:"safety_stock"`
	Reason       string `json:"reason"`
	// Learning fields (F1/F2).
	LastPurchasePrice *string `json:"last_purchase_price,omitempty"`
	LeadTimeSource    string  `json:"lead_time_source"`
	LeadTimeSamples   int     `json:"lead_time_samples"`
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
		httperr.WriteInternal(c, err)
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

			LeadTimeDays: r.LeadTimeDays,
			InTransit:    r.InTransit.String(),
			ROP:          r.ROP.String(),
			SafetyStock:  r.SafetyStock.String(),
			Reason:       r.Reason,

			LeadTimeSource:  r.LeadTimeSource,
			LeadTimeSamples: r.LeadTimeSamples,
		}
		if r.SupplierID != nil {
			s := r.SupplierID.String()
			resp.SupplierID = &s
		}
		if r.LastPurchasePrice != nil {
			p := r.LastPurchasePrice.String()
			resp.LastPurchasePrice = &p
		}
		items = append(items, resp)
	}

	c.JSON(http.StatusOK, gin.H{
		"items": items,
		"count": len(items),
		"weeks": weeks,
	})
}

// ----- Draft-batch -----

// draftBatchLineInput is one line in the POST /replenish/draft-batch request body.
type draftBatchLineInput struct {
	ProductID  string  `json:"product_id"  binding:"required,uuid"`
	SupplierID *string `json:"supplier_id,omitempty"`
	Qty        string  `json:"qty"         binding:"required"`
}

// draftBatchReq is the request body for POST /replenish/draft-batch.
type draftBatchReq struct {
	Lines []draftBatchLineInput `json:"lines" binding:"required,min=1,max=200,dive"`
}

// draftResultResp is one created draft in the response.
type draftResultResp struct {
	BillID       string  `json:"bill_id"`
	BillNo       string  `json:"bill_no"`
	SupplierID   *string `json:"supplier_id,omitempty"`
	SupplierName string  `json:"supplier_name,omitempty"`
	LineCount    int     `json:"line_count"`
}

// PostDraftBatch handles POST /api/v1/replenish/draft-batch.
//
// Groups selected replenishment lines by supplier and creates one purchase
// draft per group. Accepts an Idempotency-Key header; when Redis is available
// the middleware deduplicates identical keys transparently.
func (h *Handler) PostDraftBatch(c *gin.Context) {
	if h.batch == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "not_implemented", "detail": "batch drafting not configured"})
		return
	}

	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "detail": "tenant_id required"})
		return
	}

	var req draftBatchReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request", "detail": err.Error()})
		return
	}

	lines := make([]appreplenish.DraftBatchLine, 0, len(req.Lines))
	for _, l := range req.Lines {
		pid, err := uuid.Parse(l.ProductID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request", "detail": "invalid product_id: " + l.ProductID})
			return
		}
		qty, err := decimalutil.Parse(l.Qty, "qty")
		if err != nil || qty.IsZero() || qty.IsNegative() {
			c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request", "detail": "qty must be a positive number"})
			return
		}

		var supplierID *uuid.UUID
		if l.SupplierID != nil && *l.SupplierID != "" {
			sid, err := uuid.Parse(*l.SupplierID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request", "detail": "invalid supplier_id: " + *l.SupplierID})
				return
			}
			supplierID = &sid
		}

		lines = append(lines, appreplenish.DraftBatchLine{
			ProductID:  pid,
			SupplierID: supplierID,
			Qty:        qty,
		})
	}

	// Creator ID from JWT sub (middleware-injected). PAT auth carries no user
	// sub, so fall back to the tenant id as the integration actor — matching the
	// payment handler. Passing uuid.Nil here made the use case reject the request
	// as "creator_id is required", surfacing a confusing 500. creator_id has no FK,
	// so the tenant id is a safe sentinel for machine-driven writes.
	creatorID := resolveCreatorID(c)
	if creatorID == uuid.Nil {
		creatorID = tenantID
	}

	out, err := h.batch.Execute(c.Request.Context(), appreplenish.DraftBatchRequest{
		TenantID:  tenantID,
		CreatorID: creatorID,
		Lines:     lines,
		Remark:    "补货建议批量草稿",
	})
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}

	results := make([]draftResultResp, 0, len(out.Drafts))
	for _, d := range out.Drafts {
		r := draftResultResp{
			BillID:       d.BillID.String(),
			BillNo:       d.BillNo,
			SupplierName: d.SupplierName,
			LineCount:    d.LineCount,
		}
		if d.SupplierID != nil {
			s := d.SupplierID.String()
			r.SupplierID = &s
		}
		results = append(results, r)
	}

	c.JSON(http.StatusOK, gin.H{
		"drafts": results,
		"count":  len(results),
	})
}

// ----- Scorecard -----

// scorecardResp is the response body for GET /api/v1/replenish/scorecard.
type scorecardResp struct {
	WindowDays       int     `json:"window_days"`
	SuggestionsCount int     `json:"suggestions_count"`
	AdoptedCount     int     `json:"adopted_count"`
	AdoptionRate     float64 `json:"adoption_rate"`
	StockoutMisses   int     `json:"stockout_misses"`
}

// GetScorecard handles GET /api/v1/replenish/scorecard — the 28-day
// suggestion track record (adoption rate + stockout misses).
func (h *Handler) GetScorecard(c *gin.Context) {
	if h.scorecard == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "not_implemented", "detail": "scorecard not configured"})
		return
	}

	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "detail": "tenant_id required"})
		return
	}

	sc, err := h.scorecard.Execute(c.Request.Context(), tenantID)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}

	c.JSON(http.StatusOK, scorecardResp{
		WindowDays:       sc.WindowDays,
		SuggestionsCount: sc.SuggestionsCount,
		AdoptedCount:     sc.AdoptedCount,
		AdoptionRate:     sc.AdoptionRate,
		StockoutMisses:   sc.StockoutMisses,
	})
}

// resolveCreatorID reads the creator UUID from the OIDC subject injected by
// AuthMiddleware. The X-User-ID header fallback was removed (UAT-3 Bug 2)
// because clients could spoof bill_head.creator_id by setting it. Returns
// uuid.Nil when no sub is present — the use case validates non-nil and
// rejects gracefully.
func resolveCreatorID(c *gin.Context) uuid.UUID {
	sub, exists := c.Get(middleware.CtxKeyIDPSubject)
	if !exists {
		return uuid.Nil
	}
	s, ok := sub.(string)
	if !ok {
		return uuid.Nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}
	return id
}
