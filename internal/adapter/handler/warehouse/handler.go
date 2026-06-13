// Package warehouse implements the Gin HTTP handlers for the warehouse REST API.
package warehouse

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appwarehouse "github.com/hanmahong5-arch/lurus-tally/internal/app/warehouse"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/warehouse"
)

// WarehouseDTO is the wire representation of a warehouse.
type WarehouseDTO struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	Code      string `json:"code,omitempty"`
	Name      string `json:"name"`
	Address   string `json:"address,omitempty"`
	Manager   string `json:"manager,omitempty"`
	IsDefault bool   `json:"is_default"`
	Remark    string `json:"remark,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// WarehouseListResponse wraps the pagination envelope.
type WarehouseListResponse struct {
	Items []*WarehouseDTO `json:"items"`
	Total int             `json:"total"`
}

// createRequest is the JSON body for POST /api/v1/warehouses.
type createRequest struct {
	Code      string `json:"code"       binding:"max=64"`
	Name      string `json:"name"       binding:"required,max=128"`
	Address   string `json:"address"    binding:"max=500"`
	Manager   string `json:"manager"    binding:"max=128"`
	IsDefault bool   `json:"is_default"`
	Remark    string `json:"remark"     binding:"max=500"`
}

// updateRequest is the JSON body for PUT /api/v1/warehouses/:id.
type updateRequest struct {
	Code      *string `json:"code,omitempty"`
	Name      *string `json:"name,omitempty"    binding:"omitempty,max=128"`
	Address   *string `json:"address,omitempty" binding:"omitempty,max=500"`
	Manager   *string `json:"manager,omitempty" binding:"omitempty,max=128"`
	IsDefault *bool   `json:"is_default,omitempty"`
	Remark    *string `json:"remark,omitempty"  binding:"omitempty,max=500"`
}

// Handler groups all warehouse Gin handlers.
type Handler struct {
	create  *appwarehouse.CreateUseCase
	get     *appwarehouse.GetByIDUseCase
	list    *appwarehouse.ListUseCase
	update  *appwarehouse.UpdateUseCase
	delete  *appwarehouse.DeleteUseCase
	restore *appwarehouse.RestoreUseCase
}

// New constructs a Handler wired to the provided use cases.
func New(
	create *appwarehouse.CreateUseCase,
	get *appwarehouse.GetByIDUseCase,
	list *appwarehouse.ListUseCase,
	update *appwarehouse.UpdateUseCase,
	del *appwarehouse.DeleteUseCase,
	restore *appwarehouse.RestoreUseCase,
) *Handler {
	return &Handler{
		create:  create,
		get:     get,
		list:    list,
		update:  update,
		delete:  del,
		restore: restore,
	}
}

// RegisterRoutes registers all warehouse routes on the provided router group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/warehouses", h.List)
	rg.POST("/warehouses", h.Create)
	rg.GET("/warehouses/:id", h.GetByID)
	rg.PUT("/warehouses/:id", h.Update)
	rg.DELETE("/warehouses/:id", h.Delete)
	rg.POST("/warehouses/:id/restore", h.Restore)
}

// List handles GET /api/v1/warehouses
func (h *Handler) List(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant not identified"})
		return
	}

	f := domain.ListFilter{
		TenantID: tenantID,
		Query:    c.Query("q"),
		Limit:    queryInt(c, "limit", 20),
		Offset:   queryInt(c, "offset", 0),
	}

	items, total, err := h.list.Execute(c.Request.Context(), f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	dtos := make([]*WarehouseDTO, 0, len(items))
	for _, w := range items {
		dtos = append(dtos, toDTO(w))
	}
	c.JSON(http.StatusOK, WarehouseListResponse{Items: dtos, Total: total})
}

// Create handles POST /api/v1/warehouses
func (h *Handler) Create(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant not identified"})
		return
	}

	var req createRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	w, err := h.create.Execute(c.Request.Context(), domain.CreateInput{
		TenantID:  tenantID,
		Code:      req.Code,
		Name:      req.Name,
		Address:   req.Address,
		Manager:   req.Manager,
		IsDefault: req.IsDefault,
		Remark:    req.Remark,
	})
	if err != nil {
		if errors.Is(err, appwarehouse.ErrDuplicateName) {
			c.JSON(http.StatusConflict, gin.H{"error": "duplicate warehouse name"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.Header("Location", fmt.Sprintf("/api/v1/warehouses/%s", w.ID))
	c.JSON(http.StatusCreated, toDTO(w))
}

// GetByID handles GET /api/v1/warehouses/:id
func (h *Handler) GetByID(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant not identified"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	w, err := h.get.Execute(c.Request.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, appwarehouse.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toDTO(w))
}

// Update handles PUT /api/v1/warehouses/:id
func (h *Handler) Update(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant not identified"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var req updateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	w, err := h.update.Execute(c.Request.Context(), tenantID, id, domain.UpdateInput{
		Code:      req.Code,
		Name:      req.Name,
		Address:   req.Address,
		Manager:   req.Manager,
		IsDefault: req.IsDefault,
		Remark:    req.Remark,
	})
	if err != nil {
		if errors.Is(err, appwarehouse.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toDTO(w))
}

// Delete handles DELETE /api/v1/warehouses/:id
func (h *Handler) Delete(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant not identified"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	if err := h.delete.Execute(c.Request.Context(), tenantID, id); err != nil {
		if errors.Is(err, appwarehouse.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// Restore handles POST /api/v1/warehouses/:id/restore
func (h *Handler) Restore(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant not identified"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	w, err := h.restore.Execute(c.Request.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, appwarehouse.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toDTO(w))
}

func toDTO(w *domain.Warehouse) *WarehouseDTO {
	return &WarehouseDTO{
		ID:        w.ID.String(),
		TenantID:  w.TenantID.String(),
		Code:      w.Code,
		Name:      w.Name,
		Address:   w.Address,
		Manager:   w.Manager,
		IsDefault: w.IsDefault,
		Remark:    w.Remark,
		CreatedAt: w.CreatedAt.Format(time.RFC3339),
		UpdatedAt: w.UpdatedAt.Format(time.RFC3339),
	}
}

func queryInt(c *gin.Context, key string, defaultVal int) int {
	s := c.Query(key)
	if s == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 {
		return defaultVal
	}
	return v
}
