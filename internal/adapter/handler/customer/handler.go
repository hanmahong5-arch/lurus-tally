// Package customer implements the Gin HTTP handlers for the customer REST API.
package customer

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appcust "github.com/hanmahong5-arch/lurus-tally/internal/app/customer"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/customer"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

// CustomerDTO is the wire representation of a customer.
type CustomerDTO struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenant_id"`
	Name        string `json:"name"`
	Code        string `json:"code,omitempty"`
	ContactName string `json:"contact_name,omitempty"`
	Mobile      string `json:"mobile,omitempty"`
	Phone       string `json:"phone,omitempty"`
	Address     string `json:"address,omitempty"`
	Remark      string `json:"remark,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// CustomerListResponse wraps the pagination envelope.
type CustomerListResponse struct {
	Items []*CustomerDTO `json:"items"`
	Total int            `json:"total"`
}

// createRequest is the JSON body for POST /api/v1/customers. The max lengths
// on contact_name/mobile/phone mirror the tally.partner column widths so an
// over-long value is a 400 here rather than a truncation error (500) in SQL.
type createRequest struct {
	Name        string `json:"name"         binding:"required,max=128"`
	Code        string `json:"code"         binding:"max=64"`
	ContactName string `json:"contact_name" binding:"max=100"`
	Mobile      string `json:"mobile"       binding:"max=30"`
	Phone       string `json:"phone"        binding:"max=30"`
	Address     string `json:"address"      binding:"max=500"`
	Remark      string `json:"remark"       binding:"max=500"`
}

// updateRequest is the JSON body for PUT /api/v1/customers/:id.
type updateRequest struct {
	Code        *string `json:"code,omitempty"         binding:"omitempty,max=64"`
	Name        *string `json:"name,omitempty"         binding:"omitempty,max=128"`
	ContactName *string `json:"contact_name,omitempty" binding:"omitempty,max=100"`
	Mobile      *string `json:"mobile,omitempty"       binding:"omitempty,max=30"`
	Phone       *string `json:"phone,omitempty"        binding:"omitempty,max=30"`
	Address     *string `json:"address,omitempty"      binding:"omitempty,max=500"`
	Remark      *string `json:"remark,omitempty"       binding:"omitempty,max=500"`
}

// Handler groups all customer Gin handlers.
type Handler struct {
	create   *appcust.CreateUseCase
	get      *appcust.GetByIDUseCase
	list     *appcust.ListUseCase
	update   *appcust.UpdateUseCase
	delete   *appcust.DeleteUseCase
	restore  *appcust.RestoreUseCase
	importer Importer
}

// New constructs a Handler wired to the provided use cases.
func New(
	create *appcust.CreateUseCase,
	get *appcust.GetByIDUseCase,
	list *appcust.ListUseCase,
	update *appcust.UpdateUseCase,
	del *appcust.DeleteUseCase,
	restore *appcust.RestoreUseCase,
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

// RegisterRoutes registers all customer routes on the provided router group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/customers", h.List)
	rg.POST("/customers", h.Create)
	rg.GET("/customers/:id", h.GetByID)
	if h.importer != nil {
		rg.POST("/customers/import", h.Import)
		rg.GET("/customers/import/template.csv", h.ImportTemplate)
	}
	rg.PUT("/customers/:id", h.Update)
	rg.DELETE("/customers/:id", h.Delete)
	rg.POST("/customers/:id/restore", h.Restore)
}

// List handles GET /api/v1/customers
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
		httperr.WriteInternal(c, err)
		return
	}

	dtos := make([]*CustomerDTO, 0, len(items))
	for _, cu := range items {
		dtos = append(dtos, toDTO(cu))
	}
	c.JSON(http.StatusOK, CustomerListResponse{Items: dtos, Total: total})
}

// Create handles POST /api/v1/customers
//
// A duplicate code within the tenant surfaces from the repository as a
// unique violation (23505) and is rendered as 409 by httperr.WriteInternal.
// A same-name or same-mobile customer is a 409 with the existing id.
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

	cu, err := h.create.Execute(c.Request.Context(), domain.CreateInput{
		TenantID:    tenantID,
		Code:        req.Code,
		Name:        req.Name,
		ContactName: req.ContactName,
		Mobile:      req.Mobile,
		Phone:       req.Phone,
		Address:     req.Address,
		Remark:      req.Remark,
	})
	if err != nil {
		if errors.Is(err, appcust.ErrInvalidInput) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		var dup *appcust.DuplicateError
		if errors.As(err, &dup) {
			writeDuplicate(c, dup)
			return
		}
		httperr.WriteInternal(c, err)
		return
	}

	c.Header("Location", fmt.Sprintf("/api/v1/customers/%s", cu.ID))
	c.JSON(http.StatusCreated, toDTO(cu))
}

func writeDuplicate(c *gin.Context, dup *appcust.DuplicateError) {
	code, msg := "duplicate_customer_name", fmt.Sprintf("已存在同名客户「%s」", dup.ExistingName)
	if errors.Is(dup, appcust.ErrDuplicateMobile) {
		code, msg = "duplicate_customer_mobile", fmt.Sprintf("该手机号已属于客户「%s」", dup.ExistingName)
	}
	c.JSON(http.StatusConflict, gin.H{"error": code, "message": msg, "existing_id": dup.ExistingID.String()})
}

// GetByID handles GET /api/v1/customers/:id
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

	cu, err := h.get.Execute(c.Request.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, appcust.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, toDTO(cu))
}

// Update handles PUT /api/v1/customers/:id
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

	cu, err := h.update.Execute(c.Request.Context(), tenantID, id, domain.UpdateInput{
		Code:        req.Code,
		Name:        req.Name,
		ContactName: req.ContactName,
		Mobile:      req.Mobile,
		Phone:       req.Phone,
		Address:     req.Address,
		Remark:      req.Remark,
	})
	if err != nil {
		if errors.Is(err, appcust.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		if errors.Is(err, appcust.ErrInvalidInput) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		var dup *appcust.DuplicateError
		if errors.As(err, &dup) {
			writeDuplicate(c, dup)
			return
		}
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, toDTO(cu))
}

// Delete handles DELETE /api/v1/customers/:id
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
		if errors.Is(err, appcust.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		httperr.WriteInternal(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// Restore handles POST /api/v1/customers/:id/restore
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

	cu, err := h.restore.Execute(c.Request.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, appcust.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, toDTO(cu))
}

func toDTO(cu *domain.Customer) *CustomerDTO {
	return &CustomerDTO{
		ID:          cu.ID.String(),
		TenantID:    cu.TenantID.String(),
		Name:        cu.Name,
		Code:        cu.Code,
		ContactName: cu.ContactName,
		Mobile:      cu.Mobile,
		Phone:       cu.Phone,
		Address:     cu.Address,
		Remark:      cu.Remark,
		CreatedAt:   cu.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   cu.UpdatedAt.Format(time.RFC3339),
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
