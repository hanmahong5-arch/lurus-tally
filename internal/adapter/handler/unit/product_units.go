package unit

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	repounit "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/unit"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

// ProductUnitLister lists the units a product can be billed in.
type ProductUnitLister interface {
	ProductUnits(ctx context.Context, tenantID, productID uuid.UUID) ([]repounit.ProductUnit, error)
}

// WithProductUnits enables GET /products/:id/units. Without it the route answers 501.
func (h *Handler) WithProductUnits(l ProductUnitLister) *Handler {
	h.productUnits = l
	return h
}

// ProductUnits handles GET /api/v1/products/:id/units.
//
// Returns the product's base unit plus every unit it has a conversion factor for, so a
// bill line can only pick units the stock calculation can convert.
func (h *Handler) ProductUnits(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id required", "message": "请先登录"})
		return
	}
	if h.productUnits == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "product units not configured", "message": "商品单位功能暂未开通"})
		return
	}
	productID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid product id: must be a UUID", "message": "商品不正确"})
		return
	}
	units, err := h.productUnits.ProductUnits(c.Request.Context(), tenantID, productID)
	if err != nil {
		if errors.Is(err, repounit.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "product not found", "message": "找不到这个商品"})
			return
		}
		httperr.WriteInternal(c, err)
		return
	}
	items := make([]gin.H, len(units))
	for i, u := range units {
		items[i] = gin.H{
			"id":                u.ID,
			"code":              u.Code,
			"name":              u.Name,
			"conversion_factor": u.Factor.String(),
			"is_base":           u.IsBase,
		}
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
