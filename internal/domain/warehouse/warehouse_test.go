package warehouse_test

import (
	"testing"

	"github.com/hanmahong5-arch/lurus-tally/internal/domain/warehouse"
)

func TestWarehouse_Validate_RejectsEmptyName(t *testing.T) {
	w := &warehouse.Warehouse{}
	err := w.Validate()
	if err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
	if err.Error() != "name is required" {
		t.Errorf("expected 'name is required', got %q", err.Error())
	}
}

func TestWarehouse_Validate_AcceptsValidName(t *testing.T) {
	w := &warehouse.Warehouse{Name: "广州主仓库"}
	if err := w.Validate(); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}
