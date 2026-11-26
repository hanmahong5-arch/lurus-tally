package bill

import (
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// BatchOpen is one purchase line becoming a receipt batch, in base units.
type BatchOpen struct {
	SourceItemID uuid.UUID
	ProductID    uuid.UUID
	QtyBase      decimal.Decimal
	Factor       decimal.Decimal
	OnHandAfter  decimal.Decimal
}

// BatchDrawLine is one approved sale line. OnHandAfter is the snapshot after this line's stock-out.
type BatchDrawLine struct {
	SaleItemID   uuid.UUID
	ProductID    uuid.UUID
	QtyBase      decimal.Decimal
	Factor       decimal.Decimal
	SourceItemID uuid.UUID
	OnHandAfter  decimal.Decimal
}

// PurchaseLine is the purchase document a sale line can be pinned to.
type PurchaseLine struct {
	HeadID    uuid.UUID
	ProductID uuid.UUID
	Status    BillStatus
	SubType   BillSubType
}

// SupplyBill is a purchase or a sale shown as the other end of a receipt link.
type SupplyBill struct {
	ID          uuid.UUID `json:"id"`
	BillNo      string    `json:"bill_no"`
	Status      int16     `json:"status"`
	PartnerName string    `json:"partner_name,omitempty"`
}
