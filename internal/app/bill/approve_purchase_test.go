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

// TestApprovePurchase_AlreadyApproved_ReturnsNilIdempotent verifies that re-approving an
// already-Approved bill returns nil without emitting any stock movements.
// This is the B3 idempotent short-circuit: duplicate-click / retry scenarios must be no-ops.
func TestApprovePurchase_AlreadyApproved_ReturnsNilIdempotent(t *testing.T) {
	repo := newMockBillRepo()
	warehouseID := uuid.New()
	billID := seedDraftBill(repo, 1, warehouseID)
	// Force to approved state.
	repo.billsByID[billID].Status = domain.StatusApproved

	stockUC := newMockStockUC()
	unitRepo := newMockProductUnitRepo()
	uc := newApproveUC(repo, stockUC, unitRepo)
	err := uc.Execute(context.Background(), testTenantID, billID, uuid.New())
	if err != nil {
		t.Fatalf("expected nil for already-approved bill (idempotent), got %v", err)
	}
	// No stock movements must have been recorded.
	if len(stockUC.calls) != 0 {
		t.Errorf("expected 0 stock movements on idempotent approve, got %d", len(stockUC.calls))
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

// TestApprovePurchase_ForeignCurrency_ConvertsUnitCostToBase verifies that a non-CNY
// purchase bill converts each line's unit price to base currency (CNY) using the bill's
// exchange rate before recording the stock movement, and that a base-currency bill (rate
// unset → fall back to 1) records the price unchanged.
func TestApprovePurchase_ForeignCurrency_ConvertsUnitCostToBase(t *testing.T) {
	tests := []struct {
		name         string
		exchangeRate decimal.Decimal
		unitPrice    decimal.Decimal
		wantUnitCost decimal.Decimal
	}{
		{
			name:         "usd_bill_converts_at_7.2",
			exchangeRate: decimal.NewFromFloat(7.2),
			unitPrice:    decimal.NewFromFloat(10),
			wantUnitCost: decimal.NewFromFloat(72),
		},
		{
			name:         "base_currency_rate_one_unchanged",
			exchangeRate: decimal.NewFromInt(1),
			unitPrice:    decimal.NewFromFloat(10),
			wantUnitCost: decimal.NewFromFloat(10),
		},
		{
			name:         "unset_rate_falls_back_to_one",
			exchangeRate: decimal.Decimal{}, // zero/unset → guard falls back to rate 1
			unitPrice:    decimal.NewFromFloat(10),
			wantUnitCost: decimal.NewFromFloat(10),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockBillRepo()
			stockUC := newMockStockUC()
			unitRepo := newMockProductUnitRepo()

			warehouseID := uuid.New()
			billID := seedDraftBill(repo, 1, warehouseID)
			repo.billsByID[billID].ExchangeRateVal = tt.exchangeRate
			items := repo.itemsByBillID[billID]
			items[0].UnitPrice = tt.unitPrice
			unitRepo.set(items[0].ProductID, *items[0].UnitID, decimal.NewFromInt(1))

			uc := newApproveUC(repo, stockUC, unitRepo)
			if err := uc.Execute(context.Background(), testTenantID, billID, uuid.New()); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if len(stockUC.calls) != 1 {
				t.Fatalf("expected 1 stock movement call, got %d", len(stockUC.calls))
			}
			if !stockUC.calls[0].UnitCost.Equal(tt.wantUnitCost) {
				t.Errorf("UnitCost = %s, want %s", stockUC.calls[0].UnitCost, tt.wantUnitCost)
			}
		})
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

// 网页端建的采购单不带仓库：有默认仓时审核要成功，库存流水记在默认仓，且单据回写仓库。
func TestApprovePurchase_NoWarehouse_UsesDefaultWarehouse(t *testing.T) {
	repo := newMockBillRepo()
	stockUC := newMockStockUC()
	unitRepo := newMockProductUnitRepo()
	defaultWH := uuid.New()
	repo.defaultWH = &defaultWH

	billID := seedDraftBill(repo, 2, uuid.New())
	repo.billsByID[billID].WarehouseID = nil
	seedProductUnitFactors(unitRepo, repo.itemsByBillID[billID])

	if err := newApproveUC(repo, stockUC, unitRepo).Execute(context.Background(), testTenantID, billID, uuid.New()); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(stockUC.calls) != 2 {
		t.Fatalf("stock movements = %d, want 2", len(stockUC.calls))
	}
	for _, c := range stockUC.calls {
		if c.WarehouseID != defaultWH {
			t.Errorf("movement warehouse = %s, want default %s", c.WarehouseID, defaultWH)
		}
	}
	if repo.setWHCalls != 1 {
		t.Errorf("SetBillWarehouse calls = %d, want 1", repo.setWHCalls)
	}
	if wh := repo.billsByID[billID].WarehouseID; wh == nil || *wh != defaultWH {
		t.Errorf("bill warehouse = %v, want %s", wh, defaultWH)
	}
}

// 没仓库也没默认仓：返回 ErrWarehouseRequired，不动库存、不改状态。
func TestApprovePurchase_NoWarehouseNoDefault_ReturnsWarehouseRequired(t *testing.T) {
	repo := newMockBillRepo()
	stockUC := newMockStockUC()
	unitRepo := newMockProductUnitRepo()

	billID := seedDraftBill(repo, 2, uuid.New())
	repo.billsByID[billID].WarehouseID = nil
	seedProductUnitFactors(unitRepo, repo.itemsByBillID[billID])

	err := newApproveUC(repo, stockUC, unitRepo).Execute(context.Background(), testTenantID, billID, uuid.New())

	if !errors.Is(err, appbill.ErrWarehouseRequired) {
		t.Fatalf("err = %v, want ErrWarehouseRequired", err)
	}
	if len(stockUC.calls) != 0 {
		t.Errorf("stock movements = %d, want 0", len(stockUC.calls))
	}
	if repo.setWHCalls != 0 {
		t.Errorf("SetBillWarehouse calls = %d, want 0", repo.setWHCalls)
	}
	if repo.billsByID[billID].Status != domain.StatusDraft {
		t.Errorf("status = %d, want draft", repo.billsByID[billID].Status)
	}
}

// ----- approve with payment -----

func newApprovePayUC(repo *mockBillRepo, unitRepo *mockProductUnitRepo, payRepo *mockPaymentRepo) *appbill.ApprovePurchaseUseCase {
	return appbill.NewApprovePurchaseUseCase(repo, newMockStockUC(), unitRepo).WithPayments(payRepo)
}

// TestApprovePurchase_WithPayment_RecordsPaymentAndPaidAmount verifies paying 100 on
// approval writes one payment and lifts bill_head.paid_amount, so AP drops by 100.
func TestApprovePurchase_WithPayment_RecordsPaymentAndPaidAmount(t *testing.T) {
	repo := newMockBillRepo()
	unitRepo := newMockProductUnitRepo()
	payRepo := newMockPaymentRepo()
	billID := seedDraftBill(repo, 1, uuid.New())
	seedProductUnitFactors(unitRepo, repo.itemsByBillID[billID])

	err := newApprovePayUC(repo, unitRepo, payRepo).ExecuteWithPayment(context.Background(), appbill.ApprovePurchaseRequest{
		TenantID: testTenantID, BillID: billID, ApprovedBy: testCreatorID,
		PaidAmount: decimal.NewFromInt(100), PayType: "transfer",
	})
	if err != nil {
		t.Fatalf("ExecuteWithPayment: %v", err)
	}
	if len(payRepo.recorded) != 1 {
		t.Fatalf("payments = %d, want 1", len(payRepo.recorded))
	}
	p := payRepo.recorded[0]
	if !p.Amount.Equal(decimal.NewFromInt(100)) || p.PayType != domainpayment.PayTypeTransfer || p.BillID != billID {
		t.Errorf("payment = %+v", p)
	}
	if got := repo.billsByID[billID].PaidAmount; !got.Equal(decimal.NewFromInt(100)) {
		t.Errorf("paid_amount = %s, want 100", got)
	}
}

// TestApprovePurchase_PaidExceedsTotal_RejectedBeforeAnyWrite verifies an over-payment
// leaves the draft untouched.
func TestApprovePurchase_PaidExceedsTotal_RejectedBeforeAnyWrite(t *testing.T) {
	repo := newMockBillRepo()
	unitRepo := newMockProductUnitRepo()
	payRepo := newMockPaymentRepo()
	billID := seedDraftBill(repo, 1, uuid.New())
	seedProductUnitFactors(unitRepo, repo.itemsByBillID[billID])

	err := newApprovePayUC(repo, unitRepo, payRepo).ExecuteWithPayment(context.Background(), appbill.ApprovePurchaseRequest{
		TenantID: testTenantID, BillID: billID, ApprovedBy: testCreatorID,
		PaidAmount: decimal.NewFromInt(301),
	})
	if !errors.Is(err, appbill.ErrPaidExceedsTotal) {
		t.Fatalf("err = %v, want ErrPaidExceedsTotal", err)
	}
	if repo.billsByID[billID].Status != domain.StatusDraft || len(payRepo.recorded) != 0 {
		t.Errorf("status=%d payments=%d, want draft/0", repo.billsByID[billID].Status, len(payRepo.recorded))
	}
}

// TestApprovePurchase_InvalidPayMethod_Rejected verifies an unknown method is rejected.
func TestApprovePurchase_InvalidPayMethod_Rejected(t *testing.T) {
	repo := newMockBillRepo()
	unitRepo := newMockProductUnitRepo()
	payRepo := newMockPaymentRepo()
	billID := seedDraftBill(repo, 1, uuid.New())
	seedProductUnitFactors(unitRepo, repo.itemsByBillID[billID])

	err := newApprovePayUC(repo, unitRepo, payRepo).ExecuteWithPayment(context.Background(), appbill.ApprovePurchaseRequest{
		TenantID: testTenantID, BillID: billID, ApprovedBy: testCreatorID,
		PaidAmount: decimal.NewFromInt(50), PayType: "bitcoin",
	})
	if !errors.Is(err, appbill.ErrInvalidPayMethod) {
		t.Fatalf("err = %v, want ErrInvalidPayMethod", err)
	}
	if repo.billsByID[billID].Status != domain.StatusDraft {
		t.Errorf("status = %d, want draft", repo.billsByID[billID].Status)
	}
}

// TestApprovePurchase_AlreadyApproved_WithPayment_ReturnsConflict verifies a retry that
// carries a payment is told the payment was not booked.
func TestApprovePurchase_AlreadyApproved_WithPayment_ReturnsConflict(t *testing.T) {
	repo := newMockBillRepo()
	unitRepo := newMockProductUnitRepo()
	payRepo := newMockPaymentRepo()
	billID := seedDraftBill(repo, 1, uuid.New())
	seedProductUnitFactors(unitRepo, repo.itemsByBillID[billID])
	repo.billsByID[billID].Status = domain.StatusApproved

	err := newApprovePayUC(repo, unitRepo, payRepo).ExecuteWithPayment(context.Background(), appbill.ApprovePurchaseRequest{
		TenantID: testTenantID, BillID: billID, ApprovedBy: testCreatorID,
		PaidAmount: decimal.NewFromInt(50),
	})
	if !errors.Is(err, appbill.ErrBillAlreadyApproved) {
		t.Fatalf("err = %v, want ErrBillAlreadyApproved", err)
	}
	if len(payRepo.recorded) != 0 {
		t.Errorf("payments = %d, want 0", len(payRepo.recorded))
	}
}

// 赊账不是实付方式：采购审核同时付款时选「赊账」会被拒绝，单据保持草稿。
func TestApprovePurchase_CreditAsPaymentMethod_Rejected(t *testing.T) {
	repo := newMockBillRepo()
	unitRepo := newMockProductUnitRepo()
	payRepo := newMockPaymentRepo()
	billID := seedDraftBill(repo, 1, uuid.New())
	seedProductUnitFactors(unitRepo, repo.itemsByBillID[billID])

	err := newApprovePayUC(repo, unitRepo, payRepo).ExecuteWithPayment(context.Background(), appbill.ApprovePurchaseRequest{
		TenantID: testTenantID, BillID: billID, ApprovedBy: testCreatorID,
		PaidAmount: decimal.NewFromInt(50), PayType: "credit",
	})
	if !errors.Is(err, appbill.ErrInvalidPayMethod) {
		t.Fatalf("err = %v, want ErrInvalidPayMethod", err)
	}
	if repo.billsByID[billID].Status != domain.StatusDraft || len(payRepo.recorded) != 0 {
		t.Errorf("status=%d payments=%d, want draft/0", repo.billsByID[billID].Status, len(payRepo.recorded))
	}
}

// ----- zero unit price guard -----

// TestApprovePurchase_ZeroPriceLine_RejectedBeforeAnyWrite verifies a draft with a
// zero-priced line is refused, naming the line, and nothing is booked.
func TestApprovePurchase_ZeroPriceLine_RejectedBeforeAnyWrite(t *testing.T) {
	repo := newMockBillRepo()
	stockUC := newMockStockUC()
	unitRepo := newMockProductUnitRepo()

	billID := seedDraftBill(repo, 3, uuid.New())
	repo.itemsByBillID[billID][1].UnitPrice = decimal.Zero
	seedProductUnitFactors(unitRepo, repo.itemsByBillID[billID])

	err := newApproveUC(repo, stockUC, unitRepo).Execute(context.Background(), testTenantID, billID, uuid.New())

	if !errors.Is(err, appbill.ErrZeroPriceLines) {
		t.Fatalf("err = %v, want ErrZeroPriceLines", err)
	}
	var zerr *appbill.ZeroPriceLinesError
	if !errors.As(err, &zerr) || len(zerr.Lines) != 1 || zerr.Lines[0].LineNo != 2 {
		t.Fatalf("lines = %+v, want only line 2", zerr)
	}
	if zerr.Lines[0].ProductID != repo.itemsByBillID[billID][1].ProductID {
		t.Errorf("product id not carried on the zero-price line")
	}
	if len(stockUC.calls) != 0 {
		t.Errorf("stock movements = %d, want 0", len(stockUC.calls))
	}
	if repo.billsByID[billID].Status != domain.StatusDraft {
		t.Errorf("status = %d, want draft", repo.billsByID[billID].Status)
	}
}

// TestApprovePurchase_ZeroPriceLine_AllowedWhenConfirmedAsGift verifies the explicit
// allow flag books the line at cost 0.
func TestApprovePurchase_ZeroPriceLine_AllowedWhenConfirmedAsGift(t *testing.T) {
	repo := newMockBillRepo()
	stockUC := newMockStockUC()
	unitRepo := newMockProductUnitRepo()

	billID := seedDraftBill(repo, 2, uuid.New())
	repo.itemsByBillID[billID][0].UnitPrice = decimal.Zero
	seedProductUnitFactors(unitRepo, repo.itemsByBillID[billID])

	err := newApproveUC(repo, stockUC, unitRepo).ExecuteWithPayment(context.Background(), appbill.ApprovePurchaseRequest{
		TenantID: testTenantID, BillID: billID, ApprovedBy: uuid.New(), AllowZeroPrice: true,
	})
	if err != nil {
		t.Fatalf("ExecuteWithPayment: %v", err)
	}
	if len(stockUC.calls) != 2 {
		t.Fatalf("stock movements = %d, want 2", len(stockUC.calls))
	}
	if !stockUC.calls[0].UnitCost.IsZero() {
		t.Errorf("gift line cost = %s, want 0", stockUC.calls[0].UnitCost)
	}
	if repo.billsByID[billID].Status != domain.StatusApproved {
		t.Errorf("status = %d, want approved", repo.billsByID[billID].Status)
	}
}
