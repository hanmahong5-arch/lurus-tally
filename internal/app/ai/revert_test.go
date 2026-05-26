package ai_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appai "github.com/hanmahong5-arch/lurus-tally/internal/app/ai"
	domainai "github.com/hanmahong5-arch/lurus-tally/internal/domain/ai"
)

// --- fakes ---

type fakeStockReverter struct {
	affected int
	err      error
	gotPlanID uuid.UUID
}

func (f *fakeStockReverter) RevertStockAdjust(_ context.Context, _, _, planID uuid.UUID) (int, error) {
	f.gotPlanID = planID
	return f.affected, f.err
}

type fakePriceReverter struct {
	affected int
	err      error
	gotEntries []appai.PriceBeforeEntry
}

func (f *fakePriceReverter) RestorePrices(_ context.Context, _ uuid.UUID, entries []appai.PriceBeforeEntry) (int, error) {
	f.gotEntries = entries
	return f.affected, f.err
}

type fakePriceSnapshotStore struct {
	entries map[string][]appai.PriceBeforeEntry
	saveErr error
	getErr  error
}

func newFakePriceSnapshotStore() *fakePriceSnapshotStore {
	return &fakePriceSnapshotStore{entries: make(map[string][]appai.PriceBeforeEntry)}
}

func (f *fakePriceSnapshotStore) SaveSnapshot(_ context.Context, tenantID, planID uuid.UUID, entries []appai.PriceBeforeEntry) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.entries[planID.String()] = entries
	return nil
}

func (f *fakePriceSnapshotStore) GetSnapshot(_ context.Context, _, planID uuid.UUID) ([]appai.PriceBeforeEntry, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	entries, ok := f.entries[planID.String()]
	if !ok {
		return nil, nil
	}
	// consume-once
	delete(f.entries, planID.String())
	return entries, nil
}

// --- helper ---

func newPendingPlan(planType domainai.PlanType) *domainai.Plan {
	return &domainai.Plan{
		ID:        uuid.New(),
		TenantID:  uuid.New(),
		Type:      planType,
		Status:    domainai.PlanStatusPending,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(30 * time.Minute),
	}
}

func newConfirmedPlan(planType domainai.PlanType) *domainai.Plan {
	p := newPendingPlan(planType)
	p.Status = domainai.PlanStatusConfirmed
	return p
}

// --- tests ---

// TestReverter_StockAdjust_HappyPath verifies that a confirmed stock-adjust plan
// is reverted and its status flipped to cancelled.
func TestReverter_StockAdjust_HappyPath(t *testing.T) {
	store := newMockPlanStore()
	plan := newConfirmedPlan(domainai.PlanTypeBulkStockAdjust)
	_ = store.SavePlan(context.Background(), plan)

	sr := &fakeStockReverter{affected: 3}
	snap := newFakePriceSnapshotStore()
	r := appai.NewReverter(store, sr, &fakePriceReverter{}, snap).
		WithUndoTTL(30 * time.Second)

	res, err := r.RevertPlan(context.Background(), plan.TenantID, uuid.New(), plan.ID)
	if err != nil {
		t.Fatalf("RevertPlan: %v", err)
	}
	if res.AffectedCount != 3 {
		t.Errorf("affected=%d, want 3", res.AffectedCount)
	}
	if res.RevertedType != string(domainai.PlanTypeBulkStockAdjust) {
		t.Errorf("type=%s, want bulk_stock_adjust", res.RevertedType)
	}
	if sr.gotPlanID != plan.ID {
		t.Errorf("reverter called with wrong planID")
	}

	// Status must have been flipped to cancelled.
	persisted, _ := store.GetPlan(context.Background(), plan.TenantID, plan.ID)
	if persisted.Status != domainai.PlanStatusCancelled {
		t.Errorf("status=%s after revert, want cancelled", persisted.Status)
	}
}

// TestReverter_PriceChange_HappyPath verifies that a confirmed price-change plan
// is reverted using the before-state snapshot.
func TestReverter_PriceChange_HappyPath(t *testing.T) {
	store := newMockPlanStore()
	plan := newConfirmedPlan(domainai.PlanTypePriceChange)
	_ = store.SavePlan(context.Background(), plan)

	snap := newFakePriceSnapshotStore()
	entries := []appai.PriceBeforeEntry{
		{SKUID: uuid.New(), OldPrice: decimal.NewFromFloat(99.0)},
	}
	_ = snap.SaveSnapshot(context.Background(), plan.TenantID, plan.ID, entries)

	pr := &fakePriceReverter{affected: 1}
	r := appai.NewReverter(store, &fakeStockReverter{}, pr, snap).
		WithUndoTTL(30 * time.Second)

	res, err := r.RevertPlan(context.Background(), plan.TenantID, uuid.New(), plan.ID)
	if err != nil {
		t.Fatalf("RevertPlan: %v", err)
	}
	if res.AffectedCount != 1 {
		t.Errorf("affected=%d, want 1", res.AffectedCount)
	}
	if len(pr.gotEntries) != 1 || !pr.gotEntries[0].OldPrice.Equal(decimal.NewFromFloat(99.0)) {
		t.Errorf("wrong entries passed to price reverter: %+v", pr.gotEntries)
	}
}

