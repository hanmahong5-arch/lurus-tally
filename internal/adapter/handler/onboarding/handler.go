// Package onboarding provides the Gin HTTP handlers for the guided first-run flow.
//
// Routes:
//
//	POST /api/v1/onboarding/seed-demo   — seeds demo products, opening stock, and
//	                                      ~30 days of backdated sales (velocity)
//	POST /api/v1/onboarding/clear-demo  — clears demo data (used ones become real)
//
// The handler adapts the app-layer onboarding ports (StockInitRequest /
// DemoSaleRequest) to the real appstock.RecordMovementUseCase signature via the
// stockAdapter inner type, which serves as both initializer and sales recorder.
package onboarding

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

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
	billItemID, err := a.sales.InsertDemoSale(ctx, appob.DemoSaleBill(req))
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
	seed := appob.NewSeedDemoUseCase(productCreator, adapter, adapter)
	// The default-SKU writer (prices, unit, barcode) is optional: stores that do
	// not implement it still seed products and stock.
	if w, ok := demoRepo.(appob.DemoSKUWriter); ok {
		seed.WithSKUWriter(w)
	}
	return &Handler{
		seed:  seed,
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
// No body required. Clears the authenticated tenant's demo data. Response 200
// with the number of products cleared and the demo products that approved bills
// still use, which were kept as real products (converted_products, with a hint
// to check them by stocktake).
func (h *Handler) ClearDemo(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant not identified"})
		return
	}

	res, err := h.clear.Execute(c.Request.Context(), tenantID)
	if err != nil {
		var inUse *appob.DemoInUseError
		if errors.As(err, &inUse) {
			httperr.Write(c, httperr.Wrap(http.StatusConflict, "demo_in_use",
				"示例商品已被你已审核的单据用到（"+billNoList(inUse.BillNos, inUse.Total)+"），这些示例数据会一直算在报表里，请按下面的办法处理",
				"是你卖的货：在库存页用“盘点”把数量改成实际数；不是你卖的货：在商品页把这个示例商品删除（已审核的单据不受影响）", err))
			return
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			slog.Warn("clear demo blocked by foreign key", "tenant_id", tenantID, "constraint", pgErr.ConstraintName, "err", err)
			httperr.Write(c, httperr.Wrap(http.StatusConflict, "demo_clear_conflict",
				"示例数据这次没有清除成功，没有改动任何数据", "请稍后再试；如果一直不行请联系客服", err))
			return
		}
		httperr.Write(c, httperr.Wrap(http.StatusInternalServerError, "clear_demo_failed",
			"清除示例数据没有成功", "请稍后再试，如果一直不行请联系客服", err))
		return
	}
	c.JSON(http.StatusOK, res)
}

// billNoList renders up to five bill numbers for the owner, "等N张" beyond that.
// total is the real count (nos may be capped); it falls back to len(nos).
func billNoList(nos []string, total int) string {
	if total < len(nos) {
		total = len(nos)
	}
	if total <= 5 {
		return strings.Join(nos, "、")
	}
	return strings.Join(nos[:5], "、") + fmt.Sprintf(" 等 %d 张", total)
}

// NewForTest constructs a Handler with pre-built use cases, bypassing the
// stock adapter. Use only in unit tests.
func NewForTest(seed *appob.SeedDemoUseCase, clear *appob.ClearDemoUseCase) *Handler {
	return &Handler{seed: seed, clear: clear}
}
