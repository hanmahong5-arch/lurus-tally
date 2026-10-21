// Package bill contains use cases for the universal bill model (purchase, sale, etc.).
// BillRepo is the persistence interface; the PG implementation lives in adapter/repo/bill.
package bill

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// BillListFilter holds query parameters for listing bills.
type BillListFilter struct {
	TenantID  uuid.UUID
	BillType  domain.BillType    // empty → all types
	SubType   domain.BillSubType // empty → all sub types
	Status    *domain.BillStatus
	PartnerID *uuid.UUID
	DateFrom  *time.Time // inclusive lower bound on bill_date
	// DateTo is an EXCLUSIVE upper bound (bill_date < DateTo): callers pass the start
	// of the day after the last day to include, so a whole last day is covered.
	DateTo *time.Time
	// PaymentStatus is one of unpaid / partial / paid / open (open = unpaid + partial).
	// A non-empty value restricts the result to approved bills.
	PaymentStatus string
	// InvoiceStatus is one of none / partial / full (derived from live invoice links).
	// A non-empty value restricts the result to approved bills.
	InvoiceStatus string
	// Keyword matches bill_no or partner name case-insensitively (substring).
	Keyword string
	// CreatorID, when set, keeps only bills created by that user.
	CreatorID *uuid.UUID
	// OwnRecentApprovedSince, when set, replaces Status with "draft, or approved at or after
	// this time" (a price-blind person's own list keeps what was just approved).
	OwnRecentApprovedSince *time.Time
	// OldestFirst lists by bill_date ascending (sort=bill_date_asc); default is newest first.
	OldestFirst bool
	Page        int // 1-based; 0 treated as 1
	Size        int // 0 treated as 20
}

// BillListSummary aggregates every bill matching a BillListFilter (ignoring paging).
// Amounts are decimal strings straight from NUMERIC.
// The count and all amounts cover approved bills only (drafts and cancelled bills are left
// out). ReturnedAmount is what sales returns take off the sale totals (zero for purchases),
// so TotalAmount - ReturnedAmount - PaidAmount = UnpaidAmount.
type BillListSummary struct {
	Count          int64           `json:"count"`
	TotalAmount    decimal.Decimal `json:"total_amount"`
	PaidAmount     decimal.Decimal `json:"paid_amount"`
	ReturnedAmount decimal.Decimal `json:"returned_amount"`
	UnpaidAmount   decimal.Decimal `json:"unpaid_amount"`
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

	// SummarizeBills aggregates the bills matching f (same WHERE as ListBills, no paging).
	SummarizeBills(ctx context.Context, f BillListFilter) (BillListSummary, error)

	// NextBillNo generates the next bill number atomically inside tx.
	// Format: {prefix}-{YYYYMMDD}-{seq:04d}  e.g. "PO-20260423-0001"
	NextBillNo(ctx context.Context, tx *sql.Tx, tenantID uuid.UUID, prefix string) (string, error)

	// AcquireBillAdvisoryLock obtains a transaction-scoped advisory lock for the given bill.
	// Used by ApprovePurchase to prevent concurrent double-approval.
	AcquireBillAdvisoryLock(ctx context.Context, tx *sql.Tx, tenantID, billID uuid.UUID) error

	// UpdatePaidAmount sets the bill_head.paid_amount column within tx.
	// Replaces the current value (caller should pass the new cumulative total).
	UpdatePaidAmount(ctx context.Context, tx *sql.Tx, tenantID, billID uuid.UUID, paidAmount decimal.Decimal) error

	// ProductExists reports whether productID is an active (non-deleted) product
	// owned by tenantID. Bill creation calls this to reject references to a
	// missing or cross-tenant product before insert: the bill_item.product_id FK
	// guarantees the row exists but FK validation bypasses RLS, so it would
	// otherwise accept another tenant's product id.
	ProductExists(ctx context.Context, tenantID, productID uuid.UUID) (bool, error)

	// WarehouseExists reports whether warehouseID is an active (non-deleted)
	// warehouse owned by tenantID. Same cross-tenant rationale as ProductExists.
	WarehouseExists(ctx context.Context, tenantID, warehouseID uuid.UUID) (bool, error)

	// PartnerExists reports whether id is an active (non-deleted) partner owned by
	// tenantID. Same cross-tenant rationale as ProductExists.
	PartnerExists(ctx context.Context, tenantID, id uuid.UUID) (bool, error)

	// DefaultWarehouseID resolves the warehouse used when a bill carries none: the
	// tenant's active default warehouse, or the only active warehouse when no default
	// is flagged. Returns false when neither exists.
	DefaultWarehouseID(ctx context.Context, tenantID uuid.UUID) (uuid.UUID, bool, error)

	// SetBillWarehouse writes bill_head.warehouse_id within tx.
	SetBillWarehouse(ctx context.Context, tx *sql.Tx, tenantID, billID, warehouseID uuid.UUID) error
}
