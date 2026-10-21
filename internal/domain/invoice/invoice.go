// Package invoice contains the domain entity for the invoice registry.
//
// The tax invoice itself is issued offline at the tax bureau; Tally records the
// invoice number, amounts and which approved bills it covers. There is no
// tax-control integration. A red-letter invoice is its own row with negative
// amounts whose RedOfID points at the original, which then flips to StatusRed.
package invoice

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Direction is tally.invoice.direction.
type Direction string

const (
	// DirectionOut is a sales invoice issued to a customer (links to sale bills).
	DirectionOut Direction = "out"
	// DirectionIn is a purchase invoice received from a supplier (links to purchase bills).
	DirectionIn Direction = "in"
)

// Status is tally.invoice.status.
type Status string

const (
	StatusIssued Status = "issued"
	StatusVoid   Status = "void"
	StatusRed    Status = "red"
)

// Types are the invoice types accepted at registration.
var Types = []string{"数电普票", "数电专票", "纸质普票", "纸质专票"}

// Field width limits mirror the column widths so over-long input is a 400, not a SQL error.
const (
	maxNoRunes     = 64
	maxCodeRunes   = 32
	maxRemarkRunes = 500
	maxLinks       = 200
)

// Invoice maps to tally.invoice. Links and PartnerName are read-side only.
type Invoice struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	Direction     Direction
	InvoiceNo     string
	InvoiceCode   string
	InvoiceType   string
	PartnerID     uuid.UUID
	IssueDate     time.Time // calendar day, midnight UTC
	AmountExclTax decimal.Decimal
	TaxAmount     decimal.Decimal
	TotalAmount   decimal.Decimal
	Status        Status
	RedOfID       *uuid.UUID
	Remark        string
	CreatedBy     *uuid.UUID
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     *time.Time

	PartnerName string
	Links       []Link
}

// Link is one invoice-to-bill association (tally.invoice_bill).
type Link struct {
	BillID uuid.UUID
	BillNo string // read-side display only
	Amount decimal.Decimal
}

// CreateInput carries the fields for registering an invoice.
type CreateInput struct {
	TenantID      uuid.UUID
	Direction     Direction
	InvoiceNo     string
	InvoiceCode   string
	InvoiceType   string
	PartnerID     uuid.UUID
	IssueDate     time.Time
	AmountExclTax decimal.Decimal
	TaxAmount     decimal.Decimal
	Remark        string
	CreatedBy     *uuid.UUID
	Links         []Link
}

// ListFilter controls list queries. DateTo is an EXCLUSIVE upper bound on issue_date.
type ListFilter struct {
	TenantID  uuid.UUID
	Direction Direction
	Status    Status
	PartnerID *uuid.UUID
	BillID    *uuid.UUID // only invoices linked to this bill
	DateFrom  *time.Time
	DateTo    *time.Time
	Keyword   string // substring of invoice_no
	Limit     int
	Offset    int
}

// Summary aggregates every invoice matching a ListFilter (ignoring paging).
// Amounts exclude void invoices and originals struck by a red-letter invoice.
type Summary struct {
	Count         int64
	AmountExclTax decimal.Decimal
	TaxAmount     decimal.Decimal
	TotalAmount   decimal.Decimal
}

// ValidDirection reports whether d is a known direction.
func ValidDirection(d Direction) bool { return d == DirectionIn || d == DirectionOut }

// ValidStatus reports whether s is a known status.
func ValidStatus(s Status) bool {
	return s == StatusIssued || s == StatusVoid || s == StatusRed
}

func validType(t string) bool {
	for _, v := range Types {
		if v == t {
			return true
		}
	}
	return false
}

// fitsNumeric reports whether d fits NUMERIC(18,4): at most 4 decimals, 14 integer digits.
func fitsNumeric(d decimal.Decimal) bool {
	if !d.Equal(d.Round(4)) {
		return false
	}
	return d.Abs().LessThan(decimal.New(1, 14))
}

// Validate enforces domain invariants.
func (i *Invoice) Validate() error {
	if !ValidDirection(i.Direction) {
		return errors.New("发票方向不正确，应为销项或进项")
	}
	no := strings.TrimSpace(i.InvoiceNo)
	if no == "" {
		return errors.New("请填写发票号码")
	}
	if utf8.RuneCountInString(no) > maxNoRunes {
		return errors.New("发票号码太长，最多 64 个字符")
	}
	if utf8.RuneCountInString(i.InvoiceCode) > maxCodeRunes {
		return errors.New("发票代码太长，最多 32 个字符")
	}
	if !validType(i.InvoiceType) {
		return fmt.Errorf("发票类型不正确，可选：%s", strings.Join(Types, "、"))
	}
	if i.PartnerID == uuid.Nil {
		return errors.New("请选择往来单位")
	}
	if i.IssueDate.IsZero() {
		return errors.New("请填写开票日期")
	}
	if utf8.RuneCountInString(i.Remark) > maxRemarkRunes {
		return errors.New("备注太长，最多 500 个字")
	}
	for _, d := range []decimal.Decimal{i.AmountExclTax, i.TaxAmount, i.TotalAmount} {
		if !fitsNumeric(d) {
			return errors.New("金额最多保留 4 位小数，且不能过大")
		}
	}
	if i.RedOfID == nil {
		if !i.AmountExclTax.IsPositive() {
			return errors.New("不含税金额必须大于 0")
		}
		if i.TaxAmount.IsNegative() {
			return errors.New("税额不能为负数")
		}
	}
	if !i.AmountExclTax.Add(i.TaxAmount).Equal(i.TotalAmount) {
		return errors.New("价税合计必须等于不含税金额加税额")
	}
	return validateLinks(i.Links, i.TotalAmount, i.RedOfID != nil)
}

