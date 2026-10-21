// Package print is the cloud-print use case: lay a sale out as a receipt and
// send it to the shop's cloud printer.
package print

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	adapterprint "github.com/hanmahong5-arch/lurus-tally/internal/adapter/print"
	appacct "github.com/hanmahong5-arch/lurus-tally/internal/app/account"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domainbill "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/print"
)

var (
	// ErrPrinterNotConfigured: the shop has not set up a printer yet.
	ErrPrinterNotConfigured = errors.New("print: printer not configured")
	// ErrCloudPrintUnavailable: the platform has no cloud-print account.
	ErrCloudPrintUnavailable = errors.New("print: cloud print unavailable")
	// ErrPrinterOffline: the printer is offline or not ready; the message carries the vendor status text.
	ErrPrinterOffline = errors.New("print: printer offline")
	// ErrNotSaleBill: only sales can be printed as a receipt.
	ErrNotSaleBill = errors.New("print: not a sale bill")
)

// BillReader is the slice of the bill repository the receipt needs.
type BillReader interface {
	GetBill(ctx context.Context, tenantID, billID uuid.UUID) (*domainbill.BillHead, error)
	GetBillItems(ctx context.Context, tenantID, billID uuid.UUID) ([]*domainbill.BillItem, error)
}

// ProductNamer resolves product names for the item rows.
type ProductNamer interface {
	ProductNames(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]string, error)
}

// SettingsReader reads the shop's print settings.
type SettingsReader interface {
	Execute(ctx context.Context, tenantID uuid.UUID) (appacct.PrintSettings, error)
}

// PayTypeReader returns the pay_type of the bill's most recent payment.
type PayTypeReader interface {
	LatestPayType(ctx context.Context, tenantID, billID uuid.UUID) (string, bool, error)
}

// MemberNamer maps a bill's creator id to the member's display label.
type MemberNamer interface {
	MemberNames(ctx context.Context, tenantID uuid.UUID) (map[uuid.UUID]string, error)
}

// unnamedMember is the placeholder the member directory uses for nameless people;
// it is not worth printing on a receipt.
const unnamedMember = "未命名成员"

// payMethodLabel maps a stored pay_type to its receipt wording.
func payMethodLabel(pt string) string {
	switch pt {
	case "cash":
		return "现金"
	case "wechat":
		return "微信"
	case "alipay":
		return "支付宝"
	case "card":
		return "银行卡"
	}
	return "其他"
}

// UseCase prints receipts. Printer may be nil (platform without a cloud-print account).
type UseCase struct {
	bills    BillReader
	names    ProductNamer
	settings SettingsReader
	payTypes PayTypeReader
	members  MemberNamer
	printer  adapterprint.Printer
	now      func() time.Time
}

// New wires the use case. printer may be nil.
func New(bills BillReader, names ProductNamer, settings SettingsReader, printer adapterprint.Printer) *UseCase {
	return &UseCase{bills: bills, names: names, settings: settings, printer: printer, now: time.Now}
}

// WithReceiptExtras adds the payment method and cashier lines to the receipt.
// Either argument may be nil; a failed lookup just leaves its line off.
func (uc *UseCase) WithReceiptExtras(p PayTypeReader, m MemberNamer) *UseCase {
	uc.payTypes, uc.members = p, m
	return uc
}

// Available reports whether the platform can cloud-print at all.
func (uc *UseCase) Available() bool { return uc != nil && uc.printer != nil }

var shopZone = time.FixedZone("CST", 8*3600)

// ready resolves the printer to use: platform support, shop config, device online.
func (uc *UseCase) ready(ctx context.Context, tenantID uuid.UUID) (appacct.PrintSettings, *appacct.PrinterSettings, error) {
	if uc.printer == nil {
		return appacct.PrintSettings{}, nil, ErrCloudPrintUnavailable
	}
	s, err := uc.settings.Execute(ctx, tenantID)
	if err != nil {
		return appacct.PrintSettings{}, nil, err
	}
	if s.Printer == nil || s.Printer.SN == "" {
		return s, nil, ErrPrinterNotConfigured
	}
	st, err := uc.printer.Status(ctx, s.Printer.SN)
	if err != nil {
		return s, nil, err
	}
	if !st.Online || !st.Normal {
		return s, nil, fmt.Errorf("%w: %s", ErrPrinterOffline, st.Text)
	}
	return s, s.Printer, nil
}

