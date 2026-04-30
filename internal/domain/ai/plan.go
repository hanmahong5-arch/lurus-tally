// Package ai contains domain entities for the AI assistant feature.
package ai

import (
	"time"

	"github.com/google/uuid"
)

// PlanStatus tracks the lifecycle of a destructive operation plan.
type PlanStatus string

const (
	PlanStatusPending   PlanStatus = "pending"
	PlanStatusConfirmed PlanStatus = "confirmed"
	PlanStatusCancelled PlanStatus = "cancelled"
	PlanStatusExpired   PlanStatus = "expired"
)

// PlanType identifies which destructive operation a plan represents.
type PlanType string

const (
	PlanTypePriceChange       PlanType = "price_change"
	PlanTypeCreatePurchase    PlanType = "create_purchase_draft"
	PlanTypeBulkStockAdjust   PlanType = "bulk_stock_adjust"
)

// Plan is the entity stored in Redis while awaiting user confirmation.
// All destructive LLM tool calls return a Plan instead of executing immediately.
type Plan struct {
	ID          uuid.UUID       `json:"id"`
	TenantID    uuid.UUID       `json:"tenant_id"`
	Type        PlanType        `json:"type"`
	Status      PlanStatus      `json:"status"`
	// Payload is the serialised operation parameters (type-specific JSON).
	Payload     map[string]interface{} `json:"payload"`
	// Preview contains the human-readable summary for the confirmation card.
	Preview     PlanPreview     `json:"preview"`
	CreatedAt   time.Time       `json:"created_at"`
	ExpiresAt   time.Time       `json:"expires_at"`
}

// PlanPreview is the data needed to render the confirmation card in the UI.
type PlanPreview struct {
	// Description is one sentence summarising the operation.
	Description string           `json:"description"`
	// AffectedCount is the number of records that will be touched.
	AffectedCount int            `json:"affected_count"`
	// SampleRows is a preview of up to 10 affected records (key=name, value=change description).
	SampleRows   []SampleRow     `json:"sample_rows"`
}

// SampleRow is one row in the plan preview table.
type SampleRow struct {
	Name   string `json:"name"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// ChatTurn is one message in a conversation history.
type ChatTurn struct {
	Role    string `json:"role"` // "user" | "assistant" | "tool"
	Content string `json:"content"`
}

// ToolCallRecord stores the result of a tool invocation for audit.
type ToolCallRecord struct {
	ToolName  string
	ArgsJSON  string
	ResultJSON string
	Error     error
}
