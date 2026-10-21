// Package reports implements Gin HTTP handlers for the analytics reports REST API.
package reports

import (
	"encoding/csv"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	apprep "github.com/hanmahong5-arch/lurus-tally/internal/app/report"
	appreports "github.com/hanmahong5-arch/lurus-tally/internal/app/reports"
	"github.com/hanmahong5-arch/lurus-tally/internal/domain/permission"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

// Handler groups all analytics report REST handlers.
type Handler struct {
	uc *appreports.UseCase
}

// New constructs the Handler. uc must be non-nil.
func New(uc *appreports.UseCase) *Handler {
	return &Handler{uc: uc}
}

// RegisterRoutes mounts the read-only analytics routes onto rg.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/reports/gross-margin", h.GrossMargin)
	rg.GET("/reports/abc", h.ABCClassify)
	rg.GET("/reports/dead-stock", h.DeadStock)
	rg.GET("/reports/sales-top", h.SalesTop)
	rg.GET("/reports/monthly", h.Monthly)
	rg.GET("/reports/monthly.csv", h.MonthlyCSV)
}

// GrossMargin handles GET /api/v1/reports/gross-margin?days=30
func (h *Handler) GrossMargin(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id required"})
		return
	}
	if !middleware.CanViewCost(c) {
		middleware.WriteForbidden(c, permission.CostView)
		return
	}
	days := middleware.ParseLimitQuery(c, "days", 30, 365)
	result, err := h.uc.GrossMarginSummary(c.Request.Context(), tenantID, days)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ABCClassify handles GET /api/v1/reports/abc
func (h *Handler) ABCClassify(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id required"})
		return
	}
	result, err := h.uc.ABCClassify(c.Request.Context(), tenantID)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeadStock handles GET /api/v1/reports/dead-stock?days=90
func (h *Handler) DeadStock(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id required"})
		return
	}
	days := middleware.ParseLimitQuery(c, "days", 90, 365)
	result, err := h.uc.DeadStock(c.Request.Context(), tenantID, days)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// SalesTop handles GET /api/v1/reports/sales-top?metric=revenue&days=7&limit=10
func (h *Handler) SalesTop(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id required"})
		return
	}
	metric := c.DefaultQuery("metric", "revenue")
	switch metric {
	case "revenue", "margin", "qty":
		// valid
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "metric must be revenue|margin|qty"})
		return
	}
	if metric == "margin" && !middleware.CanViewCost(c) {
		middleware.WriteForbidden(c, permission.CostView)
		return
	}
	days := middleware.ParseLimitQuery(c, "days", 7, 365)
	limit := middleware.ParseLimitQuery(c, "limit", 10, 100)

	result, err := h.uc.SalesTop(c.Request.Context(), tenantID, metric, days, limit)
	if err != nil {
		httperr.WriteInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// Monthly handles GET /api/v1/reports/monthly?month=YYYY-MM (default: current month, Beijing time).
func (h *Handler) Monthly(c *gin.Context) {
	m, ok := h.monthly(c)
	if !ok {
		return
	}
	middleware.JSONTrimmed(c, http.StatusOK, m)
}

// MonthlyCSV handles GET /api/v1/reports/monthly.csv?month=YYYY-MM: the same figures as a
// two-column sheet the owner can hand to the accountant.
func (h *Handler) MonthlyCSV(c *gin.Context) {
	m, ok := h.monthly(c)
	if !ok {
		return
	}
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="tally-monthly-`+m.Month+`.csv"`)
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
	_, _ = c.Writer.Write([]byte{0xEF, 0xBB, 0xBF})
	cw := csv.NewWriter(c.Writer)
	rows := [][]string{
		{"项目", "金额（元）"},
		{"月份", m.Month},
		{"净销售额", m.NetSales},
	}
	if middleware.CanViewCost(c) {
		rows = append(rows, []string{"销售成本", m.CostOfGoods}, []string{"毛利", m.GrossProfit})
	}
	rows = append(rows,
		[]string{"采购净额", m.NetPurchases},
		[]string{"本月收款", m.Receipts},
		[]string{"本月付款", m.Payments},
		[]string{"月底应收", m.Receivable},
		[]string{"月底应付", m.Payable},
	)
	if middleware.CanViewCost(c) {
		rows = append(rows, []string{"当前库存金额（导出时）", m.InventoryValue})
	}
	for _, row := range rows {
		_ = cw.Write(row)
	}
	cw.Flush()
}

// monthly resolves the tenant and month, runs the report and writes the error response
// itself; ok is false when a response has already been written.
func (h *Handler) monthly(c *gin.Context) (*apprep.Monthly, bool) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id required"})
		return nil, false
	}
	m, err := h.uc.Monthly(c.Request.Context(), tenantID, c.Query("month"))
	switch {
	case errors.Is(err, apprep.ErrBadMonth), errors.Is(err, apprep.ErrFutureMonth):
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error", "message": err.Error()})
		return nil, false
	case err != nil:
		httperr.WriteInternal(c, err)
		return nil, false
	}
	return m, true
}

// resolveTenantID returns the tenant UUID set by AuthMiddleware (uuid.Nil → 401).
func resolveTenantID(c *gin.Context) uuid.UUID {
	return middleware.GetTenantID(c)
}
