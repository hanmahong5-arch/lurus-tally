// Package supplier contains the domain entity for suppliers.
package supplier

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Supplier is the domain entity for a supplier contact.
type Supplier struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	Code      string
	Name      string
	Contact   string
	Phone     string
	Email     string
	Address   string
	Remark    string
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

// Validate enforces domain invariants.
func (s *Supplier) Validate() error {
	if s.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

// CreateInput carries fields for creating a new Supplier.
type CreateInput struct {
	TenantID uuid.UUID
	Code     string
	Name     string
	Contact  string
	Phone    string
	Email    string
	Address  string
	Remark   string
}

// UpdateInput carries mutable fields (nil pointer = do not update).
type UpdateInput struct {
	Code    *string
	Name    *string
	Contact *string
	Phone   *string
	Email   *string
	Address *string
	Remark  *string
}

// ListFilter controls list queries.
type ListFilter struct {
	TenantID uuid.UUID
	Query    string // ILIKE on name or code
	Limit    int
	Offset   int
}
