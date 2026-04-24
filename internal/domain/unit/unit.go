// Package unit contains domain entities for the unit catalogue (unit_def + product_unit).
package unit

import (
	"time"

	"github.com/google/uuid"
)

// UnitType constrains what dimension a unit measures.
type UnitType string

const (
	UnitTypeCount  UnitType = "count"
	UnitTypeWeight UnitType = "weight"
	UnitTypeLength UnitType = "length"
	UnitTypeVolume UnitType = "volume"
	UnitTypeArea   UnitType = "area"
	UnitTypeTime   UnitType = "time"
)

// UnitDef is a row in the unit_def table.
// System units (is_system = true) have TenantID = uuid.Nil and are visible to all tenants.
type UnitDef struct {
	ID        uuid.UUID `json:"id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	UnitType  UnitType  `json:"unit_type"`
	IsSystem  bool      `json:"is_system"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ProductUnit maps one unit_def to one product with a conversion factor relative to the base unit.
// Exactly one ProductUnit per product must have IsBase = true; conversion_factor for the base unit is 1.
type ProductUnit struct {
	ID                uuid.UUID `json:"id"`
	TenantID          uuid.UUID `json:"tenant_id"`
	ProductID         uuid.UUID `json:"product_id"`
	UnitID            uuid.UUID `json:"unit_id"`
	ConversionFactor  string    `json:"conversion_factor"` // NUMERIC as string to avoid float
	IsBase            bool      `json:"is_base"`
	IsDefaultSale     bool      `json:"is_default_sale"`
	IsDefaultPurchase bool      `json:"is_default_purchase"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// CreateInput carries the fields needed to define a custom tenant unit.
type CreateInput struct {
	TenantID uuid.UUID
	Code     string
	Name     string
	UnitType UnitType
}

// ListFilter contains optional query parameters for listing unit_defs.
type ListFilter struct {
	TenantID uuid.UUID
	UnitType UnitType // empty = all types
}
