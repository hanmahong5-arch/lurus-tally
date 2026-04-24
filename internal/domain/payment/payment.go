// Package payment contains domain entities for payment records.
// Payments map to tally.payment_head rows. Each payment belongs to one bill.
package payment

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// PayType represents the method of payment accepted by the system.
type PayType string

const (
	PayTypeCash     PayType = "cash"
	PayTypeWechat   PayType = "wechat"
	PayTypeAlipay   PayType = "alipay"
	PayTypeCard     PayType = "card"
	PayTypeCredit   PayType = "credit"
	PayTypeTransfer PayType = "transfer"
)

// validPayTypes contains all accepted PayType values.
var validPayTypes = map[PayType]struct{}{
	PayTypeCash:     {},
	PayTypeWechat:   {},
	PayTypeAlipay:   {},
	PayTypeCard:     {},
	PayTypeCredit:   {},
	PayTypeTransfer: {},
}

// Validate returns an error if p is not a recognised PayType.
func (p PayType) Validate() error {
	if _, ok := validPayTypes[p]; !ok {
		return fmt.Errorf("payment: invalid pay_type %q; must be one of cash/wechat/alipay/card/credit/transfer", p)
	}
	return nil
}

// Payment maps to one row in tally.payment_head.
// BillID maps to the related_bill_id FK column.
type Payment struct {
	ID          uuid.UUID       `json:"id"`
	TenantID    uuid.UUID       `json:"tenant_id"`
	BillID      uuid.UUID       `json:"bill_id"`
	PayType     PayType         `json:"pay_type"`
	Amount      decimal.Decimal `json:"amount"`
	TotalAmount decimal.Decimal `json:"total_amount"` // mirrors amount (no discount in MVP)
	PartnerID   *uuid.UUID      `json:"partner_id,omitempty"`
	OperatorID  *uuid.UUID      `json:"operator_id,omitempty"`
	CreatorID   uuid.UUID       `json:"creator_id"`
	BillNo      string          `json:"bill_no,omitempty"`
	PayDate     time.Time       `json:"pay_date"`
	Remark      string          `json:"remark,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}
