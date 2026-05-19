// Package warehouse contains the domain entity for warehouses.
package warehouse

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Warehouse is the domain entity for a physical or virtual storage location.
type Warehouse struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	Code      string
	Name      string
	Address   string
	Manager   string
	IsDefault bool
	Remark    string
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

// Validate enforces domain invariants.
func (w *Warehouse) Validate() error {
	if w.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

// CreateInput carries fields for creating a new Warehouse.
type CreateInput struct {
	TenantID  uuid.UUID
	Code      string
	Name      string
	Address   string
	Manager   string
	IsDefault bool
	Remark    string
}

// UpdateInput carries mutable fields (nil pointer = do not update).
type UpdateInput struct {
	Code      *string
	Name      *string
	Address   *string
	Manager   *string
	IsDefault *bool
	Remark    *string
}

// ListFilter controls list queries.
type ListFilter struct {
	TenantID uuid.UUID
	Query    string // ILIKE on name or code
	Limit    int
	Offset   int
}
