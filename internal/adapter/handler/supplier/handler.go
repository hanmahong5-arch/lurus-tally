// Package supplier implements the Gin HTTP handlers for the supplier REST API.
package supplier

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appsupp "github.com/hanmahong5-arch/lurus-tally/internal/app/supplier"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/supplier"
)

// SupplierDTO is the wire representation of a supplier.
type SupplierDTO struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	Code      string `json:"code,omitempty"`
	Name      string `json:"name"`
	Contact   string `json:"contact,omitempty"`
	Phone     string `json:"phone,omitempty"`
	Email     string `json:"email,omitempty"`
	Address   string `json:"address,omitempty"`
	Remark    string `json:"remark,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// SupplierListResponse wraps the pagination envelope.
type SupplierListResponse struct {
	Items []*SupplierDTO `json:"items"`
	Total int            `json:"total"`
}

// createRequest is the JSON body for POST /api/v1/suppliers.
type createRequest struct {
	Code    string `json:"code"    binding:"max=64"`
	Name    string `json:"name"    binding:"required,max=128"`
	Contact string `json:"contact" binding:"max=128"`
	Phone   string `json:"phone"   binding:"max=64"`
	Email   string `json:"email"   binding:"max=128"`
	Address string `json:"address" binding:"max=500"`
	Remark  string `json:"remark"  binding:"max=500"`
}

// updateRequest is the JSON body for PUT /api/v1/suppliers/:id.
type updateRequest struct {
	Code    *string `json:"code,omitempty"`
	Name    *string `json:"name,omitempty"    binding:"omitempty,max=128"`
	Contact *string `json:"contact,omitempty" binding:"omitempty,max=128"`
	Phone   *string `json:"phone,omitempty"   binding:"omitempty,max=64"`
	Email   *string `json:"email,omitempty"   binding:"omitempty,max=128"`
	Address *string `json:"address,omitempty" binding:"omitempty,max=500"`
	Remark  *string `json:"remark,omitempty"  binding:"omitempty,max=500"`
}

// Handler groups all supplier Gin handlers.
type Handler struct {
	create  *appsupp.CreateUseCase
	get     *appsupp.GetByIDUseCase
	list    *appsupp.ListUseCase
	update  *appsupp.UpdateUseCase
	delete  *appsupp.DeleteUseCase
	restore *appsupp.RestoreUseCase
}

// New constructs a Handler wired to the provided use cases.
func New(
	create *appsupp.CreateUseCase,
	get *appsupp.GetByIDUseCase,
	list *appsupp.ListUseCase,
	update *appsupp.UpdateUseCase,
	del *appsupp.DeleteUseCase,
	restore *appsupp.RestoreUseCase,
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

// RegisterRoutes registers all supplier routes on the provided router group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/suppliers", h.List)
	rg.POST("/suppliers", h.Create)
	rg.GET("/suppliers/:id", h.GetByID)
	rg.PUT("/suppliers/:id", h.Update)
	rg.DELETE("/suppliers/:id", h.Delete)
	rg.POST("/suppliers/:id/restore", h.Restore)
}

// List handles GET /api/v1/suppliers
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

	dtos := make([]*SupplierDTO, 0, len(items))
	for _, s := range items {
		dtos = append(dtos, toDTO(s))
	}
	c.JSON(http.StatusOK, SupplierListResponse{Items: dtos, Total: total})
}

// Create handles POST /api/v1/suppliers
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

	s, err := h.create.Execute(c.Request.Context(), domain.CreateInput{
		TenantID: tenantID,
		Code:     req.Code,
		Name:     req.Name,
		Contact:  req.Contact,
		Phone:    req.Phone,
		Email:    req.Email,
		Address:  req.Address,
		Remark:   req.Remark,
	})
	if err != nil {
		if errors.Is(err, appsupp.ErrDuplicateName) {
			c.JSON(http.StatusConflict, gin.H{"error": "duplicate supplier name"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.Header("Location", fmt.Sprintf("/api/v1/suppliers/%s", s.ID))
	c.JSON(http.StatusCreated, toDTO(s))
}

// GetByID handles GET /api/v1/suppliers/:id
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

	s, err := h.get.Execute(c.Request.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, appsupp.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toDTO(s))
}

// Update handles PUT /api/v1/suppliers/:id
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

	s, err := h.update.Execute(c.Request.Context(), tenantID, id, domain.UpdateInput{
		Code:    req.Code,
		Name:    req.Name,
		Contact: req.Contact,
		Phone:   req.Phone,
		Email:   req.Email,
		Address: req.Address,
		Remark:  req.Remark,
	})
	if err != nil {
		if errors.Is(err, appsupp.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toDTO(s))
}

// Delete handles DELETE /api/v1/suppliers/:id
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
		if errors.Is(err, appsupp.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// Restore handles POST /api/v1/suppliers/:id/restore
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

	s, err := h.restore.Execute(c.Request.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, appsupp.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toDTO(s))
}

func toDTO(s *domain.Supplier) *SupplierDTO {
	return &SupplierDTO{
		ID:        s.ID.String(),
		TenantID:  s.TenantID.String(),
		Code:      s.Code,
		Name:      s.Name,
		Contact:   s.Contact,
		Phone:     s.Phone,
		Email:     s.Email,
		Address:   s.Address,
		Remark:    s.Remark,
		CreatedAt: s.CreatedAt.Format(time.RFC3339),
		UpdatedAt: s.UpdatedAt.Format(time.RFC3339),
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
