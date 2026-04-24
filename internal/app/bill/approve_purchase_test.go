package bill_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// mockStockUC implements appbill.StockMovementExecutor for approve tests.
type mockStockUC struct {
	calls     []appstock.RecordMovementRequest
	failOnIdx int // -1 = never fail
	failErr   error
}

func newMockStockUC() *mockStockUC {
	return &mockStockUC{failOnIdx: -1}
}

func (m *mockStockUC) ExecuteInTx(_ context.Context, _ *sql.Tx, req appstock.RecordMovementRequest) (*domainstock.Snapshot, error) {
	if m.failOnIdx >= 0 && len(m.calls) == m.failOnIdx {
		return nil, m.failErr
	}
	m.calls = append(m.calls, req)
	snap := &domainstock.Snapshot{
		TenantID:    req.TenantID,
		ProductID:   req.ProductID,
		WarehouseID: req.WarehouseID,
		OnHandQty:   req.Qty,
		UnitCost:    req.UnitCost,
	}
	return snap, nil
}

// mockProductUnitRepo implements appbill.ProductUnitRepo.
type mockProductUnitRepo struct {
	factors map[string]decimal.Decimal // key: productID+":"+unitID
	err     error
}

func newMockProductUnitRepo() *mockProductUnitRepo {
	return &mockProductUnitRepo{factors: make(map[string]decimal.Decimal)}
}

func (m *mockProductUnitRepo) set(productID, unitID uuid.UUID, factor decimal.Decimal) {
	m.factors[productID.String()+":"+unitID.String()] = factor
}

func (m *mockProductUnitRepo) GetConversionFactor(_ context.Context, productID, unitID uuid.UUID) (decimal.Decimal, error) {
	if m.err != nil {
		return decimal.Zero, m.err
	}
	f, ok := m.factors[productID.String()+":"+unitID.String()]
	if !ok {
		return decimal.Zero, appbill.ErrInvalidUnitForProduct
	}
	return f, nil
}

// newApproveUC is a helper that wires up ApprovePurchaseUseCase with mocks.
func newApproveUC(repo *mockBillRepo, stockUC *mockStockUC, unitRepo *mockProductUnitRepo) *appbill.ApprovePurchaseUseCase {
	return appbill.NewApprovePurchaseUseCase(repo, stockUC, unitRepo)
}

