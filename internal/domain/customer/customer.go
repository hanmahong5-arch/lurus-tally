// Package customer contains the domain entity for customers.
//
// Customers have no table of their own: they are rows of tally.partner with
// partner_type='customer' (reads also include 'both' and 'member', which are
// customer-facing partner kinds too). Sharing the partner table is what makes
// a customer directly referenceable as bill_head.partner_id on a sale bill.
package customer

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// PartnerType is the tally.partner.partner_type written for a new customer.
const PartnerType = "customer"

// ListPartnerTypes are the partner_type values surfaced by customer reads.
var ListPartnerTypes = []string{"customer", "both", "member"}

// Customer is the domain entity for a customer contact.
type Customer struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	Code        string
	Name        string
	ContactName string
	Mobile      string
	Phone       string
	Address     string
	Remark      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time
}

// Validate enforces domain invariants.
func (c *Customer) Validate() error {
	if strings.TrimSpace(c.Name) == "" {
		return errors.New("name is required")
	}
	return nil
}

// CreateInput carries fields for creating a new Customer.
type CreateInput struct {
	TenantID    uuid.UUID
	Code        string
	Name        string
	ContactName string
	Mobile      string
	Phone       string
	Address     string
	Remark      string
}

// ListFilter controls list queries.
type ListFilter struct {
	TenantID uuid.UUID
	Query    string // ILIKE on name, code, mobile or phone
	Limit    int
	Offset   int
}
