// Package replenish implements the weekly replenishment decision surface.
package replenish

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	"github.com/shopspring/decimal"
)

// DraftBatchLine is one product + quantity to include in a batch PO draft.
type DraftBatchLine struct {
	ProductID  uuid.UUID
	SupplierID *uuid.UUID // nil when no supplier is linked
	Qty        decimal.Decimal
}

// DraftBatchRequest is the input to CreateDraftBatchUseCase.
type DraftBatchRequest struct {
	TenantID  uuid.UUID
	CreatorID uuid.UUID
	Lines     []DraftBatchLine
	Remark    string
}

// DraftResult is one created purchase draft.
type DraftResult struct {
	BillID       uuid.UUID
	BillNo       string
	SupplierID   *uuid.UUID
	SupplierName string // informational; empty when no supplier
	LineCount    int
}

// DraftBatchOutput is returned by CreateDraftBatchUseCase.Execute.
type DraftBatchOutput struct {
	Drafts []DraftResult
}

// PurchaseDraftCreator is the narrow interface the batch use case needs from
// the bill domain. *appbill.CreatePurchaseDraftUseCase satisfies this.
type PurchaseDraftCreator interface {
	Execute(ctx context.Context, req appbill.CreatePurchaseDraftRequest) (*appbill.CreatePurchaseDraftOutput, error)
}

// SupplierNameResolver maps supplier UUID → display name for the response body.
// Returning an empty string for an unknown ID is acceptable — the draft still
// gets created and the FE can fetch the name from /suppliers/:id if needed.
type SupplierNameResolver interface {
	NameByID(ctx context.Context, tenantID, supplierID uuid.UUID) (string, error)
}

// ProductSupplier identifies one (product, preferred supplier) pair for batch
// price lookup. Defined here (not in the repo adapter) so the dependency keeps
// pointing adapter → app. SupplierID is nil for the no-supplier group.
type ProductSupplier struct {
	ProductID  uuid.UUID
	SupplierID *uuid.UUID
}

// PriceLookup resolves the most recent approved purchase price (already
// converted to CNY) for a set of pairs in ONE batch query — never per-product.
// The result is keyed by product only: within a single draft batch each
// product belongs to exactly one supplier group, so the product ID is a
// sufficient key and spares callers a composite-key type.
type PriceLookup interface {
	LastPurchasePrices(ctx context.Context, tenantID uuid.UUID, pairs []ProductSupplier) (map[uuid.UUID]decimal.Decimal, error)
}

// CreateDraftBatchUseCase groups selected replenishment lines by supplier and
// creates one purchase draft per group.
//
// Idempotency is enforced at the handler layer via the Idempotency-Key header
// and the Redis dedup middleware already present in the app. The use case itself
// is not responsible for dedup — it is called at most once per unique key.
//
// TODO(P1 #4): incorporate in-transit qty and lead-time-aware ROP before creating.
type CreateDraftBatchUseCase struct {
	creator  PurchaseDraftCreator
	resolver SupplierNameResolver // may be nil — names will be empty
	prices   PriceLookup          // may be nil — drafts keep zero unit prices
}

// NewCreateDraftBatchUseCase constructs the use case.
// resolver may be nil; supplier names will then be omitted from results.
func NewCreateDraftBatchUseCase(creator PurchaseDraftCreator, resolver SupplierNameResolver) *CreateDraftBatchUseCase {
	return &CreateDraftBatchUseCase{creator: creator, resolver: resolver}
}

// WithPriceLookup enables last-purchase-price backfill on created draft lines.
// nil keeps today's behavior (every line at decimal.Zero, filled in manually).
func (uc *CreateDraftBatchUseCase) WithPriceLookup(pl PriceLookup) *CreateDraftBatchUseCase {
	uc.prices = pl
	return uc
}

// Execute groups lines by supplier, creates one draft per group, and returns
// all created drafts. A partial failure (one group fails) is returned as an
// error; any drafts already persisted before the failure remain in the DB.
func (uc *CreateDraftBatchUseCase) Execute(ctx context.Context, req DraftBatchRequest) (*DraftBatchOutput, error) {
	if req.TenantID == uuid.Nil {
		return nil, fmt.Errorf("replenish batch draft: tenant_id required")
	}
	if len(req.Lines) == 0 {
		return nil, fmt.Errorf("replenish batch draft: at least one line required")
	}

	// Group lines by supplier. nil SupplierID → key "__no_supplier__".
	type group struct {
		supplierID *uuid.UUID
		lines      []DraftBatchLine
	}
	keys := make([]string, 0)
	groups := make(map[string]*group)
	for _, l := range req.Lines {
		if l.Qty.IsZero() || l.Qty.IsNegative() {
			continue // skip zero-qty lines silently
		}
		k := "__no_supplier__"
		if l.SupplierID != nil {
			k = l.SupplierID.String()
		}
		if _, ok := groups[k]; !ok {
			sid := l.SupplierID
			groups[k] = &group{supplierID: sid}
			keys = append(keys, k)
		}
		groups[k].lines = append(groups[k].lines, l)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("replenish batch draft: all lines have zero or negative qty")
	}

	remark := req.Remark
	if remark == "" {
		remark = "补货建议批量草稿"
	}

	// One batch price fetch for all (product, supplier) pairs up front — a
	// single query on the request-pinned connection, never per group or per
	// product. With no PriceLookup wired the map stays nil and every line
	// falls back to zero (today's behavior).
	var prices map[uuid.UUID]decimal.Decimal
	if uc.prices != nil {
		pairs := make([]ProductSupplier, 0, len(req.Lines))
		for _, k := range keys {
			g := groups[k]
			for _, l := range g.lines {
				pairs = append(pairs, ProductSupplier{ProductID: l.ProductID, SupplierID: g.supplierID})
			}
		}
		var perr error
		prices, perr = uc.prices.LastPurchasePrices(ctx, req.TenantID, pairs)
		if perr != nil {
			return nil, fmt.Errorf("replenish batch draft: last purchase prices: %w", perr)
		}
	}

	now := time.Now().UTC()
	out := &DraftBatchOutput{Drafts: make([]DraftResult, 0, len(keys))}

	for _, k := range keys {
		g := groups[k]
		items := make([]appbill.CreatePurchaseItemInput, 0, len(g.lines))
		for i, l := range g.lines {
			unitPrice := decimal.Zero // draft — price filled in manually before approval
			if p, ok := prices[l.ProductID]; ok {
				unitPrice = p
			}
			items = append(items, appbill.CreatePurchaseItemInput{
				ProductID: l.ProductID,
				LineNo:    i + 1,
				Qty:       l.Qty,
				UnitPrice: unitPrice,
			})
		}

		created, err := uc.creator.Execute(ctx, appbill.CreatePurchaseDraftRequest{
			TenantID:  req.TenantID,
			CreatorID: req.CreatorID,
			PartnerID: g.supplierID,
			BillDate:  now,
			Remark:    remark,
			Items:     items,
		})
		if err != nil {
			return nil, fmt.Errorf("replenish batch draft: create group %s: %w", k, err)
		}

		result := DraftResult{
			BillID:    created.BillID,
			BillNo:    created.BillNo,
			LineCount: len(items),
		}
		if g.supplierID != nil {
			sid := *g.supplierID
			result.SupplierID = &sid
			// Best-effort name resolution; missing name is not a failure.
			if uc.resolver != nil {
				if name, nerr := uc.resolver.NameByID(ctx, req.TenantID, sid); nerr == nil {
					result.SupplierName = name
				}
			}
		}
		out.Drafts = append(out.Drafts, result)
	}

	return out, nil
}
