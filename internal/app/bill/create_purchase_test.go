package bill_test

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// TestCreatePurchaseDraft_ValidInput_ReturnsBillIDAndNo verifies that a valid 3-item request
// produces a bill with a correctly formatted bill_no and non-nil bill_id.
func TestCreatePurchaseDraft_ValidInput_ReturnsBillIDAndNo(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewCreatePurchaseDraftUseCase(repo)

	req := appbill.CreatePurchaseDraftRequest{
		TenantID:    testTenantID,
		CreatorID:   testCreatorID,
		WarehouseID: &testWarehouseID,
		BillDate:    time.Now(),
		ShippingFee: decimal.NewFromFloat(10),
		TaxAmount:   decimal.NewFromFloat(5),
		Items: []appbill.CreatePurchaseItemInput{
			{ProductID: uuid.New(), Qty: decimal.NewFromFloat(10), UnitPrice: decimal.NewFromFloat(8), LineNo: 1},
			{ProductID: uuid.New(), Qty: decimal.NewFromFloat(5), UnitPrice: decimal.NewFromFloat(20), LineNo: 2},
			{ProductID: uuid.New(), Qty: decimal.NewFromFloat(3), UnitPrice: decimal.NewFromFloat(15), LineNo: 3},
		},
	}

	out, err := uc.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.BillID == uuid.Nil {
		t.Error("BillID is nil UUID")
	}

	// Verify bill_no format: PO-YYYYMMDD-NNNN
	ok, _ := regexp.MatchString(`^PO-\d{8}-\d{4}$`, out.BillNo)
	if !ok {
		t.Errorf("BillNo format invalid: %q", out.BillNo)
	}

	// Verify totals in the stored bill
	if repo.storedHead == nil {
		t.Fatal("head was not stored")
	}
	// subtotal = 10*8 + 5*20 + 3*15 = 80 + 100 + 45 = 225
	wantSubtotal := decimal.NewFromFloat(225)
	if !repo.storedHead.Subtotal.Equal(wantSubtotal) {
		t.Errorf("Subtotal = %s, want %s", repo.storedHead.Subtotal, wantSubtotal)
	}
	// total = 225 + 10 + 5 = 240
	wantTotal := decimal.NewFromFloat(240)
	if !repo.storedHead.TotalAmount.Equal(wantTotal) {
		t.Errorf("TotalAmount = %s, want %s", repo.storedHead.TotalAmount, wantTotal)
	}
	if repo.storedHead.Status != domain.StatusDraft {
		t.Errorf("Status = %d, want %d (Draft)", repo.storedHead.Status, domain.StatusDraft)
	}
	if len(repo.storedItems) != 3 {
		t.Errorf("stored item count = %d, want 3", len(repo.storedItems))
	}
}

// TestCreatePurchaseDraft_EmptyItems_Returns400 verifies that zero items returns a validation error.
func TestCreatePurchaseDraft_EmptyItems_Returns400(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewCreatePurchaseDraftUseCase(repo)

	_, err := uc.Execute(context.Background(), appbill.CreatePurchaseDraftRequest{
		TenantID:  testTenantID,
		CreatorID: testCreatorID,
		BillDate:  time.Now(),
		Items:     nil,
	})
	if err == nil {
		t.Fatal("expected validation error for zero items, got nil")
	}
}

// TestCreatePurchaseDraft_MissingTenantID_ReturnsError validates required tenant guard.
func TestCreatePurchaseDraft_MissingTenantID_ReturnsError(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewCreatePurchaseDraftUseCase(repo)

	_, err := uc.Execute(context.Background(), appbill.CreatePurchaseDraftRequest{
		CreatorID: testCreatorID,
		BillDate:  time.Now(),
		Items: []appbill.CreatePurchaseItemInput{
			{ProductID: uuid.New(), Qty: decimal.NewFromFloat(1), UnitPrice: decimal.NewFromFloat(10), LineNo: 1},
		},
	})
	if err == nil {
		t.Fatal("expected error for missing TenantID, got nil")
	}
}

