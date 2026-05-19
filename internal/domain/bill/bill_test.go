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

// TestBillHead_CanTransitionTo_LegalPairs verifies all legal BillHead.CanTransitionTo pairs.
func TestBillHead_CanTransitionTo_LegalPairs(t *testing.T) {
	cases := []struct {
		name     string
		head     domain.BillHead
		next     domain.BillStatus
		wantNil  bool
	}{
		{
			name:    "draft→approved",
			head:    domain.BillHead{Status: domain.StatusDraft},
			next:    domain.StatusApproved,
			wantNil: true,
		},
		{
			name:    "draft→cancelled",
			head:    domain.BillHead{Status: domain.StatusDraft},
			next:    domain.StatusCancelled,
			wantNil: true,
		},
		{
			name:    "approved→cancelled",
			head:    domain.BillHead{Status: domain.StatusApproved},
			next:    domain.StatusCancelled,
			wantNil: true,
		},
		{
			name:    "cancelled→draft (revision=0, first restore)",
			head:    domain.BillHead{Status: domain.StatusCancelled, Revision: 0},
			next:    domain.StatusDraft,
			wantNil: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.head.CanTransitionTo(tc.next)
			if tc.wantNil && err != nil {
				t.Errorf("expected nil, got %v", err)
			}
		})
	}
}

// TestBillHead_CanTransitionTo_IllegalPairs verifies key illegal transitions.
func TestBillHead_CanTransitionTo_IllegalPairs(t *testing.T) {
	cases := []struct {
		name string
		head domain.BillHead
		next domain.BillStatus
	}{
		{
			name: "cancelled→draft (revision=1, cap reached)",
			head: domain.BillHead{Status: domain.StatusCancelled, Revision: 1},
			next: domain.StatusDraft,
		},
		{
			name: "cancelled→draft (revision=2, already restored twice)",
			head: domain.BillHead{Status: domain.StatusCancelled, Revision: 2},
			next: domain.StatusDraft,
		},
		{
			name: "approved→draft (not allowed)",
			head: domain.BillHead{Status: domain.StatusApproved},
			next: domain.StatusDraft,
		},
		{
			name: "cancelled→approved (not allowed)",
			head: domain.BillHead{Status: domain.StatusCancelled},
			next: domain.StatusApproved,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.head.CanTransitionTo(tc.next)
			if err == nil {
				t.Errorf("expected ErrIllegalTransition, got nil")
			}
		})
	}
}
