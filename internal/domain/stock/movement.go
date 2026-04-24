// Package stock contains domain entities for inventory management.
// These are plain Go structs with no infrastructure dependencies.
package stock

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Direction represents the direction of a stock movement.
type Direction string

const (
	// DirectionIn records goods entering the warehouse (receipt, production, return).
	DirectionIn Direction = "in"
	// DirectionOut records goods leaving the warehouse (shipment, consumption, write-off).
	DirectionOut Direction = "out"
	// DirectionAdjust records an inventory count correction (shrinkage / overage).
	DirectionAdjust Direction = "adjust"
)

// Validate returns an error if d is not a recognised Direction value.
func (d Direction) Validate() error {
	switch d {
	case DirectionIn, DirectionOut, DirectionAdjust:
		return nil
	default:
		return ErrInvalidDirection
	}
}

// ReferenceType categorises the business event that caused a stock movement.
type ReferenceType string

const (
	RefPurchase ReferenceType = "purchase"
	RefSale     ReferenceType = "sale"
	RefAdjust   ReferenceType = "adjust"
	RefTransfer ReferenceType = "transfer"
	RefInit     ReferenceType = "init"
)

// Movement is the append-only record of one stock change event.
// QtyBase is always expressed in the product's base unit.
type Movement struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	ProductID     uuid.UUID
	WarehouseID   uuid.UUID
	Direction     Direction
	QtyBase       decimal.Decimal // quantity in base unit, always positive
	UnitCost      decimal.Decimal // per-base-unit cost
	TotalCost     decimal.Decimal // QtyBase * UnitCost (set by calculator)
	ReferenceType ReferenceType
	ReferenceID   *uuid.UUID
	OccurredAt    time.Time
	CreatedBy     *uuid.UUID
	Note          string
	CreatedAt     time.Time
}

// Snapshot represents the current materialised state of inventory for one SKU in one warehouse.
// It is always maintained by the CostEngine — callers must never UPDATE it directly.
type Snapshot struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	ProductID    uuid.UUID
	WarehouseID  uuid.UUID
	OnHandQty    decimal.Decimal
	AvailableQty decimal.Decimal
	UnitCost     decimal.Decimal
	CostStrategy string
	UpdatedAt    time.Time
}

// CostStrategy constants match the CHECK constraint in the migration.
const (
	CostStrategyWAC  = "wac"
	CostStrategyFIFO = "fifo"
)
