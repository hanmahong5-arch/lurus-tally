// Package product implements the Gin HTTP handlers for the product catalogue REST API.
package product

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	repoproduct "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/product"
	appproduct "github.com/hanmahong5-arch/lurus-tally/internal/app/product"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/product"
)

// Handler groups all product CRUD Gin handlers.
type Handler struct {
	create  *appproduct.CreateUseCase
	list    *appproduct.ListUseCase
	get     *appproduct.GetUseCase
	update  *appproduct.UpdateUseCase
	delete  *appproduct.DeleteUseCase
	restore *appproduct.RestoreUseCase
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
	Code                string                     `json:"code"`
	Name                string                     `json:"name"`
	Manufacturer        string                     `json:"manufacturer"`
	Model               string                     `json:"model"`
	Spec                string                     `json:"spec"`
	Brand               string                     `json:"brand"`
	Mnemonic            string                     `json:"mnemonic"`
	Color               string                     `json:"color"`
	ExpiryDays          *int                       `json:"expiry_days"`
	WeightKg            *string                    `json:"weight_kg"`
	EnableSerialNo      bool                       `json:"enable_serial_no"`
	EnableLotNo         bool                       `json:"enable_lot_no"`
	ShelfPosition       string                     `json:"shelf_position"`
	ImgURLs             []string                   `json:"img_urls"`
	Remark              string                     `json:"remark"`
	MeasurementStrategy domain.MeasurementStrategy `json:"measurement_strategy"`
	DefaultUnitID       *uuid.UUID                 `json:"default_unit_id"`
	Attributes          json.RawMessage            `json:"attributes"`
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

	p, err := h.create.Execute(c.Request.Context(), in)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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

	limit := parseIntQuery(c, "limit", 20)
	offset := parseIntQuery(c, "offset", 0)
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": out.Items, "total": out.Total})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, p)
}

// updateRequest is the JSON body for PUT /api/v1/products/:id.
type updateRequest struct {
	CategoryID          *uuid.UUID                 `json:"category_id"`
	Name                string                     `json:"name"`
	Manufacturer        string                     `json:"manufacturer"`
	Model               string                     `json:"model"`
	Spec                string                     `json:"spec"`
	Brand               string                     `json:"brand"`
	Mnemonic            string                     `json:"mnemonic"`
	Color               string                     `json:"color"`
	ExpiryDays          *int                       `json:"expiry_days"`
	WeightKg            *string                    `json:"weight_kg"`
	Enabled             *bool                      `json:"enabled"`
	EnableSerialNo      *bool                      `json:"enable_serial_no"`
	EnableLotNo         *bool                      `json:"enable_lot_no"`
	ShelfPosition       string                     `json:"shelf_position"`
	ImgURLs             []string                   `json:"img_urls"`
	Remark              string                     `json:"remark"`
	MeasurementStrategy domain.MeasurementStrategy `json:"measurement_strategy"`
	DefaultUnitID       *uuid.UUID                 `json:"default_unit_id"`
	Attributes          json.RawMessage            `json:"attributes"`
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

	p, err := h.update.Execute(c.Request.Context(), tenantID, id, in)
	if err != nil {
		if errors.Is(err, repoproduct.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, p)
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, p)
}

// resolveTenantID reads tenant UUID from the Gin context (set by AuthMiddleware)
// or falls back to the X-Tenant-ID header for development convenience.
// Returns uuid.Nil when neither source provides a valid UUID.
func resolveTenantID(c *gin.Context) uuid.UUID {
	id := middleware.GetTenantID(c)
	if id != uuid.Nil {
		return id
	}
	// Dev fallback: X-Tenant-ID header.
	if raw := c.GetHeader("X-Tenant-ID"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err == nil {
			return parsed
		}
	}
	return uuid.Nil
}

// parseIntQuery reads an integer query parameter or returns the default value.
func parseIntQuery(c *gin.Context, key string, def int) int {
	if s := c.Query(key); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 0 {
			return n
		}
	}
	return def
}
