package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domainai "github.com/hanmahong5-arch/lurus-tally/internal/domain/ai"
)

// ExecutionResult summarises the side effects of a confirmed plan.
// It is returned to the HTTP layer so the PlanCard can deep-link to the created
// document and show how many records were touched.
type ExecutionResult struct {
	Type          domainai.PlanType `json:"type"`
	AffectedCount int               `json:"affected_count"`
	// BillID is set only for create_purchase_draft plans.
	BillID *uuid.UUID `json:"bill_id,omitempty"`
	BillNo string     `json:"bill_no,omitempty"`
}

// PlanExecutor performs the real side effects of a confirmed plan.
// Implemented by DefaultPlanExecutor; the orchestrator calls it from ConfirmPlan.
type PlanExecutor interface {
	Execute(ctx context.Context, actorID uuid.UUID, plan *domainai.Plan) (*ExecutionResult, error)
}

// AuditRecord is one audit-trail row for an AI plan execution.
type AuditRecord struct {
	TenantID   uuid.UUID
	ActorID    uuid.UUID
	Action     string // "ai.plan.executed" | "ai.plan.failed"
	TargetKind string
	TargetID   string
	Payload    map[string]any
}

// AuditWriter persists an AuditRecord. Implemented in lifecycle over the account
// audit-log use case so AI writes surface in the "活动日志" tab.
type AuditWriter interface {
	Write(ctx context.Context, rec AuditRecord) error
}

// DraftLine is one resolved purchase-draft line (product already resolved to an ID).
type DraftLine struct {
	ProductID   uuid.UUID
	ProductName string
	Qty         decimal.Decimal
}

// DraftCreatorPort creates a purchase draft from resolved lines.
// Implemented in lifecycle over bill.CreatePurchaseDraftUseCase. The implementation
// owns unit-price resolution (default SKU purchase_price) so the executor stays
// free of pricing concerns.
type DraftCreatorPort interface {
	CreatePurchaseDraft(ctx context.Context, tenantID, actorID uuid.UUID, lines []DraftLine) (billID uuid.UUID, billNo string, err error)
}

// PriceChangerPort applies a price action to each product's default SKU.
// Implemented in lifecycle over sku.UpdatePriceUseCase.
type PriceChangerPort interface {
	ApplyPriceChange(ctx context.Context, tenantID uuid.UUID, productIDs []uuid.UUID, action string) (affected int, err error)
}

// StockAdjusterPort records a single adjust movement for one product in the
// tenant's default warehouse. Implemented in lifecycle over stock.RecordMovementUseCase.
type StockAdjusterPort interface {
	AdjustStock(ctx context.Context, tenantID, actorID, productID uuid.UUID, delta decimal.Decimal) error
}

// DefaultPlanExecutor is the production PlanExecutor. It resolves product
// names/filters via the AI ProductRepo and dispatches to the three ports.
type DefaultPlanExecutor struct {
	products ProductRepo
	draft    DraftCreatorPort
	price    PriceChangerPort
	stock    StockAdjusterPort
}

// NewPlanExecutor constructs a DefaultPlanExecutor.
func NewPlanExecutor(products ProductRepo, draft DraftCreatorPort, price PriceChangerPort, stock StockAdjusterPort) *DefaultPlanExecutor {
	return &DefaultPlanExecutor{products: products, draft: draft, price: price, stock: stock}
}

var _ PlanExecutor = (*DefaultPlanExecutor)(nil)

// Execute dispatches a confirmed plan to its handler by type.
func (e *DefaultPlanExecutor) Execute(ctx context.Context, actorID uuid.UUID, plan *domainai.Plan) (*ExecutionResult, error) {
	switch plan.Type {
	case domainai.PlanTypeCreatePurchase:
		return e.execPurchase(ctx, actorID, plan)
	case domainai.PlanTypePriceChange:
		return e.execPriceChange(ctx, plan)
	case domainai.PlanTypeBulkStockAdjust:
		return e.execStockAdjust(ctx, actorID, plan)
	default:
		return nil, fmt.Errorf("plan executor: unsupported plan type %q", plan.Type)
	}
}

