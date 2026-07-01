package grill_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/domain/grill"
)

func intp(i int) *int { return &i }

func TestSession_Lifecycle(t *testing.T) {
	s := grill.Session{Status: grill.SessionOpen}
	if err := s.Activate(); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if s.Status != grill.SessionActive {
		t.Fatalf("status=%q, want active", s.Status)
	}
	if err := s.BeginSettle(); err != nil {
		t.Fatalf("BeginSettle: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if s.Status != grill.SessionClosed {
		t.Fatalf("status=%q, want closed", s.Status)
	}

	// illegal transitions
	open := grill.Session{Status: grill.SessionOpen}
	if err := open.Close(); err == nil {
		t.Error("Close from open: expected error")
	}
	if err := open.BeginSettle(); err == nil {
		t.Error("BeginSettle from open: expected error")
	}
	active := grill.Session{Status: grill.SessionActive}
	if err := active.Activate(); err == nil {
		t.Error("Activate from active: expected error")
	}
}

// TestOrderItem_CalibrationGate covers INV-2 (no grilling before calibration)
// and INV-5 (calibrated qty overrides the ordered/estimated qty).
func TestOrderItem_CalibrationGate(t *testing.T) {
	o := grill.OrderItem{Status: grill.ItemPending, Qty: 10}

	if o.AccountingQty() != 10 {
		t.Errorf("AccountingQty before calibration: got %d, want ordered 10", o.AccountingQty())
	}
	if err := o.StartGrilling(); err != grill.ErrNotCalibrated {
		t.Fatalf("StartGrilling uncalibrated: got %v, want ErrNotCalibrated", err)
	}

	if err := o.Calibrate(14); err != nil {
		t.Fatalf("Calibrate: %v", err)
	}
	if o.AccountingQty() != 14 {
		t.Errorf("AccountingQty after calibration: got %d, want 14 (INV-5 override)", o.AccountingQty())
	}
	if err := o.StartGrilling(); err != nil {
		t.Fatalf("StartGrilling calibrated: %v", err)
	}
	if o.Status != grill.ItemGrilling {
		t.Errorf("status=%q, want grilling", o.Status)
	}
	if err := o.Serve(); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if o.Status != grill.ItemServed {
		t.Errorf("status=%q, want served", o.Status)
	}

	// calibrate only while pending
	g := grill.OrderItem{Status: grill.ItemGrilling}
	if err := g.Calibrate(5); err == nil {
		t.Error("Calibrate while grilling: expected error")
	}
}

func TestOrderItem_LineAmount(t *testing.T) {
	o := grill.OrderItem{
		Status:            grill.ItemPending,
		Qty:               10,
		CalibratedQty:     intp(14),
		UnitPriceSnapshot: decimal.RequireFromString("2.50"),
	}
	want := decimal.RequireFromString("35.00") // 2.50 × 14 (calibrated)
	if got := o.LineAmount(); !got.Equal(want) {
		t.Errorf("LineAmount=%s, want %s", got, want)
	}
}

func TestOrderItem_Cancel(t *testing.T) {
	for _, st := range []grill.OrderItemStatus{grill.ItemPending, grill.ItemGrilling, grill.ItemServed} {
		o := grill.OrderItem{Status: st}
		if err := o.Cancel(); err != nil {
			t.Errorf("Cancel from %q: unexpected error %v", st, err)
		}
		if o.Status != grill.ItemCancelled {
			t.Errorf("status=%q, want cancelled", o.Status)
		}
	}
	dead := grill.OrderItem{Status: grill.ItemCancelled}
	if err := dead.Cancel(); err == nil {
		t.Error("Cancel already cancelled: expected error")
	}
}

// TestOrderItem_DeductionIdempotency covers INV-3: a stable per-item movement
// key and single-deduction guard.
func TestOrderItem_DeductionIdempotency(t *testing.T) {
	id := uuid.New()
	o := grill.OrderItem{ID: id, Status: grill.ItemPending}

	if o.MovementReference() != id {
		t.Errorf("MovementReference=%v, want item id %v", o.MovementReference(), id)
	}
	if !o.NeedsDeduction() {
		t.Error("fresh item should need deduction")
	}

	mid := uuid.New()
	if err := o.MarkDeducted(mid); err != nil {
		t.Fatalf("MarkDeducted: %v", err)
	}
	if o.NeedsDeduction() {
		t.Error("item should not need deduction after MarkDeducted")
	}
	if err := o.MarkDeducted(uuid.New()); err != grill.ErrAlreadyDeducted {
		t.Errorf("second MarkDeducted: got %v, want ErrAlreadyDeducted", err)
	}

	cancelled := grill.OrderItem{ID: uuid.New(), Status: grill.ItemCancelled}
	if cancelled.NeedsDeduction() {
		t.Error("cancelled item must not need deduction")
	}
}
