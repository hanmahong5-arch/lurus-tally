package bill_test

import (
	"testing"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

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
