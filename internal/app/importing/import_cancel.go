package importing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// CancelRequest is the input to IngestCancelOrder.
type CancelRequest struct {
	TenantID        uuid.UUID
	CreatorID       uuid.UUID
	Platform        Platform
	PlatformOrderNo string
}

// CancelResult is returned on successful order cancellation.
type CancelResult struct {
	PlatformOrderNo  string
	OriginalBillID   uuid.UUID
	ReversalBillID   uuid.UUID
	ReversalBillNo   string
	NothingToReverse bool
}

// ErrNothingToReverse is returned when a cancelled or refunded order has no stock left to give back.
var ErrNothingToReverse = errors.New("importing: nothing to reverse")

// IngestCancelOrder processes an orders/cancelled webhook event.
func (uc *ImportOrdersUseCase) IngestCancelOrder(ctx context.Context, req CancelRequest) (*CancelResult, error) {
	if req.TenantID == uuid.Nil {
		return nil, fmt.Errorf("importing: tenant_id is required")
	}
	if req.CreatorID == uuid.Nil {
		return nil, fmt.Errorf("importing: creator_id is required")
	}
	if err := req.Platform.Validate(); err != nil {
		return nil, err
	}
	if req.PlatformOrderNo == "" {
		return nil, fmt.Errorf("importing: platform_order_no is required")
	}
	if uc.returnCreator == nil || uc.returnApprover == nil {
		return nil, fmt.Errorf("importing: ReturnCreator/ReturnApprover not wired; call WithReturnHandlers")
	}

	alreadyCancelled, origBillID, revBillID, err := uc.repo.IsCancelSeen(ctx, req.TenantID, string(req.Platform), req.PlatformOrderNo)
	if err != nil {
		return nil, fmt.Errorf("importing: cancel dedup check for order %s: %w", req.PlatformOrderNo, err)
	}
	if alreadyCancelled {
		res := &CancelResult{
			PlatformOrderNo:  req.PlatformOrderNo,
			OriginalBillID:   origBillID,
			ReversalBillID:   revBillID,
			NothingToReverse: revBillID == uuid.Nil,
		}
		if lk, ok := uc.repo.(reversalBillNoLookup); ok && revBillID != uuid.Nil {
			if no, lerr := lk.ReversalBillNo(ctx, req.TenantID, revBillID); lerr == nil {
				res.ReversalBillNo = no
			}
		}
		return res, nil
	}

	seen, originalBillID, err := uc.repo.IsOrderSeen(ctx, req.TenantID, string(req.Platform), req.PlatformOrderNo)
	if err != nil {
		return nil, fmt.Errorf("importing: lookup original order %s: %w", req.PlatformOrderNo, err)
	}
	if !seen {
		if merr := uc.repo.MarkCancelSeen(ctx, req.TenantID, string(req.Platform), req.PlatformOrderNo, uuid.Nil, uuid.Nil); merr != nil {
			return nil, fmt.Errorf("importing: mark_cancel_seen for unknown order %s: %w", req.PlatformOrderNo, merr)
		}
		return &CancelResult{PlatformOrderNo: req.PlatformOrderNo, NothingToReverse: true}, nil
	}

	remark := fmt.Sprintf("cancel:%s:%s:original_bill_id=%s", req.Platform, req.PlatformOrderNo, originalBillID)
	out, err := uc.returnCreator.Create(ctx, ReturnCreatorInput{
		TenantID:  req.TenantID,
		CreatorID: req.CreatorID,
		BillDate:  time.Now().UTC(),
		Remark:    remark,
		Items:     nil,
	})
	if errors.Is(err, ErrNothingToReverse) {
		return uc.cancelNothingToReverse(ctx, req, originalBillID)
	}
	if err != nil {
		return nil, fmt.Errorf("importing: create reversal bill for order %s: %w", req.PlatformOrderNo, err)
	}

	if err := uc.returnApprover.Approve(ctx, ReturnApproverInput{
		TenantID:  req.TenantID,
		BillID:    out.BillID,
		CreatorID: req.CreatorID,
	}); err != nil {
		if errors.Is(err, ErrNothingToReverse) {
			return uc.cancelNothingToReverse(ctx, req, originalBillID)
		}
		return nil, fmt.Errorf("importing: approve reversal bill for order %s: %w", req.PlatformOrderNo, err)
	}

	if err := uc.repo.MarkCancelSeen(ctx, req.TenantID, string(req.Platform), req.PlatformOrderNo, originalBillID, out.BillID); err != nil {
		return &CancelResult{
			PlatformOrderNo: req.PlatformOrderNo,
			OriginalBillID:  originalBillID,
			ReversalBillID:  out.BillID,
			ReversalBillNo:  out.BillNo,
		}, fmt.Errorf("importing: mark_cancel_seen failed (reversal committed): %w", err)
	}

	return &CancelResult{
		PlatformOrderNo: req.PlatformOrderNo,
		OriginalBillID:  originalBillID,
		ReversalBillID:  out.BillID,
		ReversalBillNo:  out.BillNo,
	}, nil
}

// cancelNothingToReverse records a cancellation that restored no stock and returns success.
func (uc *ImportOrdersUseCase) cancelNothingToReverse(ctx context.Context, req CancelRequest, originalBillID uuid.UUID) (*CancelResult, error) {
	if merr := uc.repo.MarkCancelSeen(ctx, req.TenantID, string(req.Platform), req.PlatformOrderNo, originalBillID, uuid.Nil); merr != nil {
		return nil, fmt.Errorf("importing: mark_cancel_seen for order %s: %w", req.PlatformOrderNo, merr)
	}
	return &CancelResult{
		PlatformOrderNo:  req.PlatformOrderNo,
		OriginalBillID:   originalBillID,
		NothingToReverse: true,
	}, nil
}
