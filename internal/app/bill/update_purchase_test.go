package bill_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// TestUpdatePurchaseDraft_OnlyAllowedInDraft verifies that updating an approved bill returns an error.
func TestUpdatePurchaseDraft_OnlyAllowedInDraft(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewUpdatePurchaseDraftUseCase(repo)

	warehouseID := uuid.New()
	billID := seedDraftBill(repo, 1, warehouseID)
	// Force to approved.
	repo.billsByID[billID].Status = domain.StatusApproved

	_, err := uc.Execute(context.Background(), appbill.UpdatePurchaseDraftRequest{
		TenantID: testTenantID,
		BillID:   billID,
		Items: []appbill.CreatePurchaseItemInput{
			{ProductID: uuid.New(), Qty: decimal.NewFromFloat(1), UnitPrice: decimal.NewFromFloat(10), LineNo: 1},
		},
	})
	if err == nil {
		t.Fatal("expected error when updating approved bill, got nil")
	}
}

// TestUpdatePurchaseDraft_RecalculatesTotals verifies that updating items recalculates totals.
func TestUpdatePurchaseDraft_RecalculatesTotals(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewUpdatePurchaseDraftUseCase(repo)

	warehouseID := uuid.New()
	billID := seedDraftBill(repo, 1, warehouseID)

	productID := uuid.New()
	req := appbill.UpdatePurchaseDraftRequest{
		TenantID:    testTenantID,
		BillID:      billID,
		ShippingFee: decimal.NewFromFloat(20),
		TaxAmount:   decimal.Zero,
		BillDate:    time.Now(),
		Items: []appbill.CreatePurchaseItemInput{
			{ProductID: productID, Qty: decimal.NewFromFloat(5), UnitPrice: decimal.NewFromFloat(10), LineNo: 1},
		},
	}
	head, err := uc.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// subtotal = 5*10 = 50; total = 50 + 20 = 70
	if !head.Subtotal.Equal(decimal.NewFromFloat(50)) {
		t.Errorf("Subtotal = %s, want 50", head.Subtotal)
	}
	if !head.TotalAmount.Equal(decimal.NewFromFloat(70)) {
		t.Errorf("TotalAmount = %s, want 70", head.TotalAmount)
	}
}

