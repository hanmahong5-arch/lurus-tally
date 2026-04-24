package product_test

import (
	"testing"

	"github.com/hanmahong5-arch/lurus-tally/internal/domain/product"
)

func TestMeasurementStrategy_IsValid_AcceptsAllDefinedValues(t *testing.T) {
	valid := []product.MeasurementStrategy{
		product.StrategyIndividual,
		product.StrategyWeight,
		product.StrategyLength,
		product.StrategyVolume,
		product.StrategyBatch,
		product.StrategySerial,
	}
	for _, s := range valid {
		if !s.IsValid() {
			t.Errorf("expected %q to be valid", s)
		}
	}
}

func TestMeasurementStrategy_IsValid_RejectsUnknown(t *testing.T) {
	bad := []product.MeasurementStrategy{
		"",
		"unit_count",
		"by_weight",
		"lot_based",
		"INDIVIDUAL",
		"random",
	}
	for _, s := range bad {
		if s.IsValid() {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}

func TestMeasurementStrategy_Validate_ReturnsNilForValid(t *testing.T) {
	if err := product.StrategyIndividual.Validate(); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestMeasurementStrategy_Validate_ReturnsErrorForInvalid(t *testing.T) {
	err := product.MeasurementStrategy("bad").Validate()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
