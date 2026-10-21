// Package product implements the Gin HTTP handlers for the product catalogue REST API.
package product

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/planlimit"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	repoproduct "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/product"
	appproduct "github.com/hanmahong5-arch/lurus-tally/internal/app/product"
	appsku "github.com/hanmahong5-arch/lurus-tally/internal/app/sku"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/product"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

// Handler groups all product CRUD Gin handlers.
type Handler struct {
	create   *appproduct.CreateUseCase
	list     *appproduct.ListUseCase
	get      *appproduct.GetUseCase
	update   *appproduct.UpdateUseCase
	delete   *appproduct.DeleteUseCase
	restore  *appproduct.RestoreUseCase
	prices   RetailPriceLookup
	importer ProductImporter
}

// RetailPriceLookup resolves the default SKU (and its retail price) per product.
// *sku.Repo satisfies it; nil means product responses carry no retail_price.
type RetailPriceLookup interface {
	ListDefaultSKUs(ctx context.Context, tenantID uuid.UUID, productIDs []uuid.UUID) ([]appsku.DefaultSKU, error)
}

// WithPriceLookup attaches the retail-price source used by List and GetByID.
func (h *Handler) WithPriceLookup(l RetailPriceLookup) *Handler {
	h.prices = l
	return h
}

// productView is a product plus the default SKU's retail price (NUMERIC as string).
type productView struct {
	*domain.Product
	RetailPrice    *string `json:"retail_price,omitempty"`
	WholesalePrice *string `json:"wholesale_price,omitempty"`
	PurchasePrice  *string `json:"purchase_price,omitempty"`
	// RetailPriceUnit is the unit name the SKU price is quoted in; the POS compares it to the default unit.
	RetailPriceUnit string `json:"retail_price_unit,omitempty"`
}

// withPrices decorates products with their default SKU retail price. A lookup
// failure degrades to price-less rows: the cashier can still type a price.
// showCost=false leaves the purchase price out (people without cost.view).
func (h *Handler) withPrices(ctx context.Context, tenantID uuid.UUID, items []*domain.Product, showCost bool) []productView {
	views := make([]productView, len(items))
	for i, p := range items {
		views[i] = productView{Product: p}
	}
	if h.prices == nil || len(items) == 0 {
		return views
	}
	ids := make([]uuid.UUID, len(items))
	for i, p := range items {
		ids[i] = p.ID
	}
	skus, err := h.prices.ListDefaultSKUs(ctx, tenantID, ids)
	if err != nil {
		slog.Warn("product list: default SKU price lookup failed, returning price-less rows",
			"tenant_id", tenantID, "err", err)
		return views
	}
	byProduct := make(map[uuid.UUID]appsku.DefaultSKU, len(skus))
	for _, s := range skus {
		byProduct[s.ProductID] = s
	}
	for i := range views {
		if s, ok := byProduct[views[i].ID]; ok {
			v := s.RetailPrice.String()
			views[i].RetailPrice = &v
			w := s.WholesalePrice.String()
			views[i].WholesalePrice = &w
			if showCost {
				pu := s.PurchasePrice.String()
				views[i].PurchasePrice = &pu
			}
			views[i].RetailPriceUnit = s.UnitName
		}
	}
	return views
}

// New creates a Handler wired to the provided use cases.
func New(
	create *appproduct.CreateUseCase,
	list *appproduct.ListUseCase,
	get *appproduct.GetUseCase,
	update *appproduct.UpdateUseCase,
	del *appproduct.DeleteUseCase,
	restore *appproduct.RestoreUseCase,
) *Handler {
	return &Handler{
		create:  create,
		list:    list,
		get:     get,
		update:  update,
		delete:  del,
		restore: restore,
	}
}

// createRequest is the JSON body for POST /api/v1/products.
type createRequest struct {
	CategoryID          *uuid.UUID                 `json:"category_id"`
	Code                string                     `json:"code"          binding:"max=128"`
	Name                string                     `json:"name"          binding:"max=128"`
	Manufacturer        string                     `json:"manufacturer"  binding:"max=128"`
	Model               string                     `json:"model"         binding:"max=128"`
	Spec                string                     `json:"spec"          binding:"max=128"`
	Brand               string                     `json:"brand"         binding:"max=128"`
	Mnemonic            string                     `json:"mnemonic"      binding:"max=128"`
	Color               string                     `json:"color"         binding:"max=128"`
	ExpiryDays          *int                       `json:"expiry_days"`
	WeightKg            *string                    `json:"weight_kg"`
	EnableSerialNo      bool                       `json:"enable_serial_no"`
	EnableLotNo         bool                       `json:"enable_lot_no"`
	ShelfPosition       string                     `json:"shelf_position" binding:"max=128"`
	ImgURLs             []string                   `json:"img_urls"`
	Remark              string                     `json:"remark"         binding:"max=500"`
	MeasurementStrategy domain.MeasurementStrategy `json:"measurement_strategy"`
	DefaultUnitID       *uuid.UUID                 `json:"default_unit_id"`
	Attributes          json.RawMessage            `json:"attributes"`
	RetailPrice         *string                    `json:"retail_price"`
	WholesalePrice      *string                    `json:"wholesale_price"`
	PurchasePrice       *string                    `json:"purchase_price"`
}

