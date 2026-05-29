// Package shopify implements Gin HTTP handlers for the Shopify shop-binding
// management API. Routes are registered under the authenticated /api/v1 group.
package shopify

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appshopify "github.com/hanmahong5-arch/lurus-tally/internal/app/shopify"
)

// --- application-layer interfaces ------------------------------------------

// Binder is the narrow interface for the bind use case.
type Binder interface {
	Execute(ctx context.Context, in appshopify.BindInput) (*appshopify.ShopMapping, error)
}

// Lister is the narrow interface for the list use case.
type Lister interface {
	Execute(ctx context.Context, tenantID uuid.UUID) ([]appshopify.ShopMapping, error)
}

// Unbinder is the narrow interface for the unbind use case.
type Unbinder interface {
	Execute(ctx context.Context, tenantID, id uuid.UUID) error
}

// --- wire representation ---------------------------------------------------

// shopDTO is the JSON envelope returned by GET /shopify/shops and POST /shopify/shops.
type shopDTO struct {
	ID          string `json:"id"`
	ShopDomain  string `json:"shop_domain"`
	WarehouseID string `json:"warehouse_id"`
	CreatorID   string `json:"creator_id"`
}

// bindRequest is the JSON body for POST /shopify/shops.
type bindRequest struct {
	ShopDomain  string `json:"shop_domain"  binding:"required"`
	WarehouseID string `json:"warehouse_id" binding:"required"`
}

// shopListResponse wraps the list envelope.
type shopListResponse struct {
	Items []shopDTO `json:"items"`
}

// --- handler ---------------------------------------------------------------

// Handler groups all Shopify shop-binding routes.
type Handler struct {
	bind   Binder
	list   Lister
	unbind Unbinder
}

// New constructs a Handler wired to the provided use cases.
func New(bind Binder, list Lister, unbind Unbinder) *Handler {
	return &Handler{bind: bind, list: list, unbind: unbind}
}

// RegisterRoutes mounts the three shop-binding routes onto rg.
// Expected prefix: /api/v1 (from router.go in main line).
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/shopify/shops", h.Bind)
	rg.GET("/shopify/shops", h.List)
	rg.DELETE("/shopify/shops/:id", h.Unbind)
}

// Bind handles POST /api/v1/shopify/shops.
//
// Request body: { "shop_domain": "...", "warehouse_id": "..." }
// Responses:
//
//	201 Created — binding persisted, body contains the new shopDTO
//	401 — tenant not identified
//	409 — shop domain already bound to another account
//	422 — invalid domain format or warehouse belongs to another tenant
func (h *Handler) Bind(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant not identified"})
		return
	}

	// actor: the Zitadel sub is resolved to a UUID by the main-line wire
	// (user_identity_mapping lookup). We reuse the tenant UUID as creator_id
	// fallback so that the record is always non-null, even in test environments
	// where the sub → UUID lookup is not yet wired.
	creatorID := tenantID
	if sub := middleware.GetZitadelSub(c); sub != "" {
		if parsed, err := uuid.Parse(sub); err == nil {
			creatorID = parsed
		}
	}

	var req bindRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	warehouseID, err := uuid.Parse(req.WarehouseID)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid warehouse_id"})
		return
	}

	m, err := h.bind.Execute(c.Request.Context(), appshopify.BindInput{
		TenantID:    tenantID,
		ShopDomain:  req.ShopDomain,
		WarehouseID: warehouseID,
		CreatorID:   creatorID,
	})
	if err != nil {
		switch {
		case errors.Is(err, appshopify.ErrInvalidDomain):
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		case errors.Is(err, appshopify.ErrWarehouseNotOwned):
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		case errors.Is(err, appshopify.ErrShopAlreadyBound):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		}
		return
	}

	c.JSON(http.StatusCreated, toDTO(*m))
}

// List handles GET /api/v1/shopify/shops.
//
// Returns all shop bindings for the authenticated tenant.
func (h *Handler) List(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant not identified"})
		return
	}

	items, err := h.list.Execute(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	dtos := make([]shopDTO, 0, len(items))
	for _, m := range items {
		dtos = append(dtos, toDTO(m))
	}
	c.JSON(http.StatusOK, shopListResponse{Items: dtos})
}

// Unbind handles DELETE /api/v1/shopify/shops/:id.
//
// Removes the shop binding. The operation is idempotent.
func (h *Handler) Unbind(c *gin.Context) {
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

	if err := h.unbind.Execute(c.Request.Context(), tenantID, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.Status(http.StatusNoContent)
}

// --- helpers ---------------------------------------------------------------

func toDTO(m appshopify.ShopMapping) shopDTO {
	return shopDTO{
		ID:          m.ID.String(),
		ShopDomain:  m.ShopDomain,
		WarehouseID: m.WarehouseID.String(),
		CreatorID:   m.CreatorID.String(),
	}
}
