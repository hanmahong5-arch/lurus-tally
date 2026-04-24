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
	domainpayment "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// ----- mock PaymentRepo -----

type mockPaymentRepo struct {
	recorded    []*domainpayment.Payment
	recordErr   error
	sumByBillFn func() decimal.Decimal
}

func newMockPaymentRepo() *mockPaymentRepo {
	return &mockPaymentRepo{}
}

func (m *mockPaymentRepo) Record(_ context.Context, _ *sql.Tx, p *domainpayment.Payment) error {
	if m.recordErr != nil {
		return m.recordErr
	}
	m.recorded = append(m.recorded, p)
	return nil
}

func (m *mockPaymentRepo) ListByBill(_ context.Context, _, _ uuid.UUID) ([]*domainpayment.Payment, error) {
	return m.recorded, nil
}

func (m *mockPaymentRepo) SumByBill(_ context.Context, _ *sql.Tx, _, _ uuid.UUID) (decimal.Decimal, error) {
	if m.sumByBillFn != nil {
		return m.sumByBillFn(), nil
	}
	var total decimal.Decimal
	for _, p := range m.recorded {
		total = total.Add(p.Amount)
	}
	return total, nil
}

func (m *mockPaymentRepo) WithTx(_ context.Context, fn func(tx *sql.Tx) error) error {
	return fn(nil) //nolint:staticcheck
}

var _ appbill.PaymentRecorder = (*mockPaymentRepo)(nil)

// ----- helpers -----

