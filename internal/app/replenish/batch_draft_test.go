package replenish_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	"github.com/hanmahong5-arch/lurus-tally/internal/app/replenish"
	"github.com/shopspring/decimal"
)

// stubDraftCreator is a test double for PurchaseDraftCreator.
type stubDraftCreator struct {
	calls []*appbill.CreatePurchaseDraftRequest
	err   error
	seq   int
}

func (s *stubDraftCreator) Execute(_ context.Context, req appbill.CreatePurchaseDraftRequest) (*appbill.CreatePurchaseDraftOutput, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.calls = append(s.calls, &req)
	s.seq++
	return &appbill.CreatePurchaseDraftOutput{
		BillID: uuid.New(),
		BillNo: "PO-" + string(rune('A'+s.seq-1)),
	}, nil
}

// TestCreateDraftBatch_SupplierGrouping verifies that two products with different
// suppliers produce two separate drafts.
func TestCreateDraftBatch_SupplierGrouping(t *testing.T) {
	tid := uuid.New()
	uid := uuid.New()
	sid1 := uuid.New()
	sid2 := uuid.New()
	p1 := uuid.New()
	p2 := uuid.New()
	p3 := uuid.New()

	creator := &stubDraftCreator{}
	uc := replenish.NewCreateDraftBatchUseCase(creator, nil)

	out, err := uc.Execute(context.Background(), replenish.DraftBatchRequest{
		TenantID:  tid,
		CreatorID: uid,
		Lines: []replenish.DraftBatchLine{
			{ProductID: p1, SupplierID: &sid1, Qty: decimal.NewFromInt(10)},
			{ProductID: p2, SupplierID: &sid2, Qty: decimal.NewFromInt(5)},
			{ProductID: p3, SupplierID: &sid1, Qty: decimal.NewFromInt(3)},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Drafts) != 2 {
		t.Fatalf("expected 2 drafts (one per supplier), got %d", len(out.Drafts))
	}
	if len(creator.calls) != 2 {
		t.Fatalf("expected 2 CreatePurchaseDraft calls, got %d", len(creator.calls))
	}

	// sid1 group should have 2 items (p1 + p3).
	totalItems := 0
	for _, c := range creator.calls {
		totalItems += len(c.Items)
	}
	if totalItems != 3 {
		t.Errorf("expected 3 total items across all calls, got %d", totalItems)
	}
}

// TestCreateDraftBatch_NoSupplierGroup verifies nil supplier_id lines go to their
// own draft with no partner_id set.
func TestCreateDraftBatch_NoSupplierGroup(t *testing.T) {
	tid := uuid.New()
	uid := uuid.New()
	p1 := uuid.New()

	creator := &stubDraftCreator{}
	uc := replenish.NewCreateDraftBatchUseCase(creator, nil)

	out, err := uc.Execute(context.Background(), replenish.DraftBatchRequest{
		TenantID:  tid,
		CreatorID: uid,
		Lines: []replenish.DraftBatchLine{
			{ProductID: p1, SupplierID: nil, Qty: decimal.NewFromInt(7)},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Drafts) != 1 {
		t.Fatalf("expected 1 draft, got %d", len(out.Drafts))
	}
	if creator.calls[0].PartnerID != nil {
		t.Errorf("expected nil PartnerID for no-supplier group, got %v", creator.calls[0].PartnerID)
	}
}

// TestCreateDraftBatch_ZeroQtySkipped verifies zero-qty lines are silently dropped.
func TestCreateDraftBatch_ZeroQtySkipped(t *testing.T) {
	tid := uuid.New()
	uid := uuid.New()
	sid := uuid.New()
	p1 := uuid.New()
	p2 := uuid.New()

	creator := &stubDraftCreator{}
	uc := replenish.NewCreateDraftBatchUseCase(creator, nil)

	out, err := uc.Execute(context.Background(), replenish.DraftBatchRequest{
		TenantID:  tid,
		CreatorID: uid,
		Lines: []replenish.DraftBatchLine{
			{ProductID: p1, SupplierID: &sid, Qty: decimal.Zero},          // must be skipped
			{ProductID: p2, SupplierID: &sid, Qty: decimal.NewFromInt(4)}, // kept
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Drafts) != 1 {
		t.Fatalf("expected 1 draft, got %d", len(out.Drafts))
	}
	// Only p2 should appear in the draft items.
	if creator.calls[0].Items[0].ProductID != p2 {
		t.Errorf("expected only p2 in items, got %v", creator.calls[0].Items[0].ProductID)
	}
}

// TestCreateDraftBatch_Idempotency verifies that creator error is propagated.
func TestCreateDraftBatch_CreatorError_Propagated(t *testing.T) {
	tid := uuid.New()
	uid := uuid.New()
	p1 := uuid.New()
	sid := uuid.New()

	creator := &stubDraftCreator{err: errors.New("db error")}
	uc := replenish.NewCreateDraftBatchUseCase(creator, nil)

	_, err := uc.Execute(context.Background(), replenish.DraftBatchRequest{
		TenantID:  tid,
		CreatorID: uid,
		Lines: []replenish.DraftBatchLine{
			{ProductID: p1, SupplierID: &sid, Qty: decimal.NewFromInt(5)},
		},
	})
	if err == nil {
		t.Error("expected error from creator, got nil")
	}
}

// TestCreateDraftBatch_EmptyLines returns error on empty lines.
func TestCreateDraftBatch_EmptyLines(t *testing.T) {
	creator := &stubDraftCreator{}
	uc := replenish.NewCreateDraftBatchUseCase(creator, nil)

	_, err := uc.Execute(context.Background(), replenish.DraftBatchRequest{
		TenantID:  uuid.New(),
		CreatorID: uuid.New(),
		Lines:     nil,
	})
	if err == nil {
		t.Error("expected error for empty lines, got nil")
	}
}

// stubPriceLookup is a test double for PriceLookup.
type stubPriceLookup struct {
	prices   map[uuid.UUID]decimal.Decimal
	err      error
	calls    int
	gotPairs []replenish.ProductSupplier
}

func (s *stubPriceLookup) LastPurchasePrices(_ context.Context, _ uuid.UUID, pairs []replenish.ProductSupplier) (map[uuid.UUID]decimal.Decimal, error) {
	s.calls++
	s.gotPairs = pairs
	if s.err != nil {
		return nil, s.err
	}
	return s.prices, nil
}

// TestCreateDraftBatch_Execute_PriceBackfill verifies draft lines pick up the
// batch-fetched last purchase price, with zero fallback for unknown products,
// and that the lookup runs exactly once for the whole batch.
func TestCreateDraftBatch_Execute_PriceBackfill(t *testing.T) {
	tid := uuid.New()
	uid := uuid.New()
	sid := uuid.New()
	pPriced := uuid.New()
	pUnpriced := uuid.New()

	creator := &stubDraftCreator{}
	lookup := &stubPriceLookup{prices: map[uuid.UUID]decimal.Decimal{
		pPriced: decimal.NewFromFloat(12.5),
	}}
	uc := replenish.NewCreateDraftBatchUseCase(creator, nil).WithPriceLookup(lookup)

	_, err := uc.Execute(context.Background(), replenish.DraftBatchRequest{
		TenantID:  tid,
		CreatorID: uid,
		Lines: []replenish.DraftBatchLine{
			{ProductID: pPriced, SupplierID: &sid, Qty: decimal.NewFromInt(10)},
			{ProductID: pUnpriced, SupplierID: &sid, Qty: decimal.NewFromInt(5)},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lookup.calls != 1 {
		t.Fatalf("expected exactly 1 batch price lookup, got %d", lookup.calls)
	}
	if len(lookup.gotPairs) != 2 {
		t.Fatalf("expected 2 pairs in lookup, got %d", len(lookup.gotPairs))
	}

	cases := []struct {
		name      string
		productID uuid.UUID
		wantPrice decimal.Decimal
	}{
		{"priced_product_gets_last_price", pPriced, decimal.NewFromFloat(12.5)},
		{"unknown_product_falls_back_to_zero", pUnpriced, decimal.Zero},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			found := false
			for _, call := range creator.calls {
				for _, it := range call.Items {
					if it.ProductID == tc.productID {
						found = true
						if !it.UnitPrice.Equal(tc.wantPrice) {
							t.Errorf("UnitPrice = %s, want %s", it.UnitPrice, tc.wantPrice)
						}
					}
				}
			}
			if !found {
				t.Errorf("product %s not found in any draft", tc.productID)
			}
		})
	}
}

// TestCreateDraftBatch_Execute_NilPriceLookup_ZeroPrices verifies the
// backward-compatible path: without a PriceLookup every line stays at zero.
func TestCreateDraftBatch_Execute_NilPriceLookup_ZeroPrices(t *testing.T) {
	creator := &stubDraftCreator{}
	uc := replenish.NewCreateDraftBatchUseCase(creator, nil) // no WithPriceLookup

	_, err := uc.Execute(context.Background(), replenish.DraftBatchRequest{
		TenantID:  uuid.New(),
		CreatorID: uuid.New(),
		Lines: []replenish.DraftBatchLine{
			{ProductID: uuid.New(), Qty: decimal.NewFromInt(3)},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !creator.calls[0].Items[0].UnitPrice.IsZero() {
		t.Errorf("expected zero UnitPrice without PriceLookup, got %s", creator.calls[0].Items[0].UnitPrice)
	}
}

// TestCreateDraftBatch_Execute_PriceLookupError_Propagated verifies a failing
// price lookup aborts the batch before any draft is created.
func TestCreateDraftBatch_Execute_PriceLookupError_Propagated(t *testing.T) {
	creator := &stubDraftCreator{}
	lookup := &stubPriceLookup{err: errors.New("price db error")}
	uc := replenish.NewCreateDraftBatchUseCase(creator, nil).WithPriceLookup(lookup)

	_, err := uc.Execute(context.Background(), replenish.DraftBatchRequest{
		TenantID:  uuid.New(),
		CreatorID: uuid.New(),
		Lines: []replenish.DraftBatchLine{
			{ProductID: uuid.New(), Qty: decimal.NewFromInt(1)},
		},
	})
	if err == nil {
		t.Fatal("expected error from price lookup, got nil")
	}
	if len(creator.calls) != 0 {
		t.Errorf("expected no drafts created after lookup failure, got %d", len(creator.calls))
	}
}
