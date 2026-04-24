package payment_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	apppayment "github.com/hanmahong5-arch/lurus-tally/internal/app/payment"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domainpayment "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
)

// ----- fixtures -----

var (
	testTenantID  = uuid.New()
	testCreatorID = uuid.New()
	testBillID    = uuid.New()
)

// errNotFound is used in mock to simulate not-found responses.
var errNotFound = errors.New("mock: not found")

// ----- mock BillReader -----

type mockBillReader struct {
	bills         map[uuid.UUID]*domain.BillHead
	updatePaidErr error
}

func newMockBillReader() *mockBillReader {
	return &mockBillReader{bills: make(map[uuid.UUID]*domain.BillHead)}
}

func (m *mockBillReader) seedApprovedBill(billID uuid.UUID, total, paid decimal.Decimal) {
	m.bills[billID] = &domain.BillHead{
		ID:          billID,
		TenantID:    testTenantID,
		Status:      domain.StatusApproved,
		TotalAmount: total,
		PaidAmount:  paid,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
}

func (m *mockBillReader) WithTx(_ context.Context, fn func(tx *sql.Tx) error) error {
	return fn(nil) //nolint:staticcheck
}

func (m *mockBillReader) GetBillForUpdate(_ context.Context, _ *sql.Tx, _, billID uuid.UUID) (*domain.BillHead, error) {
	h, ok := m.bills[billID]
	if !ok {
		return nil, fmt.Errorf("%w: bill %s", errNotFound, billID)
	}
	return h, nil
}

func (m *mockBillReader) UpdatePaidAmount(_ context.Context, _ *sql.Tx, _, billID uuid.UUID, paidAmount decimal.Decimal) error {
	if m.updatePaidErr != nil {
		return m.updatePaidErr
	}
	if h, ok := m.bills[billID]; ok {
		h.PaidAmount = paidAmount
	}
	return nil
}

var _ apppayment.BillReader = (*mockBillReader)(nil)

// ----- mock PaymentRepo -----

type mockPaymentRepo struct {
	recorded  []*domainpayment.Payment
	recordErr error
	sumVal    decimal.Decimal
}

func newMockPaymentRepo() *mockPaymentRepo { return &mockPaymentRepo{} }

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
	return m.sumVal, nil
}

func (m *mockPaymentRepo) WithTx(_ context.Context, fn func(tx *sql.Tx) error) error {
	return fn(nil) //nolint:staticcheck
}

var _ apppayment.PaymentRepo = (*mockPaymentRepo)(nil)

// ----- tests -----

// TestRecordPayment_ApprovedBill_UpdatesPaidAmount verifies that paying an approved bill
// records the payment and updates paid_amount.
func TestRecordPayment_ApprovedBill_UpdatesPaidAmount(t *testing.T) {
	billRepo := newMockBillReader()
	payRepo := newMockPaymentRepo()
	billRepo.seedApprovedBill(testBillID, decimal.NewFromFloat(300), decimal.NewFromFloat(0))
	payRepo.sumVal = decimal.Zero

	uc := apppayment.NewRecordPaymentUseCase(billRepo, payRepo)
	err := uc.Execute(context.Background(), apppayment.RecordPaymentRequest{
		TenantID:  testTenantID,
		BillID:    testBillID,
		CreatorID: testCreatorID,
		Amount:    decimal.NewFromFloat(150),
		PayType:   "cash",
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
	head := billRepo.bills[testBillID]
	if !head.PaidAmount.Equal(decimal.NewFromFloat(150)) {
		t.Errorf("bill paid_amount = %s, want 150", head.PaidAmount)
	}
}

// TestRecordPayment_DraftBill_ReturnsError verifies that paying a draft bill is rejected.
func TestRecordPayment_DraftBill_ReturnsError(t *testing.T) {
	billRepo := newMockBillReader()
	payRepo := newMockPaymentRepo()
	billRepo.bills[testBillID] = &domain.BillHead{
		ID:          testBillID,
		TenantID:    testTenantID,
		Status:      domain.StatusDraft,
		TotalAmount: decimal.NewFromFloat(100),
	}

	uc := apppayment.NewRecordPaymentUseCase(billRepo, payRepo)
	err := uc.Execute(context.Background(), apppayment.RecordPaymentRequest{
		TenantID:  testTenantID,
		BillID:    testBillID,
		CreatorID: testCreatorID,
		Amount:    decimal.NewFromFloat(50),
		PayType:   "cash",
	})
	if err == nil {
		t.Fatal("expected error for draft bill, got nil")
	}
}

// TestRecordPayment_OverPay_ReturnsError verifies that overpaying beyond total_amount is rejected.
func TestRecordPayment_OverPay_ReturnsError(t *testing.T) {
	billRepo := newMockBillReader()
	payRepo := newMockPaymentRepo()
	billRepo.seedApprovedBill(testBillID, decimal.NewFromFloat(100), decimal.Zero)
	payRepo.sumVal = decimal.NewFromFloat(80) // 80 already paid

	uc := apppayment.NewRecordPaymentUseCase(billRepo, payRepo)
	err := uc.Execute(context.Background(), apppayment.RecordPaymentRequest{
		TenantID:  testTenantID,
		BillID:    testBillID,
		CreatorID: testCreatorID,
		Amount:    decimal.NewFromFloat(30), // would make total 110 > 100
		PayType:   "cash",
	})
	if err == nil {
		t.Fatal("expected over-payment error, got nil")
	}
}