func validateLinks(links []Link, total decimal.Decimal, red bool) error {
	if red {
		if len(links) > 0 {
			return errors.New("红字发票不能直接关联单据")
		}
		return nil
	}
	if len(links) > maxLinks {
		return errors.New("关联的单据太多，最多 200 张")
	}
	seen := make(map[uuid.UUID]struct{}, len(links))
	sum := decimal.Zero
	for _, l := range links {
		if l.BillID == uuid.Nil {
			return errors.New("请选择要关联的单据")
		}
		if _, dup := seen[l.BillID]; dup {
			return errors.New("同一张单据不能重复关联")
		}
		seen[l.BillID] = struct{}{}
		if !l.Amount.IsPositive() {
			return errors.New("关联金额必须大于 0")
		}
		if !fitsNumeric(l.Amount) {
			return errors.New("关联金额最多保留 4 位小数")
		}
		sum = sum.Add(l.Amount)
	}
	if sum.GreaterThan(total) {
		return errors.New("关联金额合计不能超过发票的价税合计")
	}
	return nil
}

// NewInvoice builds an issued invoice from in, computing total = excl + tax, and validates it.
func NewInvoice(in CreateInput, now time.Time) (*Invoice, error) {
	inv := &Invoice{
		ID:            uuid.New(),
		TenantID:      in.TenantID,
		Direction:     in.Direction,
		InvoiceNo:     strings.TrimSpace(in.InvoiceNo),
		InvoiceCode:   strings.TrimSpace(in.InvoiceCode),
		InvoiceType:   in.InvoiceType,
		PartnerID:     in.PartnerID,
		IssueDate:     in.IssueDate,
		AmountExclTax: in.AmountExclTax,
		TaxAmount:     in.TaxAmount,
		TotalAmount:   in.AmountExclTax.Add(in.TaxAmount),
		Status:        StatusIssued,
		Remark:        in.Remark,
		CreatedBy:     in.CreatedBy,
		CreatedAt:     now,
		UpdatedAt:     now,
		Links:         in.Links,
	}
	if err := inv.Validate(); err != nil {
		return nil, err
	}
	return inv, nil
}

// NewRed builds the red-letter counterpart of orig: same parties and type, negated
// amounts, its own number, no bill links. The caller flips orig to StatusRed.
func NewRed(orig *Invoice, invoiceNo, invoiceCode string, issueDate time.Time, remark string, createdBy *uuid.UUID, now time.Time) (*Invoice, error) {
	origID := orig.ID
	red := &Invoice{
		ID:            uuid.New(),
		TenantID:      orig.TenantID,
		Direction:     orig.Direction,
		InvoiceNo:     strings.TrimSpace(invoiceNo),
		InvoiceCode:   strings.TrimSpace(invoiceCode),
		InvoiceType:   orig.InvoiceType,
		PartnerID:     orig.PartnerID,
		IssueDate:     issueDate,
		AmountExclTax: orig.AmountExclTax.Neg(),
		TaxAmount:     orig.TaxAmount.Neg(),
		TotalAmount:   orig.TotalAmount.Neg(),
		Status:        StatusIssued,
		RedOfID:       &origID,
		Remark:        remark,
		CreatedBy:     createdBy,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := red.Validate(); err != nil {
		return nil, err
	}
	return red, nil
}

// CanVoid returns why the invoice cannot be voided, or nil.
func (i *Invoice) CanVoid() error {
	switch {
	case i.RedOfID != nil:
		return errors.New("红字发票不能作废")
	case i.Status == StatusVoid:
		return errors.New("这张发票已经作废")
	case i.Status == StatusRed:
		return errors.New("这张发票已被红字发票冲销，不能作废")
	}
	return nil
}

// CanRed returns why the invoice cannot be struck by a red-letter invoice, or nil.
func (i *Invoice) CanRed() error {
	switch {
	case i.RedOfID != nil:
		return errors.New("红字发票不能再次红冲")
	case i.Status == StatusVoid:
		return errors.New("已作废的发票不能红冲")
	case i.Status == StatusRed:
		return errors.New("这张发票已经红冲过了")
	}
	return nil
}
