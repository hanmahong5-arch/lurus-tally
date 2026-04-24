package bill_test

import (
	"database/sql"
	"testing"

	repobill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/bill"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
)

// TestBillRepoPG_InterfaceSatisfied is a compile-time proof that *Repo satisfies BillRepo.
// The actual DB integration tests require a live PG instance (CI environment).
func TestBillRepoPG_InterfaceSatisfied(t *testing.T) {
	var _ appbill.BillRepo = repobill.New((*sql.DB)(nil))
	t.Log("*repobill.Repo satisfies appbill.BillRepo — compile-time check passed")
}
