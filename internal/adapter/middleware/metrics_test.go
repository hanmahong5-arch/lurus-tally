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

// TestIncWAD_IncrementsNorthStar verifies the WAD counter moves on each call.
func TestIncWAD_IncrementsNorthStar(t *testing.T) {
	wadTotal.Reset()

	tenant := "66666666-6666-6666-6666-666666666666"
	IncWAD(tenant)
	IncWAD(tenant)

	var got float64
	if perTenantEnabled {
		got = testutil.ToFloat64(wadTotal.WithLabelValues(tenant))
	} else {
		got = testutil.ToFloat64(wadTotal.WithLabelValues())
	}
	if got != 2 {
		t.Errorf("tally_wad_total = %v, want 2", got)
	}
}

// TestIncAIPlanExecuted_IncrementsByType verifies the per-type AI execution counter.
func TestIncAIPlanExecuted_IncrementsByType(t *testing.T) {
	aiPlanExecuted.Reset()

	tenant := "77777777-7777-7777-7777-777777777777"
	IncAIPlanExecuted("create_purchase_draft", tenant)

	var got float64
	if perTenantEnabled {
		got = testutil.ToFloat64(aiPlanExecuted.WithLabelValues("create_purchase_draft", tenant))
	} else {
		got = testutil.ToFloat64(aiPlanExecuted.WithLabelValues("create_purchase_draft"))
	}
	if got != 1 {
		t.Errorf("tally_ai_plan_executed_total{create_purchase_draft} = %v, want 1", got)
	}
}

// TestIncWebTelemetry_IncrementsByEvent verifies the web-telemetry funnel counter.
func TestIncWebTelemetry_IncrementsByEvent(t *testing.T) {
	webTelemetry.Reset()

	IncWebTelemetry("palette_invocation")
	IncWebTelemetry("palette_invocation")
	IncWebTelemetry("ai_drawer_open")

	if got := testutil.ToFloat64(webTelemetry.WithLabelValues("palette_invocation")); got != 2 {
		t.Errorf("tally_web_telemetry_total{palette_invocation} = %v, want 2", got)
	}
	if got := testutil.ToFloat64(webTelemetry.WithLabelValues("ai_drawer_open")); got != 1 {
		t.Errorf("tally_web_telemetry_total{ai_drawer_open} = %v, want 1", got)
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
