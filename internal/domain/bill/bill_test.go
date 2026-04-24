package bill_test

import (
	"testing"

	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// TestBillHead_SaleType_Valid verifies BillTypeSale and BillSubTypeSale constants are set correctly.
func TestBillHead_SaleType_Valid(t *testing.T) {
	if domain.BillTypeSale != "出库" {
		t.Errorf("BillTypeSale = %q, want %q", domain.BillTypeSale, "出库")
	}
	if domain.BillSubTypeSale != "销售" {
		t.Errorf("BillSubTypeSale = %q, want %q", domain.BillSubTypeSale, "销售")
	}
}

// TestBillHead_ReceivableAmount_Normal verifies receivable = total - paid.
func TestBillHead_ReceivableAmount_Normal(t *testing.T) {
	h := &domain.BillHead{
		TotalAmount: decimal.NewFromFloat(100),
		PaidAmount:  decimal.NewFromFloat(40),
	}
	want := decimal.NewFromFloat(60)
	if !h.ReceivableAmount().Equal(want) {
		t.Errorf("ReceivableAmount = %s, want %s", h.ReceivableAmount(), want)
	}
}

// TestBillHead_ReceivableAmount_ClampedToZero verifies negative result clamps to 0.
func TestBillHead_ReceivableAmount_ClampedToZero(t *testing.T) {
	h := &domain.BillHead{
		TotalAmount: decimal.NewFromFloat(100),
		PaidAmount:  decimal.NewFromFloat(110),
	}
	if !h.ReceivableAmount().IsZero() {
		t.Errorf("expected zero, got %s", h.ReceivableAmount())
	}
}

// TestBillStatus_Transitions_Legal verifies draft → approved and draft → cancelled are allowed.
func TestBillStatus_Transitions_Legal(t *testing.T) {
	cases := []struct {
		from domain.BillStatus
		to   domain.BillStatus
	}{
		{domain.StatusDraft, domain.StatusApproved},
		{domain.StatusDraft, domain.StatusCancelled},
	}
	for _, tc := range cases {
		if !tc.from.CanTransitionTo(tc.to) {
			t.Errorf("expected %d → %d to be legal", tc.from, tc.to)
		}
	}
}

// TestBillStatus_Transitions_Illegal verifies approved → cancelled and cancelled → anything are blocked.
func TestBillStatus_Transitions_Illegal(t *testing.T) {
	cases := []struct {
		from domain.BillStatus
		to   domain.BillStatus
	}{
		{domain.StatusApproved, domain.StatusCancelled},
		{domain.StatusApproved, domain.StatusDraft},
		{domain.StatusCancelled, domain.StatusDraft},
		{domain.StatusCancelled, domain.StatusApproved},
	}
	for _, tc := range cases {
		if tc.from.CanTransitionTo(tc.to) {
			t.Errorf("expected %d → %d to be illegal", tc.from, tc.to)
		}
	}
}
