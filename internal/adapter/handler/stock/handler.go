// Package stock implements the Gin HTTP handlers for the stock management REST API.
package stock

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// Handler groups all stock REST handlers.
type Handler struct {
	record        *appstock.RecordMovementUseCase
	getSnapshot   *appstock.GetSnapshotUseCase
	listSnapshots *appstock.ListSnapshotsUseCase
	listMovements *appstock.ListMovementsUseCase
}

// New constructs the handler. All use cases must be non-nil.
func New(
	record *appstock.RecordMovementUseCase,
	getSnapshot *appstock.GetSnapshotUseCase,
	listSnapshots *appstock.ListSnapshotsUseCase,
	listMovements *appstock.ListMovementsUseCase,
) *Handler {
	return &Handler{
		record:        record,
		getSnapshot:   getSnapshot,
		listSnapshots: listSnapshots,
		listMovements: listMovements,
	}
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

	qty, err := decimal.NewFromString(req.Qty)
	if err != nil || qty.IsZero() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid qty: must be a non-zero decimal"})
		return
	}

	unitCost := decimal.Zero
	if req.UnitCost != "" {
		unitCost, err = decimal.NewFromString(req.UnitCost)
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
				"error":     "insufficient_stock",
				"available": ise.Available.String(),
				"requested": ise.Requested.String(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
		Limit:    parseIntQuery(c, "limit", 20),
		Offset:   parseIntQuery(c, "offset", 0),
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
		Limit:    parseIntQuery(c, "limit", 50),
		Offset:   parseIntQuery(c, "offset", 0),
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": mvs})
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

func parseIntQuery(c *gin.Context, key string, def int) int {
	if s := c.Query(key); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 0 {
			return n
		}
	}
	return def
}
