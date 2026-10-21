package payment

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Receipt directions mirror the receivables summary: ar = a customer pays us (sales),
// ap = we pay a supplier (purchases).
const (
	DirectionReceivable = "ar"
	DirectionPayable    = "ap"
)

// Statement line kinds.
const (
	LineBill    = "bill"    // a sale / purchase that raised the balance
	LineReturn  = "return"  // a return (or purchase-return offset) that lowered it
	LinePayment = "payment" // money received / paid (a negative payment is a refund)
)

// OpenBill is one approved bill that still has a balance, as the ledger sees it.
type OpenBill struct {
	BillID      uuid.UUID       `json:"bill_id"`
	BillNo      string          `json:"bill_no"`
	BillDate    time.Time       `json:"bill_date"`
	Total       decimal.Decimal `json:"total"`
	Outstanding decimal.Decimal `json:"outstanding"`
}

// StatementQuery selects one partner's statement for the calendar days From..To inclusive.
type StatementQuery struct {
	TenantID  uuid.UUID
	Direction string
	PartnerID uuid.UUID
	From, To  time.Time
}

// StatementLine is one dated movement on the partner's account. Delta is its effect on
// what is owed: bills add, returns and payments subtract (a refund adds back).
type StatementLine struct {
	Date    time.Time       `json:"date"`
	Kind    string          `json:"kind"`
	BillID  *uuid.UUID      `json:"bill_id,omitempty"`
	BillNo  string          `json:"bill_no"`
	Method  string          `json:"method,omitempty"`
	Remark  string          `json:"remark,omitempty"`
	Delta   decimal.Decimal `json:"delta"`
	Balance decimal.Decimal `json:"balance"`
}

// Statement is the account of one partner over a period. Opening and Closing are the
// per-bill balances (floored at zero, each rounded to cents) as at the start and end of
// the period, so Closing equals the partner's row in the ageing at the same day.
// Adjustment is whatever that flooring moved which the flows do not explain; it is zero
// in the normal case.
type Statement struct {
	Direction   string          `json:"direction"`
	PartnerID   uuid.UUID       `json:"partner_id"`
	PartnerName string          `json:"partner_name"`
	From        time.Time       `json:"from"`
	To          time.Time       `json:"to"`
	Opening     decimal.Decimal `json:"opening"`
	Billed      decimal.Decimal `json:"billed"`
	Returned    decimal.Decimal `json:"returned"`
	Paid        decimal.Decimal `json:"paid"`
	Adjustment  decimal.Decimal `json:"adjustment"`
	Closing     decimal.Decimal `json:"closing"`
	Lines       []StatementLine `json:"lines"`
}

// PartnerLedger is the read-side port behind receipts and statements: the partner's open
// bills oldest first, and the dated account of a period.
type PartnerLedger interface {
	// OpenBills returns approved bills with a positive balance, oldest first.
	OpenBills(ctx context.Context, tenantID uuid.UUID, direction string, partnerID uuid.UUID) ([]OpenBill, error)
	// Statement builds the account of one partner for the period.
	Statement(ctx context.Context, q StatementQuery) (*Statement, error)
}

// StatementUseCase serves the partner statement.
type StatementUseCase struct{ ledger PartnerLedger }

// NewStatementUseCase constructs the use case.
func NewStatementUseCase(ledger PartnerLedger) *StatementUseCase {
	return &StatementUseCase{ledger: ledger}
}

// Execute validates the query and returns the statement.
func (uc *StatementUseCase) Execute(ctx context.Context, q StatementQuery) (*Statement, error) {
	if !validDirection(q.Direction) {
		return nil, ErrInvalidReceiptDirection
	}
	if q.To.Before(q.From) {
		return nil, ErrInvalidStatementRange
	}
	return uc.ledger.Statement(ctx, q)
}
