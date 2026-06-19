// Package onboarding provides the Gin HTTP handlers for the guided first-run flow.
//
// Routes:
//
//	POST /api/v1/onboarding/seed-demo   — seeds demo products, opening stock, and
//	                                      ~30 days of backdated sales (velocity)
//	POST /api/v1/onboarding/clear-demo  — removes all demo-marked rows
//
// The handler adapts the app-layer onboarding ports (StockInitRequest /
// DemoSaleRequest) to the real appstock.RecordMovementUseCase signature via the
// stockAdapter inner type, which serves as both initializer and sales recorder.
package onboarding

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appob "github.com/hanmahong5-arch/lurus-tally/internal/app/onboarding"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

// stockAdapter bridges the onboarding ports → appstock.RecordMovementUseCase.
// It implements both appob.StockInitializer (the opening 'in' receipt) and
// appob.SalesRecorder (backdated demo sales, recorded as a sale bill + its
// linked 'out' movement via the injected DemoSaleWriter).
type stockAdapter struct {
	uc    *appstock.RecordMovementUseCase
	sales appob.DemoSaleWriter
}

// Execute records the opening receipt as a RefInit 'in' movement, backdated to
// the caller-supplied OccurredAt (zero → now() downstream).
func (a *stockAdapter) Execute(ctx context.Context, req appob.StockInitRequest) (*domainstock.Snapshot, error) {
	snap, err := a.uc.Execute(ctx, appstock.RecordMovementRequest{
		TenantID:      req.TenantID,
		ProductID:     req.ProductID,
		WarehouseID:   req.WarehouseID,
		Direction:     domainstock.DirectionIn,
		Qty:           req.Qty,
		ConvFactor:    "1",
		UnitCost:      req.UnitCost,
		CostStrategy:  domainstock.CostStrategyWAC,
		ReferenceType: domainstock.RefInit,
		OccurredAt:    req.OccurredAt,
	})
	if err != nil {
		return nil, fmt.Errorf("onboarding stock adapter: %w", err)
	}
	return snap, nil
}

// RecordSale records one backdated demo sale as a real approved sale: it writes
// the sale bill first (so revenue/margin reports light up) and then records the
// linked 'out' movement (so velocity / low-stock / digest light up). The
// movement's reference_id is the new bill_item id (reference_type=sale) — the
// same shape a real approved sale has. The WAC engine overwrites the out unit
// cost with the prevailing average, so none is supplied to the movement.
func (a *stockAdapter) RecordSale(ctx context.Context, req appob.DemoSaleRequest) error {
	billItemID, err := a.sales.InsertDemoSale(ctx, appob.DemoSaleBill{
		TenantID:    req.TenantID,
		ProductID:   req.ProductID,
		WarehouseID: req.WarehouseID,
		Qty:         req.Qty,
		UnitPrice:   req.UnitPrice,
		UnitCost:    req.UnitCost,
		OccurredAt:  req.OccurredAt,
	})
	if err != nil {
		return fmt.Errorf("onboarding sales adapter: write demo sale bill: %w", err)
	}
	if _, err := a.uc.Execute(ctx, appstock.RecordMovementRequest{
		TenantID:      req.TenantID,
		ProductID:     req.ProductID,
		WarehouseID:   req.WarehouseID,
		Direction:     domainstock.DirectionOut,
		Qty:           req.Qty,
		ConvFactor:    "1",
		CostStrategy:  domainstock.CostStrategyWAC,
		ReferenceType: domainstock.RefSale,
		ReferenceID:   &billItemID,
		OccurredAt:    req.OccurredAt,
	}); err != nil {
		return fmt.Errorf("onboarding sales adapter: record sale movement: %w", err)
	}
	return nil
}

// Handler groups the onboarding HTTP handlers.
type Handler struct {
	seed  *appob.SeedDemoUseCase
	clear *appob.ClearDemoUseCase
}

// demoStore is the onboarding persistence surface: it writes demo sale bills
// (for the seed) and deletes all demo rows (for clear-demo). *repoonboarding.Repo
// satisfies it, so the lifecycle call site is unchanged.
type demoStore interface {
	appob.DemoDeleter
	appob.DemoSaleWriter
}

// New constructs a Handler.
//
// Supervisor wiring:
//
//	stockUC  = the existing *appstock.RecordMovementUseCase (already wired in lifecycle/app.go)
//	productCreator = the existing *appproduct.CreateUseCase
//	demoRepo = *repoonboarding.Repo backed by the shared *sql.DB (deleter + sale writer)
//
//	onboardingHandler := handleronboarding.New(
//	    appproduct.NewCreateUseCase(productRepo),
//	    recordMovementUC,
//	    repoonboarding.New(db),
//	)
func New(
	productCreator appob.ProductCreator,
	stockUC *appstock.RecordMovementUseCase,
	demoRepo demoStore,
) *Handler {
	// adapter is the initializer ('in'), the sales recorder (bill + 'out'
	// movement), and uses demoRepo to persist the sale bills.
	adapter := &stockAdapter{uc: stockUC, sales: demoRepo}
	return &Handler{
		seed:  appob.NewSeedDemoUseCase(productCreator, adapter, adapter),
		clear: appob.NewClearDemoUseCase(demoRepo),
	}
}

// RegisterRoutes registers the onboarding routes on the provided router group.
// Call as: onboardingHandler.RegisterRoutes(api)  (where api = r.Group("/api/v1")).
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/onboarding/seed-demo", h.SeedDemo)
	rg.POST("/onboarding/clear-demo", h.ClearDemo)
}

// seedDemoRequest is the JSON body for POST /api/v1/onboarding/seed-demo.
type seedDemoRequest struct {
	Persona     string `json:"persona"      binding:"required"`
	WarehouseID string `json:"warehouse_id" binding:"required"`
}

// SeedDemo handles POST /api/v1/onboarding/seed-demo.
//
// Body: { "persona": "cross_border"|"retail"|"horticulture", "warehouse_id": "<uuid>" }
// Response 200: { "products_created": N }
func (h *Handler) SeedDemo(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant not identified"})
		return
	}

	var req seedDemoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	warehouseID, err := uuid.Parse(req.WarehouseID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid warehouse_id"})
		return
	}

	persona := appob.Persona(req.Persona)
	switch persona {
	case appob.PersonaCrossBorder, appob.PersonaRetail, appob.PersonaHorticulture:
		// valid
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "persona must be cross_border, retail, or horticulture"})
		return
	}

	result, err := h.seed.Execute(c.Request.Context(), appob.SeedInput{
		TenantID:    tenantID,
		WarehouseID: warehouseID,
		Persona:     persona,
	})
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ClearDemo handles POST /api/v1/onboarding/clear-demo.
//
// No body required. Deletes all demo-marked rows for the authenticated tenant.
// Response 204 on success.
func (h *Handler) ClearDemo(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant not identified"})
		return
	}

	if err := h.clear.Execute(c.Request.Context(), tenantID); err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// NewForTest constructs a Handler with pre-built use cases, bypassing the
// stock adapter. Use only in unit tests.
func NewForTest(seed *appob.SeedDemoUseCase, clear *appob.ClearDemoUseCase) *Handler {
	return &Handler{seed: seed, clear: clear}
}
