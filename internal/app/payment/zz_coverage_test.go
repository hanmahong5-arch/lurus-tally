package payment_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	apppayment "github.com/hanmahong5-arch/lurus-tally/internal/app/payment"
	domainbill "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domainpayment "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
)

// ----- extra fakes with fully independent error-injection knobs -----
// (kept separate from mockBillReader/mockPaymentRepo in record_test.go so this
// file never touches or depends on the shape of those pre-existing mocks.)

type zzBillReader struct {
	bill          *domainbill.BillHead
	getBillErr    error
	updatePaidErr error

	withTxCalls    int
	updatePaidCall bool
	lastPaidAmount decimal.Decimal
}

func (f *zzBillReader) WithTx(_ context.Context, fn func(tx *sql.Tx) error) error {
	f.withTxCalls++
	return fn(nil) //nolint:staticcheck
}

func (f *zzBillReader) GetBillForUpdate(_ context.Context, _ *sql.Tx, _, _ uuid.UUID) (*domainbill.BillHead, error) {
	if f.getBillErr != nil {
		return nil, f.getBillErr
	}
	return f.bill, nil
}

func (f *zzBillReader) UpdatePaidAmount(_ context.Context, _ *sql.Tx, _, _ uuid.UUID, paidAmount decimal.Decimal) error {
	f.updatePaidCall = true
	f.lastPaidAmount = paidAmount
	if f.updatePaidErr != nil {
		return f.updatePaidErr
	}
	return nil
}

var _ apppayment.BillReader = (*zzBillReader)(nil)

type zzPaymentRepo struct {
	sumVal    decimal.Decimal
	sumErr    error
	recordErr error
	listVal   []*domainpayment.Payment
	listErr   error

	recordCall bool
	recorded   *domainpayment.Payment
}

func (f *zzPaymentRepo) Record(_ context.Context, _ *sql.Tx, p *domainpayment.Payment) error {
	f.recordCall = true
	if f.recordErr != nil {
		return f.recordErr
	}
	f.recorded = p
	return nil
}

func (f *zzPaymentRepo) ListByBill(_ context.Context, _, _ uuid.UUID) ([]*domainpayment.Payment, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listVal, nil
}

func (f *zzPaymentRepo) SumByBill(_ context.Context, _ *sql.Tx, _, _ uuid.UUID) (decimal.Decimal, error) {
	if f.sumErr != nil {
		return decimal.Zero, f.sumErr
	}
	return f.sumVal, nil
}

func (f *zzPaymentRepo) WithTx(_ context.Context, fn func(tx *sql.Tx) error) error {
	return fn(nil) //nolint:staticcheck
}

var _ apppayment.PaymentRepo = (*zzPaymentRepo)(nil)

func approvedBill(billID uuid.UUID, total decimal.Decimal, partnerID *uuid.UUID) *domainbill.BillHead {
	return &domainbill.BillHead{
		ID:          billID,
		TenantID:    testTenantID,
		Status:      domainbill.StatusApproved,
		TotalAmount: total,
		PartnerID:   partnerID,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
}

// ----- RecordPaymentUseCase: nil guards -----

func TestZZRecordPayment_NilGuards(t *testing.T) {
	cases := []struct {
		name string
		req  apppayment.RecordPaymentRequest
		want string
	}{
		{
			name: "tenant_id nil",
			req: apppayment.RecordPaymentRequest{
				BillID: testBillID, CreatorID: testCreatorID, Amount: decimal.NewFromFloat(10), PayType: "cash",
			},
			want: "tenant_id is required",
		},
		{
			name: "bill_id nil",
			req: apppayment.RecordPaymentRequest{
				TenantID: testTenantID, CreatorID: testCreatorID, Amount: decimal.NewFromFloat(10), PayType: "cash",
			},
			want: "bill_id is required",
		},
		{
			name: "creator_id nil",
			req: apppayment.RecordPaymentRequest{
				TenantID: testTenantID, BillID: testBillID, Amount: decimal.NewFromFloat(10), PayType: "cash",
			},
			want: "creator_id is required",
		},
		{
			name: "amount zero",
			req: apppayment.RecordPaymentRequest{
				TenantID: testTenantID, BillID: testBillID, CreatorID: testCreatorID, Amount: decimal.Zero, PayType: "cash",
			},
			want: "amount must be positive",
		},
		{
			name: "amount negative",
			req: apppayment.RecordPaymentRequest{
				TenantID: testTenantID, BillID: testBillID, CreatorID: testCreatorID, Amount: decimal.NewFromFloat(-5), PayType: "cash",
			},
			want: "amount must be positive",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			billRepo := &zzBillReader{bill: approvedBill(testBillID, decimal.NewFromFloat(100), nil)}
			payRepo := &zzPaymentRepo{}

			uc := apppayment.NewRecordPaymentUseCase(billRepo, payRepo)
			err := uc.Execute(context.Background(), tc.req)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want substring %q", err.Error(), tc.want)
			}
			// Guard checks happen before WithTx is ever opened.
			if billRepo.withTxCalls != 0 {
				t.Errorf("WithTx calls = %d, want 0 (guard must short-circuit before tx)", billRepo.withTxCalls)
			}
			if payRepo.recordCall {
				t.Error("Record must not be called when guard rejects request")
			}
		})
	}
}

