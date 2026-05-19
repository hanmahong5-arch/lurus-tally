// Package supplier contains application-layer use cases for suppliers.
package supplier

import (
	"context"
	"errors"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/supplier"
)

// ErrNotFound is returned when the requested supplier does not exist.
var ErrNotFound = errors.New("supplier not found")

// ErrDuplicateName is returned when a supplier name already exists for the tenant.
var ErrDuplicateName = errors.New("supplier duplicate name")

// Repository abstracts the persistence layer for Supplier.
type Repository interface {
	Create(ctx context.Context, s *domain.Supplier) error
	GetByID(ctx context.Context, tenantID, id uuid.UUID) (*domain.Supplier, error)
	List(ctx context.Context, f domain.ListFilter) ([]*domain.Supplier, int, error)
	Update(ctx context.Context, s *domain.Supplier) error
	Delete(ctx context.Context, tenantID, id uuid.UUID) error
	Restore(ctx context.Context, tenantID, id uuid.UUID) (*domain.Supplier, error)
}
