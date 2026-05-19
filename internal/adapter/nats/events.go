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

// --- PSI_TELEMETRY (S0.Q3) ---
//
// Browser-side product telemetry: ⌘K invocations, AI Drawer opens, plan
// accept/reject, onboarding aha-clicks, Cmd+Z usage. Kept on its own
// subject family so retention can be tuned independently of business
// PSI_EVENTS (telemetry is short-lived; business events drive replay).
//
// Stream config (deploy concern, NOT in this commit): add `PSI_TELEMETRY.>`
// to an existing stream OR provision a dedicated `PSI_TELEMETRY` stream
// with 30d retention before enabling TALLY_BACKEND_TELEMETRY_URL in prod.

// AllowedWebTelemetryEvents is the canonical allow-list. Mirror this
// exactly in `web/lib/telemetry.ts` TelemetryEvent union and
// `web/app/api/otel-events/route.ts` VALID_EVENTS — drift will cause
// silent allow-list rejection at the backend.
var AllowedWebTelemetryEvents = map[string]struct{}{
	"draft_restore":                {},
	"undo_used":                    {},
	"palette_invocation":           {},
	"ai_drawer_open":               {},
	"plan_accept_rate":             {},
	"onboarding_first_po_exported": {},
	"cmd_z_used":                   {},
}

// SubjectWebTelemetry returns the JetStream subject for a web telemetry
// event. eventName must be in AllowedWebTelemetryEvents; callers should
// validate before calling.
func SubjectWebTelemetry(eventName string) string {
	return "PSI_TELEMETRY.web." + eventName
}
