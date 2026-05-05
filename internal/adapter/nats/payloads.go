package nats

import (
	"encoding/json"
	"time"
)

// Event is the canonical envelope for every message on stream PSI_EVENTS.
// All typed publish methods construct one of these and marshal it as JSON.
//
// Field semantics:
//   - EventID: server-assigned UUID v4; consumers may dedupe on this.
//   - EventType: dot-separated taxonomy (see EventType* constants).
//   - TenantID: caller-supplied tenant scope; required, non-empty.
//   - OccurredAt: wall-clock time the business event happened (UTC, RFC3339).
//   - Source: always "tally" when published by this service.
//   - Payload: the per-EventType struct from this file, JSON-encoded.
type Event struct {
	EventID    string          `json:"event_id"`
	EventType  string          `json:"event_type"`
	TenantID   string          `json:"tenant_id"`
	OccurredAt time.Time       `json:"occurred_at"`
	Source     string          `json:"source"`
	Payload    json.RawMessage `json:"payload"`
}

// StockMovementRecordedPayload describes a stock movement that committed.
//
// Direction is one of "in" / "out" / "transfer" / "adjust" (domain-defined).
// QtyDelta and OnHandAfter and UnitCost are decimal strings — parse with
// shopspring/decimal; never float64.
type StockMovementRecordedPayload struct {
	ProductID     string `json:"product_id"`
	WarehouseID   string `json:"warehouse_id"`
	Direction     string `json:"direction"`
	QtyDelta      string `json:"qty_delta"`
	OnHandAfter   string `json:"on_hand_after"`
	UnitCost      string `json:"unit_cost"`
	ReferenceType string `json:"reference_type"`
	ReferenceID   string `json:"reference_id"`
}

// StockSnapshotUpdatedPayload reflects the post-write snapshot for a SKU/warehouse.
type StockSnapshotUpdatedPayload struct {
	ProductID    string `json:"product_id"`
	WarehouseID  string `json:"warehouse_id"`
	OnHandQty    string `json:"on_hand_qty"`
	AvailableQty string `json:"available_qty"`
}

// BillCreatedPayload announces a new bill (purchase / sale / etc.).
// BillType examples: "purchase_in", "sale_out", "stock_transfer".
type BillCreatedPayload struct {
	BillID      string `json:"bill_id"`
	BillNo      string `json:"bill_no"`
	BillType    string `json:"bill_type"`
	TotalAmount string `json:"total_amount"`
	TenantID    string `json:"tenant_id"`
}

// BillApprovedPayload announces a bill moved to approved state.
type BillApprovedPayload struct {
	BillID      string `json:"bill_id"`
	BillNo      string `json:"bill_no"`
	BillType    string `json:"bill_type"`
	TotalAmount string `json:"total_amount"`
}

// BillRejectedPayload announces a bill was rejected.
// RejectionReason should be human-readable, ≤ 500 chars.
type BillRejectedPayload struct {
	BillID          string `json:"bill_id"`
	BillNo          string `json:"bill_no"`
	RejectionReason string `json:"rejection_reason"`
}

// LowStockAlertPayload signals on-hand has fallen at or below threshold.
type LowStockAlertPayload struct {
	ProductID   string `json:"product_id"`
	WarehouseID string `json:"warehouse_id"`
	CurrentQty  string `json:"current_qty"`
	Threshold   string `json:"threshold"`
}

// OverstockAlertPayload signals on-hand has exceeded the configured ceiling.
type OverstockAlertPayload struct {
	ProductID   string `json:"product_id"`
	WarehouseID string `json:"warehouse_id"`
	CurrentQty  string `json:"current_qty"`
	Threshold   string `json:"threshold"`
}
