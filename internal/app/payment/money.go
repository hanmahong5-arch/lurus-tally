package payment

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domainpayment "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
)

// MoneyStore keeps statement lines. MarkMatched runs inside the payment transaction.
type MoneyStore interface {
	InsertNew(ctx context.Context, tenant uuid.UUID, channel, account string, lines []domainpayment.ParsedLine) (inserted, skipped int, err error)
	ListOpen(ctx context.Context, tenant uuid.UUID) ([]domainpayment.MoneyLine, error)
	Get(ctx context.Context, tenant, id uuid.UUID) (domainpayment.MoneyLine, error)
	MarkMatched(ctx context.Context, tx *sqlTx, tenant, id, bill, payment uuid.UUID) error
	Ignore(ctx context.Context, tenant, id uuid.UUID) error
}

// MoneyBillReader lists approved sales and purchases a statement line can settle.
type MoneyBillReader interface {
	ListOpenMoneyBills(ctx context.Context, tenant uuid.UUID) ([]domainpayment.MoneyBill, error)
	OpenMoneyBill(ctx context.Context, tenant, bill uuid.UUID) (domainpayment.MoneyBill, error)
}

// MoneyCandidate is one bill a statement line might settle.
type MoneyCandidate struct {
	BillID      uuid.UUID       `json:"bill_id"`
	BillNo      string          `json:"bill_no"`
	PartnerName string          `json:"partner_name"`
	Outstanding decimal.Decimal `json:"outstanding"`
	Direction   string          `json:"direction"`
}

// MoneyView is an unmatched statement line plus the bills that share its amount.
type MoneyView struct {
	ID           uuid.UUID        `json:"id"`
	Channel      string           `json:"channel"`
	BookedAt     string           `json:"booked_at"`
	Amount       decimal.Decimal  `json:"amount"`
	Counterparty string           `json:"counterparty"`
	Memo         string           `json:"memo"`
	ExternalID   string           `json:"external_id"`
	Candidates   []MoneyCandidate `json:"candidates"`
	Suggested    bool             `json:"suggested"`
}

// MoneyUseCase imports statement files and books a confirmed line onto a bill.
type MoneyUseCase struct {
	lines  MoneyStore
	bills  MoneyBillReader
	record *RecordPaymentUseCase
}

// NewMoneyUseCase constructs the use case.
func NewMoneyUseCase(lines MoneyStore, bills MoneyBillReader, record *RecordPaymentUseCase) *MoneyUseCase {
	return &MoneyUseCase{lines: lines, bills: bills, record: record}
}

// Import parses a statement file and stores lines the tenant does not already have.
func (uc *MoneyUseCase) Import(ctx context.Context, tenant uuid.UUID, account string, raw []byte) (channel string, inserted, skipped int, err error) {
	channel, fileAccount, lines, err := domainpayment.ParseMoneyFile(raw)
	if err != nil {
		return "", 0, 0, err
	}
	account = strings.TrimSpace(account)
	if account == "" {
		account = fileAccount
	}
	if len([]rune(account)) > 64 {
		return "", 0, 0, domainpayment.ErrMoneyAccount
	}
	inserted, skipped, err = uc.lines.InsertNew(ctx, tenant, channel, account, lines)
	return channel, inserted, skipped, err
}

// List returns open lines with amount matches. Nothing is written.
func (uc *MoneyUseCase) List(ctx context.Context, tenant uuid.UUID) ([]MoneyView, error) {
	lines, err := uc.lines.ListOpen(ctx, tenant)
	if err != nil {
		return nil, err
	}
	bills, err := uc.bills.ListOpenMoneyBills(ctx, tenant)
	if err != nil {
		return nil, err
	}
	out := make([]MoneyView, 0, len(lines))
	for _, ln := range lines {
		suggested, hits := domainpayment.SuggestMoney(ln.Amount, ln.Counterparty, ln.Memo, bills)
		view := MoneyView{
			ID: ln.ID, Channel: ln.Channel, BookedAt: ln.BookedAt.Format("2006-01-02T15:04:05Z07:00"),
			Amount: ln.Amount, Counterparty: ln.Counterparty, Memo: ln.Memo, ExternalID: ln.ExternalID,
			Candidates: make([]MoneyCandidate, 0, len(hits)), Suggested: suggested,
		}
		for _, h := range hits {
			view.Candidates = append(view.Candidates, MoneyCandidate{
				BillID: h.ID, BillNo: h.BillNo, PartnerName: h.PartnerName, Outstanding: h.Outstanding, Direction: h.Direction,
			})
		}
		out = append(out, view)
	}
	return out, nil
}

// Confirm books the line's amount on the bill and marks the line, in one transaction.
func (uc *MoneyUseCase) Confirm(ctx context.Context, tenant, creator, lineID, billID uuid.UUID) error {
	line, err := uc.lines.Get(ctx, tenant, lineID)
	if err != nil {
		return err
	}
	if line.Status == domainpayment.MoneyMatched {
		if line.BillID != nil && *line.BillID == billID {
			return nil
		}
		return domainpayment.ErrMoneyClosed
	}
	if line.Status != domainpayment.MoneyOpen {
		return domainpayment.ErrMoneyClosed
	}
	bill, err := uc.bills.OpenMoneyBill(ctx, tenant, billID)
	if err != nil {
		return err
	}
	want := DirectionReceivable
	if line.Amount.IsNegative() {
		want = DirectionPayable
	}
	if bill.Direction != want {
		return domainpayment.ErrMoneyDirection
	}
	_, err = uc.record.ExecuteReturning(ctx, RecordPaymentRequest{
		TenantID: tenant, BillID: billID, CreatorID: creator, Amount: line.Amount.Abs(),
		PayType: channelPayType(line.Channel), Remark: moneyRemark(line.ExternalID),
		after: func(tx *sqlTx, paymentID uuid.UUID) error {
			return uc.lines.MarkMatched(ctx, tx, tenant, lineID, billID, paymentID)
		},
	})
	if err == nil {
		return nil
	}
	again, gerr := uc.lines.Get(ctx, tenant, lineID)
	if gerr == nil && again.Status == domainpayment.MoneyMatched && again.BillID != nil && *again.BillID == billID {
		return nil
	}
	return err
}

// Ignore leaves the line out of the open list and does not book a payment.
func (uc *MoneyUseCase) Ignore(ctx context.Context, tenant, id uuid.UUID) error {
	return uc.lines.Ignore(ctx, tenant, id)
}

func channelPayType(channel string) string {
	switch channel {
	case domainpayment.ChannelWechat:
		return string(domainpayment.PayTypeWechat)
	case domainpayment.ChannelAlipay:
		return string(domainpayment.PayTypeAlipay)
	default:
		return string(domainpayment.PayTypeBank)
	}
}

func moneyRemark(externalID string) string {
	s := "流水 " + externalID
	r := []rune(s)
	if len(r) > 200 {
		return string(r[:200])
	}
	return s
}
