package bill

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// ErrCannotRestoreApproved is returned when attempting to restore an approved purchase bill.
// Approved bills require a purchase-return flow, not a status rollback.
var ErrCannotRestoreApproved = errors.New("bill: cannot restore an approved bill")

// RestorePurchaseUseCase sets a cancelled purchase bill back to draft status.
// The operation is idempotent: restoring an already-draft bill returns nil.
type RestorePurchaseUseCase struct {
	repo BillRepo
}

// NewRestorePurchaseUseCase constructs the use case.
func NewRestorePurchaseUseCase(repo BillRepo) *RestorePurchaseUseCase {
	return &RestorePurchaseUseCase{repo: repo}
}

// Execute restores the bill to draft. Returns:
//   - nil when the bill is already in draft (idempotent).
//   - ErrCannotRestoreApproved when the bill is approved.
//   - ErrBillNotFound when the bill does not exist.
func (uc *RestorePurchaseUseCase) Execute(ctx context.Context, tenantID, billID uuid.UUID) error {
	if tenantID == uuid.Nil {
		return fmt.Errorf("restore purchase: tenant_id is required")
	}
	if billID == uuid.Nil {
		return fmt.Errorf("restore purchase: bill_id is required")
	}

	return uc.repo.WithTx(ctx, func(tx *sql.Tx) error {
		head, err := uc.repo.GetBillForUpdate(ctx, tx, tenantID, billID)
		if err != nil {
			return fmt.Errorf("restore purchase: %w", err)
		}

		if head.Status == domain.StatusApproved {
			return fmt.Errorf("%w: approved bills require a purchase-return flow", ErrCannotRestoreApproved)
		}

		if head.Status == domain.StatusDraft {
			// Already in draft — idempotent no-op.
			return nil
		}

		// Only cancelled bills can be restored.
		if head.Status != domain.StatusCancelled {
			return fmt.Errorf("restore purchase: %w: current status is %d", ErrInvalidBillStatus, head.Status)
		}

		if err := uc.repo.UpdateBillStatus(ctx, tx, tenantID, billID, domain.StatusDraft, nil); err != nil {
			return fmt.Errorf("restore purchase: update status: %w", err)
		}
		return nil
	})
}
