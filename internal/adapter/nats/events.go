package nats

// Event taxonomy for stream PSI_EVENTS.
//
// Subjects follow the convention: PSI_EVENTS.<aggregate>.<verb>
// All payloads use string for monetary and quantity fields to avoid
// float precision loss; consumers should parse them with shopspring/decimal
// (or equivalent) — never with strconv.ParseFloat for amounts.
//
// Contract reference: doc/coord/contracts.md § PSI_EVENTS.

// Source identifies the originating service in every Event envelope.
const Source = "tally"

// EventType constants — keep in sync with SubjectFor below and contracts.md.
const (
	EventTypeStockMovementRecorded = "stock.movement_recorded"
	EventTypeStockSnapshotUpdated  = "stock.snapshot_updated"
	EventTypeBillCreated           = "bill.created"
	EventTypeBillApproved          = "bill.approved"
	EventTypeBillRejected          = "bill.rejected"
	EventTypeAlertLowStock         = "alert.low_stock"
	EventTypeAlertOverstock        = "alert.overstock"
)

// Subject constants — fully qualified JetStream subjects.
const (
	SubjectStockMovementRecorded = "PSI_EVENTS." + EventTypeStockMovementRecorded
	SubjectStockSnapshotUpdated  = "PSI_EVENTS." + EventTypeStockSnapshotUpdated
	SubjectBillCreated           = "PSI_EVENTS." + EventTypeBillCreated
	SubjectBillApproved          = "PSI_EVENTS." + EventTypeBillApproved
	SubjectBillRejected          = "PSI_EVENTS." + EventTypeBillRejected
	SubjectAlertLowStock         = "PSI_EVENTS." + EventTypeAlertLowStock
	SubjectAlertOverstock        = "PSI_EVENTS." + EventTypeAlertOverstock
)

// SubjectFor returns the JetStream subject for a given event type.
// Unknown types are still namespaced under PSI_EVENTS for forward-compat.
func SubjectFor(eventType string) string {
	return "PSI_EVENTS." + eventType
}
