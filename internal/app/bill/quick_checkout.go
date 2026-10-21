package bill

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// QuickCheckoutRequest is the input to QuickCheckoutUseCase.
// Designed for POS single-step: create draft + approve + record payment in one transaction.
type QuickCheckoutRequest struct {
	TenantID     uuid.UUID
	CreatorID    uuid.UUID
	PartnerID    *uuid.UUID // customer profile; when set it lands on bill_head and the payment
	CustomerName string     // stored as remark in bill_head when PartnerID is absent
	// Remark is the cashier's free note; it is always kept on the bill, after the walk-in name.
	Remark        string
	Items         []SaleItem
	PaymentMethod string
	PaidAmount    decimal.Decimal
	// DiscountAmount is the whole-bill discount (整单优惠), deducted from the line total.
	DiscountAmount decimal.Decimal

	// ClientRef is the till-generated idempotency reference (<= 64 chars); empty = none.
	// A second checkout with the same ref in a tenant is refused with *DuplicateCheckoutError.
	ClientRef string
	// Source is "pos" (default) or "pos_offline". Only pos_offline may take stock below zero.
	Source string
	// OccurredAt is when the sale happened at the till; zero = now. Must lie within
	// [now-30d, now+5m]. It dates the bill, the stock movements and the payment.
	OccurredAt time.Time
}

// Checkout sources accepted by quick checkout.
const (
	SourcePOS        = "pos"
	SourcePOSOffline = "pos_offline"
)

// Bounds of a till-reported sale time and of the client reference.
const (
	MaxClientRefLen      = 64
	maxOccurredAtFuture  = 5 * time.Minute
	maxOccurredAtPastAge = 30 * 24 * time.Hour
)

// ErrCheckoutAlreadyExists matches a *DuplicateCheckoutError.
var ErrCheckoutAlreadyExists = errors.New("bill: checkout with this client_ref already exists")

// DuplicateCheckoutError says the (tenant, client_ref) pair is already booked and names the
// bill, so the till can mark its queue entry done instead of retrying.
type DuplicateCheckoutError struct {
	BillID uuid.UUID
	BillNo string
}

func (e *DuplicateCheckoutError) Error() string {
	return fmt.Sprintf("%s (bill_no=%s)", ErrCheckoutAlreadyExists.Error(), e.BillNo)
}

// Is makes errors.Is(err, ErrCheckoutAlreadyExists) true.
func (e *DuplicateCheckoutError) Is(target error) bool { return target == ErrCheckoutAlreadyExists }

// ErrDuplicateClientRef is what the repository returns from CreateBill when the
// (tenant_id, client_ref) unique index rejects the insert.
var ErrDuplicateClientRef = errors.New("bill: duplicate client_ref")

// clientRefFinder is an optional capability of BillRepo: look a bill up by its client_ref.
// Test doubles that lack it skip the pre-check; the unique index still guards the insert.
type clientRefFinder interface {
	FindBillByClientRef(ctx context.Context, tenantID uuid.UUID, clientRef string) (billID uuid.UUID, billNo string, found bool, err error)
}

// negativeStockRecorder is an optional capability of BillRepo: leave a "please count this
// product" note in the operation log, inside the checkout transaction.
type negativeStockRecorder interface {
	RecordNegativeStock(ctx context.Context, tx *sql.Tx, tenantID, billID, userID, productID uuid.UUID, onHandAfter decimal.Decimal) error
}

// QuickCheckoutResult is returned on success.
type QuickCheckoutResult struct {
	BillID           uuid.UUID
	BillNo           string
	TotalAmount      decimal.Decimal
	ReceivableAmount decimal.Decimal
	// ChangeAmount is the cash handed back when the tendered amount exceeds the total.
	// It is never booked as a payment.
	ChangeAmount decimal.Decimal
}

