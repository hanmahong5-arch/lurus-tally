package bill

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

type (
	BatchOpen           = domain.BatchOpen
	BatchDrawLine       = domain.BatchDrawLine
	BatchShortError     = domain.BatchShortError
	BatchWarehouseError = domain.BatchWarehouseError
	PurchaseLine        = domain.PurchaseLine
	SupplyBill          = domain.SupplyBill
)

var ErrBatchOutOfSync = domain.ErrBatchOutOfSync

// SupplyReader is what the bill screens need to show the other end of a receipt.
type SupplyReader interface {
	OpenedSales(ctx context.Context, tenantID, purchaseID uuid.UUID) ([]SupplyBill, error)
	SourcePurchase(ctx context.Context, tenantID, saleID uuid.UUID) (*SupplyBill, error)
	BatchRemaining(ctx context.Context, tenantID uuid.UUID, itemIDs []uuid.UUID) (map[uuid.UUID]decimal.Decimal, error)
}

// ReceiptLedger records which purchase line goods came from. Nil on a use case
// means that deployment does not track receipts; stock still moves.
type ReceiptLedger interface {
	Open(ctx context.Context, tx *sqlTx, tenant, warehouse uuid.UUID, lines []BatchOpen, at time.Time) error
	Draw(ctx context.Context, tx *sqlTx, tenant, warehouse uuid.UUID, lines []BatchDrawLine, allowNegative bool) error
	Restore(ctx context.Context, tx *sqlTx, tenant, saleItem uuid.UUID, qtyBase decimal.Decimal) error
	GiveBack(ctx context.Context, tx *sqlTx, tenant, purchaseItem uuid.UUID, qtyBase decimal.Decimal) error
}

// WithBatches turns receipt tracking on for purchase approval.
func (uc *ApprovePurchaseUseCase) WithBatches(l ReceiptLedger) *ApprovePurchaseUseCase {
	uc.batches = l
	return uc
}

// WithBatches turns receipt tracking on for sale approval.
func (uc *ApproveSaleUseCase) WithBatches(l ReceiptLedger) *ApproveSaleUseCase {
	uc.batches = l
	return uc
}

// WithBatches turns receipt tracking on for a supplier return.
func (uc *PurchaseReturnUseCase) WithBatches(l ReceiptLedger) *PurchaseReturnUseCase {
	uc.batches = l
	return uc
}

// WithBatches turns receipt tracking on for a customer return.
func (uc *ApproveReturnBillUseCase) WithBatches(l ReceiptLedger) *ApproveReturnBillUseCase {
	uc.batches = l
	return uc
}
