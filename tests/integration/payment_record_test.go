//go:build integration

// Package integration — payment recording regression test.
//
// WHY THIS TEST EXISTS
// --------------------
// POST /api/v1/payments was permanently 422'ing (UAT 2026-06-16 finding J2):
// payment repo SumByBill ran
//
//	SELECT COALESCE(SUM(amount),0) ... FOR UPDATE
//
// which PostgreSQL rejects with SQLSTATE 0A000 ("FOR UPDATE is not allowed with
// aggregate functions"). RecordPaymentUseCase therefore could never commit a
// payment. The over-payment serialisation it intended is already provided by
// GetBillForUpdate locking the bill_head row, so the FOR UPDATE on the aggregate
// was both illegal and redundant. This test drives the REAL use case + repos
// against real Postgres and asserts a payment records, accumulates across calls,
// and that the over-payment guard still rejects past the bill total.
//
// Run:
//
//	go test -tags integration -run TestRecordPayment ./tests/integration/ -timeout 360s -v
package integration

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"

	repobill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/bill"
	repopayment "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/payment"
	apppayment "github.com/hanmahong5-arch/lurus-tally/internal/app/payment"
)

func TestRecordPayment_AccumulatesAndGuardsOverpayment(t *testing.T) {
	db, cleanup := sqlRealDB(t)
	defer cleanup()

	ctx := context.Background()

	tenantID := insertTenant(t, db, ctx)
	supplierID := insertPartner(t, db, ctx, tenantID, "Pay Supplier Co", "supplier")

	// Approved purchase bill (status=2). insertBillHead leaves total_amount at its
	// default, so set a known bill total to exercise the over-payment guard.
	billID := insertBillHead(t, db, ctx, tenantID, "入库", "采购", 2, &supplierID, time.Now().Add(-24*time.Hour))
	// Set a known bill total; also set remark='' to match production bills (CreateBill
	// always writes a non-NULL remark — the bare fixture leaves it NULL, which the
	// shared scanBillHead does not accept).
	if _, err := db.ExecContext(ctx,
		`UPDATE tally.bill_head SET total_amount = '1000', remark = '' WHERE id = $1`, billID); err != nil {
		t.Fatalf("set bill total_amount: %v", err)
	}

	billRepo := repobill.New(db)
	payRepo := repopayment.New(db)
	uc := apppayment.NewRecordPaymentUseCase(billRepo, payRepo)

	creatorID := uuid.New()

	// 1. First payment of 300 must SUCCEED (pre-fix: SQLSTATE 0A000 from SumByBill).
	if err := uc.Execute(ctx, apppayment.RecordPaymentRequest{
		TenantID:  tenantID,
		BillID:    billID,
		CreatorID: creatorID,
		Amount:    decimal.NewFromInt(300),
		PayType:   "bank",
		Remark:    "first",
	}); err != nil {
		t.Fatalf("first payment must record (FOR UPDATE+SUM bug), got: %v", err)
	}

	// 2. Second payment of 200 accumulates to 500 — proves SumByBill reads the
	//    committed prior payment correctly without the FOR UPDATE on the aggregate.
	if err := uc.Execute(ctx, apppayment.RecordPaymentRequest{
		TenantID:  tenantID,
		BillID:    billID,
		CreatorID: creatorID,
		Amount:    decimal.NewFromInt(200),
		PayType:   "bank",
		Remark:    "second",
	}); err != nil {
		t.Fatalf("second payment must record, got: %v", err)
	}

	// Two payment_head rows exist and bill_head.paid_amount == 500.
	var payCount int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM tally.payment_head WHERE related_bill_id = $1 AND deleted_at IS NULL`,
		billID).Scan(&payCount); err != nil {
		t.Fatalf("count payments: %v", err)
	}
	if payCount != 2 {
		t.Errorf("expected 2 payment rows, got %d", payCount)
	}
	var paid string
	if err := db.QueryRowContext(ctx,
		`SELECT paid_amount::text FROM tally.bill_head WHERE id = $1`, billID).Scan(&paid); err != nil {
		t.Fatalf("read paid_amount: %v", err)
	}
	if got, _ := decimal.NewFromString(paid); !got.Equal(decimal.NewFromInt(500)) {
		t.Errorf("paid_amount: got %s, want 500", paid)
	}

	// 3. A third payment of 600 (would total 1100 > 1000) must be REJECTED — the
	//    accumulation read is correct, so the over-payment guard still fires.
	if err := uc.Execute(ctx, apppayment.RecordPaymentRequest{
		TenantID:  tenantID,
		BillID:    billID,
		CreatorID: creatorID,
		Amount:    decimal.NewFromInt(600),
		PayType:   "bank",
		Remark:    "overpay",
	}); err == nil {
		t.Fatal("over-payment (500+600 > 1000) must be rejected, got nil error")
	}

	// The rejected payment did not persist: still 2 rows, paid_amount still 500.
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM tally.payment_head WHERE related_bill_id = $1 AND deleted_at IS NULL`,
		billID).Scan(&payCount); err != nil {
		t.Fatalf("recount payments: %v", err)
	}
	if payCount != 2 {
		t.Errorf("over-payment must not persist: expected 2 rows, got %d", payCount)
	}

	t.Logf("PASS: payment records, accumulates to 500, over-payment rejected")
}
