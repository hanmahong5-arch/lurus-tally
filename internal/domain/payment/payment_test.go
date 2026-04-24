package payment_test

import (
	"testing"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
)

// TestPayment_PayType_Valid verifies all accepted pay types pass validation.
func TestPayment_PayType_Valid(t *testing.T) {
	validTypes := []domain.PayType{
		domain.PayTypeCash,
		domain.PayTypeWechat,
		domain.PayTypeAlipay,
		domain.PayTypeCard,
		domain.PayTypeCredit,
		domain.PayTypeTransfer,
	}
	for _, pt := range validTypes {
		if err := pt.Validate(); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", pt, err)
		}
	}
}

// TestPayment_PayType_Invalid verifies unrecognised strings fail validation.
func TestPayment_PayType_Invalid(t *testing.T) {
	invalid := []domain.PayType{"", "CASH", "bitcoin", "unknown"}
	for _, pt := range invalid {
		if err := pt.Validate(); err == nil {
			t.Errorf("Validate(%q) = nil, want error", pt)
		}
	}
}
