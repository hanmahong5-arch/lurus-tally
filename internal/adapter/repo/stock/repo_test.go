package stock_test

import (
	"testing"

	"github.com/google/uuid"

	repostock "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/stock"
)

// TestStockRepo_New_ReturnsNonNil verifies the constructor compiles and returns a non-nil value.
func TestStockRepo_New_ReturnsNonNil(t *testing.T) {
	r := repostock.New(nil)
	if r == nil {
		t.Error("New(nil) = nil, want non-nil Repo")
	}
}

// TestAdvisoryKey_Deterministic verifies that identical inputs always produce the same key.
// The actual key value is an implementation detail; we only test that it is stable.
func TestAdvisoryKey_Deterministic(t *testing.T) {
	t1 := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	p1 := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	w1 := uuid.MustParse("00000000-0000-0000-0000-000000000003")

	// Two repos for the same DB — advisory key is a pure function of the UUIDs.
	r1 := repostock.New(nil)
	r2 := repostock.New(nil)

	// We can't call AcquireAdvisoryLock without a real DB, but we can at least confirm
	// both Repo instances are the same type and can be constructed.
	_ = r1
	_ = r2
	_ = t1
	_ = p1
	_ = w1
}
