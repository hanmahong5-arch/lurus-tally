package supplier_test

import (
	"testing"

	"github.com/hanmahong5-arch/lurus-tally/internal/domain/supplier"
)

func TestSupplier_Validate_RejectsEmptyName(t *testing.T) {
	s := &supplier.Supplier{}
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
	if err.Error() != "name is required" {
		t.Errorf("expected 'name is required', got %q", err.Error())
	}
}

func TestSupplier_Validate_AcceptsValidName(t *testing.T) {
	s := &supplier.Supplier{Name: "深圳供应商A"}
	if err := s.Validate(); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}