// TestCreatePurchaseDraft_WithUSD_StoresCurrencyAndRate verifies multi-currency fields.
// Input: currency=USD, exchange_rate=7.25, original total=100 USD
// Expected: bill_head.currency="USD", exchange_rate=7.25, amount_local=100, total_amount=725 CNY
func TestCreatePurchaseDraft_WithUSD_StoresCurrencyAndRate(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewCreatePurchaseDraftUseCase(repo)

	// 100 USD total in original currency (1 item: qty=10, price=10 USD)
	req := appbill.CreatePurchaseDraftRequest{
		TenantID:     testTenantID,
		CreatorID:    testCreatorID,
		BillDate:     time.Now(),
		Currency:     "USD",
		ExchangeRate: decimal.NewFromFloat(7.25),
		Items: []appbill.CreatePurchaseItemInput{
			{ProductID: uuid.New(), Qty: decimal.NewFromFloat(10), UnitPrice: decimal.NewFromFloat(10), LineNo: 1},
		},
	}

	out, err := uc.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.BillID == uuid.Nil {
		t.Error("BillID is nil")
	}

	head := repo.storedHead
	if head == nil {
		t.Fatal("head not stored")
	}
	if head.Currency != "USD" {
		t.Errorf("Currency = %q, want USD", head.Currency)
	}
	if !head.ExchangeRateVal.Equal(decimal.NewFromFloat(7.25)) {
		t.Errorf("ExchangeRateVal = %s, want 7.25", head.ExchangeRateVal)
	}
	// amount_local = 100 USD (original currency total)
	wantAmountLocal := decimal.NewFromFloat(100)
	if !head.AmountLocal.Equal(wantAmountLocal) {
		t.Errorf("AmountLocal = %s, want 100 (USD)", head.AmountLocal)
	}
	// total_amount = 100 * 7.25 = 725 CNY
	wantTotalCNY := decimal.NewFromFloat(725)
	if !head.TotalAmount.Equal(wantTotalCNY) {
		t.Errorf("TotalAmount = %s, want 725 (CNY equivalent)", head.TotalAmount)
	}
}

// TestCreatePurchaseDraft_WithCNY_ExchangeRateIsOne verifies CNY shortcut.
func TestCreatePurchaseDraft_WithCNY_ExchangeRateIsOne(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewCreatePurchaseDraftUseCase(repo)

	req := appbill.CreatePurchaseDraftRequest{
		TenantID:  testTenantID,
		CreatorID: testCreatorID,
		BillDate:  time.Now(),
		Currency:  "CNY",
		Items: []appbill.CreatePurchaseItemInput{
			{ProductID: uuid.New(), Qty: decimal.NewFromFloat(1), UnitPrice: decimal.NewFromFloat(100), LineNo: 1},
		},
	}

	_, err := uc.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	head := repo.storedHead
	if !head.ExchangeRateVal.Equal(decimal.NewFromInt(1)) {
		t.Errorf("ExchangeRateVal = %s, want 1 for CNY", head.ExchangeRateVal)
	}
	if !head.AmountLocal.Equal(head.TotalAmount) {
		t.Errorf("AmountLocal (%s) != TotalAmount (%s) for CNY bill", head.AmountLocal, head.TotalAmount)
	}
}

// TestCreatePurchaseDraft_ForeignCurrencyZeroRate_ReturnsError validates exchange_rate guard.
func TestCreatePurchaseDraft_ForeignCurrencyZeroRate_ReturnsError(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewCreatePurchaseDraftUseCase(repo)

	req := appbill.CreatePurchaseDraftRequest{
		TenantID:     testTenantID,
		CreatorID:    testCreatorID,
		BillDate:     time.Now(),
		Currency:     "USD",
		ExchangeRate: decimal.Zero, // missing exchange rate
		Items: []appbill.CreatePurchaseItemInput{
			{ProductID: uuid.New(), Qty: decimal.NewFromFloat(1), UnitPrice: decimal.NewFromFloat(10), LineNo: 1},
		},
	}

	_, err := uc.Execute(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for zero exchange_rate with USD, got nil")
	}
}

// --- shared mock repo for app/bill tests ---