// TestReverter_StockAdjust_AlreadyReverted_ReturnsErrAlreadyReverted verifies
// idempotency: a plan whose status is already cancelled cannot be reverted again.
func TestReverter_StockAdjust_AlreadyReverted_ReturnsErrAlreadyReverted(t *testing.T) {
	store := newMockPlanStore()
	plan := newConfirmedPlan(domainai.PlanTypeBulkStockAdjust)
	plan.Status = domainai.PlanStatusCancelled
	_ = store.SavePlan(context.Background(), plan)

	r := appai.NewReverter(store, &fakeStockReverter{}, &fakePriceReverter{}, newFakePriceSnapshotStore())

	_, err := r.RevertPlan(context.Background(), plan.TenantID, uuid.New(), plan.ID)
	if err != appai.ErrAlreadyReverted {
		t.Fatalf("err=%v, want ErrAlreadyReverted", err)
	}
}

// TestReverter_WindowClosed_ReturnsErrRevertWindowClosed verifies that plans older
// than UndoTTL are rejected.
func TestReverter_WindowClosed_ReturnsErrRevertWindowClosed(t *testing.T) {
	store := newMockPlanStore()
	plan := newConfirmedPlan(domainai.PlanTypeBulkStockAdjust)
	plan.CreatedAt = time.Now().Add(-60 * time.Second) // older than 30 s TTL
	_ = store.SavePlan(context.Background(), plan)

	r := appai.NewReverter(store, &fakeStockReverter{}, &fakePriceReverter{}, newFakePriceSnapshotStore()).
		WithUndoTTL(30 * time.Second)

	_, err := r.RevertPlan(context.Background(), plan.TenantID, uuid.New(), plan.ID)
	if err != appai.ErrRevertWindowClosed {
		t.Fatalf("err=%v, want ErrRevertWindowClosed", err)
	}
}

// TestReverter_NotFound_ReturnsErrPlanNotFound verifies missing plan handling.
func TestReverter_NotFound_ReturnsErrPlanNotFound(t *testing.T) {
	store := newMockPlanStore()
	r := appai.NewReverter(store, &fakeStockReverter{}, &fakePriceReverter{}, newFakePriceSnapshotStore())

	_, err := r.RevertPlan(context.Background(), uuid.New(), uuid.New(), uuid.New())
	if err != appai.ErrPlanNotFound {
		t.Fatalf("err=%v, want ErrPlanNotFound", err)
	}
}

// TestReverter_PurchaseDraft_ReturnsErrPlanNotRevertible verifies that purchase-draft
// plans are rejected (purchase undo is handled client-side via bill cancel).
func TestReverter_PurchaseDraft_ReturnsErrPlanNotRevertible(t *testing.T) {
	store := newMockPlanStore()
	plan := newConfirmedPlan(domainai.PlanTypeCreatePurchase)
	_ = store.SavePlan(context.Background(), plan)

	r := appai.NewReverter(store, &fakeStockReverter{}, &fakePriceReverter{}, newFakePriceSnapshotStore())

	_, err := r.RevertPlan(context.Background(), plan.TenantID, uuid.New(), plan.ID)
	if err != appai.ErrPlanNotRevertible {
		t.Fatalf("err=%v, want ErrPlanNotRevertible", err)
	}
}

// TestReverter_PriceChange_NoSnapshot_ReturnsErrRevertWindowClosed verifies that
// a missing price snapshot (TTL expired or already consumed) returns the correct error.
func TestReverter_PriceChange_NoSnapshot_ReturnsErrRevertWindowClosed(t *testing.T) {
	store := newMockPlanStore()
	plan := newConfirmedPlan(domainai.PlanTypePriceChange)
	_ = store.SavePlan(context.Background(), plan)

	// No snapshot saved — simulates TTL expiry.
	r := appai.NewReverter(store, &fakeStockReverter{}, &fakePriceReverter{}, newFakePriceSnapshotStore()).
		WithUndoTTL(30 * time.Second)

	_, err := r.RevertPlan(context.Background(), plan.TenantID, uuid.New(), plan.ID)
	if err != appai.ErrRevertWindowClosed {
		t.Fatalf("err=%v, want ErrRevertWindowClosed", err)
	}
}