// TestUpdatePurchaseDraft_ForeignCurrencyKeepsRate verifies that editing a
// foreign-currency draft converts at the stored rate instead of falling back to 1:1.
func TestUpdatePurchaseDraft_ForeignCurrencyKeepsRate(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewUpdatePurchaseDraftUseCase(repo)

	billID := seedDraftBill(repo, 1, uuid.New())
	repo.billsByID[billID].Currency = "USD"
	repo.billsByID[billID].ExchangeRateVal = decimal.RequireFromString("7.2")

	head, err := uc.Execute(context.Background(), appbill.UpdatePurchaseDraftRequest{
		TenantID:    testTenantID,
		BillID:      billID,
		ShippingFee: decimal.NewFromInt(10),
		BillDate:    time.Now(),
		Items: []appbill.CreatePurchaseItemInput{
			{ProductID: uuid.New(), Qty: decimal.NewFromInt(2), UnitPrice: decimal.NewFromInt(100), LineNo: 1},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// original-currency total = 2*100 + 10 = 210; CNY total = 210 * 7.2 = 1512
	if !head.AmountLocal.Equal(decimal.NewFromInt(210)) {
		t.Errorf("AmountLocal = %s, want 210", head.AmountLocal)
	}
	if !head.TotalAmount.Equal(decimal.NewFromInt(1512)) {
		t.Errorf("TotalAmount = %s, want 1512", head.TotalAmount)
	}
}

// TestUpdatePurchaseDraft_KeepPricesAfterClerkEdit verifies that a price-blind edit
// keeps the unit price and fees the manager set, and prices new lines at zero.
func TestUpdatePurchaseDraft_KeepPricesAfterClerkEdit(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewUpdatePurchaseDraftUseCase(repo)

	billID := seedDraftBill(repo, 1, uuid.New())
	repo.billsByID[billID].ShippingFee = decimal.NewFromInt(4)
	productID := repo.itemsByBillID[billID][0].ProductID
	repo.itemsByBillID[billID][0].UnitPrice = decimal.NewFromInt(8)
	repo.itemsByBillID[billID][0].UnitID = nil // same (base) unit as the edit sends
	newProduct := uuid.New()

	head, err := uc.Execute(context.Background(), appbill.UpdatePurchaseDraftRequest{
		TenantID:   testTenantID,
		BillID:     billID,
		KeepPrices: true,
		Items: []appbill.CreatePurchaseItemInput{
			{ProductID: productID, Qty: decimal.NewFromInt(5), LineNo: 1},
			{ProductID: newProduct, Qty: decimal.NewFromInt(2), LineNo: 2},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// 5*8 + 2*0 = 40, plus kept shipping 4 = 44
	if !head.Subtotal.Equal(decimal.NewFromInt(40)) || !head.TotalAmount.Equal(decimal.NewFromInt(44)) {
		t.Errorf("Subtotal=%s Total=%s, want 40 / 44", head.Subtotal, head.TotalAmount)
	}
	if !head.ShippingFee.Equal(decimal.NewFromInt(4)) {
		t.Errorf("ShippingFee = %s, want 4", head.ShippingFee)
	}
	items := repo.itemsByBillID[billID]
	if len(items) != 2 || !items[0].UnitPrice.Equal(decimal.NewFromInt(8)) || !items[1].UnitPrice.IsZero() {
		t.Errorf("unit prices not kept: %+v", items)
	}
}

// unitFactors answers conversion factors by unit alone (the seeded lines use random
// products); a unit it does not know fails like a missing product_unit row.
type unitFactors map[uuid.UUID]decimal.Decimal

func (u unitFactors) GetConversionFactor(_ context.Context, _, unitID uuid.UUID) (decimal.Decimal, error) {
	f, ok := u[unitID]
	if !ok {
		return decimal.Zero, appbill.ErrInvalidUnitForProduct
	}
	return f, nil
}

// keepPriceUnitCase runs a price-blind edit that switches the first line from oldUnit
// to newUnit and returns the stored unit price. units nil means no lookup is wired.
func keepPriceUnitCase(t *testing.T, units unitFactors, oldUnit, newUnit *uuid.UUID, price string) decimal.Decimal {
	t.Helper()
	repo := newMockBillRepo()
	uc := appbill.NewUpdatePurchaseDraftUseCase(repo)
	if units != nil {
		uc = uc.WithUnitRepo(units)
	}
	billID := seedDraftBill(repo, 1, uuid.New())
	productID := repo.itemsByBillID[billID][0].ProductID
	repo.itemsByBillID[billID][0].UnitID = oldUnit
	repo.itemsByBillID[billID][0].UnitPrice = decimal.RequireFromString(price)

	_, err := uc.Execute(context.Background(), appbill.UpdatePurchaseDraftRequest{
		TenantID:   testTenantID,
		BillID:     billID,
		KeepPrices: true,
		Items: []appbill.CreatePurchaseItemInput{
			{ProductID: productID, UnitID: newUnit, Qty: decimal.NewFromInt(1), LineNo: 1},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return repo.itemsByBillID[billID][0].UnitPrice
}

// TestUpdatePurchaseDraft_KeepPricesFollowsUnitChange verifies that switching a line's
// unit rescales the kept price by the conversion factors instead of carrying the old
// per-unit number over to the new unit.
func TestUpdatePurchaseDraft_KeepPricesFollowsUnitChange(t *testing.T) {
	box, piece := uuid.New(), uuid.New()
	units := unitFactors{box: decimal.NewFromInt(12), piece: decimal.NewFromInt(1)}

	// box (12 pieces) -> piece: 12 per box is 1 per piece.
	if got := keepPriceUnitCase(t, units, &box, &piece, "12"); !got.Equal(decimal.NewFromInt(1)) {
		t.Errorf("box->piece price = %s, want 1", got)
	}
	// piece -> box: 1 per piece is 12 per box.
	if got := keepPriceUnitCase(t, units, &piece, &box, "1"); !got.Equal(decimal.NewFromInt(12)) {
		t.Errorf("piece->box price = %s, want 12", got)
	}
	// unchanged unit keeps the number as is.
	if got := keepPriceUnitCase(t, units, &box, &box, "12"); !got.Equal(decimal.NewFromInt(12)) {
		t.Errorf("same unit price = %s, want 12", got)
	}
	// no unit on the old line is the base unit (factor 1).
	if got := keepPriceUnitCase(t, units, nil, &box, "1"); !got.Equal(decimal.NewFromInt(12)) {
		t.Errorf("base->box price = %s, want 12", got)
	}
	// a non-terminating ratio is rounded to the column's 6 places.
	three := uuid.New()
	units[three] = decimal.NewFromInt(3)
	if got := keepPriceUnitCase(t, units, &three, &piece, "1"); !got.Equal(decimal.RequireFromString("0.333333")) {
		t.Errorf("three->piece price = %s, want 0.333333", got)
	}
}

// TestUpdatePurchaseDraft_KeepPricesUnitChangeUnknownFactorIsZero verifies that when
// the factor cannot be resolved the price is cleared for the boss to fill in, never
// guessed.
func TestUpdatePurchaseDraft_KeepPricesUnitChangeUnknownFactorIsZero(t *testing.T) {
	box, piece := uuid.New(), uuid.New()
	// the lookup knows nothing: every GetConversionFactor fails
	if got := keepPriceUnitCase(t, unitFactors{}, &box, &piece, "12"); !got.IsZero() {
		t.Errorf("unknown factor price = %s, want 0", got)
	}
	// no unit lookup wired at all
	if got := keepPriceUnitCase(t, nil, &box, &piece, "12"); !got.IsZero() {
		t.Errorf("no lookup price = %s, want 0", got)
	}
	// but an unchanged unit needs no lookup
	if got := keepPriceUnitCase(t, nil, &box, &box, "12"); !got.Equal(decimal.NewFromInt(12)) {
		t.Errorf("same unit without lookup price = %s, want 12", got)
	}
}
