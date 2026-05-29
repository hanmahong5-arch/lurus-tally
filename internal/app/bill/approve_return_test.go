package bill_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// seedReturnDraftBill seeds a draft return-stock bill (入库/销售退货) and returns its ID.
func seedReturnDraftBill(repo *mockBillRepo, n int, warehouseID uuid.UUID) uuid.UUID {
	billID := uuid.New()
	now := time.Now()
	head := &domain.BillHead{
		ID:          billID,
		TenantID:    testTenantID,
		BillNo:      "RT-20260528-0001",
		BillType:    domain.BillTypePurchase,
		SubType:     domain.BillSubTypeSaleReturn,
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
		items[i] = &domain.BillItem{
			ID:         uuid.New(),
			TenantID:   testTenantID,
			HeadID:     billID,
			ProductID:  uuid.New(),
			LineNo:     i + 1,
			Qty:        decimal.NewFromFloat(5),
			UnitPrice:  decimal.NewFromFloat(60),
			LineAmount: decimal.NewFromFloat(300),
		}
	}
	repo.billsByID[billID] = head
	repo.itemsByBillID[billID] = items
	return billID
}

// TestApproveReturnBill_HappyPath_StockMovementsIn verifies that approving a 2-item return
// creates 2 stock-in movements with direction=in and reference_type=return.
func TestApproveReturnBill_HappyPath_StockMovementsIn(t *testing.T) {
	repo := newMockBillRepo()
	stockUC := newMockStockUC()
	warehouseID := uuid.New()
	billID := seedReturnDraftBill(repo, 2, warehouseID)

	uc := appbill.NewApproveReturnBillUseCase(repo, stockUC)
	if err := uc.Execute(context.Background(), appbill.ApproveReturnRequest{
		TenantID:  testTenantID,
		BillID:    billID,
		CreatorID: testCreatorID,
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(stockUC.calls) != 2 {
		t.Errorf("stock movements = %d, want 2", len(stockUC.calls))
	}
	for _, c := range stockUC.calls {
		if c.Direction != domainstock.DirectionIn {
			t.Errorf("direction = %s, want in", c.Direction)
		}
		if c.ReferenceType != domainstock.RefReturn {
			t.Errorf("reference_type = %s, want return", c.ReferenceType)
		}
		if c.WarehouseID != warehouseID {
			t.Errorf("warehouse_id mismatch: got %s", c.WarehouseID)
		}
	}

	head := repo.billsByID[billID]
	if head.Status != domain.StatusApproved {
		t.Errorf("status = %d, want %d (Approved)", head.Status, domain.StatusApproved)
	}
	if head.ApprovedAt == nil {
		t.Error("approved_at is nil")
	}
}

// TestApproveReturnBill_AlreadyApproved_IdempotentNoOp verifies that re-approving a bill
// that is already in Approved status returns nil without emitting any stock movements.
func TestApproveReturnBill_AlreadyApproved_IdempotentNoOp(t *testing.T) {
	repo := newMockBillRepo()
	warehouseID := uuid.New()
	billID := seedReturnDraftBill(repo, 1, warehouseID)
	// Force to approved state.
	repo.billsByID[billID].Status = domain.StatusApproved

	stockUC := newMockStockUC()
	uc := appbill.NewApproveReturnBillUseCase(repo, stockUC)
	if err := uc.Execute(context.Background(), appbill.ApproveReturnRequest{
		TenantID:  testTenantID,
		BillID:    billID,
		CreatorID: testCreatorID,
	}); err != nil {
		t.Fatalf("expected nil for already-approved bill (idempotent), got %v", err)
	}
	if len(stockUC.calls) != 0 {
		t.Errorf("expected 0 stock movements for idempotent call, got %d", len(stockUC.calls))
	}
}
