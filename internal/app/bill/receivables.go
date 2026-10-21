package bill

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Receivable directions: ar = customers owe us (sales), ap = we owe suppliers (purchases).
const (
	ReceivableAR = "ar"
	ReceivableAP = "ap"
)

// ErrInvalidReceivableDirection is returned for a direction other than ar / ap.
var ErrInvalidReceivableDirection = errors.New("receivables: direction must be ar or ap")

// receivablesLocation cuts "today" and bill dates at Asia/Shanghai wall-clock days.
// A fixed zone avoids LoadLocation, which needs tzdata that scratch images lack.
var receivablesLocation = time.FixedZone("CST", 8*3600)

// PartnerReceivable is one partner's outstanding balance split by bill age.
// PartnerID is nil for bills without a partner (walk-in customers).
type PartnerReceivable struct {
	PartnerID      *uuid.UUID      `json:"partner_id"`
	PartnerName    string          `json:"partner_name"`
	Unpaid         decimal.Decimal `json:"unpaid"`
	B0to30         decimal.Decimal `json:"b0_30"`
	B31to60        decimal.Decimal `json:"b31_60"`
	B61to90        decimal.Decimal `json:"b61_90"`
	B90Plus        decimal.Decimal `json:"b90p"`
	OldestBillDate *time.Time      `json:"oldest_bill_date"`
}

// ReceivablesQuery selects approved bills dated on or before AsOf (a calendar day).
type ReceivablesQuery struct {
	TenantID  uuid.UUID
	Direction string
	AsOf      time.Time
}

// ReceivablesRepo is the read-side persistence port for the receivables summary.
// It is separate from BillRepo so the many BillRepo test doubles stay untouched.
type ReceivablesRepo interface {
	// PartnerReceivables returns partners with a positive balance, largest first.
	PartnerReceivables(ctx context.Context, q ReceivablesQuery) ([]PartnerReceivable, error)
}

// ReceivablesOutput is the per-partner list plus the grand total row.
type ReceivablesOutput struct {
	Direction string
	AsOf      time.Time
	Items     []PartnerReceivable
	Total     PartnerReceivable
}

// ReceivablesUseCase summarises who owes whom and for how long.
type ReceivablesUseCase struct {
	repo ReceivablesRepo
	now  func() time.Time
}

// NewReceivablesUseCase constructs the use case.
func NewReceivablesUseCase(repo ReceivablesRepo) *ReceivablesUseCase {
	return &ReceivablesUseCase{repo: repo, now: time.Now}
}

// Execute returns the summary as of asOf (nil = today, Shanghai time).
func (uc *ReceivablesUseCase) Execute(ctx context.Context, tenantID uuid.UUID, direction string, asOf *time.Time) (*ReceivablesOutput, error) {
	if tenantID == uuid.Nil {
		return nil, fmt.Errorf("receivables: tenant_id is required")
	}
	if direction != ReceivableAR && direction != ReceivableAP {
		return nil, ErrInvalidReceivableDirection
	}
	var day time.Time
	if asOf != nil {
		y, m, dd := asOf.Date()
		day = time.Date(y, m, dd, 0, 0, 0, 0, receivablesLocation)
	} else {
		y, m, dd := uc.now().In(receivablesLocation).Date()
		day = time.Date(y, m, dd, 0, 0, 0, 0, receivablesLocation)
	}
	rows, err := uc.repo.PartnerReceivables(ctx, ReceivablesQuery{TenantID: tenantID, Direction: direction, AsOf: day})
	if err != nil {
		return nil, fmt.Errorf("receivables: %w", err)
	}
	if rows == nil {
		rows = []PartnerReceivable{}
	}
	out := &ReceivablesOutput{Direction: direction, AsOf: day, Items: rows}
	for _, r := range rows {
		out.Total.Unpaid = out.Total.Unpaid.Add(r.Unpaid)
		out.Total.B0to30 = out.Total.B0to30.Add(r.B0to30)
		out.Total.B31to60 = out.Total.B31to60.Add(r.B31to60)
		out.Total.B61to90 = out.Total.B61to90.Add(r.B61to90)
		out.Total.B90Plus = out.Total.B90Plus.Add(r.B90Plus)
		if r.OldestBillDate != nil && (out.Total.OldestBillDate == nil || r.OldestBillDate.Before(*out.Total.OldestBillDate)) {
			t := *r.OldestBillDate
			out.Total.OldestBillDate = &t
		}
	}
	return out, nil
}