// ----- RecordPaymentUseCase: wrapped errors from each collaborator call -----

func TestZZRecordPayment_GetBillForUpdate_ErrorWrapped(t *testing.T) {
	underlying := errors.New("boom: row lock timeout")
	billRepo := &zzBillReader{getBillErr: underlying}
	payRepo := &zzPaymentRepo{}

	uc := apppayment.NewRecordPaymentUseCase(billRepo, payRepo)
	err := uc.Execute(context.Background(), apppayment.RecordPaymentRequest{
		TenantID: testTenantID, BillID: testBillID, CreatorID: testCreatorID,
		Amount: decimal.NewFromFloat(10), PayType: "cash",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "load bill") {
		t.Errorf("error = %q, want to contain 'load bill'", err.Error())
	}
	if !errors.Is(err, underlying) {
		t.Errorf("error chain does not wrap underlying error: %v", err)
	}
	if payRepo.recordCall {
		t.Error("Record must not be called after load-bill failure")
	}
}

func TestZZRecordPayment_SumByBill_ErrorWrapped(t *testing.T) {
	underlying := errors.New("boom: sum query failed")
	billRepo := &zzBillReader{bill: approvedBill(testBillID, decimal.NewFromFloat(100), nil)}
	payRepo := &zzPaymentRepo{sumErr: underlying}

	uc := apppayment.NewRecordPaymentUseCase(billRepo, payRepo)
	err := uc.Execute(context.Background(), apppayment.RecordPaymentRequest{
		TenantID: testTenantID, BillID: testBillID, CreatorID: testCreatorID,
		Amount: decimal.NewFromFloat(10), PayType: "cash",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "sum by bill") {
		t.Errorf("error = %q, want to contain 'sum by bill'", err.Error())
	}
	if !errors.Is(err, underlying) {
		t.Errorf("error chain does not wrap underlying error: %v", err)
	}
	if payRepo.recordCall {
		t.Error("Record must not be called after sum-by-bill failure")
	}
	if billRepo.updatePaidCall {
		t.Error("UpdatePaidAmount must not be called after sum-by-bill failure")
	}
}

func TestZZRecordPayment_Record_ErrorWrapped(t *testing.T) {
	underlying := errors.New("boom: insert failed")
	billRepo := &zzBillReader{bill: approvedBill(testBillID, decimal.NewFromFloat(100), nil)}
	payRepo := &zzPaymentRepo{sumVal: decimal.NewFromFloat(20), recordErr: underlying}

	uc := apppayment.NewRecordPaymentUseCase(billRepo, payRepo)
	err := uc.Execute(context.Background(), apppayment.RecordPaymentRequest{
		TenantID: testTenantID, BillID: testBillID, CreatorID: testCreatorID,
		Amount: decimal.NewFromFloat(10), PayType: "cash",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "persist") {
		t.Errorf("error = %q, want to contain 'persist'", err.Error())
	}
	if !errors.Is(err, underlying) {
		t.Errorf("error chain does not wrap underlying error: %v", err)
	}
	if billRepo.updatePaidCall {
		t.Error("UpdatePaidAmount must not be called when Record fails (no double-recording)")
	}
}

func TestZZRecordPayment_UpdatePaidAmount_ErrorWrapped(t *testing.T) {
	underlying := errors.New("boom: update failed")
	billRepo := &zzBillReader{bill: approvedBill(testBillID, decimal.NewFromFloat(100), nil), updatePaidErr: underlying}
	payRepo := &zzPaymentRepo{sumVal: decimal.NewFromFloat(20)}

	uc := apppayment.NewRecordPaymentUseCase(billRepo, payRepo)
	err := uc.Execute(context.Background(), apppayment.RecordPaymentRequest{
		TenantID: testTenantID, BillID: testBillID, CreatorID: testCreatorID,
		Amount: decimal.NewFromFloat(10), PayType: "cash",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "update paid_amount") {
		t.Errorf("error = %q, want to contain 'update paid_amount'", err.Error())
	}
	if !errors.Is(err, underlying) {
		t.Errorf("error chain does not wrap underlying error: %v", err)
	}
	// Record must have already happened (it runs before UpdatePaidAmount) — this
	// asserts ordering, not that the failure is silently swallowed.
	if !payRepo.recordCall {
		t.Error("Record should have been invoked before UpdatePaidAmount")
	}
}

// ----- RecordPaymentUseCase: over-payment guard boundary -----

func TestZZRecordPayment_OverPayBoundary(t *testing.T) {
	// bill.TotalAmount = 100, current paid = 80.
	// newPaid = 80 + amount. Hand-computed boundary: amount=20 -> newPaid=100 (equal, allowed);
	// amount=20.01 -> newPaid=100.01 (exceeds, rejected).
	t.Run("newPaid == TotalAmount is allowed", func(t *testing.T) {
		billRepo := &zzBillReader{bill: approvedBill(testBillID, decimal.NewFromFloat(100), nil)}
		payRepo := &zzPaymentRepo{sumVal: decimal.NewFromFloat(80)}

		uc := apppayment.NewRecordPaymentUseCase(billRepo, payRepo)
		err := uc.Execute(context.Background(), apppayment.RecordPaymentRequest{
			TenantID: testTenantID, BillID: testBillID, CreatorID: testCreatorID,
			Amount: decimal.NewFromFloat(20), PayType: "cash",
		})
		if err != nil {
			t.Fatalf("Execute: unexpected error at exact boundary: %v", err)
		}
		if !payRepo.recordCall {
			t.Error("Record should have been called for an allowed boundary payment")
		}
		wantPaid := decimal.NewFromFloat(100) // 80 + 20, hand-computed
		if !billRepo.lastPaidAmount.Equal(wantPaid) {
			t.Errorf("UpdatePaidAmount received %s, want %s (cumulative sum)", billRepo.lastPaidAmount, wantPaid)
		}
	})

	t.Run("newPaid just above TotalAmount is rejected, no writes", func(t *testing.T) {
		billRepo := &zzBillReader{bill: approvedBill(testBillID, decimal.NewFromFloat(100), nil)}
		payRepo := &zzPaymentRepo{sumVal: decimal.NewFromFloat(80)}

		uc := apppayment.NewRecordPaymentUseCase(billRepo, payRepo)
		err := uc.Execute(context.Background(), apppayment.RecordPaymentRequest{
			TenantID: testTenantID, BillID: testBillID, CreatorID: testCreatorID,
			Amount: decimal.NewFromFloat(20.01), PayType: "cash",
		})
		if err == nil {
			t.Fatal("expected over-payment error, got nil")
		}
		if !strings.Contains(err.Error(), "would exceed bill total") {
			t.Errorf("error = %q, want over-payment message", err.Error())
		}
		if payRepo.recordCall {
			t.Error("Record must NOT be called on over-payment rejection (no double-recording)")
		}
		if billRepo.updatePaidCall {
			t.Error("UpdatePaidAmount must NOT be called on over-payment rejection (no receivable overflow)")
		}
	})
}

// ----- RecordPaymentUseCase: cumulative paid_amount, PartnerID linkage, PayType fallback -----

func TestZZRecordPayment_CumulativePaidAmount_NotOverwritten(t *testing.T) {
	billRepo := &zzBillReader{bill: approvedBill(testBillID, decimal.NewFromFloat(500), nil)}
	payRepo := &zzPaymentRepo{sumVal: decimal.NewFromFloat(120)} // current paid before this payment

	uc := apppayment.NewRecordPaymentUseCase(billRepo, payRepo)
	err := uc.Execute(context.Background(), apppayment.RecordPaymentRequest{
		TenantID: testTenantID, BillID: testBillID, CreatorID: testCreatorID,
		Amount: decimal.NewFromFloat(75), PayType: "cash",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// newPaid must be the SUM (120+75=195), never just the new amount (75).
	want := decimal.NewFromFloat(195)
	if !billRepo.lastPaidAmount.Equal(want) {
		t.Errorf("UpdatePaidAmount received %s, want cumulative %s", billRepo.lastPaidAmount, want)
	}
	if billRepo.lastPaidAmount.Equal(decimal.NewFromFloat(75)) {
		t.Error("paid_amount must not be overwritten with the bare new-payment amount")
	}
}

func TestZZRecordPayment_PartnerID_CopiedWhenNonNil(t *testing.T) {
	partnerID := uuid.New()
	billRepo := &zzBillReader{bill: approvedBill(testBillID, decimal.NewFromFloat(100), &partnerID)}
	payRepo := &zzPaymentRepo{sumVal: decimal.Zero}

	uc := apppayment.NewRecordPaymentUseCase(billRepo, payRepo)
	err := uc.Execute(context.Background(), apppayment.RecordPaymentRequest{
		TenantID: testTenantID, BillID: testBillID, CreatorID: testCreatorID,
		Amount: decimal.NewFromFloat(10), PayType: "cash",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if payRepo.recorded == nil {
		t.Fatal("expected payment to be recorded")
	}
	if payRepo.recorded.PartnerID == nil || *payRepo.recorded.PartnerID != partnerID {
		t.Errorf("PartnerID = %v, want %s copied from bill_head", payRepo.recorded.PartnerID, partnerID)
	}
}

func TestZZRecordPayment_PartnerID_NilWhenBillHasNone(t *testing.T) {
	billRepo := &zzBillReader{bill: approvedBill(testBillID, decimal.NewFromFloat(100), nil)}
	payRepo := &zzPaymentRepo{sumVal: decimal.Zero}

	uc := apppayment.NewRecordPaymentUseCase(billRepo, payRepo)
	err := uc.Execute(context.Background(), apppayment.RecordPaymentRequest{
		TenantID: testTenantID, BillID: testBillID, CreatorID: testCreatorID,
		Amount: decimal.NewFromFloat(10), PayType: "cash",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if payRepo.recorded.PartnerID != nil {
		t.Errorf("PartnerID = %v, want nil (bill_head has no partner)", payRepo.recorded.PartnerID)
	}
}

func TestZZRecordPayment_PayType_InvalidFallsBackToCash(t *testing.T) {
	cases := []string{"", "bogus-pay-type", "BITCOIN"}
	for _, pt := range cases {
		t.Run(pt, func(t *testing.T) {
			billRepo := &zzBillReader{bill: approvedBill(testBillID, decimal.NewFromFloat(100), nil)}
			payRepo := &zzPaymentRepo{sumVal: decimal.Zero}

			uc := apppayment.NewRecordPaymentUseCase(billRepo, payRepo)
			err := uc.Execute(context.Background(), apppayment.RecordPaymentRequest{
				TenantID: testTenantID, BillID: testBillID, CreatorID: testCreatorID,
				Amount: decimal.NewFromFloat(10), PayType: pt,
			})
			if err != nil {
				t.Fatalf("Execute: unexpected error for invalid pay_type %q: %v", pt, err)
			}
			if payRepo.recorded.PayType != domainpayment.PayTypeCash {
				t.Errorf("PayType = %q, want fallback %q", payRepo.recorded.PayType, domainpayment.PayTypeCash)
			}
		})
	}
}

func TestZZRecordPayment_PayType_ValidIsKept(t *testing.T) {
	billRepo := &zzBillReader{bill: approvedBill(testBillID, decimal.NewFromFloat(100), nil)}
	payRepo := &zzPaymentRepo{sumVal: decimal.Zero}

	uc := apppayment.NewRecordPaymentUseCase(billRepo, payRepo)
	err := uc.Execute(context.Background(), apppayment.RecordPaymentRequest{
		TenantID: testTenantID, BillID: testBillID, CreatorID: testCreatorID,
		Amount: decimal.NewFromFloat(10), PayType: string(domainpayment.PayTypeWechat),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if payRepo.recorded.PayType != domainpayment.PayTypeWechat {
		t.Errorf("PayType = %q, want kept %q", payRepo.recorded.PayType, domainpayment.PayTypeWechat)
	}
}

func TestZZRecordPayment_DraftBill_RejectedInsideTx(t *testing.T) {
	billRepo := &zzBillReader{bill: &domainbill.BillHead{
		ID: testBillID, TenantID: testTenantID, Status: domainbill.StatusDraft, TotalAmount: decimal.NewFromFloat(100),
	}}
	payRepo := &zzPaymentRepo{}

	uc := apppayment.NewRecordPaymentUseCase(billRepo, payRepo)
	err := uc.Execute(context.Background(), apppayment.RecordPaymentRequest{
		TenantID: testTenantID, BillID: testBillID, CreatorID: testCreatorID,
		Amount: decimal.NewFromFloat(10), PayType: "cash",
	})
	if err == nil {
		t.Fatal("expected error for non-approved bill")
	}
	if !strings.Contains(err.Error(), "bill must be approved") {
		t.Errorf("error = %q, want approved-status message", err.Error())
	}
	if billRepo.withTxCalls != 1 {
		t.Errorf("WithTx calls = %d, want 1 (status check happens inside tx)", billRepo.withTxCalls)
	}
	if payRepo.recordCall {
		t.Error("Record must not be called for a non-approved bill")
	}
}

// ----- ListPaymentsUseCase -----

func TestZZListPayments_NilGuards(t *testing.T) {
	cases := []struct {
		name     string
		tenantID uuid.UUID
		billID   uuid.UUID
		want     string
	}{
		{"tenant_id nil", uuid.Nil, testBillID, "tenant_id is required"},
		{"bill_id nil", testTenantID, uuid.Nil, "bill_id is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payRepo := &zzPaymentRepo{}
			uc := apppayment.NewListPaymentsUseCase(payRepo)
			payments, err := uc.Execute(context.Background(), tc.tenantID, tc.billID)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want substring %q", err.Error(), tc.want)
			}
			if payments != nil {
				t.Errorf("payments = %v, want nil on error", payments)
			}
		})
	}
}

func TestZZListPayments_RepoError_Wrapped(t *testing.T) {
	underlying := errors.New("boom: list query failed")
	payRepo := &zzPaymentRepo{listErr: underlying}
	uc := apppayment.NewListPaymentsUseCase(payRepo)

	payments, err := uc.Execute(context.Background(), testTenantID, testBillID)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, underlying) {
		t.Errorf("error chain does not wrap underlying error: %v", err)
	}
	if !strings.Contains(err.Error(), "list payments") {
		t.Errorf("error = %q, want to contain 'list payments'", err.Error())
	}
	if payments != nil {
		t.Errorf("payments = %v, want nil on error", payments)
	}
}

func TestZZListPayments_NilRepoSlice_NormalisedToEmpty(t *testing.T) {
	payRepo := &zzPaymentRepo{listVal: nil}
	uc := apppayment.NewListPaymentsUseCase(payRepo)

	payments, err := uc.Execute(context.Background(), testTenantID, testBillID)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if payments == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(payments) != 0 {
		t.Errorf("len(payments) = %d, want 0", len(payments))
	}
}
