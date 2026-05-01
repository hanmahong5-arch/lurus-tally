// Package project implements the Gin HTTP handlers for the project REST API.
package project

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appproject "github.com/hanmahong5-arch/lurus-tally/internal/app/project"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/project"
)

const dateLayout = "2006-01-02"

// ProjectDTO is the wire representation of a project.
type ProjectDTO struct {
	ID             string  `json:"id"`
	TenantID       string  `json:"tenant_id"`
	Code           string  `json:"code"`
	Name           string  `json:"name"`
	CustomerID     *string `json:"customer_id,omitempty"`
	ContractAmount *string `json:"contract_amount,omitempty"`
	StartDate      *string `json:"start_date,omitempty"`
	EndDate        *string `json:"end_date,omitempty"`
	Status         string  `json:"status"`
	Address        string  `json:"address"`
	Manager        string  `json:"manager"`
	Remark         string  `json:"remark"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

// ProjectListResponse wraps the pagination envelope.
type ProjectListResponse struct {
	Items []*ProjectDTO `json:"items"`
	Total int           `json:"total"`
}

// createRequest is the JSON body for POST /api/v1/projects.
type createRequest struct {
	Code           string  `json:"code"`
	Name           string  `json:"name"`
	CustomerID     *string `json:"customer_id,omitempty"`
	ContractAmount *string `json:"contract_amount,omitempty"`
	StartDate      *string `json:"start_date,omitempty"`
	EndDate        *string `json:"end_date,omitempty"`
	Status         string  `json:"status"`
	Address        string  `json:"address"`
	Manager        string  `json:"manager"`
	Remark         string  `json:"remark"`
}

// updateRequest is the JSON body for PUT /api/v1/projects/:id.
type updateRequest struct {
	Code           *string `json:"code,omitempty"`
	Name           *string `json:"name,omitempty"`
	CustomerID     *string `json:"customer_id,omitempty"`
	ContractAmount *string `json:"contract_amount,omitempty"`
	StartDate      *string `json:"start_date,omitempty"`
	EndDate        *string `json:"end_date,omitempty"`
	Status         *string `json:"status,omitempty"`
	Address        *string `json:"address,omitempty"`
	Manager        *string `json:"manager,omitempty"`
	Remark         *string `json:"remark,omitempty"`
}

// ProjectHandler groups all project Gin handlers.
type ProjectHandler struct {
	create  *appproject.CreateUseCase
	get     *appproject.GetByIDUseCase
	list    *appproject.ListUseCase
	update  *appproject.UpdateUseCase
	delete  *appproject.DeleteUseCase
	restore *appproject.RestoreUseCase
}

// NewProjectHandler constructs a ProjectHandler wired to the provided use cases.
func NewProjectHandler(
	create *appproject.CreateUseCase,
	get *appproject.GetByIDUseCase,
	list *appproject.ListUseCase,
	update *appproject.UpdateUseCase,
	del *appproject.DeleteUseCase,
	restore *appproject.RestoreUseCase,
) *ProjectHandler {
	return &ProjectHandler{
		create:  create,
		get:     get,
		list:    list,
		update:  update,
		delete:  del,
		restore: restore,
	}
}

// RegisterRoutes registers all project routes on the provided router group.
func (h *ProjectHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/projects", h.List)
	rg.POST("/projects", h.Create)
	rg.GET("/projects/:id", h.GetByID)
	rg.PUT("/projects/:id", h.Update)
	rg.DELETE("/projects/:id", h.Delete)
	rg.POST("/projects/:id/restore", h.Restore)
}

// List handles GET /api/v1/projects
func (h *ProjectHandler) List(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	f := domain.ListFilter{
		TenantID: tenantID,
		Query:    c.Query("q"),
	}
	if s := c.Query("status"); s != "" {
		st := domain.ProjectStatus(s)
		f.Status = &st
	}
	if cid := c.Query("customer_id"); cid != "" {
		parsed, err := uuid.Parse(cid)
		if err == nil {
			f.CustomerID = &parsed
		}
	}
	f.Limit = queryInt(c, "limit", 20)
	f.Offset = queryInt(c, "offset", 0)

	items, total, err := h.list.Execute(c.Request.Context(), f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	dtos := make([]*ProjectDTO, 0, len(items))
	for _, p := range items {
		dtos = append(dtos, toDTO(p))
	}
	c.JSON(http.StatusOK, ProjectListResponse{Items: dtos, Total: total})
}

// Create handles POST /api/v1/projects
func (h *ProjectHandler) Create(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	var req createRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var customerID *uuid.UUID
	if req.CustomerID != nil {
		id, err := uuid.Parse(*req.CustomerID)
		if err == nil {
			customerID = &id
		}
	}

	startDate, err := parseOptionalDate(req.StartDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid start_date: " + err.Error()})
		return
	}
	endDate, err := parseOptionalDate(req.EndDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid end_date: " + err.Error()})
		return
	}

	in := domain.CreateInput{
		TenantID:       tenantID,
		Code:           req.Code,
		Name:           req.Name,
		CustomerID:     customerID,
		ContractAmount: req.ContractAmount,
		StartDate:      startDate,
		EndDate:        endDate,
		Status:         domain.ProjectStatus(req.Status),
		Address:        req.Address,
		Manager:        req.Manager,
		Remark:         req.Remark,
	}

	p, err := h.create.Execute(c.Request.Context(), in)
	if err != nil {
		if errors.Is(err, appproject.ErrDuplicateCode) {
			c.JSON(http.StatusConflict, gin.H{"error": "duplicate code"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.Header("Location", fmt.Sprintf("/api/v1/projects/%s", p.ID))
	c.JSON(http.StatusCreated, toDTO(p))
}

// GetByID handles GET /api/v1/projects/:id
func (h *ProjectHandler) GetByID(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	p, err := h.get.Execute(c.Request.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, appproject.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toDTO(p))
}

// Update handles PUT /api/v1/projects/:id
func (h *ProjectHandler) Update(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
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

	var statusPtr *domain.ProjectStatus
	if req.Status != nil {
		st := domain.ProjectStatus(*req.Status)
		statusPtr = &st
	}

	var customerID *uuid.UUID
	if req.CustomerID != nil {
		parsed, parseErr := uuid.Parse(*req.CustomerID)
		if parseErr == nil {
			customerID = &parsed
		}
	}

	startDate, err := parseOptionalDate(req.StartDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid start_date: " + err.Error()})
		return
	}
	endDate, err := parseOptionalDate(req.EndDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid end_date: " + err.Error()})
		return
	}

	in := domain.UpdateInput{
		Code:           req.Code,
		Name:           req.Name,
		CustomerID:     customerID,
		ContractAmount: req.ContractAmount,
		StartDate:      startDate,
		EndDate:        endDate,
		Status:         statusPtr,
		Address:        req.Address,
		Manager:        req.Manager,
		Remark:         req.Remark,
	}

	p, err := h.update.Execute(c.Request.Context(), tenantID, id, in)
	if err != nil {
		if errors.Is(err, appproject.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toDTO(p))
}

// Delete handles DELETE /api/v1/projects/:id
func (h *ProjectHandler) Delete(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	if err := h.delete.Execute(c.Request.Context(), tenantID, id); err != nil {
		if errors.Is(err, appproject.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// Restore handles POST /api/v1/projects/:id/restore
func (h *ProjectHandler) Restore(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	p, err := h.restore.Execute(c.Request.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, appproject.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toDTO(p))
}

// toDTO converts a domain.Project to a ProjectDTO for wire serialization.
func toDTO(p *domain.Project) *ProjectDTO {
	dto := &ProjectDTO{
		ID:             p.ID.String(),
		TenantID:       p.TenantID.String(),
		Code:           p.Code,
		Name:           p.Name,
		ContractAmount: p.ContractAmount,
		Status:         string(p.Status),
		Address:        p.Address,
		Manager:        p.Manager,
		Remark:         p.Remark,
		CreatedAt:      p.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      p.UpdatedAt.Format(time.RFC3339),
	}
	if p.CustomerID != nil {
		s := p.CustomerID.String()
		dto.CustomerID = &s
	}
	if p.StartDate != nil {
		s := p.StartDate.Format(dateLayout)
		dto.StartDate = &s
	}
	if p.EndDate != nil {
		s := p.EndDate.Format(dateLayout)
		dto.EndDate = &s
	}
	return dto
}

// parseOptionalDate parses a *string date in "2006-01-02" format into *time.Time.
func parseOptionalDate(s *string) (*time.Time, error) {
	if s == nil || *s == "" {
		return nil, nil
	}
	t, err := time.Parse(dateLayout, *s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// queryInt extracts an integer query parameter with a default fallback.
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