// QuickCheckoutUseCase performs a single-transaction POS checkout:
// create sale draft → approve (stock out) → record payment.
type QuickCheckoutUseCase struct {
	repo      BillRepo
	approveUC *ApproveSaleUseCase
	now       func() time.Time // injectable clock; time.Now by default
}

// NewQuickCheckoutUseCase constructs the use case.
func NewQuickCheckoutUseCase(repo BillRepo, approveUC *ApproveSaleUseCase) *QuickCheckoutUseCase {
	return &QuickCheckoutUseCase{
		repo:      repo,
		approveUC: approveUC,
		now:       time.Now,
	}
}

// resolveOccurredAt validates the till-reported sale time against the clock: not more than
// 5 minutes ahead (clock skew), not older than 30 days. Zero means "now".
func resolveOccurredAt(reported, now time.Time) (time.Time, error) {
	if reported.IsZero() {
		return now, nil
	}
	if reported.After(now.Add(maxOccurredAtFuture)) {
		return time.Time{}, fmt.Errorf("%w: occurred_at is in the future", ErrValidation)
	}
	if reported.Before(now.Add(-maxOccurredAtPastAge)) {
		return time.Time{}, fmt.Errorf("%w: occurred_at is older than 30 days", ErrValidation)
	}
	return reported.UTC(), nil
}

