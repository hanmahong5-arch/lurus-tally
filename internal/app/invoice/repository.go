// Package invoice contains application-layer use cases for the invoice registry.
package invoice

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domainbill "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/invoice"
)

var (
	// ErrNotFound is returned when the invoice does not exist for the tenant.
	ErrNotFound = errors.New("invoice not found")
	// ErrInvalidInput wraps a domain validation failure (handler maps to 400).
	ErrInvalidInput = errors.New("invoice invalid input")
	// ErrDuplicateNo is returned when (tenant, direction, invoice_no) is already registered (409).
	ErrDuplicateNo = errors.New("invoice duplicate number")
	// ErrConflict wraps a state conflict such as voiding an already-voided invoice (409).
	ErrConflict = errors.New("invoice state conflict")
	// ErrRule wraps a business-rule rejection on otherwise well-formed input (422).
	ErrRule = errors.New("invoice rule violation")
)

// Error is a user-facing message of one of the sentinel kinds above;
// errors.Is(err, Kind) holds and Error() is the bare Chinese message.
type Error struct {
	Kind error
	Msg  string
}

func (e *Error) Error() string        { return e.Msg }
func (e *Error) Is(target error) bool { return target == e.Kind }

func newErr(kind error, msg string) *Error { return &Error{Kind: kind, Msg: msg} }

// MessageOf returns the user-facing message carried in err's chain, if any.
func MessageOf(err error) (string, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e.Msg, true
	}
	return "", false
}

// BillRef is the slice of a bill_head row the linking rules need.
type BillRef struct {
	ID          uuid.UUID
	BillNo      string
	SubType     domainbill.BillSubType
	Status      domainbill.BillStatus
	PartnerID   *uuid.UUID
	TotalAmount decimal.Decimal
	// Currency is the bill's ISO currency; empty means CNY.
	Currency string
	// Invoiced is the amount already covered by live ('issued') invoices.
	Invoiced decimal.Decimal
}

// Repository abstracts persistence for the invoice registry. Methods taking a
// *sql.Tx run inside the caller's WithTx transaction.
type Repository interface {
	WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error

	// PartnerName returns the live partner's name, or ErrNotFound.
	PartnerName(ctx context.Context, tx *sql.Tx, tenantID, partnerID uuid.UUID) (string, error)
	// LockBills row-locks the tenant's live bills among ids (FOR UPDATE, ordered by id)
	// and reports each one's already-invoiced amount. Missing ids are simply absent.
	LockBills(ctx context.Context, tx *sql.Tx, tenantID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]BillRef, error)
	// Insert writes the invoice head and its links. A duplicate number yields ErrDuplicateNo.
	Insert(ctx context.Context, tx *sql.Tx, inv *domain.Invoice) error
	// GetForUpdate loads and row-locks one invoice (without links), or ErrNotFound.
	GetForUpdate(ctx context.Context, tx *sql.Tx, tenantID, id uuid.UUID) (*domain.Invoice, error)
	// SetStatus moves an invoice from one status to another; false when it was not in `from`.
	SetStatus(ctx context.Context, tx *sql.Tx, tenantID, id uuid.UUID, from, to domain.Status) (bool, error)

	GetByID(ctx context.Context, tenantID, id uuid.UUID) (*domain.Invoice, error)
	List(ctx context.Context, f domain.ListFilter) ([]*domain.Invoice, int, error)
	Summarize(ctx context.Context, f domain.ListFilter) (domain.Summary, error)
}
