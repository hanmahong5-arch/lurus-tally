// Package audit is the shop operation log: who approved, voided or changed
// which document. Rows are written by the recorder middleware (one per
// successful write on a bill route) and read back by GET /api/v1/audit.
package audit

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Actions stored in tally.audit_log.action. The web never shows these; they are
// mapped to Chinese by ActionLabel.
const (
	ActionBillCreated  = "bill.created"
	ActionBillUpdated  = "bill.updated"
	ActionBillApproved = "bill.approved"
	ActionBillVoided   = "bill.voided"
	ActionBillRestored = "bill.restored"
	ActionBillReturned = "bill.returned"
	// ActionStockNegative marks an offline sale that took a product below zero; the
	// changes.reason text is the to-do ("请盘点：<商品>（当前 x）").
	ActionStockNegative = "stock.negative"
)

// Resources stored in tally.audit_log.resource.
const (
	ResourcePurchaseBill = "purchase_bill"
	ResourceSaleBill     = "sale_bill"
)

// Entry is one row to append.
type Entry struct {
	TenantID   uuid.UUID
	UserID     uuid.UUID // uuid.Nil = system / automation
	Action     string
	Resource   string
	ResourceID uuid.UUID // uuid.Nil = none
	Changes    []byte    // JSON, may be empty
	ClientIP   string
}

// Row is one stored entry as read back, with the document number joined in.
type Row struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	Action     string
	Resource   string
	ResourceID uuid.UUID
	ResourceNo string
	Changes    []byte
	CreatedAt  time.Time
}

// Filter narrows a listing. Zero values mean "no filter".
type Filter struct {
	Resource   string
	ResourceID uuid.UUID
	From       *time.Time
	To         *time.Time
	Limit      int
	Offset     int
}

// Repo is the persistence surface.
type Repo interface {
	Append(ctx context.Context, e Entry) error
	List(ctx context.Context, tenantID uuid.UUID, f Filter) ([]Row, int, error)
}

// MemberNamer maps creator ids to display names (repo/team Store.MemberNames).
type MemberNamer interface {
	MemberNames(ctx context.Context, tenantID uuid.UUID) (map[uuid.UUID]string, error)
}
