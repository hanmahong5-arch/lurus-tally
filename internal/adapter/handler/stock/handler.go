// Package stock implements the Gin HTTP handlers for the stock management REST API.
package stock

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appreplenish "github.com/hanmahong5-arch/lurus-tally/internal/app/replenish"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/decimalutil"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

// Handler groups all stock REST handlers.
type Handler struct {
	record        *appstock.RecordMovementUseCase
	getSnapshot   *appstock.GetSnapshotUseCase
	listSnapshots *appstock.ListSnapshotsUseCase
	listMovements *appstock.ListMovementsUseCase
	listLowStock  *appreplenish.ListLowStockUseCase
}

// New constructs the handler. All use cases must be non-nil.
func New(
	record *appstock.RecordMovementUseCase,
	getSnapshot *appstock.GetSnapshotUseCase,
	listSnapshots *appstock.ListSnapshotsUseCase,
	listMovements *appstock.ListMovementsUseCase,
	listLowStock *appreplenish.ListLowStockUseCase,
) *Handler {
	return &Handler{
		record:        record,
		getSnapshot:   getSnapshot,
		listSnapshots: listSnapshots,
		listMovements: listMovements,
		listLowStock:  listLowStock,
	}
}

// RegisterRoutes mounts read-only stock routes onto the given router group.
// POST /movements is intentionally NOT exposed: stock mutations must flow
// through bill approval (Epic 6/7) to keep movement -> reference_id integrity.
// A dev-only POST may be enabled in future via a separate guarded group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/stock/snapshots", h.ListSnapshots)
	rg.GET("/stock/snapshots/:product_id/:warehouse_id", h.GetSnapshot)
	rg.GET("/stock/movements", h.ListMovements)
	rg.GET("/stock/alerts/low-stock", h.ListLowStock)
}

// ListLowStock handles GET /api/v1/stock/alerts/low-stock.
// Returns products whose available stock has fallen at or below their
// auto-computed reorder point (learned demand + lead time; zero-config).
// An explicit per-product low_safe_qty, when set, overrides the learned ROP.
func (h *Handler) ListLowStock(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id required"})
		return
	}
	limit := middleware.ParseLimitQuery(c, "limit", 200, middleware.DefaultMaxPageLimit)
	rows, err := h.listLowStock.Execute(c.Request.Context(), tenantID, limit)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	if rows == nil {
		rows = []appreplenish.LowStockRow{}
	}
	c.JSON(http.StatusOK, gin.H{"items": rows, "count": len(rows)})
}

// postMovementRequest is the JSON body for POST /api/v1/stock/movements.
type postMovementRequest struct {
	ProductID     uuid.UUID  `json:"product_id"     binding:"required"`
	WarehouseID   uuid.UUID  `json:"warehouse_id"   binding:"required"`
	Direction     string     `json:"direction"      binding:"required"`
	Qty           string     `json:"qty"            binding:"required"`
	UnitID        *uuid.UUID `json:"unit_id"`
	ConvFactor    string     `json:"conv_factor"`
	UnitCost      string     `json:"unit_cost"`
	CostStrategy  string     `json:"cost_strategy"`
	ReferenceType string     `json:"reference_type" binding:"required"`
	ReferenceID   *uuid.UUID `json:"reference_id"`
	Note          string     `json:"note"`
}

// PostMovement handles POST /api/v1/stock/movements.
// V1: dev-internal endpoint for testing. Epic 6/7 call RecordMovementUseCase directly.
func (h *Handler) PostMovement(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id required"})
		return
	}

	var req postMovementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	qty, err := decimalutil.Parse(req.Qty, "qty")
	if err != nil || qty.IsZero() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid qty: must be a non-zero decimal"})
		return
	}

	unitCost := decimal.Zero
	if req.UnitCost != "" {
		unitCost, err = decimalutil.Parse(req.UnitCost, "unit_cost")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid unit_cost: must be a decimal"})
			return
		}
	}

	dir := domain.Direction(req.Direction)
	if err := dir.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid direction: must be in|out|adjust"})
		return
	}

	refType := domain.ReferenceType(req.ReferenceType)

	usReq := appstock.RecordMovementRequest{
		TenantID:      tenantID,
		ProductID:     req.ProductID,
		WarehouseID:   req.WarehouseID,
		Direction:     dir,
		Qty:           qty,
		ConvFactor:    req.ConvFactor,
		UnitCost:      unitCost,
		CostStrategy:  req.CostStrategy,
		ReferenceType: refType,
		ReferenceID:   req.ReferenceID,
		Note:          req.Note,
	}

	snap, err := h.record.Execute(c.Request.Context(), usReq)
	if err != nil {
		var ise *appstock.InsufficientStockError
		if errors.As(err, &ise) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error":      "insufficient_stock",
				"product_id": ise.ProductID.String(),
				"available":  ise.Available.String(),
				"requested":  ise.Requested.String(),
			})
			return
		}
		httperr.WriteInternal(c, err)
		return
	}

	c.JSON(http.StatusCreated, snap)
}

// GetSnapshot handles GET /api/v1/stock/snapshots/:product_id/:warehouse_id.
func (h *Handler) GetSnapshot(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id required"})
		return
	}

	productID, err := uuid.Parse(c.Param("product_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid product_id"})
		return
	}
	warehouseID, err := uuid.Parse(c.Param("warehouse_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid warehouse_id"})
		return
	}

	snap, err := h.getSnapshot.Execute(c.Request.Context(), tenantID, productID, warehouseID)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	if snap == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "snapshot not found"})
		return
	}
	c.JSON(http.StatusOK, snap)
}

// ListSnapshots handles GET /api/v1/stock/snapshots.
func (h *Handler) ListSnapshots(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id required"})
		return
	}

	f := appstock.ListSnapshotsFilter{
		TenantID: tenantID,
		Limit:    middleware.ParseLimitQuery(c, "limit", 20, middleware.DefaultMaxPageLimit),
		Offset:   middleware.ParseOffsetQuery(c, "offset"),
	}
	if v := c.Query("product_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			f.ProductID = id
		}
	}
	if v := c.Query("warehouse_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			f.WarehouseID = id
		}
	}

	snaps, err := h.listSnapshots.Execute(c.Request.Context(), f)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": snaps})
}

// ListMovements handles GET /api/v1/stock/movements.
func (h *Handler) ListMovements(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id required"})
		return
	}

	f := appstock.MovementFilter{
		TenantID: tenantID,
		Limit:    middleware.ParseLimitQuery(c, "limit", 50, middleware.DefaultMaxPageLimit),
		Offset:   middleware.ParseOffsetQuery(c, "offset"),
	}
	if v := c.Query("product_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			f.ProductID = id
		}
	}
	if v := c.Query("warehouse_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			f.WarehouseID = id
		}
	}

	mvs, err := h.listMovements.Execute(c.Request.Context(), f)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": mvs})
}

// resolveTenantID returns the tenant UUID injected by AuthMiddleware.
// uuid.Nil → caller MUST return 401. No header fallback (see bill/handler.go).
func resolveTenantID(c *gin.Context) uuid.UUID {
	return middleware.GetTenantID(c)
}
