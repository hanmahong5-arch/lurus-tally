package bill

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httperr"
)

// WithReceivables enables GET /receivables. Without it the route answers 501.
func (h *Handler) WithReceivables(uc *appbill.ReceivablesUseCase) *Handler {
	h.receivables = uc
	return h
}

type receivableRow struct {
	PartnerID      *uuid.UUID `json:"partner_id"`
	PartnerName    string     `json:"partner_name"`
	Unpaid         string     `json:"unpaid"`
	B0to30         string     `json:"b0_30"`
	B31to60        string     `json:"b31_60"`
	B61to90        string     `json:"b61_90"`
	B90Plus        string     `json:"b90p"`
	OldestBillDate string     `json:"oldest_bill_date"`
}

func newReceivableRow(r appbill.PartnerReceivable) receivableRow {
	s := func(d decimal.Decimal) string { return d.StringFixed(2) }
	row := receivableRow{
		PartnerID: r.PartnerID, PartnerName: r.PartnerName,
		Unpaid: s(r.Unpaid), B0to30: s(r.B0to30), B31to60: s(r.B31to60), B61to90: s(r.B61to90), B90Plus: s(r.B90Plus),
	}
	if r.OldestBillDate != nil {
		row.OldestBillDate = r.OldestBillDate.In(listLocation).Format("2006-01-02")
	}
	return row
}

// Receivables handles GET /api/v1/receivables?direction=ar|ap&as_of=YYYY-MM-DD
//
// Open balance per partner over approved bills, split by bill age (0-30 / 31-60 / 61-90 /
// over 90 days), largest first, plus a grand total. Walk-in bills with no partner come
// back as one row with a null partner_id.
func (h *Handler) Receivables(c *gin.Context) {
	tenantID := resolveTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, errResp("unauthorized", "tenant_id required", ""))
		return
	}
	if h.receivables == nil {
		c.JSON(http.StatusNotImplemented, errResp("not_implemented", "欠款汇总暂未开通", ""))
		return
	}
	direction := c.DefaultQuery("direction", appbill.ReceivableAR)
	var asOf *time.Time
	if s := c.Query("as_of"); s != "" {
		t, err := time.ParseInLocation("2006-01-02", s, listLocation)
		if err != nil {
			c.JSON(http.StatusBadRequest, errResp("validation_error", "截止日期格式不正确，应为 年-月-日", ""))
			return
		}
		asOf = &t
	}

	out, err := h.receivables.Execute(c.Request.Context(), tenantID, direction, asOf)
	if err != nil {
		if errors.Is(err, appbill.ErrInvalidReceivableDirection) {
			c.JSON(http.StatusBadRequest, errResp("validation_error", "direction 只能是 ar（应收）或 ap（应付）", ""))
			return
		}
		httperr.WriteInternal(c, err)
		return
	}
	items := make([]receivableRow, len(out.Items))
	for i, r := range out.Items {
		items[i] = newReceivableRow(r)
		if r.PartnerID == nil && items[i].PartnerName == "" {
			items[i].PartnerName = "未指定往来单位"
		}
	}
	total := newReceivableRow(out.Total)
	total.PartnerName = "合计"
	c.JSON(http.StatusOK, gin.H{
		"direction": out.Direction,
		"as_of":     out.AsOf.Format("2006-01-02"),
		"items":     items,
		"total":     total,
	})
}
