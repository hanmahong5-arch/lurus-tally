package bill

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// OriginRef locates the sale a return bill gives back. The importer writes it into the
// return bill's remark: cancellations carry "original_bill_id=<uuid>", partial refunds
// carry the platform and order number the sale was imported under.
type OriginRef struct {
	BillID          uuid.UUID // set when the remark names the original bill directly
	Platform        string    // set for refunds, together with PlatformOrderNo
	PlatformOrderNo string
}

const (
	originBillIDKey = "original_bill_id="
	refundIDKey     = ":refund_id="
)

// ParseOriginRef reads the original-sale pointer out of a return bill remark.
// ok is false when the remark carries none (hand-made returns).
func ParseOriginRef(remark string) (OriginRef, bool) {
	if i := strings.LastIndex(remark, originBillIDKey); i >= 0 {
		raw := strings.TrimSpace(remark[i+len(originBillIDKey):])
		if id, err := uuid.Parse(raw); err == nil {
			return OriginRef{BillID: id}, true
		}
		return OriginRef{}, false
	}
	// "refund:<platform>:<order_no>:refund_id=<id>"; the order number may itself hold ':'.
	if rest, found := strings.CutPrefix(remark, "refund:"); found {
		platform, tail, ok := strings.Cut(rest, ":")
		end := strings.LastIndex(tail, refundIDKey)
		if ok && end > 0 && platform != "" {
			return OriginRef{Platform: platform, PlatformOrderNo: tail[:end]}, true
		}
	}
	return OriginRef{}, false
}

// returnCostSource is an optional capability of BillRepo: it answers "what did the goods
// cost when they left", so a return restores stock at that cost instead of the refund
// price. The production repo implements it; test doubles that do not fall back to zero cost.
type returnCostSource interface {
	// LinkedBillID returns bill_head.link_bill_id of billID, when set.
	LinkedBillID(ctx context.Context, tenantID, billID uuid.UUID) (uuid.UUID, bool, error)
	// ImportedOrderBillID resolves the sale bill an imported platform order became.
	ImportedOrderBillID(ctx context.Context, tenantID uuid.UUID, platform, orderNo string) (uuid.UUID, bool, error)
	// SaleOutUnitCost is the quantity-weighted per-base-unit cost of the stock-out
	// movements the sale bill booked for productID.
	SaleOutUnitCost(ctx context.Context, tenantID, saleBillID, productID uuid.UUID) (decimal.Decimal, bool, error)
	// SnapshotUnitCost is the current average cost of productID in the warehouse.
	SnapshotUnitCost(ctx context.Context, tenantID, productID, warehouseID uuid.UUID) (decimal.Decimal, bool, error)
}

// resolveOriginBill finds the sale bill the return head points back to, or uuid.Nil.
func resolveOriginBill(ctx context.Context, src returnCostSource, head *domain.BillHead) (uuid.UUID, error) {
	if id, ok, err := src.LinkedBillID(ctx, head.TenantID, head.ID); err != nil {
		return uuid.Nil, fmt.Errorf("resolve linked bill: %w", err)
	} else if ok {
		return id, nil
	}
	ref, ok := ParseOriginRef(head.Remark)
	if !ok {
		return uuid.Nil, nil
	}
	if ref.BillID != uuid.Nil {
		return ref.BillID, nil
	}
	id, found, err := src.ImportedOrderBillID(ctx, head.TenantID, ref.Platform, ref.PlatformOrderNo)
	if err != nil {
		return uuid.Nil, fmt.Errorf("resolve imported order bill: %w", err)
	}
	if !found {
		return uuid.Nil, nil
	}
	return id, nil
}
