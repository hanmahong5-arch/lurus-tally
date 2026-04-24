// Package product contains domain entities and value objects for the product catalogue.
package product

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Product is the central entity in the product catalogue.
// All amounts/quantities are stored in the base unit defined by product_unit.is_base.
// The attributes JSONB field stores profile-specific fields:
//   - cross_border: hs_code, en_name, origin
//   - retail: is_bulk, allow_credit
type Product struct {
	ID                  uuid.UUID           `json:"id"`
	TenantID            uuid.UUID           `json:"tenant_id"`
	CategoryID          *uuid.UUID          `json:"category_id,omitempty"`
	Code                string              `json:"code"`
	Name                string              `json:"name"`
	Manufacturer        string              `json:"manufacturer,omitempty"`
	Model               string              `json:"model,omitempty"`
	Spec                string              `json:"spec,omitempty"`
	Brand               string              `json:"brand,omitempty"`
	Mnemonic            string              `json:"mnemonic,omitempty"`
	Color               string              `json:"color,omitempty"`
	ExpiryDays          *int                `json:"expiry_days,omitempty"`
	WeightKg            *string             `json:"weight_kg,omitempty"` // NUMERIC as string to avoid float
	Enabled             bool                `json:"enabled"`
	EnableSerialNo      bool                `json:"enable_serial_no"`
	EnableLotNo         bool                `json:"enable_lot_no"`
	ShelfPosition       string              `json:"shelf_position,omitempty"`
	ImgURLs             []string            `json:"img_urls,omitempty"`
	Remark              string              `json:"remark,omitempty"`
	MeasurementStrategy MeasurementStrategy `json:"measurement_strategy"`
	DefaultUnitID       *uuid.UUID          `json:"default_unit_id,omitempty"`
	Attributes          json.RawMessage     `json:"attributes"`
	CreatedAt           time.Time           `json:"created_at"`
	UpdatedAt           time.Time           `json:"updated_at"`
}

// CreateInput carries all fields required to create a new product.
// Code must be unique per tenant (enforced by DB UNIQUE index).
type CreateInput struct {
	TenantID            uuid.UUID
	CategoryID          *uuid.UUID
	Code                string
	Name                string
	Manufacturer        string
	Model               string
	Spec                string
	Brand               string
	Mnemonic            string
	Color               string
	ExpiryDays          *int
	WeightKg            *string
	EnableSerialNo      bool
	EnableLotNo         bool
	ShelfPosition       string
	ImgURLs             []string
	Remark              string
	MeasurementStrategy MeasurementStrategy
	DefaultUnitID       *uuid.UUID
	Attributes          json.RawMessage
}

// UpdateInput carries mutable fields for an existing product.
// Zero values are treated as "no change" by the use case layer.
type UpdateInput struct {
	CategoryID          *uuid.UUID
	Name                string
	Manufacturer        string
	Model               string
	Spec                string
	Brand               string
	Mnemonic            string
	Color               string
	ExpiryDays          *int
	WeightKg            *string
	Enabled             *bool
	EnableSerialNo      *bool
	EnableLotNo         *bool
	ShelfPosition       string
	ImgURLs             []string
	Remark              string
	MeasurementStrategy MeasurementStrategy
	DefaultUnitID       *uuid.UUID
	Attributes          json.RawMessage
}

// ListFilter contains optional query parameters for listing products.
type ListFilter struct {
	TenantID         uuid.UUID
	Query            string          // full-text search on name/code/mnemonic
	AttributesFilter json.RawMessage // @> JSONB containment filter
	Enabled          *bool
	Limit            int
	Offset           int
}
