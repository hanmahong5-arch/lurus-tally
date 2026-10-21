// Package stock implements the Gin HTTP handlers for the stock management REST API.
package stock

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
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
	adjust        *appstock.AdjustUseCase
	names         ProductNamer
	extra         []func(rg *gin.RouterGroup)
}

// ProductNamer resolves product names for the error a count gives to someone who cannot
// fill in a purchase price. Without it the message simply carries no names.
type ProductNamer interface {
	ProductNames(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]string, error)
}

// WithProductNames lets a count refusal name the products that still need a price.
func (h *Handler) WithProductNames(n ProductNamer) *Handler {
	h.names = n
	return h
}

// WithAdjust enables POST /stock/adjustments (stock count). Without it the route is not mounted.
func (h *Handler) WithAdjust(uc *appstock.AdjustUseCase) *Handler {
	h.adjust = uc
	return h
}

// WithExtraRoutes lets the composition root mount sibling routes that share this
// router group (and so its auth, tenant and idempotency middleware) without
// changing router.New: fn is called with the group in RegisterRoutes.
func (h *Handler) WithExtraRoutes(fn func(rg *gin.RouterGroup)) *Handler {
	h.extra = append(h.extra, fn)
	return h
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
	if h.adjust != nil {
		// Stock count: writes adjust movements for the difference only (never a bill).
		rg.POST("/stock/adjustments", h.PostAdjustment)
	}
	for _, fn := range h.extra {
		fn(rg)
	}
}

// postAdjustmentRequest is the JSON body for POST /api/v1/stock/adjustments.
type postAdjustmentRequest struct {
	WarehouseID uuid.UUID `json:"warehouse_id" binding:"required"`
	Items       []struct {
		ProductID  uuid.UUID `json:"product_id"`
		CountedQty string    `json:"counted_qty"`
		// UnitCost is the purchase price per base unit, only needed when the book is empty
		// and no price is on file (the response then asks for it with code cost_required).
		UnitCost string `json:"unit_cost"`
	} `json:"items"`
	Remark string `json:"remark" binding:"max=200"`
}

// PostAdjustment handles POST /api/v1/stock/adjustments: it compares each counted
// quantity with the book quantity and writes an adjust movement for the difference.
// Errors the shopkeeper can act on are returned as plain Chinese messages.
func (h *Handler) PostAdjustment(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "请先登录"})
		return
	}
	var req postAdjustmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "盘点单格式不正确，请选择仓库并填写盘点数量"})
		return
	}
	// Only people who may see cost can seed a purchase price through a count; for
	// everyone else unit_cost is dropped, so a count cannot write cost.
	canCost := middleware.CanViewCost(c)
	items := make([]appstock.AdjustItem, 0, len(req.Items))
	for _, it := range req.Items {
		// A blank count is "not counted yet", never zero: reading it as 0 would clear the stock.
		if strings.TrimSpace(it.CountedQty) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请填写盘点数量"})
			return
		}
		qty, err := decimalutil.Parse(it.CountedQty, "counted_qty")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "盘点数量请填写数字"})
			return
		}
		cost := decimal.Zero
		if canCost && strings.TrimSpace(it.UnitCost) != "" {
			if cost, err = decimalutil.Parse(it.UnitCost, "unit_cost"); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "进货单价请填写数字"})
				return
			}
		}
		items = append(items, appstock.AdjustItem{ProductID: it.ProductID, CountedQty: qty, UnitCost: cost})
	}
	in := appstock.AdjustRequest{TenantID: tenantID, WarehouseID: req.WarehouseID, Items: items, Remark: req.Remark}
	if sub := middleware.ActorUUID(c); sub != uuid.Nil {
		in.CreatedBy = &sub
	}
	res, err := h.adjust.Execute(c.Request.Context(), in)
	if err != nil {
		var ae *appstock.AdjustError
		if errors.As(err, &ae) {
			body := gin.H{"error": ae.Msg}
			status := http.StatusBadRequest
			if ae.Code == appstock.AdjustCostRequired && !canCost {
				// Not theirs to fill in: name the products so the owner knows what to price.
				refs := h.productNames(c.Request.Context(), tenantID, ae.ProductIDs)
				names := make([]string, len(refs))
				for i, r := range refs {
					names[i] = r.Name
				}
				body["error"] = noCostMessage(names)
				body["product_names"] = names
				body["products"] = refs // id + name, so same-named products stay distinct on screen
				status = http.StatusUnprocessableEntity
			}
			if ae.Code != "" {
				body["code"] = ae.Code
				ids := make([]string, len(ae.ProductIDs))
				for i, id := range ae.ProductIDs {
					ids[i] = id.String()
				}
				body["product_ids"] = ids
			}
			c.JSON(status, body)
			return
		}
		var ise *appstock.InsufficientStockError
		if errors.As(err, &ise) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "库存数量不足，无法完成盘点调整"})
			return
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" { // foreign_key_violation
			c.JSON(http.StatusBadRequest, gin.H{"error": "盘点单里的商品或仓库已不存在，请刷新页面后重试"})
			return
		}
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusCreated, res)
}

// productNames returns the names of ids in order, skipping any it cannot resolve; a lookup
// failure only costs the names, never the refusal.
// productRef is one product named in a count refusal.
type productRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (h *Handler) productNames(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) []productRef {
	out := []productRef{}
	if h.names == nil || len(ids) == 0 {
		return out
	}
	m, err := h.names.ProductNames(ctx, tenantID, ids)
	if err != nil {
		return out
	}
	for _, id := range ids {
		if n := strings.TrimSpace(m[id]); n != "" {
			out = append(out, productRef{ID: id.String(), Name: n})
		}
	}
	return out
}

func noCostMessage(names []string) string {
	const base = "这些商品还没有进价，请老板或店长先填进价，或由他们来盘点"
	if len(names) == 0 {
		return base
	}
	return base + "：" + strings.Join(names, "、")
}

// ListLowStock handles GET /api/v1/stock/alerts/low-stock.
// Returns products whose available stock has fallen at or below their
// auto-computed reorder point (learned demand + lead time; zero-config).
// An explicit per-product low_safe_qty, when set, overrides the learned ROP.
func (h *Handler) ListLowStock(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "请先登录"})
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
		c.JSON(http.StatusUnauthorized, gin.H{"error": "请先登录"})
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

	middleware.JSONTrimmed(c, http.StatusCreated, snap)
}

// GetSnapshot handles GET /api/v1/stock/snapshots/:product_id/:warehouse_id.
func (h *Handler) GetSnapshot(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "请先登录"})
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
	middleware.JSONTrimmed(c, http.StatusOK, snap)
}

// ListSnapshots handles GET /api/v1/stock/snapshots.
func (h *Handler) ListSnapshots(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "请先登录"})
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
	middleware.JSONTrimmed(c, http.StatusOK, gin.H{"items": snaps})
}

// ListMovements handles GET /api/v1/stock/movements.
func (h *Handler) ListMovements(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "请先登录"})
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
	middleware.JSONTrimmed(c, http.StatusOK, gin.H{"items": mvs})
}

// resolveTenantID returns the tenant UUID injected by AuthMiddleware.
// uuid.Nil → caller MUST return 401. No header fallback (see bill/handler.go).
func resolveTenantID(c *gin.Context) uuid.UUID {
	return middleware.GetTenantID(c)
}
