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
			{ProductID: p1, SupplierID: &sid, Qty: decimal.Zero},       // must be skipped
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
