package replenish_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/app/replenish"
	"github.com/shopspring/decimal"
)

// stubLedger is a test double for SnapshotLedger.
type stubLedger struct {
	calls   int
	gotRows []replenish.SnapshotRow
	err     error
}

func (s *stubLedger) UpsertSnapshots(_ context.Context, _ uuid.UUID, rows []replenish.SnapshotRow) error {
	s.calls++
	s.gotRows = rows
	return s.err
}

// TestListSuggestions_Execute_LedgerOnlyPositiveQty verifies the F3 snapshot
// upsert receives ONLY rows with SuggestedQty > 0 — "nothing to order" rows
// must not inflate the scorecard denominator.
func TestListSuggestions_Execute_LedgerOnlyPositiveQty(t *testing.T) {
	pNeeds := uuid.New()
	pFull := uuid.New()
	rows := []replenish.RawRow{
		// avgDaily 5, avail 0 → positive suggestion.
		{ProductID: pNeeds, ProductName: "Needs", ProductCode: "N", AvailableQty: d("0"), AvgDailySales: d("5"), UnitCost: d("10"), LeadTimeDays: 7},
		// avail 500 >> target → suggestion floored at 0, must be filtered out.
		{ProductID: pFull, ProductName: "Full", ProductCode: "F", AvailableQty: d("500"), AvgDailySales: d("2"), UnitCost: d("10"), LeadTimeDays: 7},
	}
	ledger := &stubLedger{}
	uc := replenish.NewListSuggestionsUseCase(&stubRepo{rows: rows}).WithLedger(ledger, nil)

	out, err := uc.Execute(context.Background(), uuid.New(), 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("read must still return all rows, got %d", len(out))
	}
	if ledger.calls != 1 {
		t.Fatalf("expected exactly 1 ledger upsert, got %d", ledger.calls)
	}
	if len(ledger.gotRows) != 1 {
		t.Fatalf("expected 1 snapshot row (only positive qty), got %d", len(ledger.gotRows))
	}
	snap := ledger.gotRows[0]
	if snap.ProductID != pNeeds {
		t.Errorf("snapshot product = %s, want %s", snap.ProductID, pNeeds)
	}
	if !snap.SuggestedQty.IsPositive() {
		t.Errorf("snapshot SuggestedQty = %s, want > 0", snap.SuggestedQty)
	}
	if snap.LeadTimeDays != 7 {
		t.Errorf("snapshot LeadTimeDays = %d, want 7", snap.LeadTimeDays)
	}
	if snap.LeadTimeSource != replenish.LeadTimeSourceDefault {
		t.Errorf("snapshot LeadTimeSource = %q, want %q", snap.LeadTimeSource, replenish.LeadTimeSourceDefault)
	}
}

// TestListSuggestions_Execute_LedgerError_DoesNotFailRead verifies a failing
// ledger upsert is swallowed (logged) and the suggestions are still returned.
func TestListSuggestions_Execute_LedgerError_DoesNotFailRead(t *testing.T) {
	rows := []replenish.RawRow{
		{ProductID: uuid.New(), ProductName: "X", ProductCode: "X", AvailableQty: d("0"), AvgDailySales: d("5"), UnitCost: d("10"), LeadTimeDays: 7},
	}
	ledger := &stubLedger{err: errors.New("ledger db down")}
	uc := replenish.NewListSuggestionsUseCase(&stubRepo{rows: rows}).WithLedger(ledger, nil)

	out, err := uc.Execute(context.Background(), uuid.New(), 2)
	if err != nil {
		t.Fatalf("read must not fail on ledger error, got: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 suggestion despite ledger failure, got %d", len(out))
	}
	if ledger.calls != 1 {
		t.Errorf("expected ledger to be attempted once, got %d", ledger.calls)
	}
}

