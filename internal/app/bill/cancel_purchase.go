package bill

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// CancelPurchaseUseCase cancels a draft purchase bill.
// Approved bills cannot be cancelled; they require a purchase-return flow.
type CancelPurchaseUseCase struct {
	repo BillRepo
}

// NewCancelPurchaseUseCase constructs the use case.
func NewCancelPurchaseUseCase(repo BillRepo) *CancelPurchaseUseCase {
	return &CancelPurchaseUseCase{repo: repo}
}

// Execute cancels the given bill. Returns ErrCannotCancelApproved when the bill is approved.
func (uc *CancelPurchaseUseCase) Execute(ctx context.Context, tenantID, billID uuid.UUID) error {
	if tenantID == uuid.Nil {
		return fmt.Errorf("cancel purchase: tenant_id is required")
	}
	if billID == uuid.Nil {
		return fmt.Errorf("cancel purchase: bill_id is required")
	}

	return uc.repo.WithTx(ctx, func(tx *sql.Tx) error {
		head, err := uc.repo.GetBillForUpdate(ctx, tx, tenantID, billID)
		if err != nil {
			return fmt.Errorf("cancel purchase: %w", err)
		}

		if head.Status == domain.StatusApproved {
			return fmt.Errorf("%w: approved bills require a purchase-return flow; action: POST /api/v1/purchase-bills/%s/return", ErrCannotCancelApproved, billID)
		}
		if head.Status == domain.StatusCancelled {
			// Already cancelled — idempotent no-op.
			return nil
		}
		if !head.Status.CanTransitionTo(domain.StatusCancelled) {
			return fmt.Errorf("cancel purchase: %w: current status is %d", ErrInvalidBillStatus, head.Status)
		}

		if err := uc.repo.UpdateBillStatus(ctx, tx, tenantID, billID, domain.StatusCancelled, nil); err != nil {
			return fmt.Errorf("cancel purchase: update status: %w", err)
		}
		return nil
	})
}
