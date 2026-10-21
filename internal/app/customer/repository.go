// Package customer contains application-layer use cases for customers.
package customer

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/customer"
)

// ErrNotFound is returned when the requested customer does not exist.
var ErrNotFound = errors.New("customer not found")

// ErrInvalidInput wraps a domain validation failure so the handler can map it
// to 400 without string matching. Persistence failures (including a unique
// violation on the per-tenant code) are wrapped with %w instead and keep their
// driver error in the chain for httperr's classification.
var ErrInvalidInput = errors.New("customer invalid input")

// ErrDuplicateName / ErrDuplicateMobile are matched with errors.Is against a
// *DuplicateError; use errors.As to read the existing customer behind it.
var (
	ErrDuplicateName   = errors.New("customer duplicate name")
	ErrDuplicateMobile = errors.New("customer duplicate mobile")
)

// DuplicateError reports an active customer that already owns the submitted
// name or mobile. Kind is ErrDuplicateName or ErrDuplicateMobile.
type DuplicateError struct {
	Kind         error
	ExistingID   uuid.UUID
	ExistingName string
}

func (e *DuplicateError) Error() string {
	return fmt.Sprintf("%v: %q (%s)", e.Kind, e.ExistingName, e.ExistingID)
}

// Is lets errors.Is(err, ErrDuplicateName) see through the wrapper.
func (e *DuplicateError) Is(target error) bool { return target == e.Kind }

// Repository abstracts the persistence layer for Customer.
type Repository interface {
	Create(ctx context.Context, c *domain.Customer) error
	GetByID(ctx context.Context, tenantID, id uuid.UUID) (*domain.Customer, error)
	List(ctx context.Context, f domain.ListFilter) ([]*domain.Customer, int, error)
	// FindActiveByName returns the oldest live customer whose name equals name
	// case-insensitively after trimming and collapsing whitespace (the caller
	// passes an already-normalised name), or ErrNotFound.
	FindActiveByName(ctx context.Context, tenantID uuid.UUID, name string) (*domain.Customer, error)
	// FindActiveByMobile returns the oldest live customer with exactly this
	// mobile, or ErrNotFound.
	FindActiveByMobile(ctx context.Context, tenantID uuid.UUID, mobile string) (*domain.Customer, error)
}