var (
	testTenantID    = uuid.New()
	testCreatorID   = uuid.New()
	testWarehouseID = uuid.New()
)

type mockBillRepo struct {
	storedHead  *domain.BillHead
	storedItems []*domain.BillItem
	seqCounter  int

	// For GetBill / GetBillForUpdate
	billsByID     map[uuid.UUID]*domain.BillHead
	itemsByBillID map[uuid.UUID][]*domain.BillItem

	// Error overrides for testing failure paths
	updateStatusErr error
}

func newMockBillRepo() *mockBillRepo {
	return &mockBillRepo{
		billsByID:     make(map[uuid.UUID]*domain.BillHead),
		itemsByBillID: make(map[uuid.UUID][]*domain.BillItem),
	}
}

func (m *mockBillRepo) WithTx(_ context.Context, fn func(tx *sql.Tx) error) error {
	return fn(nil) //nolint:staticcheck // nil tx is acceptable in unit tests (mock repo ignores it)
}

func (m *mockBillRepo) CreateBill(_ context.Context, _ *sql.Tx, head *domain.BillHead, items []*domain.BillItem) error {
	m.storedHead = head
	m.storedItems = items
	m.billsByID[head.ID] = head
	m.itemsByBillID[head.ID] = items
	return nil
}

func (m *mockBillRepo) GetBillForUpdate(_ context.Context, _ *sql.Tx, _, billID uuid.UUID) (*domain.BillHead, error) {
	h, ok := m.billsByID[billID]
	if !ok {
		return nil, appbill.ErrBillNotFound
	}
	return h, nil
}

func (m *mockBillRepo) GetBill(_ context.Context, _, billID uuid.UUID) (*domain.BillHead, error) {
	h, ok := m.billsByID[billID]
	if !ok {
		return nil, appbill.ErrBillNotFound
	}
	return h, nil
}

func (m *mockBillRepo) GetBillItems(_ context.Context, _, billID uuid.UUID) ([]*domain.BillItem, error) {
	return m.itemsByBillID[billID], nil
}

func (m *mockBillRepo) UpdateBillStatus(_ context.Context, _ *sql.Tx, _, billID uuid.UUID, status domain.BillStatus, meta map[string]any) error {
	if m.updateStatusErr != nil {
		return m.updateStatusErr
	}
	h, ok := m.billsByID[billID]
	if !ok {
		return appbill.ErrBillNotFound
	}
	h.Status = status
	if at, ok := meta["approved_at"]; ok {
		t := at.(time.Time)
		h.ApprovedAt = &t
	}
	if by, ok := meta["approved_by"]; ok {
		id := by.(uuid.UUID)
		h.ApprovedBy = &id
	}
	return nil
}

func (m *mockBillRepo) UpdateBill(_ context.Context, _ *sql.Tx, head *domain.BillHead, items []*domain.BillItem) error {
	m.storedHead = head
	m.storedItems = items
	m.billsByID[head.ID] = head
	m.itemsByBillID[head.ID] = items
	return nil
}

func (m *mockBillRepo) ListBills(_ context.Context, _ appbill.BillListFilter) ([]domain.BillHead, int64, error) {
	var bills []domain.BillHead
	for _, h := range m.billsByID {
		bills = append(bills, *h)
	}
	return bills, int64(len(bills)), nil
}

func (m *mockBillRepo) NextBillNo(_ context.Context, _ *sql.Tx, _ uuid.UUID, prefix string) (string, error) {
	m.seqCounter++
	date := time.Now().Format("20060102")
	return fmt.Sprintf("%s-%s-%04d", prefix, date, m.seqCounter), nil
}

func (m *mockBillRepo) AcquireBillAdvisoryLock(_ context.Context, _ *sql.Tx, _, _ uuid.UUID) error {
	return nil
}

func (m *mockBillRepo) UpdatePaidAmount(_ context.Context, _ *sql.Tx, _, billID uuid.UUID, paidAmount decimal.Decimal) error {
	h, ok := m.billsByID[billID]
	if !ok {
		return appbill.ErrBillNotFound
	}
	h.PaidAmount = paidAmount
	return nil
}