// Create handles POST /api/v1/products.
// Requires tenant_id in Gin context (injected by AuthMiddleware in Story 2.1).
// In the interim, accepts tenant_id from the request header X-Tenant-ID for development.
func (h *Handler) Create(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id required: set X-Tenant-ID header or authenticate"})
		return
	}

	var req createRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	in := domain.CreateInput{
		TenantID:            tenantID,
		CategoryID:          req.CategoryID,
		Code:                req.Code,
		Name:                req.Name,
		Manufacturer:        req.Manufacturer,
		Model:               req.Model,
		Spec:                req.Spec,
		Brand:               req.Brand,
		Mnemonic:            req.Mnemonic,
		Color:               req.Color,
		ExpiryDays:          req.ExpiryDays,
		WeightKg:            req.WeightKg,
		EnableSerialNo:      req.EnableSerialNo,
		EnableLotNo:         req.EnableLotNo,
		ShelfPosition:       req.ShelfPosition,
		ImgURLs:             req.ImgURLs,
		Remark:              req.Remark,
		MeasurementStrategy: req.MeasurementStrategy,
		DefaultUnitID:       req.DefaultUnitID,
		Attributes:          req.Attributes,
	}

	p, err := h.create.ExecuteWithPrices(c.Request.Context(), in, appsku.PriceInput{
		Retail: req.RetailPrice, Wholesale: req.WholesalePrice, Purchase: purchaseInput(c, req.PurchasePrice),
	})
	if err != nil {
		writeSaveError(c, err)
		return
	}
	c.JSON(http.StatusCreated, p)
}

// List handles GET /api/v1/products.
// Query params: q (search), limit, offset, enabled (bool).
// Body (optional): {"attributes_filter": {...}} for JSONB containment search.
func (h *Handler) List(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id required"})
		return
	}

	limit := middleware.ParseLimitQuery(c, "limit", 20, middleware.DefaultMaxPageLimit)
	offset := middleware.ParseOffsetQuery(c, "offset")
	q := c.Query("q")

	var attrsFilter json.RawMessage
	if raw := c.Query("attributes_filter"); raw != "" {
		attrsFilter = json.RawMessage(raw)
	}

	filter := domain.ListFilter{
		TenantID:         tenantID,
		Query:            q,
		AttributesFilter: attrsFilter,
		Limit:            limit,
		Offset:           offset,
	}
	if enabledStr := c.Query("enabled"); enabledStr != "" {
		b := enabledStr == "true"
		filter.Enabled = &b
	}

	out, err := h.list.Execute(c.Request.Context(), filter)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": h.withPrices(c.Request.Context(), tenantID, out.Items, middleware.CanViewCost(c)), "total": out.Total})
}

// GetByID handles GET /api/v1/products/:id.
func (h *Handler) GetByID(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id required"})
		return
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid product id: must be a UUID"})
		return
	}

	p, err := h.get.Execute(c.Request.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, repoproduct.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
			return
		}
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, h.withPrices(c.Request.Context(), tenantID, []*domain.Product{p}, middleware.CanViewCost(c))[0])
}

