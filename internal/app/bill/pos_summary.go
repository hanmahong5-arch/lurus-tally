package bill

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// posLocation cuts POS shifts at Asia/Shanghai wall-clock days. A fixed zone avoids
// LoadLocation, which needs tzdata that scratch images lack.
var posLocation = time.FixedZone("CST", 8*3600)

// POSPayTypeTotal is what one payment method collected on the day's approved sales.
type POSPayTypeTotal struct {
	PayType string          `json:"pay_type"`
	Count   int64           `json:"count"`
	Amount  decimal.Decimal `json:"amount"`
}

// POSSummary is the shift-end view of one Shanghai calendar day. Count / TotalAmount /
// PaidAmount cover approved sale bills only (gross, before returns); Returns* cover
// approved sales returns dated that day. CreditAmount is the still-unpaid remainder of
// those bills net of their returns, floored at zero per bill. RefundCount and RefundAmount
// are the number and total of refund payments (negative payments on sale bills) paid out that
// day, whatever day the sale was written.
type POSSummary struct {
	Date         string
	Count        int64
	TotalAmount  decimal.Decimal
	PaidAmount   decimal.Decimal
	CreditAmount decimal.Decimal
	ByPayType    []POSPayTypeTotal
	ReturnCount  int64
	ReturnAmount decimal.Decimal
	RefundCount  int64
	RefundAmount decimal.Decimal
}

// POSSummaryRange is the half-open [From, To) window of the day being summarised.
type POSSummaryRange struct {
	TenantID uuid.UUID
	From     time.Time
	To       time.Time
	// CreatorID, when set, narrows the shift to one person's bills and payments.
	CreatorID *uuid.UUID
}

// POSSummaryRepo is the read port for the POS shift summary. The PG repo implements it
// next to BillRepo; callers discover it by type assertion so BillRepo doubles stay untouched.
type POSSummaryRepo interface {
	POSSummary(ctx context.Context, q POSSummaryRange) (POSSummary, error)
}

// ParsePOSDate resolves a YYYY-MM-DD query into the Shanghai day window. Empty means
// today (Shanghai) as seen from now.
func ParsePOSDate(s string, now time.Time) (from, to time.Time, err error) {
	if s == "" {
		s = now.In(posLocation).Format("2006-01-02")
	}
	from, err = time.ParseInLocation("2006-01-02", s, posLocation)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("pos summary: bad date %q: %w", s, err)
	}
	return from, from.AddDate(0, 0, 1), nil
}

// GetPOSSummary loads the summary for the day window and stamps its date label. A non-nil
// creatorID makes it that person's own shift instead of the whole shop's.
func GetPOSSummary(ctx context.Context, repo POSSummaryRepo, tenantID uuid.UUID, from, to time.Time, creatorID *uuid.UUID) (POSSummary, error) {
	if tenantID == uuid.Nil {
		return POSSummary{}, fmt.Errorf("pos summary: tenant_id is required")
	}
	out, err := repo.POSSummary(ctx, POSSummaryRange{TenantID: tenantID, From: from, To: to, CreatorID: creatorID})
	if err != nil {
		return POSSummary{}, err
	}
	out.Date = from.In(posLocation).Format("2006-01-02")
	if out.ByPayType == nil {
		out.ByPayType = []POSPayTypeTotal{}
	}
	return out, nil
}

// POSCashierTotal is one person's slice of the day: the approved sale bills they wrote
// (Count / TotalAmount / PaidAmount) and the refunds they paid out (RefundAmount).
// CreatorID is uuid.Nil for rows written without a creator.
type POSCashierTotal struct {
	CreatorID    uuid.UUID
	Count        int64
	TotalAmount  decimal.Decimal
	PaidAmount   decimal.Decimal
	RefundAmount decimal.Decimal
}

// POSCashierRepo is the optional read port that splits the day by creator. Callers discover
// it by type assertion, like POSSummaryRepo. q.CreatorID is ignored.
type POSCashierRepo interface {
	POSCashiers(ctx context.Context, q POSSummaryRange) ([]POSCashierTotal, error)
}

// GetPOSCashiers loads the per-creator split for the day window, biggest takings first.
func GetPOSCashiers(ctx context.Context, repo POSCashierRepo, tenantID uuid.UUID, from, to time.Time) ([]POSCashierTotal, error) {
	if tenantID == uuid.Nil {
		return nil, fmt.Errorf("pos cashiers: tenant_id is required")
	}
	out, err := repo.POSCashiers(ctx, POSSummaryRange{TenantID: tenantID, From: from, To: to})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		if c := out[i].PaidAmount.Cmp(out[j].PaidAmount); c != 0 {
			return c > 0
		}
		return out[i].CreatorID.String() < out[j].CreatorID.String()
	})
	return out, nil
}
