package payment

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domainpayment "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
)

// ErrInvalidReceiptDirection is returned for a direction other than ar / ap.
var ErrInvalidReceiptDirection = errors.New("receipt: direction must be ar or ap")

// ErrReceiptAmount is returned for an amount that is not a positive sum of whole cents.
var ErrReceiptAmount = errors.New("receipt: amount must be positive with at most 2 decimals")

// ErrInvalidStatementRange is returned when a statement ends before it starts.
var ErrInvalidStatementRange = errors.New("statement: to is before from")

// ErrPartnerNotFound is returned when the partner does not exist in the tenant.
var ErrPartnerNotFound = errors.New("partner not found")

// ExceedsPartnerDebtError is returned when the money is more than the partner owes in total.
// Its text is the user-facing Chinese message.
type ExceedsPartnerDebtError struct {
	Amount  decimal.Decimal
	Owed    decimal.Decimal
	Payable bool
}

func (e *ExceedsPartnerDebtError) Error() string {
	if e.Owed.IsZero() {
		if e.Payable {
			return "目前没有欠该供应商的款"
		}
		return "该客户目前没有欠款"
	}
	if e.Payable {
		return fmt.Sprintf("我们只欠该供应商 ¥%s，本次 ¥%s 超过了", e.Owed.StringFixed(2), e.Amount.StringFixed(2))
	}
	return fmt.Sprintf("该客户只欠 ¥%s，本次 ¥%s 超过了", e.Owed.StringFixed(2), e.Amount.StringFixed(2))
}

// ReceiptRequest is one lump-sum payment from (or to) a partner.
type ReceiptRequest struct {
	TenantID  uuid.UUID
	CreatorID uuid.UUID
	Direction string
	PartnerID uuid.UUID
	Amount    decimal.Decimal
	PayType   string
	Remark    string
}

// Allocation is the share of a receipt that settles one bill.
type Allocation struct {
	BillID      uuid.UUID       `json:"bill_id"`
	BillNo      string          `json:"bill_no"`
	BillDate    time.Time       `json:"bill_date"`
	Outstanding decimal.Decimal `json:"outstanding"`
	Applied     decimal.Decimal `json:"applied"`
	Remaining   decimal.Decimal `json:"remaining"`
}

// ReceiptResult lists which bills a receipt covers, oldest first. Owed is the partner's
// total balance before the receipt. Booked is what was written to the payments: always the
// whole Amount. Diff is the sub-cent tail of legacy bills settled in full, booked less their
// raw remainder (negative when the raw figure was higher); it is known only once committed,
// so a preview reports 0.
type ReceiptResult struct {
	Owed        decimal.Decimal `json:"owed"`
	Amount      decimal.Decimal `json:"amount"`
	Booked      decimal.Decimal `json:"booked"`
	Diff        decimal.Decimal `json:"diff"`
	Allocations []Allocation    `json:"allocations"`
}

// ReceiptUseCase settles a lump sum against a partner's oldest open bills first.
type ReceiptUseCase struct {
	ledger      PartnerLedger
	billRepo    BillReader
	paymentRepo PaymentRepo
}

// NewReceiptUseCase constructs the use case.
func NewReceiptUseCase(ledger PartnerLedger, billRepo BillReader, paymentRepo PaymentRepo) *ReceiptUseCase {
	return &ReceiptUseCase{ledger: ledger, billRepo: billRepo, paymentRepo: paymentRepo}
}

// billLocker is an optional capability of the bill reader: the per-bill advisory lock the
// return approvals take. The production repo implements it.
type billLocker interface {
	AcquireBillAdvisoryLock(ctx context.Context, tx *sql.Tx, tenantID, billID uuid.UUID) error
}

func validDirection(d string) bool { return d == DirectionReceivable || d == DirectionPayable }

func checkReceipt(direction string, amount decimal.Decimal) error {
	if !validDirection(direction) {
		return ErrInvalidReceiptDirection
	}
	if !amount.IsPositive() || !amount.Equal(amount.Round(2)) {
		return ErrReceiptAmount
	}
	return nil
}

// allocate spreads amount over open (oldest first); rest is what no bill could take.
func allocate(open []OpenBill, amount decimal.Decimal) (out []Allocation, rest decimal.Decimal) {
	rest = amount
	for _, b := range open {
		if !rest.IsPositive() {
			break
		}
		if !b.Outstanding.IsPositive() {
			continue
		}
		applied := decimal.Min(rest, b.Outstanding)
		out = append(out, Allocation{
			BillID: b.BillID, BillNo: b.BillNo, BillDate: b.BillDate,
			Outstanding: b.Outstanding, Applied: applied, Remaining: b.Outstanding.Sub(applied),
		})
		rest = rest.Sub(applied)
	}
	return out, rest
}

func totalOwed(open []OpenBill) decimal.Decimal {
	sum := decimal.Zero
	for _, b := range open {
		if b.Outstanding.IsPositive() {
			sum = sum.Add(b.Outstanding)
		}
	}
	return sum
}

// Preview shows which bills amount would settle, without writing anything.
func (uc *ReceiptUseCase) Preview(ctx context.Context, tenantID uuid.UUID, direction string, partnerID uuid.UUID, amount decimal.Decimal) (*ReceiptResult, error) {
	if err := checkReceipt(direction, amount); err != nil {
		return nil, err
	}
	open, err := uc.ledger.OpenBills(ctx, tenantID, direction, partnerID)
	if err != nil {
		return nil, fmt.Errorf("receipt: open bills: %w", err)
	}
	owed := totalOwed(open)
	allocs, rest := allocate(open, amount)
	if rest.IsPositive() {
		return nil, &ExceedsPartnerDebtError{Amount: amount, Owed: owed, Payable: direction == DirectionPayable}
	}
	return &ReceiptResult{Owed: owed, Amount: amount, Booked: amount, Allocations: allocs}, nil
}