// Execute performs create + approve + payment in a single transaction.
func (uc *QuickCheckoutUseCase) Execute(ctx context.Context, req QuickCheckoutRequest) (*QuickCheckoutResult, error) {
	if req.TenantID == uuid.Nil {
		return nil, fmt.Errorf("%w: tenant_id is required", ErrValidation)
	}
	if req.CreatorID == uuid.Nil {
		return nil, fmt.Errorf("%w: creator_id is required", ErrValidation)
	}
	if len(req.Items) == 0 {
		return nil, fmt.Errorf("%w: at least one item is required", ErrValidation)
	}

	// Resolve warehouse from the first item (POS is single-warehouse) and reject
	// any product/warehouse reference outside this tenant before opening the
	// write transaction. validateRefs closes a cross-tenant hole that the
	// bill_item / bill_head FKs miss because FK checks bypass RLS.
	var warehouseID *uuid.UUID
	if len(req.Items) > 0 && req.Items[0].WarehouseID != uuid.Nil {
		w := req.Items[0].WarehouseID
		warehouseID = &w
	}
	productIDs := make([]uuid.UUID, 0, len(req.Items))
	for _, it := range req.Items {
		productIDs = append(productIDs, it.ProductID)
	}
	if err := validateRefs(ctx, uc.repo, req.TenantID, productIDs, warehouseID); err != nil {
		return nil, err
	}

	if req.PaidAmount.IsNegative() {
		return nil, fmt.Errorf("%w: paid_amount must be non-negative", ErrValidation)
	}
	if !req.PaidAmount.Equal(req.PaidAmount.Round(2)) {
		return nil, fmt.Errorf("%w: paid_amount has more than 2 decimals", ErrValidation)
	}
	if req.DiscountAmount.IsNegative() {
		return nil, fmt.Errorf("%w: discount_amount must be non-negative", ErrValidation)
	}
	if _, err := resolvePayType(req.PaymentMethod); err != nil {
		return nil, err
	}
	if err := validatePartner(ctx, uc.repo, req.TenantID, req.PartnerID); err != nil {
		return nil, err
	}

	if len(req.ClientRef) > MaxClientRefLen {
		return nil, fmt.Errorf("%w: client_ref is longer than %d characters", ErrValidation, MaxClientRefLen)
	}
	source := req.Source
	switch source {
	case "":
		source = SourcePOS
	case SourcePOS, SourcePOSOffline:
	default:
		return nil, fmt.Errorf("%w: unsupported source %q", ErrValidation, source)
	}
	// 离线重放必须带 client_ref 与 occurred_at，否则不放开负库存，防止在线端自报来源绕过库存校验。
	if source == SourcePOSOffline && (req.ClientRef == "" || req.OccurredAt.IsZero()) {
		return nil, fmt.Errorf("%w: pos_offline requires client_ref and occurred_at", ErrValidation)
	}
	clock := uc.now
	if clock == nil {
		clock = time.Now
	}
	nowTS := clock().UTC()
	occurredAt, err := resolveOccurredAt(req.OccurredAt, nowTS)
	if err != nil {
		return nil, err
	}
	hasOccurredAt := !req.OccurredAt.IsZero()

	// Cheap replay check before burning a bill number; the unique index closes the race.
	if req.ClientRef != "" {
		dup, err := uc.findDuplicate(ctx, req.TenantID, req.ClientRef)
		if err != nil {
			return nil, err
		}
		if dup != nil {
			return nil, dup
		}
	}

	var result QuickCheckoutResult

	if err := uc.repo.WithTx(ctx, func(tx *sql.Tx) error {
		billID := uuid.New()
		billNo, err := uc.repo.NextBillNo(ctx, tx, req.TenantID, "SL")
		if err != nil {
			return fmt.Errorf("quick checkout: generate bill_no: %w", err)
		}

		// Build items and totals (shared logic with CreateSaleUseCase).
		billItems, subtotal, err := assembleSaleItems(req.TenantID, billID, req.Items)
		if err != nil {
			return fmt.Errorf("quick checkout: validate items: %w", err)
		}
		if req.DiscountAmount.GreaterThan(subtotal) {
			return fmt.Errorf("%w: discount_amount exceeds the line total", ErrValidation)
		}
		totalAmount := subtotal.Sub(req.DiscountAmount).Round(2)

		now := nowTS

		// Only the part of the tendered amount that settles the bill is booked;
		// the rest is change handed back to the customer.
		recorded := req.PaidAmount
		change := decimal.Zero
		if recorded.GreaterThan(totalAmount) {
			change = recorded.Sub(totalAmount)
			recorded = totalAmount
		}

		remark := ""
		if req.PartnerID == nil {
			remark = req.CustomerName
		}
		if hasOccurredAt {
			// 离线单按重放时刻入账（不改写已结班次），实际发生时间仅留在备注与 occurred_at。
			if remark != "" {
				remark += "；"
			}
			remark += "离线销售，实际发生于 " + occurredAt.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04")
		}
		if note := strings.TrimSpace(req.Remark); note != "" {
			if remark != "" {
				remark += "；"
			}
			remark += note
		}

		head := &domain.BillHead{
			ID:          billID,
			TenantID:    req.TenantID,
			BillNo:      billNo,
			BillType:    domain.BillTypeSale,
			SubType:     domain.BillSubTypeSale,
			Status:      domain.StatusDraft,
			PartnerID:   req.PartnerID,
			WarehouseID: warehouseID,
			CreatorID:   req.CreatorID,
			BillDate:    now,
			Subtotal:    subtotal,
			TotalAmount: totalAmount,
			Remark:      remark,
			CreatedAt:   now,
			UpdatedAt:   now,
			ClientRef:   req.ClientRef,
			Source:      source,
		}
		if hasOccurredAt {
			t := occurredAt
			head.OccurredAt = &t
		}

		if err := uc.repo.CreateBill(ctx, tx, head, billItems); err != nil {
			return fmt.Errorf("quick checkout: create bill: %w", err)
		}

		// Offline sales may take stock below zero; each such product is noted for a recount.
		var negatives []negativeLine

		// Approve (stock out movements + status update + optional payment).
		if err := uc.approveUC.ExecuteInTx(ctx, tx, ApproveSaleRequest{
			TenantID:   req.TenantID,
			BillID:     billID,
			CreatorID:  req.CreatorID,
			PaidAmount: recorded,
			PayType:    req.PaymentMethod,
			// The tendered cash and change are kept on the payment so a reprint from any device shows them.
			PaymentRemark: cashTenderRemark(req.PaymentMethod, req.PaidAmount, change),
			AllowNegative: source == SourcePOSOffline,
			OnNegativeStock: func(productID uuid.UUID, onHand decimal.Decimal) {
				negatives = append(negatives, negativeLine{productID: productID, onHand: onHand})
			},
		}); err != nil {
			return fmt.Errorf("quick checkout: approve: %w", err)
		}
		if rec, ok := uc.repo.(negativeStockRecorder); ok {
			for _, n := range negatives {
				if err := rec.RecordNegativeStock(ctx, tx, req.TenantID, billID, req.CreatorID, n.productID, n.onHand); err != nil {
					return fmt.Errorf("quick checkout: record negative stock: %w", err)
				}
			}
		}

		receivable := totalAmount.Sub(recorded)

		result = QuickCheckoutResult{
			BillID:           billID,
			BillNo:           billNo,
			TotalAmount:      totalAmount,
			ReceivableAmount: receivable,
			ChangeAmount:     change,
		}
		return nil
	}); err != nil {
		if req.ClientRef != "" && errors.Is(err, ErrDuplicateClientRef) {
			// Lost the race to a concurrent replay of the same checkout: report the winner.
			dup, ferr := uc.findDuplicate(ctx, req.TenantID, req.ClientRef)
			if ferr != nil {
				return nil, ferr
			}
			if dup != nil {
				return nil, dup
			}
		}
		return nil, err
	}

	return &result, nil
}

