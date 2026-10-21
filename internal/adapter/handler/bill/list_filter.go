package bill

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// maxKeywordRunes caps the free-text search term (q).
const maxKeywordRunes = 64

// listLocation interprets date-only filters as Asia/Shanghai wall-clock days.
// A fixed zone avoids LoadLocation, which needs tzdata that scratch images lack.
var listLocation = time.FixedZone("CST", 8*3600)

// parseListFilter fills the filter fields shared by the purchase and sale list
// endpoints from query: status, partner_id, date_from, date_to, payment_status, q, mine.
// creatorID is the current user (uuid.Nil when unresolved) and is only needed for mine=1.
// Any invalid value yields an error whose text is a user-facing Chinese message.
func parseListFilter(query url.Values, creatorID uuid.UUID, f *appbill.BillListFilter) error {
	if s := query.Get("status"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			return errors.New("单据状态不正确")
		}
		st := domain.BillStatus(n)
		switch st {
		case domain.StatusDraft, domain.StatusApproved, domain.StatusCancelled:
		default:
			return errors.New("单据状态不正确")
		}
		f.Status = &st
	}
	if pid := query.Get("partner_id"); pid != "" {
		id, err := uuid.Parse(pid)
		if err != nil {
			return errors.New("往来单位不正确")
		}
		f.PartnerID = &id
	}

	var from, to *time.Time
	if s := query.Get("date_from"); s != "" {
		t, err := time.ParseInLocation("2006-01-02", s, listLocation)
		if err != nil {
			return errors.New("开始日期格式不正确，应为 年-月-日")
		}
		from = &t
	}
	if s := query.Get("date_to"); s != "" {
		t, err := time.ParseInLocation("2006-01-02", s, listLocation)
		if err != nil {
			return errors.New("结束日期格式不正确，应为 年-月-日")
		}
		// Inclusive last day → exclusive start of the next day.
		next := t.AddDate(0, 0, 1)
		to = &next
	}
	if from != nil && to != nil && !from.Before(*to) {
		return errors.New("开始日期不能晚于结束日期")
	}
	f.DateFrom, f.DateTo = from, to

	if ps := query.Get("payment_status"); ps != "" {
		switch ps {
		case domain.PaymentUnpaid, domain.PaymentPartial, domain.PaymentPaid, "open":
		default:
			return errors.New("收付款状态不正确")
		}
		if f.Status != nil && *f.Status != domain.StatusApproved {
			return errors.New("按收付款状态筛选时只能查看已审核的单据")
		}
		f.PaymentStatus = ps
	}

	if is := query.Get("invoice_status"); is != "" {
		switch is {
		case domain.InvoiceNone, domain.InvoicePartial, domain.InvoiceFull:
		default:
			return errors.New("开票状态不正确")
		}
		if f.Status != nil && *f.Status != domain.StatusApproved {
			return errors.New("按开票状态筛选时只能查看已审核的单据")
		}
		f.InvoiceStatus = is
	}

	if q := strings.TrimSpace(query.Get("q")); q != "" {
		if utf8.RuneCountInString(q) > maxKeywordRunes {
			return errors.New("搜索关键字太长，最多 64 个字")
		}
		f.Keyword = q
	}

	switch query.Get("sort") {
	case "", "bill_date_desc":
	case "bill_date_asc":
		f.OldestFirst = true
	default:
		return errors.New("排序方式不正确")
	}

	if m := query.Get("mine"); m != "" && m != "0" {
		if m != "1" {
			return errors.New("“只看我的”参数不正确")
		}
		if creatorID == uuid.Nil {
			return errors.New("无法识别当前用户，不能只看我的单据")
		}
		f.CreatorID = &creatorID
	}
	return nil
}

// listItem is one row of the purchase/sale list response: the bill head plus derived
// settlement fields. partner_name rides on the embedded head.
type listItem struct {
	domain.BillHead
	// PartnerName shadows the embedded omitempty field so the key is always present.
	PartnerName   string `json:"partner_name"`
	PaymentStatus string `json:"payment_status"`
	UnpaidAmount  string `json:"unpaid_amount"`
	// InvoicedAmount / InvoiceStatus (none/partial/full) come from live invoice links.
	InvoicedAmount string `json:"invoiced_amount"`
	InvoiceStatus  string `json:"invoice_status"`
}

// unpaid_amount is net of sales returns (zero returned on purchases). saleListItem
// additionally keeps the sale-side receivable_amount (clamped at zero).
type saleListItem struct {
	listItem
	ReceivableAmount string `json:"receivable_amount"`
}

func newListItem(b domain.BillHead) listItem {
	return listItem{
		BillHead:       b,
		PartnerName:    b.PartnerName,
		PaymentStatus:  b.PaymentStatus(),
		UnpaidAmount:   decimal.Max(b.TotalAmount.Sub(b.ReturnedAmount), decimal.Zero).Sub(b.PaidAmount).String(),
		InvoicedAmount: b.InvoicedAmount.String(),
		InvoiceStatus:  b.InvoiceStatus(),
	}
}

// listSummaryJSON renders the aggregate block of the list response.
func listSummaryJSON(s appbill.BillListSummary) gin.H {
	return gin.H{
		"count":           s.Count,
		"total_amount":    s.TotalAmount.String(),
		"paid_amount":     s.PaidAmount.String(),
		"returned_amount": s.ReturnedAmount.String(),
		"unpaid_amount":   s.UnpaidAmount.String(),
	}
}
