// Package grill is the pure domain core of the BBQ-stall real-time fulfillment
// module (PRD: 烧烤摊实时履约模块). Phase 1 scope only: the temporary runtime
// entities (session / person / order item / shared charge), the dine-in state
// machines, the mandatory calibration gate, and the settlement math. NO
// scheduling, hardware, takeout or reservation — those are later phases.
//
// It is a same-repo package (DECISION-1) but has ZERO infrastructure deps:
// persistence (RLS-isolated temporary tables), the inventory deduction (tally's
// RecordMovementUseCase, "下单即扣" route R1) and the settlement write-back live
// in the app/adapter layers and merely drive these pure transitions. The
// accounting invariants are encoded and unit-tested here without a database:
//
//	INV-2  an item cannot enter grilling before it has been calibrated.
//	INV-3  each order item maps to at most one outbound movement (stable key).
//	INV-5  the calibrated qty, once set, is the accounting truth over any estimate.
//
// (INV-1 settlement and the share split modes live in settlement.go.)
package grill

import (
	"errors"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ErrInvalidTransition is returned for a state transition the machine forbids.
var ErrInvalidTransition = errors.New("grill: invalid state transition")

// ErrNotCalibrated is returned when an item is pushed to grilling before the
// mandatory calibration gate (INV-2).
var ErrNotCalibrated = errors.New("grill: order item must be calibrated before grilling")

// ErrAlreadyDeducted is returned when an item that already wrote its inventory
// movement is asked to deduct again — the guard behind idempotency (INV-3).
var ErrAlreadyDeducted = errors.New("grill: order item already has a stock movement")

// SessionStatus is the lifecycle of a table session.
type SessionStatus string

const (
	SessionOpen     SessionStatus = "open"     // table opened, no activity yet
	SessionActive   SessionStatus = "active"   // has people / orders
	SessionSettling SessionStatus = "settling" // checkout in progress
	SessionClosed   SessionStatus = "closed"   // terminal
)

// SessionType distinguishes the three customer flows. Phase 1 only exercises
// DineIn; the other two are domain vocabulary for later phases.
type SessionType string

const (
	DineIn      SessionType = "dine_in"
	Takeout     SessionType = "takeout"
	Reservation SessionType = "reservation"
)

// OrderItemStatus is the lifecycle of a single ordered line.
type OrderItemStatus string

const (
	ItemPending   OrderItemStatus = "pending"
	ItemGrilling  OrderItemStatus = "grilling"
	ItemServed    OrderItemStatus = "served"
	ItemCancelled OrderItemStatus = "cancelled" // 退单; record retained, excluded from total
)

// SplitMode is how a SharedCharge is apportioned across people.
type SplitMode string

const (
	// SplitEqual: Amount is the total, split evenly; session contributes Amount.
	SplitEqual SplitMode = "equal"
	// SplitByCount: Amount is the total, weighted by each person's item count;
	// session contributes Amount.
	SplitByCount SplitMode = "by_count"
	// SplitFixedPerPerson: Amount is charged to EACH person; session contributes
	// Amount × number-of-people.
	SplitFixedPerPerson SplitMode = "fixed_per_person"
)

// Session is a table session: born on open, gone on close. table_no/color are
// the human identity carriers (dine-in); no real customer identity is required.
type Session struct {
	ID       uuid.UUID
	TenantID uuid.UUID
	TableNo  string
	Color    string
	Type     SessionType
	Status   SessionStatus
}

// Activate moves an open session to active (first person/order arrived).
func (s *Session) Activate() error {
	if s.Status != SessionOpen {
		return ErrInvalidTransition
	}
	s.Status = SessionActive
	return nil
}

// BeginSettle moves an active session into checkout.
func (s *Session) BeginSettle() error {
	if s.Status != SessionActive {
		return ErrInvalidTransition
	}
	s.Status = SessionSettling
	return nil
}

// Close terminally closes a settling session.
func (s *Session) Close() error {
	if s.Status != SessionSettling {
		return ErrInvalidTransition
	}
	s.Status = SessionClosed
	return nil
}

// Person is a seat at a session. label is a seat number / nickname; never a real
// name. Supports mid-session join and early leave.
type Person struct {
	ID        uuid.UUID
	SessionID uuid.UUID
	Label     string
}

// OrderItem is one ordered line pinned to a session (and optionally a person).
type OrderItem struct {
	ID        uuid.UUID
	SessionID uuid.UUID
	PersonID  *uuid.UUID // nil = charged to the table, not a person

	SKUID             uuid.UUID       // points at a tally product (接点①)
	Qty               int             // ordered / device-estimated qty
	CalibratedQty     *int            // voice-calibrated truth; nil until calibrated (INV-5)
	UnitPriceSnapshot decimal.Decimal // price snapshot at order time (immune to later repricing)

	Status     OrderItemStatus
	MovementID *uuid.UUID // backfilled after the outbound movement is written (INV-3)
}

// Calibrate records the calibrated qty, the accounting truth (INV-5). Allowed
// only while pending — calibration is the gate that precedes grilling.
func (o *OrderItem) Calibrate(qty int) error {
	if o.Status != ItemPending {
		return ErrInvalidTransition
	}
	q := qty
	o.CalibratedQty = &q
	return nil
}

// StartGrilling moves a pending item to grilling. INV-2: it must be calibrated
// first — the calibration gate is what keeps the books straight.
func (o *OrderItem) StartGrilling() error {
	if o.Status != ItemPending {
		return ErrInvalidTransition
	}
	if o.CalibratedQty == nil {
		return ErrNotCalibrated
	}
	o.Status = ItemGrilling
	return nil
}

// Serve moves a grilling item to served.
func (o *OrderItem) Serve() error {
	if o.Status != ItemGrilling {
		return ErrInvalidTransition
	}
	o.Status = ItemServed
	return nil
}

// Cancel marks any non-cancelled item as cancelled (退单). The record is kept
// (never deleted); settlement excludes it. The compensating reverse movement is
// an infrastructure concern driven off this transition.
func (o *OrderItem) Cancel() error {
	if o.Status == ItemCancelled {
		return ErrInvalidTransition
	}
	o.Status = ItemCancelled
	return nil
}

// AccountingQty is the qty the books use: calibrated when present, else the
// ordered qty (INV-5).
func (o *OrderItem) AccountingQty() int {
	if o.CalibratedQty != nil {
		return *o.CalibratedQty
	}
	return o.Qty
}

// LineAmount is unit price × accounting qty. It is the item's intrinsic amount;
// cancellation is handled by settlement (which excludes cancelled lines), not by
// zeroing here.
func (o *OrderItem) LineAmount() decimal.Decimal {
	return o.UnitPriceSnapshot.Mul(decimal.NewFromInt(int64(o.AccountingQty())))
}

// MovementReference is the stable idempotency key for the item's outbound
// movement: the order item id. Retried deductions reuse it so tally writes at
// most one movement per item (INV-3).
func (o *OrderItem) MovementReference() uuid.UUID {
	return o.ID
}

// NeedsDeduction reports whether this item still owes an inventory deduction:
// not cancelled and no movement yet recorded.
func (o *OrderItem) NeedsDeduction() bool {
	return o.Status != ItemCancelled && o.MovementID == nil
}

// MarkDeducted records the movement id written by 接点②. Calling it twice is an
// error — the guard that, with MovementReference, enforces single deduction.
func (o *OrderItem) MarkDeducted(movementID uuid.UUID) error {
	if o.MovementID != nil {
		return ErrAlreadyDeducted
	}
	id := movementID
	o.MovementID = &id
	return nil
}

// SharedCharge is a session-level surcharge (炭火费 / 最低消费 / 茶水) apportioned
// across people by SplitMode.
type SharedCharge struct {
	ID        uuid.UUID
	SessionID uuid.UUID
	Label     string
	Amount    decimal.Decimal
	SplitMode SplitMode
}
