package bill

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	"github.com/hanmahong5-arch/lurus-tally/internal/domain/permission"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

// WithLastPrices enables GET /last-prices. Without it the route answers 501.
func (h *Handler) WithLastPrices(uc *appbill.LastPricesUseCase) *Handler {
	h.lastPrices = uc
	return h
}

// LastPrices handles GET /api/v1/last-prices?partner_id=&product_ids=a,b&type=sale|purchase
//
// Returns, per product this partner has an approved bill for, the unit price and date
// of the latest one, quoted in the unit that line used (base_unit_price is per base
// unit). Products with no history fall back to the default price (source "default");
// without a default price they are left out.
func (h *Handler) LastPrices(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", "tenant_id required", ""))
		return
	}
	if h.lastPrices == nil {
		c.JSON(http.StatusNotImplemented, errResp("not_implemented", "上次成交价暂未开通", ""))
		return
	}
	var partnerID uuid.UUID
	if s := c.Query("partner_id"); s != "" {
		id, err := uuid.Parse(s)
		if err != nil {
			c.JSON(http.StatusBadRequest, errResp("validation_error", "往来单位不正确", ""))
			return
		}
		partnerID = id
	}
	var productIDs []uuid.UUID
	for _, s := range strings.Split(c.Query("product_ids"), ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		id, err := uuid.Parse(s)
		if err != nil {
			c.JSON(http.StatusBadRequest, errResp("validation_error", "商品不正确", ""))
			return
		}
		productIDs = append(productIDs, id)
	}
	if len(productIDs) > appbill.MaxLastPriceProducts {
		c.JSON(http.StatusBadRequest, errResp("validation_error", "一次最多查询 200 个商品", ""))
		return
	}

	priceType := c.DefaultQuery("type", appbill.LastPriceSale)
	if priceType == appbill.LastPricePurchase && !middleware.CanViewCost(c) {
		middleware.WriteForbidden(c, permission.CostView)
		return
	}
	rows, err := h.lastPrices.Execute(c.Request.Context(), tenantID, partnerID, priceType, productIDs)
	if err != nil {
		if errors.Is(err, appbill.ErrInvalidLastPriceType) {
			c.JSON(http.StatusBadRequest, errResp("validation_error", "type 只能是 sale（销售）或 purchase（采购）", ""))
			return
		}
		httperr.WriteInternal(c, err)
		return
	}
	items := make([]gin.H, len(rows))
	for i, r := range rows {
		item := gin.H{
			"product_id":      r.ProductID,
			"unit_price":      priceString(r.UnitPrice),
			"unit_name":       r.UnitName,
			"base_unit_price": priceString(r.BasePrice().Round(6)),
			"factor":          r.Factor.String(),
			"source":          r.Source,
			"bill_date":       "",
		}
		if r.UnitID != uuid.Nil {
			item["unit_id"] = r.UnitID
		}
		if r.Source == appbill.LastPriceSourceLast {
			item["bill_date"] = r.BillDate.In(listLocation).Format("2006-01-02")
		}
		items[i] = item
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// priceString keeps two decimals, widening only when the stored price has more.
func priceString(d decimal.Decimal) string {
	s := d.StringFixed(2)
	if p, err := decimal.NewFromString(s); err == nil && p.Equal(d) {
		return s
	}
	return d.String()
}