// seedDraftBill seeds a draft bill with n items in the mock repo and returns the bill ID.
func seedDraftBill(repo *mockBillRepo, n int, warehouseID uuid.UUID) uuid.UUID {
	billID := uuid.New()
	now := time.Now()
	head := &domain.BillHead{
		ID:          billID,
		TenantID:    testTenantID,
		BillNo:      "PO-20260423-0001",
		BillType:    domain.BillTypePurchase,
		SubType:     domain.BillSubTypePurchase,
		Status:      domain.StatusDraft,
		WarehouseID: &warehouseID,
		CreatorID:   testCreatorID,
		BillDate:    now,
		Subtotal:    decimal.NewFromFloat(300),
		TotalAmount: decimal.NewFromFloat(300),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	items := make([]*domain.BillItem, n)
	for i := 0; i < n; i++ {
		uid := uuid.New()
		items[i] = &domain.BillItem{
			ID:         uuid.New(),
			TenantID:   testTenantID,
			HeadID:     billID,
			ProductID:  uuid.New(),
			UnitID:     &uid,
			LineNo:     i + 1,
			Qty:        decimal.NewFromFloat(10),
			UnitPrice:  decimal.NewFromFloat(10),
			LineAmount: decimal.NewFromFloat(100),
		}
	}
	repo.billsByID[billID] = head
	repo.itemsByBillID[billID] = items
	return billID
}

// seedProductUnitFactors registers a conversion factor of 1 for each item's unit.
func seedProductUnitFactors(unitRepo *mockProductUnitRepo, items []*domain.BillItem) {
	for _, it := range items {
		if it.UnitID != nil {
			unitRepo.set(it.ProductID, *it.UnitID, decimal.NewFromInt(1))
		}
	}
}

// TestApprovePurchase_HappyPath_StockMovementsCreated verifies that approving a 3-item bill
// creates 3 stock movements and sets the bill status to approved.
func TestApprovePurchase_HappyPath_StockMovementsCreated(t *testing.T) {
	repo := newMockBillRepo()
	stockUC := newMockStockUC()
	unitRepo := newMockProductUnitRepo()

	warehouseID := uuid.New()
	billID := seedDraftBill(repo, 3, warehouseID)
	seedProductUnitFactors(unitRepo, repo.itemsByBillID[billID])

	approvedBy := uuid.New()
	uc := newApproveUC(repo, stockUC, unitRepo)
	if err := uc.Execute(context.Background(), testTenantID, billID, approvedBy); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(stockUC.calls) != 3 {
		t.Errorf("stock movements = %d, want 3", len(stockUC.calls))
	}
	for _, c := range stockUC.calls {
		if c.Direction != domainstock.DirectionIn {
			t.Errorf("direction = %s, want in", c.Direction)
		}
		if c.ReferenceType != domainstock.RefPurchase {
			t.Errorf("reference_type = %s, want purchase", c.ReferenceType)
		}
	}

	head := repo.billsByID[billID]
	if head.Status != domain.StatusApproved {
		t.Errorf("status = %d, want %d (Approved)", head.Status, domain.StatusApproved)
	}
	if head.ApprovedAt == nil {
		t.Error("approved_at is nil")
	}
	if head.ApprovedBy == nil || *head.ApprovedBy != approvedBy {
		t.Error("approved_by mismatch")
	}
}

// TestApprovePurchase_AlreadyApproved_Returns409 verifies that re-approving returns conflict.
func TestApprovePurchase_AlreadyApproved_Returns409(t *testing.T) {
	repo := newMockBillRepo()
	warehouseID := uuid.New()
	billID := seedDraftBill(repo, 1, warehouseID)
	// Force to approved state.
	repo.billsByID[billID].Status = domain.StatusApproved

	unitRepo := newMockProductUnitRepo()
	uc := newApproveUC(repo, newMockStockUC(), unitRepo)
	err := uc.Execute(context.Background(), testTenantID, billID, uuid.New())
	if err == nil {
		t.Fatal("expected error for already-approved bill, got nil")
	}
	if !errors.Is(err, appbill.ErrInvalidBillStatus) {
		t.Errorf("expected ErrInvalidBillStatus, got %v", err)
	}
}

// TestApprovePurchase_InvalidUnit_RollsBackAll verifies that a unit conversion failure
// on the 3rd item causes a complete rollback (no stock movements persisted).
func TestApprovePurchase_InvalidUnit_RollsBackAll(t *testing.T) {
	repo := newMockBillRepo()
	stockUC := newMockStockUC()
	unitRepo := newMockProductUnitRepo()

	warehouseID := uuid.New()
	billID := seedDraftBill(repo, 3, warehouseID)
	items := repo.itemsByBillID[billID]

	// Register factors only for items 0 and 1; item 2 will fail.
	unitRepo.set(items[0].ProductID, *items[0].UnitID, decimal.NewFromInt(1))
	unitRepo.set(items[1].ProductID, *items[1].UnitID, decimal.NewFromInt(1))
	// item 2 has no registered factor → ErrInvalidUnitForProduct

	uc := newApproveUC(repo, stockUC, unitRepo)
	err := uc.Execute(context.Background(), testTenantID, billID, uuid.New())
	if err == nil {
		t.Fatal("expected error for invalid unit, got nil")
	}

	// Because the mock WithTx executes fn(nil) and returns fn's error without an actual
	// DB rollback, we verify the use case returned an error and the bill is still draft.
	head := repo.billsByID[billID]
	if head.Status != domain.StatusDraft {
		t.Errorf("bill status = %d after failure, want %d (Draft) — rollback should have kept it draft", head.Status, domain.StatusDraft)
	}
}

// TestApprovePurchase_WAC_CostUpdated verifies that the unit cost from the item is passed
// correctly to the stock use case (WAC recalculation happens inside stock UC).
func TestApprovePurchase_WAC_CostUpdated(t *testing.T) {
	repo := newMockBillRepo()
	stockUC := newMockStockUC()
	unitRepo := newMockProductUnitRepo()

	warehouseID := uuid.New()
	billID := seedDraftBill(repo, 1, warehouseID)
	items := repo.itemsByBillID[billID]
	items[0].UnitPrice = decimal.NewFromFloat(12)
	unitRepo.set(items[0].ProductID, *items[0].UnitID, decimal.NewFromInt(1))

	uc := newApproveUC(repo, stockUC, unitRepo)
	if err := uc.Execute(context.Background(), testTenantID, billID, uuid.New()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(stockUC.calls) != 1 {
		t.Fatalf("expected 1 stock movement call, got %d", len(stockUC.calls))
	}
	if !stockUC.calls[0].UnitCost.Equal(decimal.NewFromFloat(12)) {
		t.Errorf("UnitCost = %s, want 12", stockUC.calls[0].UnitCost)
	}
}
