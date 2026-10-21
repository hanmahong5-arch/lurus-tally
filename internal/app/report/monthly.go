// Package report builds the month-end reconciliation figures shown on the reports page.
// The pure rules (month window, net figures) live here; the SQL sits in the reports repo.
package report

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// businessTZ is the zone months are cut in (Beijing, no DST).
var businessTZ = time.FixedZone("Asia/Shanghai", 8*3600)

// ErrBadMonth is returned for a month that is not YYYY-MM or is outside the supported range.
var ErrBadMonth = errors.New("月份格式不正确，请用 年-月，例如 2026-09")

// ErrFutureMonth is returned for a well-formed month that has not started yet.
var ErrFutureMonth = errors.New("只能查看本月及以前的月份")

// ParseMonth resolves month (YYYY-MM, "" = the current Beijing month) to the half-open
// window [from, to) in Beijing midnight, plus the normalised label.
func ParseMonth(month string, now time.Time) (from, to time.Time, label string, err error) {
	cur := now.In(businessTZ)
	if month == "" {
		month = cur.Format("2006-01")
	}
	t, perr := time.ParseInLocation("2006-01", month, businessTZ)
	if perr != nil || t.Format("2006-01") != month {
		return time.Time{}, time.Time{}, "", ErrBadMonth
	}
	if t.Year() < 2000 {
		return time.Time{}, time.Time{}, "", ErrBadMonth
	}
	if t.After(cur) {
		return time.Time{}, time.Time{}, "", ErrFutureMonth
	}
	return t, t.AddDate(0, 1, 0), month, nil
}

// Figures are the raw sums for one month, all in yuan. Sales and Purchases are approved
// bills dated in the month; the returns are the approved return bills dated in the month.
type Figures struct {
	Sales           decimal.Decimal
	SaleReturns     decimal.Decimal
	CostOfGoods     decimal.Decimal // stock-out cost of sales less cost of goods taken back
	Purchases       decimal.Decimal
	PurchaseReturns decimal.Decimal
	Receipts        decimal.Decimal // cash received on sale bills, net of refunds
	Payments        decimal.Decimal // cash paid on purchase bills, net of refunds
	Receivable      decimal.Decimal // month-end receivable, same rule as the ageing
	Payable         decimal.Decimal // month-end payable, same rule as the ageing
	InventoryValue  decimal.Decimal // on-hand quantity x unit cost
}

// Repo is the read port the monthly report needs. from is inclusive, to exclusive.
type Repo interface {
	MonthlyFigures(ctx context.Context, tenantID uuid.UUID, from, to time.Time) (Figures, error)
}

// Monthly is the JSON body of GET /reports/monthly. Amounts are strings with two decimals.
type Monthly struct {
	Month          string `json:"month"`
	DateFrom       string `json:"date_from"`
	DateTo         string `json:"date_to"` // last day of the month, inclusive
	NetSales       string `json:"net_sales"`
	CostOfGoods    string `json:"cost_of_goods"`
	GrossProfit    string `json:"gross_profit"`
	NetPurchases   string `json:"net_purchases"`
	Receipts       string `json:"receipts"`
	Payments       string `json:"payments"`
	Receivable     string `json:"receivable"`
	Payable        string `json:"payable"`
	InventoryValue string `json:"inventory_value"`
}

func money(d decimal.Decimal) string { return d.StringFixed(2) }

// Build derives the net figures from raw sums.
func Build(month string, f Figures) Monthly {
	netSales := f.Sales.Sub(f.SaleReturns)
	return Monthly{
		Month:          month,
		NetSales:       money(netSales),
		CostOfGoods:    money(f.CostOfGoods),
		GrossProfit:    money(netSales.Sub(f.CostOfGoods)),
		NetPurchases:   money(f.Purchases.Sub(f.PurchaseReturns)),
		Receipts:       money(f.Receipts),
		Payments:       money(f.Payments),
		Receivable:     money(f.Receivable),
		Payable:        money(f.Payable),
		InventoryValue: money(f.InventoryValue),
	}
}

// Service answers the monthly report from a Repo.
type Service struct {
	repo Repo
	now  func() time.Time
}

// NewService builds a Service over repo.
func NewService(repo Repo) *Service { return &Service{repo: repo, now: time.Now} }

// Monthly returns the report for month (YYYY-MM, "" = current month).
func (s *Service) Monthly(ctx context.Context, tenantID uuid.UUID, month string) (*Monthly, error) {
	from, to, label, err := ParseMonth(month, s.now())
	if err != nil {
		return nil, err
	}
	f, err := s.repo.MonthlyFigures(ctx, tenantID, from, to)
	if err != nil {
		return nil, err
	}
	m := Build(label, f)
	m.DateFrom = from.Format("2006-01-02")
	m.DateTo = to.AddDate(0, 0, -1).Format("2006-01-02")
	return &m, nil
}