type negativeLine struct {
	productID uuid.UUID
	onHand    decimal.Decimal
}

// findDuplicate returns the booked checkout for ref as a *DuplicateCheckoutError, or nil when none.
func (uc *QuickCheckoutUseCase) findDuplicate(ctx context.Context, tenantID uuid.UUID, ref string) (*DuplicateCheckoutError, error) {
	f, ok := uc.repo.(clientRefFinder)
	if !ok {
		return nil, nil
	}
	id, no, found, err := f.FindBillByClientRef(ctx, tenantID, ref)
	if err != nil {
		return nil, fmt.Errorf("quick checkout: look up client_ref: %w", err)
	}
	if !found {
		return nil, nil
	}
	return &DuplicateCheckoutError{BillID: id, BillNo: no}, nil
}

// cashTenderRemark is the 实收/找零 line kept on a cash payment; other methods have no change.
func cashTenderRemark(method string, received, change decimal.Decimal) string {
	if (method != "" && method != "cash") || !received.IsPositive() {
		return ""
	}
	return fmt.Sprintf("实收 %s，找零 %s", received.StringFixed(2), change.StringFixed(2))
}

// assembleSaleItems converts SaleItem slice to domain.BillItem slice and returns total amount.
func assembleSaleItems(tenantID, billID uuid.UUID, items []SaleItem) ([]*domain.BillItem, decimal.Decimal, error) {
	out := make([]*domain.BillItem, 0, len(items))
	var total decimal.Decimal
	for _, it := range items {
		if it.ProductID == uuid.Nil {
			return nil, decimal.Zero, fmt.Errorf("%w: item product_id is required", ErrValidation)
		}
		if it.Qty.IsZero() || it.Qty.IsNegative() {
			return nil, decimal.Zero, fmt.Errorf("%w: item qty must be positive", ErrValidation)
		}
		if it.UnitPrice.IsNegative() {
			return nil, decimal.Zero, fmt.Errorf("%w: item unit_price must be non-negative", ErrValidation)
		}
		lineAmt := it.Qty.Mul(it.UnitPrice).Round(2)
		total = total.Add(lineAmt)
		out = append(out, &domain.BillItem{
			ID:         uuid.New(),
			TenantID:   tenantID,
			HeadID:     billID,
			ProductID:  it.ProductID,
			UnitID:     it.UnitID,
			UnitName:   it.UnitName,
			LineNo:     it.LineNo,
			Qty:        it.Qty,
			UnitPrice:  it.UnitPrice,
			LineAmount: lineAmt,
		})
	}
	return out, total.Round(2), nil
}
