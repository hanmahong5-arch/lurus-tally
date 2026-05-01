// Package horticulture contains application-layer use cases for the nursery dictionary.
package horticulture

import (
	"context"
	"errors"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/horticulture"
)

// ErrNotFound is returned when the requested nursery dict entry does not exist.
var ErrNotFound = errors.New("nursery dict entry not found")

// ErrDuplicateName is returned when a name already exists for the tenant.
var ErrDuplicateName = errors.New("nursery dict duplicate name")

// Repository abstracts the persistence layer for NurseryDict.
type Repository interface {
	Create(ctx context.Context, d *domain.NurseryDict) error
	GetByID(ctx context.Context, tenantID, id uuid.UUID) (*domain.NurseryDict, error)
	List(ctx context.Context, f domain.ListFilter) ([]*domain.NurseryDict, int, error)
	Update(ctx context.Context, d *domain.NurseryDict) error
	Delete(ctx context.Context, tenantID, id uuid.UUID) error
	Restore(ctx context.Context, tenantID, id uuid.UUID) (*domain.NurseryDict, error)
}
