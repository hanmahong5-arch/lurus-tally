// Package payment_test contains compile-time interface compliance checks for Repo.
// Integration tests against a real DB are deferred to CI (testcontainers).
package payment_test

import (
	"database/sql"
	"testing"

	repopayment "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/payment"
	apppayment "github.com/hanmahong5-arch/lurus-tally/internal/app/payment"
)

// TestPaymentRepo_InterfaceCompliance verifies that *Repo satisfies PaymentRepo at compile time.
func TestPaymentRepo_InterfaceCompliance(t *testing.T) {
	var _ apppayment.PaymentRepo = repopayment.New((*sql.DB)(nil))
}
