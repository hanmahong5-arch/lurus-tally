// Package bill contains domain entities for the universal bill model (purchase, sale, transfer, adjust).
// All bill types share the same bill_head + bill_item table pair, differentiated by BillType/BillSubType.
package bill

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// BillStatus mirrors the SMALLINT values in bill_head.status (jshERP-compatible).
type BillStatus int16

const (
	StatusDraft     BillStatus = 0
	StatusApproved  BillStatus = 2
	StatusCancelled BillStatus = 9
)

// BillType is the broad bill category (maps to bill_head.bill_type).
type BillType string

const (
	BillTypePurchase BillType = "入库"
	BillTypeSale     BillType = "出库"
)

// BillSubType is the specific business process (maps to bill_head.sub_type).
type BillSubType string

const (
	BillSubTypePurchase BillSubType = "采购"
	BillSubTypeSale     BillSubType = "销售"
)

// ErrInvalidTransition is returned when the requested status transition is not allowed.
var ErrInvalidTransition = errors.New("bill: invalid status transition")

// CanTransitionTo returns whether the status machine allows moving from s to next.
// Allowed transitions:
//   - Draft     → Approved
//   - Draft     → Cancelled
//   - Approved  → (none; reversal handled by a separate purchase-return bill)
//   - Cancelled → (terminal)
func (s BillStatus) CanTransitionTo(next BillStatus) bool {
	switch s {
	case StatusDraft:
		return next == StatusApproved || next == StatusCancelled
	default:
		return false
	}
}

// BillHead maps to the tally.bill_head table.
// Fields are camelCase; JSON tags use snake_case to match the API contract.
type BillHead struct {
	ID          uuid.UUID   `json:"id"`
	TenantID    uuid.UUID   `json:"tenant_id"`
	BillNo      string      `json:"bill_no"`
	BillType    BillType    `json:"bill_type"`
	SubType     BillSubType `json:"sub_type"`
	Status      BillStatus  `json:"status"`
	PartnerID   *uuid.UUID  `json:"partner_id,omitempty"`
	WarehouseID *uuid.UUID  `json:"warehouse_id,omitempty"`
	OperatorID  *uuid.UUID  `json:"operator_id,omitempty"`
	CreatorID   uuid.UUID   `json:"creator_id"`
	BillDate    time.Time   `json:"bill_date"`

	// Financial totals (NUMERIC(18,4))
	Subtotal    decimal.Decimal `json:"subtotal"`
	ShippingFee decimal.Decimal `json:"shipping_fee"`
	TaxAmount   decimal.Decimal `json:"tax_amount"`
	TotalAmount decimal.Decimal `json:"total_amount"`

	// Approval metadata
	ApprovedAt *time.Time `json:"approved_at,omitempty"`
	ApprovedBy *uuid.UUID `json:"approved_by,omitempty"`

	Remark    string    `json:"remark,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// BillItem maps to the tally.bill_item table.
type BillItem struct {
	ID         uuid.UUID       `json:"id"`
	TenantID   uuid.UUID       `json:"tenant_id"`
	HeadID     uuid.UUID       `json:"head_id"`
	ProductID  uuid.UUID       `json:"product_id"`
	UnitID     *uuid.UUID      `json:"unit_id,omitempty"`
	UnitName   string          `json:"unit_name,omitempty"`
	LineNo     int             `json:"line_no"`
	Qty        decimal.Decimal `json:"qty"`
	UnitPrice  decimal.Decimal `json:"unit_price"`
	LineAmount decimal.Decimal `json:"line_amount"` // = Qty * UnitPrice
	Remark     string          `json:"remark,omitempty"`
}
