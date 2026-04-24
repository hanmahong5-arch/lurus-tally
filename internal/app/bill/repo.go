// Package bill contains use cases for the universal bill model (purchase, sale, etc.).
// BillRepo is the persistence interface; the PG implementation lives in adapter/repo/bill.
package bill

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// BillListFilter holds query parameters for listing bills.
type BillListFilter struct {
	TenantID  uuid.UUID
	BillType  domain.BillType // empty → all types
	Status    *domain.BillStatus
	PartnerID *uuid.UUID
	DateFrom  *time.Time
	DateTo    *time.Time
	Page      int // 1-based; 0 treated as 1
	Size      int // 0 treated as 20
}

// BillRepo is the persistence interface for bill_head and bill_item operations.
// All mutating methods that accept *sql.Tx operate within the caller's transaction.
// WithTx is the boundary helper that opens/commits/rolls back a transaction.
type BillRepo interface {
	// WithTx executes fn inside a new PG transaction.
	WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error

	// CreateBill inserts bill_head and all items atomically within tx.
	CreateBill(ctx context.Context, tx *sql.Tx, head *domain.BillHead, items []*domain.BillItem) error

	// GetBillForUpdate returns the bill_head with a SELECT FOR UPDATE row lock.
	// Must be called inside a transaction (tx must not be nil).
	GetBillForUpdate(ctx context.Context, tx *sql.Tx, tenantID, billID uuid.UUID) (*domain.BillHead, error)

	// GetBill returns the bill_head without a lock.
	GetBill(ctx context.Context, tenantID, billID uuid.UUID) (*domain.BillHead, error)

	// GetBillItems returns all items for a bill ordered by line_no.
	GetBillItems(ctx context.Context, tenantID, billID uuid.UUID) ([]*domain.BillItem, error)

	// UpdateBillStatus sets status + optional approval metadata within tx.
	// meta keys: "approved_at" (time.Time), "approved_by" (uuid.UUID).
	UpdateBillStatus(ctx context.Context, tx *sql.Tx, tenantID, billID uuid.UUID, status domain.BillStatus, meta map[string]any) error

	// UpdateBill replaces subtotal/shipping_fee/tax_amount/total_amount and all items within tx.
	// Used by UpdatePurchaseDraft.
	UpdateBill(ctx context.Context, tx *sql.Tx, head *domain.BillHead, items []*domain.BillItem) error

	// ListBills returns paginated bill_head rows matching the filter, plus the total count.
	ListBills(ctx context.Context, f BillListFilter) ([]domain.BillHead, int64, error)

	// NextBillNo generates the next bill number atomically inside tx.
	// Format: {prefix}-{YYYYMMDD}-{seq:04d}  e.g. "PO-20260423-0001"
	NextBillNo(ctx context.Context, tx *sql.Tx, tenantID uuid.UUID, prefix string) (string, error)

	// AcquireBillAdvisoryLock obtains a transaction-scoped advisory lock for the given bill.
	// Used by ApprovePurchase to prevent concurrent double-approval.
	AcquireBillAdvisoryLock(ctx context.Context, tx *sql.Tx, tenantID, billID uuid.UUID) error
}
