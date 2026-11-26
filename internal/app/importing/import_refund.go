package importing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// RefundLine is one SKU line in a partial or full refund.
type RefundLine struct {
	PlatformSKU  string
	Qty          decimal.Decimal
	RefundAmount decimal.Decimal
}

// RefundRequest is the input to IngestRefund.
type RefundRequest struct {
	TenantID         uuid.UUID
	CreatorID        uuid.UUID
	WarehouseID      uuid.UUID
	Platform         Platform
	PlatformOrderNo  string
	PlatformRefundID string
	Currency         string
	RefundDate       time.Time
	Lines            []RefundLine
}

// RefundResult is returned on successful refund ingestion.
type RefundResult struct {
	PlatformOrderNo  string
	PlatformRefundID string
	BillID           uuid.UUID
	BillNo           string
	NothingToReverse bool
}

// IngestRefund processes a refunds/create webhook event for full or partial refunds.
func (uc *ImportOrdersUseCase) IngestRefund(ctx context.Context, req RefundRequest) (*RefundResult, error) {
	if err := uc.validateRefundRequest(req); err != nil {
		return nil, err
	}

	alreadySeen, existingBillID, err := uc.repo.IsRefundSeen(ctx, req.TenantID, string(req.Platform), req.PlatformRefundID)
	if err != nil {
		return nil, fmt.Errorf("importing: refund dedup check for %s: %w", req.PlatformRefundID, err)
	}
	if alreadySeen {
		res := &RefundResult{
			PlatformOrderNo:  req.PlatformOrderNo,
			PlatformRefundID: req.PlatformRefundID,
			BillID:           existingBillID,
			NothingToReverse: existingBillID == uuid.Nil,
		}
		if lk, ok := uc.repo.(reversalBillNoLookup); ok && existingBillID != uuid.Nil {
			if no, lerr := lk.ReversalBillNo(ctx, req.TenantID, existingBillID); lerr == nil {
				res.BillNo = no
			}
		}
		return res, nil
	}

	if seen, _, serr := uc.repo.IsOrderSeen(ctx, req.TenantID, string(req.Platform), req.PlatformOrderNo); serr != nil {
		return nil, fmt.Errorf("importing: lookup order %s for refund: %w", req.PlatformOrderNo, serr)
	} else if !seen {
		return &RefundResult{
			PlatformOrderNo:  req.PlatformOrderNo,
			PlatformRefundID: req.PlatformRefundID,
			NothingToReverse: true,
		}, nil
	}

	items, err := uc.resolveRefundItems(ctx, req)
	if err != nil {
		return nil, err
	}

	warehouseID := req.WarehouseID
	remark := fmt.Sprintf("refund:%s:%s:refund_id=%s", req.Platform, req.PlatformOrderNo, req.PlatformRefundID)
	out, err := uc.returnCreator.Create(ctx, ReturnCreatorInput{
		TenantID:    req.TenantID,
		CreatorID:   req.CreatorID,
		WarehouseID: &warehouseID,
		BillDate:    req.RefundDate,
		Remark:      remark,
		Items:       items,
	})
	if errors.Is(err, ErrNothingToReverse) {
		return uc.refundNothingToReverse(ctx, req)
	}
	if err != nil {
		return nil, fmt.Errorf("importing: create refund bill for %s: %w", req.PlatformRefundID, err)
	}

	if err := uc.returnApprover.Approve(ctx, ReturnApproverInput{
		TenantID:  req.TenantID,
		BillID:    out.BillID,
		CreatorID: req.CreatorID,
	}); err != nil {
		if errors.Is(err, ErrNothingToReverse) {
			return uc.refundNothingToReverse(ctx, req)
		}
		return nil, fmt.Errorf("importing: approve refund bill for %s: %w", req.PlatformRefundID, err)
	}

	if err := uc.repo.MarkRefundSeen(ctx, req.TenantID, string(req.Platform), req.PlatformOrderNo, req.PlatformRefundID, out.BillID); err != nil {
		return &RefundResult{
			PlatformOrderNo:  req.PlatformOrderNo,
			PlatformRefundID: req.PlatformRefundID,
			BillID:           out.BillID,
			BillNo:           out.BillNo,
		}, fmt.Errorf("importing: mark_refund_seen failed (bill committed): %w", err)
	}

	return &RefundResult{
		PlatformOrderNo:  req.PlatformOrderNo,
		PlatformRefundID: req.PlatformRefundID,
		BillID:           out.BillID,
		BillNo:           out.BillNo,
	}, nil
}

// validateRefundRequest validates required fields and return wiring for IngestRefund.
func (uc *ImportOrdersUseCase) validateRefundRequest(req RefundRequest) error {
	if req.TenantID == uuid.Nil {
		return fmt.Errorf("importing: tenant_id is required")
	}
	if req.CreatorID == uuid.Nil {
		return fmt.Errorf("importing: creator_id is required")
	}
	if req.WarehouseID == uuid.Nil {
		return fmt.Errorf("importing: warehouse_id is required")
	}
	if err := req.Platform.Validate(); err != nil {
		return err
	}
	if req.PlatformOrderNo == "" {
		return fmt.Errorf("importing: platform_order_no is required")
	}
	if req.PlatformRefundID == "" {
		return fmt.Errorf("importing: platform_refund_id is required")
	}
	if len(req.Lines) == 0 {
		return fmt.Errorf("importing: refund has no lines")
	}
	if uc.returnCreator == nil || uc.returnApprover == nil {
		return fmt.Errorf("importing: ReturnCreator/ReturnApprover not wired; call WithReturnHandlers")
	}
	return nil
}

// resolveRefundItems resolves SKUs and converts FX for each RefundLine.
func (uc *ImportOrdersUseCase) resolveRefundItems(ctx context.Context, req RefundRequest) ([]SaleLineItem, error) {
	emptyHints := map[string]uuid.UUID{}
	items := make([]SaleLineItem, 0, len(req.Lines))
	for i, rl := range req.Lines {
		productID, unknown, resolveErr := uc.resolveSKU(ctx, req.TenantID, string(req.Platform), rl.PlatformSKU, emptyHints)
		if resolveErr != nil {
			return nil, fmt.Errorf("importing: refund resolve sku %s: %w", rl.PlatformSKU, resolveErr)
		}
		if unknown {
			return nil, fmt.Errorf("importing: refund sku %s not in import_sku_map; map it before re-sending", rl.PlatformSKU)
		}
		price, fxErr := uc.convertPriceFX(ctx, req.TenantID, rl.RefundAmount, req.Currency, req.RefundDate)
		if fxErr != nil {
			return nil, fmt.Errorf("importing: fx for refund %s: %w", req.PlatformRefundID, fxErr)
		}
		items = append(items, SaleLineItem{
			ProductID: productID,
			LineNo:    i + 1,
			Qty:       rl.Qty,
			UnitPrice: price,
		})
	}
	return items, nil
}

// refundNothingToReverse records a refund that restored no stock and returns success.
func (uc *ImportOrdersUseCase) refundNothingToReverse(ctx context.Context, req RefundRequest) (*RefundResult, error) {
	if merr := uc.repo.MarkRefundSeen(ctx, req.TenantID, string(req.Platform), req.PlatformOrderNo, req.PlatformRefundID, uuid.Nil); merr != nil {
		return nil, fmt.Errorf("importing: mark_refund_seen for %s: %w", req.PlatformRefundID, merr)
	}
	return &RefundResult{
		PlatformOrderNo:  req.PlatformOrderNo,
		PlatformRefundID: req.PlatformRefundID,
		NothingToReverse: true,
	}, nil
}