// seedSaleDraftBill seeds a sale draft bill in the mock repo.
func seedSaleDraftBill(repo *mockBillRepo, n int, warehouseID uuid.UUID) uuid.UUID {
	billID := uuid.New()
	now := time.Now()
	head := &domain.BillHead{
		ID:          billID,
		TenantID:    testTenantID,
		BillNo:      "SL-20260423-0001",
		BillType:    domain.BillTypeSale,
		SubType:     domain.BillSubTypeSale,
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

func newApproveSaleUC(repo *mockBillRepo, stockUC *mockStockUC, unitRepo *mockProductUnitRepo, payRepo *mockPaymentRepo) *appbill.ApproveSaleUseCase {
	return appbill.NewApproveSaleUseCase(repo, stockUC, unitRepo, payRepo)
}

// ----- tests -----

// TestApproveSale_AllItemsInStock_ApproveSucceeds verifies that approving a 3-item sale
// creates 3 stock-out movements and sets bill status to approved.
func TestApproveSale_AllItemsInStock_ApproveSucceeds(t *testing.T) {
	repo := newMockBillRepo()
	stockUC := newMockStockUC()
	unitRepo := newMockProductUnitRepo()
	payRepo := newMockPaymentRepo()

	warehouseID := uuid.New()
	billID := seedSaleDraftBill(repo, 3, warehouseID)
	seedProductUnitFactors(unitRepo, repo.itemsByBillID[billID])

	uc := newApproveSaleUC(repo, stockUC, unitRepo, payRepo)
	err := uc.Execute(context.Background(), appbill.ApproveSaleRequest{
		TenantID:  testTenantID,
		BillID:    billID,
		CreatorID: testCreatorID,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(stockUC.calls) != 3 {
		t.Errorf("stock movements = %d, want 3", len(stockUC.calls))
	}
	for _, c := range stockUC.calls {
		if c.Direction != domainstock.DirectionOut {
			t.Errorf("direction = %s, want out", c.Direction)
		}
		if c.ReferenceType != domainstock.RefSale {
			t.Errorf("reference_type = %s, want sale", c.ReferenceType)
		}
	}

	head := repo.billsByID[billID]
	if head.Status != domain.StatusApproved {
		t.Errorf("status = %d, want %d (Approved)", head.Status, domain.StatusApproved)
	}
}

// TestApproveSale_OneItemInsufficient_RollsBack verifies that the second item triggering
// InsufficientStockError leaves the bill in draft status and skips payment.
func TestApproveSale_OneItemInsufficient_RollsBack(t *testing.T) {
	repo := newMockBillRepo()
	stockUC := newMockStockUC()
	// Force failure on second call (index 1)
	stockUC.failOnIdx = 1
	stockUC.failErr = &appstock.InsufficientStockError{
		Available: decimal.NewFromFloat(5),
		Requested: decimal.NewFromFloat(10),
	}
	unitRepo := newMockProductUnitRepo()
	payRepo := newMockPaymentRepo()

	warehouseID := uuid.New()
	billID := seedSaleDraftBill(repo, 3, warehouseID)
	seedProductUnitFactors(unitRepo, repo.itemsByBillID[billID])

	uc := newApproveSaleUC(repo, stockUC, unitRepo, payRepo)
	err := uc.Execute(context.Background(), appbill.ApproveSaleRequest{
		TenantID:   testTenantID,
		BillID:     billID,
		CreatorID:  testCreatorID,
		PaidAmount: decimal.NewFromFloat(100),
		PayType:    "cash",
	})
	if err == nil {
		t.Fatal("expected error for insufficient stock, got nil")
	}

	head := repo.billsByID[billID]
	if head.Status != domain.StatusDraft {
		t.Errorf("status = %d after failure, want %d (Draft)", head.Status, domain.StatusDraft)
	}
	if len(payRepo.recorded) != 0 {
		t.Error("expected no payment recorded after rollback")
	}
}

// TestApproveSale_WithPaidAmount_RecordsPayment verifies that paid_amount > 0 writes a payment.
func TestApproveSale_WithPaidAmount_RecordsPayment(t *testing.T) {
	repo := newMockBillRepo()
	stockUC := newMockStockUC()
	unitRepo := newMockProductUnitRepo()
	payRepo := newMockPaymentRepo()

	warehouseID := uuid.New()
	billID := seedSaleDraftBill(repo, 1, warehouseID)
	seedProductUnitFactors(unitRepo, repo.itemsByBillID[billID])

	uc := newApproveSaleUC(repo, stockUC, unitRepo, payRepo)
	err := uc.Execute(context.Background(), appbill.ApproveSaleRequest{
		TenantID:   testTenantID,
		BillID:     billID,
		CreatorID:  testCreatorID,
		PaidAmount: decimal.NewFromFloat(150),
		PayType:    "wechat",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(payRepo.recorded) != 1 {
		t.Errorf("payment records = %d, want 1", len(payRepo.recorded))
	}
	if !payRepo.recorded[0].Amount.Equal(decimal.NewFromFloat(150)) {
		t.Errorf("payment amount = %s, want 150", payRepo.recorded[0].Amount)
	}
	// bill paid_amount updated
	head := repo.billsByID[billID]
	if !head.PaidAmount.Equal(decimal.NewFromFloat(150)) {
		t.Errorf("bill paid_amount = %s, want 150", head.PaidAmount)
	}
}

// TestApproveSale_ZeroPaidAmount_SkipsPayment verifies paid_amount=0 skips the payment record.
func TestApproveSale_ZeroPaidAmount_SkipsPayment(t *testing.T) {
	repo := newMockBillRepo()
	stockUC := newMockStockUC()
	unitRepo := newMockProductUnitRepo()
	payRepo := newMockPaymentRepo()

	warehouseID := uuid.New()
	billID := seedSaleDraftBill(repo, 1, warehouseID)
	seedProductUnitFactors(unitRepo, repo.itemsByBillID[billID])

	uc := newApproveSaleUC(repo, stockUC, unitRepo, payRepo)
	err := uc.Execute(context.Background(), appbill.ApproveSaleRequest{
		TenantID:  testTenantID,
		BillID:    billID,
		CreatorID: testCreatorID,
		// PaidAmount zero — credit scenario
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(payRepo.recorded) != 0 {
		t.Errorf("payment records = %d, want 0 (no payment on zero paid_amount)", len(payRepo.recorded))
	}
}

// TestApproveSale_AlreadyApproved_ReturnsError verifies re-approval returns invalid status error.
func TestApproveSale_AlreadyApproved_ReturnsError(t *testing.T) {
	repo := newMockBillRepo()
	warehouseID := uuid.New()
	billID := seedSaleDraftBill(repo, 1, warehouseID)
	repo.billsByID[billID].Status = domain.StatusApproved

	uc := newApproveSaleUC(repo, newMockStockUC(), newMockProductUnitRepo(), newMockPaymentRepo())
	err := uc.Execute(context.Background(), appbill.ApproveSaleRequest{
		TenantID:  testTenantID,
		BillID:    billID,
		CreatorID: testCreatorID,
	})
	if err == nil {
		t.Fatal("expected error for already-approved bill, got nil")
	}
	if !errors.Is(err, appbill.ErrInvalidBillStatus) {
		t.Errorf("expected ErrInvalidBillStatus, got %v", err)
	}
}
