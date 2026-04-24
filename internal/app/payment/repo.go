// Package payment contains use cases for payment recording and retrieval.
// PaymentRepo is the persistence interface; the PG implementation lives in adapter/repo/payment.
package payment

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domainbill "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
)

// PaymentRepo is the persistence interface for tally.payment_head operations.
type PaymentRepo interface {
	// Record inserts a new payment_head row within the caller's transaction.
	Record(ctx context.Context, tx *sql.Tx, p *domain.Payment) error

	// ListByBill returns all non-deleted payment_head rows for the given bill.
	ListByBill(ctx context.Context, tenantID, billID uuid.UUID) ([]*domain.Payment, error)

	// SumByBill returns the total paid amount for the given bill.
	// Uses SELECT ... FOR UPDATE to serialise concurrent payments.
	SumByBill(ctx context.Context, tx *sql.Tx, tenantID, billID uuid.UUID) (decimal.Decimal, error)

	// WithTx executes fn inside a new PG transaction.
	WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error
}

// BillReader is the minimal bill persistence interface needed by payment use cases.
// Defined here to break the import cycle between app/payment and app/bill.
type BillReader interface {
	// WithTx executes fn inside a new PG transaction.
	WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error

	// GetBillForUpdate returns the bill_head with a SELECT FOR UPDATE row lock.
	GetBillForUpdate(ctx context.Context, tx *sql.Tx, tenantID, billID uuid.UUID) (*domainbill.BillHead, error)

	// UpdatePaidAmount sets the bill_head.paid_amount column within tx.
	UpdatePaidAmount(ctx context.Context, tx *sql.Tx, tenantID, billID uuid.UUID, paidAmount decimal.Decimal) error
}
