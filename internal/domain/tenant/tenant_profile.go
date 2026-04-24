// Package tenant contains domain entities for tenant management.
package tenant

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// ProfileType represents the business profile chosen by the tenant owner.
type ProfileType string

const (
	ProfileTypeCrossBorder ProfileType = "cross_border"
	ProfileTypeRetail      ProfileType = "retail"
	ProfileTypeHybrid      ProfileType = "hybrid" // reserved for admin backend; not user-selectable
)

// InventoryMethod represents the costing/inventory valuation method.
type InventoryMethod string

const (
	InventoryMethodFIFO        InventoryMethod = "fifo"
	InventoryMethodWAC         InventoryMethod = "wac"
	InventoryMethodByWeight    InventoryMethod = "by_weight"
	InventoryMethodBatch       InventoryMethod = "batch"
	InventoryMethodBulkMerged  InventoryMethod = "bulk_merged"
)

// defaultInventoryMethod returns the recommended costing method for the given profile.
func defaultInventoryMethod(p ProfileType) InventoryMethod {
	switch p {
	case ProfileTypeCrossBorder:
		return InventoryMethodFIFO
	default:
		return InventoryMethodWAC
	}
}

// ErrProfileAlreadySet is returned when a tenant attempts to choose a profile that is already set.
var ErrProfileAlreadySet = errors.New("tenant profile already set")

// ErrInvalidProfileType is returned when an unsupported profile_type is provided.
var ErrInvalidProfileType = errors.New("invalid profile type: must be 'cross_border' or 'retail'")

// TenantProfile is the profile record for a tenant.
type TenantProfile struct {
	ID              uuid.UUID       `json:"id"`
	TenantID        uuid.UUID       `json:"tenant_id"`
	ProfileType     ProfileType     `json:"profile_type"`
	InventoryMethod InventoryMethod `json:"inventory_method"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// NewTenantProfile builds a new TenantProfile with sensible defaults for the given type.
// It validates that profile_type is one of the user-selectable values (not hybrid).
func NewTenantProfile(tenantID uuid.UUID, pt ProfileType) (*TenantProfile, error) {
	if pt != ProfileTypeCrossBorder && pt != ProfileTypeRetail {
		return nil, ErrInvalidProfileType
	}
	now := time.Now().UTC()
	return &TenantProfile{
		ID:              uuid.New(),
		TenantID:        tenantID,
		ProfileType:     pt,
		InventoryMethod: defaultInventoryMethod(pt),
		CreatedAt:       now,
		UpdatedAt:       now,
	}, nil
}

// UserIdentityMapping maps a Zitadel OIDC sub to an internal tally tenant.
type UserIdentityMapping struct {
	ID          uuid.UUID `json:"id"`
	TenantID    uuid.UUID `json:"tenant_id"`
	ZitadelSub  string    `json:"zitadel_sub"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Role        string    `json:"role"`
	IsOwner     bool      `json:"is_owner"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