func (e *DefaultPlanExecutor) execPurchase(ctx context.Context, actorID uuid.UUID, plan *domainai.Plan) (*ExecutionResult, error) {
	var payload struct {
		Items []struct {
			ProductName string  `json:"product_name"`
			Qty         float64 `json:"qty"`
		} `json:"items"`
	}
	if err := decodePayload(plan.Payload, &payload); err != nil {
		return nil, fmt.Errorf("plan executor: decode purchase payload: %w", err)
	}
	if len(payload.Items) == 0 {
		return nil, fmt.Errorf("plan executor: purchase plan has no items")
	}

	lines := make([]DraftLine, 0, len(payload.Items))
	for _, it := range payload.Items {
		id, ok, err := e.resolveByName(ctx, plan.TenantID, it.ProductName)
		if err != nil {
			return nil, fmt.Errorf("plan executor: resolve product %q: %w", it.ProductName, err)
		}
		if !ok {
			return nil, fmt.Errorf("plan executor: product %q not found — cannot create draft", it.ProductName)
		}
		lines = append(lines, DraftLine{
			ProductID:   id,
			ProductName: it.ProductName,
			Qty:         decimal.NewFromFloat(it.Qty),
		})
	}

	billID, billNo, err := e.draft.CreatePurchaseDraft(ctx, plan.TenantID, actorID, lines)
	if err != nil {
		return nil, fmt.Errorf("plan executor: create purchase draft: %w", err)
	}
	return &ExecutionResult{
		Type:          plan.Type,
		AffectedCount: len(lines),
		BillID:        &billID,
		BillNo:        billNo,
	}, nil
}

func (e *DefaultPlanExecutor) execPriceChange(ctx context.Context, plan *domainai.Plan) (*ExecutionResult, error) {
	var payload struct {
		Filter string `json:"filter"`
		Action string `json:"action"`
	}
	if err := decodePayload(plan.Payload, &payload); err != nil {
		return nil, fmt.Errorf("plan executor: decode price payload: %w", err)
	}

	ids, err := e.resolveByFilter(ctx, plan.TenantID, payload.Filter)
	if err != nil {
		return nil, fmt.Errorf("plan executor: resolve filter %q: %w", payload.Filter, err)
	}

	affected, err := e.price.ApplyPriceChange(ctx, plan.TenantID, ids, payload.Action)
	if err != nil {
		return nil, fmt.Errorf("plan executor: apply price change: %w", err)
	}
	return &ExecutionResult{Type: plan.Type, AffectedCount: affected}, nil
}

func (e *DefaultPlanExecutor) execStockAdjust(ctx context.Context, actorID uuid.UUID, plan *domainai.Plan) (*ExecutionResult, error) {
	var payload struct {
		Filter string  `json:"filter"`
		Delta  float64 `json:"delta"`
	}
	if err := decodePayload(plan.Payload, &payload); err != nil {
		return nil, fmt.Errorf("plan executor: decode stock payload: %w", err)
	}

	ids, err := e.resolveByFilter(ctx, plan.TenantID, payload.Filter)
	if err != nil {
		return nil, fmt.Errorf("plan executor: resolve filter %q: %w", payload.Filter, err)
	}

	delta := decimal.NewFromFloat(payload.Delta)
	affected := 0
	for _, id := range ids {
		if err := e.stock.AdjustStock(ctx, plan.TenantID, actorID, id, delta); err != nil {
			// Fail fast: bulk adjusts are not atomic across products, so report how
			// many succeeded before the failure rather than claiming full success.
			return &ExecutionResult{Type: plan.Type, AffectedCount: affected},
				fmt.Errorf("plan executor: adjust stock for product %s (after %d succeeded): %w", id, affected, err)
		}
		affected++
	}
	return &ExecutionResult{Type: plan.Type, AffectedCount: affected}, nil
}

// resolveByName resolves a single product name to an ID, preferring an exact
// (case-insensitive) name match before falling back to the first search hit.
func (e *DefaultPlanExecutor) resolveByName(ctx context.Context, tenantID uuid.UUID, name string) (uuid.UUID, bool, error) {
	rows, err := e.products.SearchProducts(ctx, tenantID, name)
	if err != nil {
		return uuid.Nil, false, err
	}
	if len(rows) == 0 {
		return uuid.Nil, false, nil
	}
	for _, r := range rows {
		if strings.EqualFold(strings.TrimSpace(r.Name), strings.TrimSpace(name)) {
			return r.ID, true, nil
		}
	}
	return rows[0].ID, true, nil
}

// resolveByFilter returns all product IDs matching a filter string.
func (e *DefaultPlanExecutor) resolveByFilter(ctx context.Context, tenantID uuid.UUID, filter string) ([]uuid.UUID, error) {
	rows, err := e.products.SearchProducts(ctx, tenantID, filter)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return ids, nil
}

// decodePayload re-marshals a plan payload map and unmarshals it into v.
// This normalises types regardless of whether the payload was built in-process
// (typed) or round-tripped through Redis (generic map[string]interface{}).
func decodePayload(payload map[string]interface{}, v interface{}) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("unmarshal payload: %w", err)
	}
	return nil
}