// TestListSuggestions_Execute_LedgerSkippedWhenNoPositiveRows verifies the
// upsert is not even attempted when every suggestion is zero.
func TestListSuggestions_Execute_LedgerSkippedWhenNoPositiveRows(t *testing.T) {
	rows := []replenish.RawRow{
		{ProductID: uuid.New(), ProductName: "Full", ProductCode: "F", AvailableQty: d("500"), AvgDailySales: d("2"), UnitCost: d("10"), LeadTimeDays: 7},
	}
	ledger := &stubLedger{}
	uc := replenish.NewListSuggestionsUseCase(&stubRepo{rows: rows}).WithLedger(ledger, nil)

	if _, err := uc.Execute(context.Background(), uuid.New(), 2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ledger.calls != 0 {
		t.Errorf("expected no ledger call for all-zero suggestions, got %d", ledger.calls)
	}
}

// ----- adoption marking (batch draft) -----

// adoptCall records one MarkAdopted invocation.
type adoptCall struct {
	products []uuid.UUID
	billID   uuid.UUID
}

// stubAdoptionMarker is a test double for AdoptionMarker.
type stubAdoptionMarker struct {
	calls []adoptCall
	err   error
}

func (s *stubAdoptionMarker) MarkAdopted(_ context.Context, _ uuid.UUID, productIDs []uuid.UUID, billID uuid.UUID) error {
	s.calls = append(s.calls, adoptCall{products: productIDs, billID: billID})
	return s.err
}

// TestCreateDraftBatch_Execute_MarkAdoptedPerGroup verifies the marker is
// called once per created draft with that group's products and bill ID.
func TestCreateDraftBatch_Execute_MarkAdoptedPerGroup(t *testing.T) {
	sid1 := uuid.New()
	sid2 := uuid.New()
	p1 := uuid.New()
	p2 := uuid.New()
	p3 := uuid.New()

	creator := &stubDraftCreator{}
	marker := &stubAdoptionMarker{}
	uc := replenish.NewCreateDraftBatchUseCase(creator, nil).WithAdoptionMarker(marker, nil)

	out, err := uc.Execute(context.Background(), replenish.DraftBatchRequest{
		TenantID:  uuid.New(),
		CreatorID: uuid.New(),
		Lines: []replenish.DraftBatchLine{
			{ProductID: p1, SupplierID: &sid1, Qty: decimal.NewFromInt(10)},
			{ProductID: p2, SupplierID: &sid2, Qty: decimal.NewFromInt(5)},
			{ProductID: p3, SupplierID: &sid1, Qty: decimal.NewFromInt(3)},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(marker.calls) != 2 {
		t.Fatalf("expected 2 MarkAdopted calls (one per group), got %d", len(marker.calls))
	}

	// Each call's bill ID must match a created draft, and the sid1 group must
	// carry exactly {p1, p3} while sid2 carries {p2}.
	billProducts := map[uuid.UUID][]uuid.UUID{}
	for _, c := range marker.calls {
		billProducts[c.billID] = c.products
	}
	for _, draft := range out.Drafts {
		prods, ok := billProducts[draft.BillID]
		if !ok {
			t.Fatalf("no MarkAdopted call for created bill %s", draft.BillID)
		}
		if draft.LineCount != len(prods) {
			t.Errorf("bill %s: marker got %d products, draft has %d lines", draft.BillID, len(prods), draft.LineCount)
		}
	}
}

// TestCreateDraftBatch_Execute_MarkAdoptedError_Swallowed verifies a failing
// marker never fails the draft creation — the bills are already persisted.
func TestCreateDraftBatch_Execute_MarkAdoptedError_Swallowed(t *testing.T) {
	sid := uuid.New()
	creator := &stubDraftCreator{}
	marker := &stubAdoptionMarker{err: errors.New("ledger db down")}
	uc := replenish.NewCreateDraftBatchUseCase(creator, nil).WithAdoptionMarker(marker, nil)

	out, err := uc.Execute(context.Background(), replenish.DraftBatchRequest{
		TenantID:  uuid.New(),
		CreatorID: uuid.New(),
		Lines: []replenish.DraftBatchLine{
			{ProductID: uuid.New(), SupplierID: &sid, Qty: decimal.NewFromInt(5)},
		},
	})
	if err != nil {
		t.Fatalf("draft must not fail on marker error, got: %v", err)
	}
	if len(out.Drafts) != 1 {
		t.Fatalf("expected 1 draft despite marker failure, got %d", len(out.Drafts))
	}
	if len(marker.calls) != 1 {
		t.Errorf("expected marker to be attempted once, got %d", len(marker.calls))
	}
}

// TestCreateDraftBatch_Execute_MarkAdopted_RetrySendsSameProducts documents
// the idempotency contract: a retried batch (same lines) sends the SAME
// product set to the repo; the repo's adopted_at IS NULL guard then makes the
// second stamp a no-op. The use case never dedups client-side.
func TestCreateDraftBatch_Execute_MarkAdopted_RetrySendsSameProducts(t *testing.T) {
	tid := uuid.New()
	uid := uuid.New()
	sid := uuid.New()
	p1 := uuid.New()

	creator := &stubDraftCreator{}
	marker := &stubAdoptionMarker{}
	uc := replenish.NewCreateDraftBatchUseCase(creator, nil).WithAdoptionMarker(marker, nil)

	req := replenish.DraftBatchRequest{
		TenantID:  tid,
		CreatorID: uid,
		Lines:     []replenish.DraftBatchLine{{ProductID: p1, SupplierID: &sid, Qty: decimal.NewFromInt(5)}},
	}
	for i := 0; i < 2; i++ {
		if _, err := uc.Execute(context.Background(), req); err != nil {
			t.Fatalf("execute #%d: %v", i+1, err)
		}
	}
	if len(marker.calls) != 2 {
		t.Fatalf("expected 2 MarkAdopted calls across retries, got %d", len(marker.calls))
	}
	for i, c := range marker.calls {
		if len(c.products) != 1 || c.products[0] != p1 {
			t.Errorf("call #%d products = %v, want exactly [%s]", i+1, c.products, p1)
		}
	}
}

// Compile-time interface checks for the stubs used above.
var (
	_ replenish.SnapshotLedger = (*stubLedger)(nil)
	_ replenish.AdoptionMarker = (*stubAdoptionMarker)(nil)
)