// PrintReceipt prints the sale billID on the shop's printer, times copies (1..3), and returns the vendor order id.
func (uc *UseCase) PrintReceipt(ctx context.Context, tenantID, billID uuid.UUID, times int) (string, error) {
	if times < 1 {
		times = 1
	}
	if times > 3 {
		times = 3
	}
	s, p, err := uc.ready(ctx, tenantID)
	if err != nil {
		return "", err
	}
	rcpt, err := uc.build(ctx, tenantID, billID, s)
	if err != nil {
		return "", err
	}
	return uc.printer.Print(ctx, p.SN, domain.RenderFeie(rcpt, domain.ParsePaper(p.Paper)), times)
}

// PrintTest prints a short sample page to check the connection.
func (uc *UseCase) PrintTest(ctx context.Context, tenantID uuid.UUID) (string, error) {
	s, p, err := uc.ready(ctx, tenantID)
	if err != nil {
		return "", err
	}
	shop := s.ShopName
	if shop == "" {
		shop = "测试小票"
	}
	rcpt := domain.TestPage(shop, uc.now().In(shopZone))
	return uc.printer.Print(ctx, p.SN, domain.RenderFeie(rcpt, domain.ParsePaper(p.Paper)), 1)
}

// StatusInfo is what the settings page and the till show about the printer.
type StatusInfo struct {
	Configured          bool
	Online              bool
	Status              string
	CloudPrintAvailable bool
}

// Status reports configuration and device state. A failing vendor call is not
// an error: the page shows "状态未知" instead of breaking.
func (uc *UseCase) Status(ctx context.Context, tenantID uuid.UUID) (StatusInfo, error) {
	info := StatusInfo{CloudPrintAvailable: uc.printer != nil}
	if uc.printer == nil {
		return info, nil
	}
	s, err := uc.settings.Execute(ctx, tenantID)
	if err != nil {
		return info, err
	}
	if s.Printer == nil || s.Printer.SN == "" {
		return info, nil
	}
	info.Configured = true
	st, err := uc.printer.Status(ctx, s.Printer.SN)
	if err != nil {
		info.Status = "状态未知，稍后再试"
		return info, nil
	}
	info.Online = st.Online && st.Normal
	info.Status = strings.TrimSpace(st.Text)
	return info, nil
}

func (uc *UseCase) build(ctx context.Context, tenantID, billID uuid.UUID, s appacct.PrintSettings) (domain.Receipt, error) {
	head, err := uc.bills.GetBill(ctx, tenantID, billID)
	if err != nil {
		return domain.Receipt{}, err
	}
	if head.BillType != domainbill.BillTypeSale {
		return domain.Receipt{}, ErrNotSaleBill
	}
	items, err := uc.bills.GetBillItems(ctx, tenantID, billID)
	if err != nil {
		return domain.Receipt{}, err
	}
	ids := make([]uuid.UUID, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ProductID)
	}
	names := map[uuid.UUID]string{}
	if uc.names != nil && len(ids) > 0 {
		if names, err = uc.names.ProductNames(ctx, tenantID, ids); err != nil {
			return domain.Receipt{}, err
		}
	}
	at := head.CreatedAt
	if head.OccurredAt != nil {
		at = *head.OccurredAt
	}
	r := domain.Receipt{
		ShopName: s.ShopName, Phone: s.Phone, Address: s.Address, Footer: s.ReceiptFooter,
		BillNo: head.BillNo, OccurredAt: at.In(shopZone),
		Total: money(head.TotalAmount),
	}
	for _, it := range items {
		r.Lines = append(r.Lines, domain.Line{
			Name: names[it.ProductID], Qty: it.Qty.String(), Unit: it.UnitName,
			Price: money(it.UnitPrice), Amount: money(it.LineAmount),
		})
	}
	if head.PaidAmount.IsPositive() {
		r.Paid = money(head.PaidAmount)
		if head.PaidAmount.GreaterThan(head.TotalAmount) {
			r.Change = money(head.PaidAmount.Sub(head.TotalAmount))
		}
	}
	if uc.payTypes != nil && head.PaidAmount.IsPositive() {
		if pt, ok, err := uc.payTypes.LatestPayType(ctx, tenantID, billID); err == nil && ok {
			r.PayMethod = payMethodLabel(pt)
		}
	}
	if uc.members != nil && head.CreatorID != uuid.Nil {
		if names, err := uc.members.MemberNames(ctx, tenantID); err == nil {
			if n := strings.TrimSpace(names[head.CreatorID]); n != unnamedMember {
				r.Cashier = n
			}
		}
	}
	return r, nil
}

func money(d decimal.Decimal) string { return d.StringFixed(2) }

// IsBillNotFound reports whether err is the bill repository's not-found error.
func IsBillNotFound(err error) bool { return errors.Is(err, appbill.ErrBillNotFound) }
