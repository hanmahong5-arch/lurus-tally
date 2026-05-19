// Package warehouse contains application-layer use cases for warehouses.
package warehouse

import (
	"context"
	"errors"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/warehouse"
)

// ErrNotFound is returned when the requested warehouse does not exist.
var ErrNotFound = errors.New("warehouse not found")

// ErrDuplicateName is returned when a warehouse name already exists for the tenant.
var ErrDuplicateName = errors.New("warehouse duplicate name")

// Repository abstracts the persistence layer for Warehouse.
type Repository interface {
	Create(ctx context.Context, w *domain.Warehouse) error
	GetByID(ctx context.Context, tenantID, id uuid.UUID) (*domain.Warehouse, error)
	List(ctx context.Context, f domain.ListFilter) ([]*domain.Warehouse, int, error)
	Update(ctx context.Context, w *domain.Warehouse) error
	Delete(ctx context.Context, tenantID, id uuid.UUID) error
	Restore(ctx context.Context, tenantID, id uuid.UUID) (*domain.Warehouse, error)
}
