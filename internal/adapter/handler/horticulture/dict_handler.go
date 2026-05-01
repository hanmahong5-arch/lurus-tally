// Package horticulture implements the Gin HTTP handlers for the nursery dictionary REST API.
package horticulture

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	apphort "github.com/hanmahong5-arch/lurus-tally/internal/app/horticulture"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/horticulture"
)

// NurseryDictDTO is the wire representation of a nursery dict entry.
type NurseryDictDTO struct {
	ID            string          `json:"id"`
	TenantID      string          `json:"tenant_id"`
	Name          string          `json:"name"`
	LatinName     string          `json:"latin_name"`
	Family        string          `json:"family"`
	Genus         string          `json:"genus"`
	Type          string          `json:"type"`
	IsEvergreen   bool            `json:"is_evergreen"`
	ClimateZones  []string        `json:"climate_zones"`
	BestSeason    [2]int          `json:"best_season"`
	SpecTemplate  json.RawMessage `json:"spec_template"`
	DefaultUnitID *string         `json:"default_unit_id,omitempty"`
	PhotoURL      string          `json:"photo_url"`
	Remark        string          `json:"remark"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// ListResponse wraps the pagination envelope.
type ListResponse struct {
	Items []*NurseryDictDTO `json:"items"`
	Total int               `json:"total"`
}

// createRequest is the JSON body for POST /api/v1/nursery-dict.
type createRequest struct {
	Name          string          `json:"name"`
	LatinName     string          `json:"latin_name"`
	Family        string          `json:"family"`
	Genus         string          `json:"genus"`
	Type          string          `json:"type"`
	IsEvergreen   bool            `json:"is_evergreen"`
	ClimateZones  []string        `json:"climate_zones"`
	BestSeason    [2]int          `json:"best_season"`
	SpecTemplate  json.RawMessage `json:"spec_template"`
	DefaultUnitID *string         `json:"default_unit_id,omitempty"`
	PhotoURL      string          `json:"photo_url"`
	Remark        string          `json:"remark"`
}

// updateRequest is the JSON body for PUT /api/v1/nursery-dict/:id.
type updateRequest struct {
	Name          *string         `json:"name"`
	LatinName     *string         `json:"latin_name"`
	Family        *string         `json:"family"`
	Genus         *string         `json:"genus"`
	Type          *string         `json:"type"`
	IsEvergreen   *bool           `json:"is_evergreen"`
	ClimateZones  []string        `json:"climate_zones"`
	BestSeason    *[2]int         `json:"best_season"`
	SpecTemplate  json.RawMessage `json:"spec_template"`
	DefaultUnitID *string         `json:"default_unit_id"`
	PhotoURL      *string         `json:"photo_url"`
	Remark        *string         `json:"remark"`
}

// DictHandler groups all nursery dict Gin handlers.
type DictHandler struct {
	create  *apphort.CreateUseCase
	get     *apphort.GetByIDUseCase
	list    *apphort.ListUseCase
	update  *apphort.UpdateUseCase
	delete  *apphort.DeleteUseCase
	restore *apphort.RestoreUseCase
}

// NewDictHandler constructs a DictHandler wired to the provided use cases.
func NewDictHandler(
	create *apphort.CreateUseCase,
	get *apphort.GetByIDUseCase,
	list *apphort.ListUseCase,
	update *apphort.UpdateUseCase,
	del *apphort.DeleteUseCase,
	restore *apphort.RestoreUseCase,
) *DictHandler {
	return &DictHandler{
		create:  create,
		get:     get,
		list:    list,
		update:  update,
		delete:  del,
		restore: restore,
	}
}

// RegisterRoutes registers all nursery dict routes on the provided router group.
func (h *DictHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/nursery-dict", h.List)
	rg.POST("/nursery-dict", h.Create)
	rg.GET("/nursery-dict/:id", h.GetByID)
	rg.PUT("/nursery-dict/:id", h.Update)
	rg.DELETE("/nursery-dict/:id", h.Delete)
	rg.POST("/nursery-dict/:id/restore", h.Restore)
}

// List handles GET /api/v1/nursery-dict
func (h *DictHandler) List(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	f := domain.ListFilter{
		TenantID: tenantID,
		Query:    c.Query("q"),
	}
	if t := c.Query("type"); t != "" {
		nt := domain.NurseryType(t)
		f.Type = &nt
	}
	if ie := c.Query("is_evergreen"); ie != "" {
		v, err := strconv.ParseBool(ie)
		if err == nil {
			f.IsEvergreen = &v
		}
	}
	f.Limit = queryInt(c, "limit", 20)
	f.Offset = queryInt(c, "offset", 0)

	items, total, err := h.list.Execute(c.Request.Context(), f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	dtos := make([]*NurseryDictDTO, 0, len(items))
	for _, d := range items {
		dtos = append(dtos, toDTO(d))
	}
	c.JSON(http.StatusOK, ListResponse{Items: dtos, Total: total})
}

// Create handles POST /api/v1/nursery-dict
func (h *DictHandler) Create(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	var req createRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	nt := domain.NurseryType(req.Type)
	if req.Type == "" {
		nt = domain.NurseryTypeTree
	}

	var defaultUnitID *uuid.UUID
	if req.DefaultUnitID != nil {
		id, err := uuid.Parse(*req.DefaultUnitID)
		if err == nil {
			defaultUnitID = &id
		}
	}

	in := domain.CreateInput{
		TenantID:      tenantID,
		Name:          req.Name,
		LatinName:     req.LatinName,
		Family:        req.Family,
		Genus:         req.Genus,
		Type:          nt,
		IsEvergreen:   req.IsEvergreen,
		ClimateZones:  req.ClimateZones,
		BestSeason:    req.BestSeason,
		SpecTemplate:  req.SpecTemplate,
		DefaultUnitID: defaultUnitID,
		PhotoURL:      req.PhotoURL,
		Remark:        req.Remark,
	}

	d, err := h.create.Execute(c.Request.Context(), in)
	if err != nil {
		if errors.Is(err, apphort.ErrDuplicateName) {
			c.JSON(http.StatusConflict, gin.H{"error": "duplicate name"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.Header("Location", fmt.Sprintf("/api/v1/nursery-dict/%s", d.ID))
	c.JSON(http.StatusCreated, toDTO(d))
}

// GetByID handles GET /api/v1/nursery-dict/:id
func (h *DictHandler) GetByID(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	d, err := h.get.Execute(c.Request.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, apphort.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toDTO(d))
}

// Update handles PUT /api/v1/nursery-dict/:id
func (h *DictHandler) Update(c *gin.Context) {
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

	var ntPtr *domain.NurseryType
	if req.Type != nil {
		nt := domain.NurseryType(*req.Type)
		ntPtr = &nt
	}
	var defaultUnitID *uuid.UUID
	if req.DefaultUnitID != nil {
		parsed, parseErr := uuid.Parse(*req.DefaultUnitID)
		if parseErr == nil {
			defaultUnitID = &parsed
		}
	}

	in := domain.UpdateInput{
		Name:          req.Name,
		LatinName:     req.LatinName,
		Family:        req.Family,
		Genus:         req.Genus,
		Type:          ntPtr,
		IsEvergreen:   req.IsEvergreen,
		ClimateZones:  req.ClimateZones,
		BestSeason:    req.BestSeason,
		SpecTemplate:  req.SpecTemplate,
		DefaultUnitID: defaultUnitID,
		PhotoURL:      req.PhotoURL,
		Remark:        req.Remark,
	}

	d, err := h.update.Execute(c.Request.Context(), tenantID, id, in)
	if err != nil {
		if errors.Is(err, apphort.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toDTO(d))
}

// Delete handles DELETE /api/v1/nursery-dict/:id
func (h *DictHandler) Delete(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	if err := h.delete.Execute(c.Request.Context(), tenantID, id); err != nil {
		if errors.Is(err, apphort.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// Restore handles POST /api/v1/nursery-dict/:id/restore
func (h *DictHandler) Restore(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	d, err := h.restore.Execute(c.Request.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, apphort.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toDTO(d))
}

// toDTO converts a domain.NurseryDict to a NurseryDictDTO for wire serialization.
func toDTO(d *domain.NurseryDict) *NurseryDictDTO {
	spec := d.SpecTemplate
	if len(spec) == 0 {
		spec = json.RawMessage("{}")
	}
	zones := d.ClimateZones
	if zones == nil {
		zones = []string{}
	}
	dto := &NurseryDictDTO{
		ID:           d.ID.String(),
		TenantID:     d.TenantID.String(),
		Name:         d.Name,
		LatinName:    d.LatinName,
		Family:       d.Family,
		Genus:        d.Genus,
		Type:         string(d.Type),
		IsEvergreen:  d.IsEvergreen,
		ClimateZones: zones,
		BestSeason:   d.BestSeason,
		SpecTemplate: spec,
		PhotoURL:     d.PhotoURL,
		Remark:       d.Remark,
		CreatedAt:    d.CreatedAt,
		UpdatedAt:    d.UpdatedAt,
	}
	if d.DefaultUnitID != nil {
		s := d.DefaultUnitID.String()
		dto.DefaultUnitID = &s
	}
	return dto
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
