package ai_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appai "github.com/hanmahong5-arch/lurus-tally/internal/app/ai"
	domainai "github.com/hanmahong5-arch/lurus-tally/internal/domain/ai"
)

// --- mock ports ---

type fakeDraftCreator struct {
	gotLines []appai.DraftLine
	billID   uuid.UUID
	billNo   string
	err      error
}

func (f *fakeDraftCreator) CreatePurchaseDraft(_ context.Context, _, _ uuid.UUID, lines []appai.DraftLine) (uuid.UUID, string, error) {
	f.gotLines = lines
	return f.billID, f.billNo, f.err
}

type fakePriceChanger struct {
	gotIDs   []uuid.UUID
	gotAct   string
	affected int
	err      error
}

func (f *fakePriceChanger) ApplyPriceChange(_ context.Context, _ uuid.UUID, ids []uuid.UUID, action string) (int, error) {
	f.gotIDs = ids
	f.gotAct = action
	return f.affected, f.err
}

type fakeStockAdjuster struct {
	calls int
	err   error
}

func (f *fakeStockAdjuster) AdjustStock(_ context.Context, _, _, _ uuid.UUID, _ decimal.Decimal) error {
	f.calls++
	return f.err
}

func newExecutor(rows []appai.ProductRow, d *fakeDraftCreator, p *fakePriceChanger, s *fakeStockAdjuster) *appai.DefaultPlanExecutor {
	return appai.NewPlanExecutor(&mockProductRepo{rows: rows}, d, p, s)
}

func TestExecutor_Purchase_ResolvesNamesAndCreatesDraft(t *testing.T) {
	tenantID := uuid.New()
	prodID := uuid.New()
	billID := uuid.New()
	d := &fakeDraftCreator{billID: billID, billNo: "PO-1"}
	ex := newExecutor([]appai.ProductRow{{ID: prodID, Name: "Widget A"}}, d, &fakePriceChanger{}, &fakeStockAdjuster{})

	plan := &domainai.Plan{
		ID: uuid.New(), TenantID: tenantID, Type: domainai.PlanTypeCreatePurchase,
		Status: domainai.PlanStatusConfirmed, ExpiresAt: time.Now().Add(time.Hour),
		Payload: map[string]interface{}{
			"items": []map[string]interface{}{{"product_name": "Widget A", "qty": 5.0}},
		},
	}
	res, err := ex.Execute(context.Background(), uuid.New(), plan)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.BillID == nil || *res.BillID != billID || res.BillNo != "PO-1" {
		t.Errorf("unexpected result: %+v", res)
	}
	if len(d.gotLines) != 1 || d.gotLines[0].ProductID != prodID || !d.gotLines[0].Qty.Equal(decimal.NewFromInt(5)) {
		t.Errorf("unexpected lines: %+v", d.gotLines)
	}
}

func TestExecutor_Purchase_UnresolvedProduct_Errors(t *testing.T) {
	d := &fakeDraftCreator{}
	ex := newExecutor(nil, d, &fakePriceChanger{}, &fakeStockAdjuster{}) // SearchProducts returns nothing

	plan := &domainai.Plan{
		ID: uuid.New(), TenantID: uuid.New(), Type: domainai.PlanTypeCreatePurchase,
		Payload: map[string]interface{}{
			"items": []map[string]interface{}{{"product_name": "Ghost", "qty": 1.0}},
		},
	}
	if _, err := ex.Execute(context.Background(), uuid.New(), plan); err == nil {
		t.Fatal("expected error for unresolved product")
	}
	if d.gotLines != nil {
		t.Error("draft creator must not be called when a product is unresolved")
	}
}

func TestExecutor_PriceChange_ResolvesFilterAndApplies(t *testing.T) {
	p := &fakePriceChanger{affected: 3}
	rows := []appai.ProductRow{{ID: uuid.New(), Name: "A"}, {ID: uuid.New(), Name: "B"}}
	ex := newExecutor(rows, &fakeDraftCreator{}, p, &fakeStockAdjuster{})

	plan := &domainai.Plan{
		ID: uuid.New(), TenantID: uuid.New(), Type: domainai.PlanTypePriceChange,
		Payload: map[string]interface{}{"filter": "brand-x", "action": "+5%"},
	}
	res, err := ex.Execute(context.Background(), uuid.New(), plan)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.AffectedCount != 3 {
		t.Errorf("affected=%d, want 3", res.AffectedCount)
	}
	if p.gotAct != "+5%" || len(p.gotIDs) != 2 {
		t.Errorf("price changer got action=%q ids=%d", p.gotAct, len(p.gotIDs))
	}
}

func TestExecutor_StockAdjust_AppliesPerProduct(t *testing.T) {
	s := &fakeStockAdjuster{}
	rows := []appai.ProductRow{{ID: uuid.New(), Name: "A"}, {ID: uuid.New(), Name: "B"}}
	ex := newExecutor(rows, &fakeDraftCreator{}, &fakePriceChanger{}, s)

	plan := &domainai.Plan{
		ID: uuid.New(), TenantID: uuid.New(), Type: domainai.PlanTypeBulkStockAdjust,
		Payload: map[string]interface{}{"filter": "all", "delta": -2.0},
	}
	res, err := ex.Execute(context.Background(), uuid.New(), plan)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.AffectedCount != 2 || s.calls != 2 {
		t.Errorf("affected=%d calls=%d, want 2/2", res.AffectedCount, s.calls)
	}
}

func TestExecutor_StockAdjust_FailFast_ReportsPartial(t *testing.T) {
	s := &fakeStockAdjuster{err: errors.New("insufficient")}
	rows := []appai.ProductRow{{ID: uuid.New(), Name: "A"}}
	ex := newExecutor(rows, &fakeDraftCreator{}, &fakePriceChanger{}, s)

	plan := &domainai.Plan{
		ID: uuid.New(), TenantID: uuid.New(), Type: domainai.PlanTypeBulkStockAdjust,
		Payload: map[string]interface{}{"filter": "all", "delta": -2.0},
	}
	if _, err := ex.Execute(context.Background(), uuid.New(), plan); err == nil {
		t.Fatal("expected error when an adjust fails")
	}
}
