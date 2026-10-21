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

// TestRecordPayment_InvalidPayType_Rejected verifies that an unknown payment method is
// rejected instead of being silently recorded as cash.
func TestRecordPayment_InvalidPayType_Rejected(t *testing.T) {
	billRepo := newMockBillReader()
	payRepo := newMockPaymentRepo()
	billRepo.seedApprovedBill(testBillID, decimal.NewFromFloat(300), decimal.Zero)

	uc := apppayment.NewRecordPaymentUseCase(billRepo, payRepo)
	err := uc.Execute(context.Background(), apppayment.RecordPaymentRequest{
		TenantID:  testTenantID,
		BillID:    testBillID,
		CreatorID: testCreatorID,
		Amount:    decimal.NewFromFloat(10),
		PayType:   "bitcoin",
	})
	if !errors.Is(err, apppayment.ErrInvalidPayType) {
		t.Fatalf("err = %v, want ErrInvalidPayType", err)
	}
	if len(payRepo.recorded) != 0 {
		t.Errorf("nothing should be recorded, got %d", len(payRepo.recorded))
	}
}

// TestRecordPayment_EmptyPayType_DefaultsToCash keeps the "not specified" case working.
func TestRecordPayment_EmptyPayType_DefaultsToCash(t *testing.T) {
	billRepo := newMockBillReader()
	payRepo := newMockPaymentRepo()
	billRepo.seedApprovedBill(testBillID, decimal.NewFromFloat(300), decimal.Zero)

	uc := apppayment.NewRecordPaymentUseCase(billRepo, payRepo)
	if err := uc.Execute(context.Background(), apppayment.RecordPaymentRequest{
		TenantID: testTenantID, BillID: testBillID, CreatorID: testCreatorID,
		Amount: decimal.NewFromFloat(10),
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(payRepo.recorded) != 1 || payRepo.recorded[0].PayType != "cash" {
		t.Errorf("want one cash payment, got %+v", payRepo.recorded)
	}
}

// ----- net-of-returns over-payment guard -----

// returningBillReader adds the optional ReturnedAmount capability to the mock reader.
type returningBillReader struct {
	*mockBillReader
	returned decimal.Decimal
}

func (r *returningBillReader) ReturnedAmount(_ context.Context, _, _ uuid.UUID) (decimal.Decimal, error) {
	return r.returned, nil
}

// TestRecordPayment_ReturnsPersistedPayment verifies the created payment is handed back
// so the API can echo it.
func TestRecordPayment_ReturnsPersistedPayment(t *testing.T) {
	billRepo := newMockBillReader()
	payRepo := newMockPaymentRepo()
	billRepo.seedApprovedBill(testBillID, decimal.NewFromInt(300), decimal.Zero)

	uc := apppayment.NewRecordPaymentUseCase(billRepo, payRepo)
	p, err := uc.ExecuteReturning(context.Background(), apppayment.RecordPaymentRequest{
		TenantID: testTenantID, BillID: testBillID, CreatorID: testCreatorID,
		Amount: decimal.NewFromInt(100), PayType: "wechat", Remark: "定金",
	})
	if err != nil {
		t.Fatalf("ExecuteReturning: %v", err)
	}
	if p == nil || p.ID == uuid.Nil || p.BillID != testBillID || !p.Amount.Equal(decimal.NewFromInt(100)) ||
		p.PayType != domainpayment.PayTypeWechat || p.Remark != "定金" || p.PayDate.IsZero() {
		t.Errorf("payment = %+v", p)
	}
}

// TestRecordPayment_ExceedsNetOfReturns_ReturnsChineseError verifies a sale of 300 with
// 200 returned and 50 paid only has 50 left to collect: 60 is refused with a
// receivable-worded error, 50 is accepted.
func TestRecordPayment_ExceedsNetOfReturns_ReturnsChineseError(t *testing.T) {
	base := newMockBillReader()
	base.seedApprovedBill(testBillID, decimal.NewFromInt(300), decimal.NewFromInt(50))
	base.bills[testBillID].BillType = domain.BillTypeSale
	billRepo := &returningBillReader{mockBillReader: base, returned: decimal.NewFromInt(200)}
	payRepo := newMockPaymentRepo()
	payRepo.sumVal = decimal.NewFromInt(50)
	uc := apppayment.NewRecordPaymentUseCase(billRepo, payRepo)

	_, err := uc.ExecuteReturning(context.Background(), apppayment.RecordPaymentRequest{
		TenantID: testTenantID, BillID: testBillID, CreatorID: testCreatorID, Amount: decimal.NewFromInt(60),
	})
	var ex *apppayment.ExceedsOutstandingError
	if !errors.As(err, &ex) {
		t.Fatalf("err = %v, want *ExceedsOutstandingError", err)
	}
	if want := "本次 ¥60.00 超过待收 ¥50.00"; ex.Error() != want {
		t.Errorf("message = %q, want %q", ex.Error(), want)
	}
	if len(payRepo.recorded) != 0 {
		t.Errorf("payments = %d, want 0", len(payRepo.recorded))
	}

	if _, err := uc.ExecuteReturning(context.Background(), apppayment.RecordPaymentRequest{
		TenantID: testTenantID, BillID: testBillID, CreatorID: testCreatorID, Amount: decimal.NewFromInt(50),
	}); err != nil {
		t.Fatalf("paying the exact net balance: %v", err)
	}
}

// TestRecordPayment_PurchaseExceeds_UsesPayableWording verifies purchases say 待付.
func TestRecordPayment_PurchaseExceeds_UsesPayableWording(t *testing.T) {
	billRepo := newMockBillReader()
	billRepo.seedApprovedBill(testBillID, decimal.NewFromInt(100), decimal.Zero)
	billRepo.bills[testBillID].BillType = domain.BillTypePurchase
	uc := apppayment.NewRecordPaymentUseCase(billRepo, newMockPaymentRepo())

	_, err := uc.ExecuteReturning(context.Background(), apppayment.RecordPaymentRequest{
		TenantID: testTenantID, BillID: testBillID, CreatorID: testCreatorID, Amount: decimal.NewFromInt(101),
	})
	if err == nil || err.Error() != "本次 ¥101.00 超过待付 ¥100.00" {
		t.Fatalf("err = %v, want 本次 ¥101.00 超过待付 ¥100.00", err)
	}
}

// TestRecordPayment_NotApproved_ReturnsSentinel verifies a draft bill yields ErrBillNotApproved.
func TestRecordPayment_NotApproved_ReturnsSentinel(t *testing.T) {
	billRepo := newMockBillReader()
	billRepo.seedApprovedBill(testBillID, decimal.NewFromInt(100), decimal.Zero)
	billRepo.bills[testBillID].Status = domain.StatusDraft
	uc := apppayment.NewRecordPaymentUseCase(billRepo, newMockPaymentRepo())

	err := uc.Execute(context.Background(), apppayment.RecordPaymentRequest{
		TenantID: testTenantID, BillID: testBillID, CreatorID: testCreatorID, Amount: decimal.NewFromInt(10),
	})
	if !errors.Is(err, apppayment.ErrBillNotApproved) {
		t.Fatalf("err = %v, want ErrBillNotApproved", err)
	}
}

// TestRecordPayment_PurchaseReturn_Rejected verifies a return to a supplier takes no payment.
func TestRecordPayment_PurchaseReturn_Rejected(t *testing.T) {
	billRepo := newMockBillReader()
	billRepo.seedApprovedBill(testBillID, decimal.NewFromInt(100), decimal.Zero)
	billRepo.bills[testBillID].SubType = domain.BillSubTypePurchaseReturn
	uc := apppayment.NewRecordPaymentUseCase(billRepo, newMockPaymentRepo())

	err := uc.Execute(context.Background(), apppayment.RecordPaymentRequest{
		TenantID: testTenantID, BillID: testBillID, CreatorID: testCreatorID, Amount: decimal.NewFromInt(10),
	})
	if !errors.Is(err, apppayment.ErrBillNotPayable) {
		t.Fatalf("err = %v, want ErrBillNotPayable", err)
	}
}

// 赊账是「晚点付」，不是收到钱的方式：登记收付款时一律拒绝。
func TestRecordPayment_CreditIsNotAPaymentMethod(t *testing.T) {
	billRepo := newMockBillReader()
	payRepo := newMockPaymentRepo()
	billRepo.seedApprovedBill(testBillID, decimal.NewFromInt(100), decimal.Zero)
	uc := apppayment.NewRecordPaymentUseCase(billRepo, payRepo)
	_, err := uc.ExecuteReturning(context.Background(), apppayment.RecordPaymentRequest{
		TenantID: testTenantID, BillID: testBillID, CreatorID: testCreatorID, Amount: decimal.NewFromInt(10), PayType: "credit",
	})
	if !errors.Is(err, apppayment.ErrCreditNotPayment) {
		t.Fatalf("err = %v, want ErrCreditNotPayment", err)
	}
	if len(payRepo.recorded) != 0 || !billRepo.bills[testBillID].PaidAmount.IsZero() {
		t.Fatal("nothing may be booked")
	}
}

// 金额最多两位小数，否则返回的和库里存的会对不上。
func TestRecordPayment_MoreThanTwoDecimalsRejected(t *testing.T) {
	billRepo := newMockBillReader()
	billRepo.seedApprovedBill(testBillID, decimal.NewFromInt(100), decimal.Zero)
	uc := apppayment.NewRecordPaymentUseCase(billRepo, newMockPaymentRepo())
	_, err := uc.ExecuteReturning(context.Background(), apppayment.RecordPaymentRequest{
		TenantID: testTenantID, BillID: testBillID, CreatorID: testCreatorID, Amount: decimal.RequireFromString("0.123"),
	})
	if !errors.Is(err, apppayment.ErrReceiptAmount) {
		t.Fatalf("err = %v, want ErrReceiptAmount", err)
	}
}

// txReturningReader reads the returned amount on the transaction; the plain variant must not be used.
type txReturningReader struct {
	*returningBillReader
	txCalls int
}

func (r *txReturningReader) ReturnedAmountTx(_ context.Context, _ *sql.Tx, _, _ uuid.UUID) (decimal.Decimal, error) {
	r.txCalls++
	return r.returned, nil
}

func TestRecordPayment_ReadsReturnedAmountInsideTheTransaction(t *testing.T) {
	base := newMockBillReader()
	base.seedApprovedBill(testBillID, decimal.NewFromInt(300), decimal.Zero)
	base.bills[testBillID].BillType = domain.BillTypeSale
	billRepo := &txReturningReader{returningBillReader: &returningBillReader{mockBillReader: base, returned: decimal.NewFromInt(250)}}
	uc := apppayment.NewRecordPaymentUseCase(billRepo, newMockPaymentRepo())
	_, err := uc.ExecuteReturning(context.Background(), apppayment.RecordPaymentRequest{
		TenantID: testTenantID, BillID: testBillID, CreatorID: testCreatorID, Amount: decimal.NewFromInt(60),
	})
	var ex *apppayment.ExceedsOutstandingError
	if !errors.As(err, &ex) || billRepo.txCalls != 1 {
		t.Fatalf("err = %v txCalls = %d, want exceeds via the tx read", err, billRepo.txCalls)
	}
}

// A bill whose total carries four decimals (booked before totals were rounded to the cent)
// is settled by paying the remainder rounded to the cent; the raw remainder is booked.
func TestRecordPayment_LegacyFourDecimalTotal_SettlesWithRoundedRemainder(t *testing.T) {
	billRepo := newMockBillReader()
	payRepo := newMockPaymentRepo()
	billRepo.seedApprovedBill(testBillID, dec("41.625"), decimal.Zero)

	uc := apppayment.NewRecordPaymentUseCase(billRepo, payRepo)
	err := uc.Execute(context.Background(), apppayment.RecordPaymentRequest{
		TenantID: testTenantID, BillID: testBillID, CreatorID: testCreatorID,
		Amount: dec("41.63"), PayType: "cash",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(payRepo.recorded) != 1 || !payRepo.recorded[0].Amount.Equal(dec("41.625")) {
		t.Fatalf("recorded = %+v, want one payment of 41.625", payRepo.recorded)
	}
	if !billRepo.bills[testBillID].PaidAmount.Equal(dec("41.625")) {
		t.Errorf("paid_amount = %s, want 41.625", billRepo.bills[testBillID].PaidAmount)
	}
}

// Paying more than the rounded remainder is still an overpayment.
func TestRecordPayment_LegacyFourDecimalTotal_OneCentOverRejected(t *testing.T) {
	billRepo := newMockBillReader()
	payRepo := newMockPaymentRepo()
	billRepo.seedApprovedBill(testBillID, dec("41.625"), decimal.Zero)

	uc := apppayment.NewRecordPaymentUseCase(billRepo, payRepo)
	err := uc.Execute(context.Background(), apppayment.RecordPaymentRequest{
		TenantID: testTenantID, BillID: testBillID, CreatorID: testCreatorID,
		Amount: dec("41.64"), PayType: "cash",
	})
	var ex *apppayment.ExceedsOutstandingError
	if !errors.As(err, &ex) {
		t.Fatalf("err = %v, want ExceedsOutstandingError", err)
	}
}
