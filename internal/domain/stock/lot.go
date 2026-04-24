package stock

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Lot tracks one batch of inbound inventory (FIFO queue entry).
// When QtyRemaining reaches zero the lot is fully consumed and ignored in FIFO drain queries.
type Lot struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	ProductID        uuid.UUID
	WarehouseID      uuid.UUID
	LotNo            string
	Qty              decimal.Decimal // original inbound quantity
	QtyRemaining     decimal.Decimal // unconsumed quantity; decremented on FIFO out
	UnitCost         decimal.Decimal // cost per base unit for this lot
	ReceivedAt       time.Time       // FIFO order key — oldest lots consumed first
	SourceMovementID *uuid.UUID
	CreatedAt        time.Time
}