// updateRequest is the JSON body for PUT /api/v1/products/:id.
type updateRequest struct {
	CategoryID          *uuid.UUID                 `json:"category_id"`
	Name                string                     `json:"name"          binding:"max=128"`
	Manufacturer        string                     `json:"manufacturer"  binding:"max=128"`
	Model               string                     `json:"model"         binding:"max=128"`
	Spec                string                     `json:"spec"          binding:"max=128"`
	Brand               string                     `json:"brand"         binding:"max=128"`
	Mnemonic            string                     `json:"mnemonic"      binding:"max=128"`
	Color               string                     `json:"color"         binding:"max=128"`
	ExpiryDays          *int                       `json:"expiry_days"`
	WeightKg            *string                    `json:"weight_kg"`
	Enabled             *bool                      `json:"enabled"`
	EnableSerialNo      *bool                      `json:"enable_serial_no"`
	EnableLotNo         *bool                      `json:"enable_lot_no"`
	ShelfPosition       string                     `json:"shelf_position" binding:"max=128"`
	ImgURLs             []string                   `json:"img_urls"`
	Remark              string                     `json:"remark"         binding:"max=500"`
	MeasurementStrategy domain.MeasurementStrategy `json:"measurement_strategy"`
	DefaultUnitID       *uuid.UUID                 `json:"default_unit_id"`
	Attributes          json.RawMessage            `json:"attributes"`
	RetailPrice         *string                    `json:"retail_price"`
	WholesalePrice      *string                    `json:"wholesale_price"`
	PurchasePrice       *string                    `json:"purchase_price"`
}

// Update handles PUT /api/v1/products/:id.
func (h *Handler) Update(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id required"})
		return
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid product id: must be a UUID"})
		return
	}

	var req updateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	in := domain.UpdateInput{
		CategoryID:          req.CategoryID,
		Name:                req.Name,
		Manufacturer:        req.Manufacturer,
		Model:               req.Model,
		Spec:                req.Spec,
		Brand:               req.Brand,
		Mnemonic:            req.Mnemonic,
		Color:               req.Color,
		ExpiryDays:          req.ExpiryDays,
		WeightKg:            req.WeightKg,
		Enabled:             req.Enabled,
		EnableSerialNo:      req.EnableSerialNo,
		EnableLotNo:         req.EnableLotNo,
		ShelfPosition:       req.ShelfPosition,
		ImgURLs:             req.ImgURLs,
		Remark:              req.Remark,
		MeasurementStrategy: req.MeasurementStrategy,
		DefaultUnitID:       req.DefaultUnitID,
		Attributes:          req.Attributes,
	}

	p, err := h.update.ExecuteWithPrices(c.Request.Context(), tenantID, id, in, appsku.PriceInput{
		Retail: req.RetailPrice, Wholesale: req.WholesalePrice, Purchase: purchaseInput(c, req.PurchasePrice),
	})
	if err != nil {
		if errors.Is(err, repoproduct.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
			return
		}
		writeSaveError(c, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

// writeSaveError answers a failed create/update with 400. Price validation failures
// and save-validation failures carry a plain Chinese message and are returned as-is;
// anything else is logged and answered with a generic Chinese message.
func writeSaveError(c *gin.Context, err error) {
	if planlimit.Write(c, err) {
		return
	}
	var pe *appsku.PriceError
	if errors.As(err, &pe) {
		c.JSON(http.StatusBadRequest, gin.H{"error": pe.Msg})
		return
	}
	var se *appproduct.SaveError
	if errors.As(err, &se) {
		c.JSON(http.StatusBadRequest, gin.H{"error": se.Msg})
		return
	}
	if strings.Contains(err.Error(), "name is required") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写商品名称"})
		return
	}
	slog.Warn("product save rejected", "err", err)
	c.JSON(http.StatusBadRequest, gin.H{"error": "商品信息填写有误，请检查后重试"})
}

// Delete handles DELETE /api/v1/products/:id.
func (h *Handler) Delete(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id required"})
		return
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid product id: must be a UUID"})
		return
	}

	if err := h.delete.Execute(c.Request.Context(), tenantID, id); err != nil {
		if errors.Is(err, repoproduct.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
			return
		}
		httperr.WriteInternal(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// Restore handles POST /api/v1/products/:id/restore.
// It un-deletes a soft-deleted product and returns the restored product JSON.
func (h *Handler) Restore(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id required"})
		return
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid product id: must be a UUID"})
		return
	}

	p, err := h.restore.Execute(c.Request.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, repoproduct.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "product not found or already active"})
			return
		}
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

// resolveTenantID returns the tenant UUID injected by AuthMiddleware.
// uuid.Nil → caller MUST return 401. No header fallback: any client could
// otherwise spoof X-Tenant-ID against a misconfigured deploy.
func resolveTenantID(c *gin.Context) uuid.UUID {
	return middleware.GetTenantID(c)
}

// purchaseInput drops a submitted purchase price from people who cannot see
// cost, so a clerk's save never overwrites (or reveals by echo) the real one.
func purchaseInput(c *gin.Context, v *string) *string {
	if !middleware.CanViewCost(c) {
		return nil
	}
	return v
}