// Execute books the receipt: one payment per settled bill, all in one transaction. Each
// bill is locked and re-read before it is paid, so the balances used are the committed
// ones, not the ledger's snapshot.
func (uc *ReceiptUseCase) Execute(ctx context.Context, req ReceiptRequest) (*ReceiptResult, error) {
	if req.TenantID == uuid.Nil || req.PartnerID == uuid.Nil || req.CreatorID == uuid.Nil {
		return nil, fmt.Errorf("receipt: tenant, partner and creator are required")
	}
	if err := checkReceipt(req.Direction, req.Amount); err != nil {
		return nil, err
	}
	payType, err := resolvePayType(req.PayType)
	if err != nil {
		return nil, err
	}
	payable := req.Direction == DirectionPayable
	wantType := domain.BillTypeSale
	if payable {
		wantType = domain.BillTypePurchase
	}

	candidates, err := uc.ledger.OpenBills(ctx, req.TenantID, req.Direction, req.PartnerID)
	if err != nil {
		return nil, fmt.Errorf("receipt: open bills: %w", err)
	}

	var result *ReceiptResult
	err = uc.billRepo.WithTx(ctx, func(tx *sql.Tx) error {
		live := make([]OpenBill, 0, len(candidates))
		paids := map[uuid.UUID]decimal.Decimal{}
		raws := map[uuid.UUID]decimal.Decimal{}
		totals := map[uuid.UUID]decimal.Decimal{}
		for _, c := range candidates {
			// A sales return approval locks the original sale with this advisory lock; take
			// it first so the returned amount read below cannot be stale.
			if bl, ok := uc.billRepo.(billLocker); ok && !payable {
				if err := bl.AcquireBillAdvisoryLock(ctx, tx, req.TenantID, c.BillID); err != nil {
					return fmt.Errorf("receipt: lock bill: %w", err)
				}
			}
			head, err := uc.billRepo.GetBillForUpdate(ctx, tx, req.TenantID, c.BillID)
			if err != nil {
				return fmt.Errorf("receipt: load bill: %w", err)
			}
			if head.Status != domain.StatusApproved || head.BillType != wantType ||
				head.SubType == domain.BillSubTypePurchaseReturn ||
				head.PartnerID == nil || *head.PartnerID != req.PartnerID {
				continue
			}
			paid, err := uc.paymentRepo.SumByBill(ctx, tx, req.TenantID, c.BillID)
			if err != nil {
				return fmt.Errorf("receipt: sum by bill: %w", err)
			}
			net := head.TotalAmount
			if head.BillType == domain.BillTypeSale {
				returned, err := readReturned(ctx, uc.billRepo, tx, req.TenantID, c.BillID)
				if err != nil {
					return fmt.Errorf("receipt: returned amount: %w", err)
				}
				net = decimal.Max(head.TotalAmount.Sub(returned), decimal.Zero)
			}
			raws[c.BillID] = decimal.Max(net.Sub(paid), decimal.Zero)
			c.Outstanding = raws[c.BillID].Round(2)
			live = append(live, c)
			paids[c.BillID] = paid
			totals[c.BillID] = head.TotalAmount
		}

		// Legacy bills may carry fractions of a cent: the debt is the sum of the per-bill
		// rounded figures, which is what the ageing shows.
		owed := decimal.Zero
		for _, c := range live {
			owed = owed.Add(c.Outstanding)
		}
		allocs, rest := allocate(live, req.Amount)
		if rest.IsPositive() {
			return &ExceedsPartnerDebtError{Amount: req.Amount, Owed: owed, Payable: payable}
		}

		now := time.Now().UTC()
		// Every payment is booked at the rounded figure the ageing shows, so the payments of one
		// receipt add up to exactly the money handed over and the books match the till. A bill
		// settled in full is therefore paid up to under one cent more or less than its raw
		// remainder (its paid_amount is capped at the bill total); tail is that difference (booked less raw) over the bills settled in full.
		booked, tail := decimal.Zero, decimal.Zero
		for _, a := range allocs {
			p := &domainpayment.Payment{
				ID: uuid.New(), TenantID: req.TenantID, BillID: a.BillID, PayType: payType,
				Amount: a.Applied, TotalAmount: a.Applied, CreatorID: req.CreatorID,
				PayDate: now, Remark: req.Remark, CreatedAt: now, PartnerID: &req.PartnerID,
			}
			if err := uc.paymentRepo.Record(ctx, tx, p); err != nil {
				return fmt.Errorf("receipt: persist: %w", err)
			}
			// The bill table forbids paid above total, so a fraction of a cent over is not stored
			// on the bill; the payment row above still carries the whole cent.
			paidNow := decimal.Min(paids[a.BillID].Add(a.Applied), totals[a.BillID])
			if a.Remaining.IsZero() {
				// Settled in full: book the raw remainder so a bill rounded down never stays partial.
				paidNow = decimal.Min(paids[a.BillID].Add(raws[a.BillID]), totals[a.BillID])
			}
			if err := uc.billRepo.UpdatePaidAmount(ctx, tx, req.TenantID, a.BillID, paidNow); err != nil {
				return fmt.Errorf("receipt: update paid_amount: %w", err)
			}
			booked = booked.Add(a.Applied)
			if a.Remaining.IsZero() {
				tail = tail.Add(a.Applied.Sub(raws[a.BillID]))
			}
		}
		result = &ReceiptResult{Owed: owed, Amount: req.Amount, Booked: booked, Diff: tail, Allocations: allocs}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
