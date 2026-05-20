package middleware

// metrics_test.go is in package middleware so it can access the unexported
// counter/gauge vars and reset them between tests.

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestIncBillApproved_IncrementsPurchaseCounter verifies approve purchase path
// increments tally_bill_approved_total{type="purchase"}.
func TestIncBillApproved_IncrementsPurchaseCounter(t *testing.T) {
	billApproved.Reset()

	tenant := "11111111-1111-1111-1111-111111111111"
	IncBillApproved("purchase", tenant)

	labels := billLabelValues("purchase", tenant)
	got := testutil.ToFloat64(billApproved.WithLabelValues(labels...))
	if got != 1 {
		t.Errorf("tally_bill_approved_total{purchase} = %v, want 1", got)
	}
}

// TestIncBillApproved_IncrementsSaleCounter verifies the sale path.
func TestIncBillApproved_IncrementsSaleCounter(t *testing.T) {
	billApproved.Reset()

	tenant := "22222222-2222-2222-2222-222222222222"
	IncBillApproved("sale", tenant)

	labels := billLabelValues("sale", tenant)
	got := testutil.ToFloat64(billApproved.WithLabelValues(labels...))
	if got != 1 {
		t.Errorf("tally_bill_approved_total{sale} = %v, want 1", got)
	}
}

// TestIncBillCancelled_IncrementsCounter verifies the cancelled counter.
func TestIncBillCancelled_IncrementsCounter(t *testing.T) {
	billCancelled.Reset()

	tenant := "33333333-3333-3333-3333-333333333333"
	IncBillCancelled("purchase", tenant)

	labels := billLabelValues("purchase", tenant)
	got := testutil.ToFloat64(billCancelled.WithLabelValues(labels...))
	if got != 1 {
		t.Errorf("tally_bill_cancelled_total{purchase} = %v, want 1", got)
	}
}

// TestIncPaymentCreated_IncrementsCounter verifies the payment counter.
func TestIncPaymentCreated_IncrementsCounter(t *testing.T) {
	paymentCreated.Reset()

	tenant := "44444444-4444-4444-4444-444444444444"
	IncPaymentCreated("CNY", tenant)

	labels := paymentLabelValues("CNY", tenant)
	got := testutil.ToFloat64(paymentCreated.WithLabelValues(labels...))
	if got != 1 {
		t.Errorf("tally_payment_created_total{CNY} = %v, want 1", got)
	}
}

// TestIncStockMovement_IncrementsInAndOut verifies stock movement counter.
func TestIncStockMovement_IncrementsInAndOut(t *testing.T) {
	stockMovement.Reset()

	tenant := "55555555-5555-5555-5555-555555555555"
	IncStockMovement("in", tenant)
	IncStockMovement("in", tenant)
	IncStockMovement("out", tenant)

	inLabels := stockLabelValues("in", tenant)
	outLabels := stockLabelValues("out", tenant)

	gotIn := testutil.ToFloat64(stockMovement.WithLabelValues(inLabels...))
	if gotIn != 2 {
		t.Errorf("tally_stock_movement_total{in} = %v, want 2", gotIn)
	}
	gotOut := testutil.ToFloat64(stockMovement.WithLabelValues(outLabels...))
	if gotOut != 1 {
		t.Errorf("tally_stock_movement_total{out} = %v, want 1", gotOut)
	}
}

// TestSetOutboxPending_SetsGauge verifies the outbox gauge is set to the given value.
func TestSetOutboxPending_SetsGauge(t *testing.T) {
	outboxPendingCount.Set(0)

	SetOutboxPending(42)

	got := testutil.ToFloat64(outboxPendingCount)
	if got != 42 {
		t.Errorf("tally_outbox_pending_count = %v, want 42", got)
	}
}

// TestSetOutboxOldestAge_SetsGauge verifies the outbox age gauge.
func TestSetOutboxOldestAge_SetsGauge(t *testing.T) {
	outboxOldestAgeSeconds.Set(0)

	SetOutboxOldestAge(300.5)

	got := testutil.ToFloat64(outboxOldestAgeSeconds)
	if got != 300.5 {
		t.Errorf("tally_outbox_oldest_age_seconds = %v, want 300.5", got)
	}
}
