package stock_test

import (
	"testing"

	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	"github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// stubProfile implements appstock.Profile for testing.
type stubProfile struct{ method string }

func (s stubProfile) InventoryMethod() string { return s.method }

func TestCalculatorFactory_WAC_SelectedForRetail(t *testing.T) {
	calc := appstock.NewCalculator(stubProfile{"wac"}, nil)
	if calc.Name() != stock.CostStrategyWAC {
		t.Errorf("NewCalculator(wac).Name() = %q, want %q", calc.Name(), stock.CostStrategyWAC)
	}
}

func TestCalculatorFactory_FIFO_SelectedForCrossBorder(t *testing.T) {
	calc := appstock.NewCalculator(stubProfile{"fifo"}, nil)
	if calc.Name() != stock.CostStrategyFIFO {
		t.Errorf("NewCalculator(fifo).Name() = %q, want %q", calc.Name(), stock.CostStrategyFIFO)
	}
}

func TestCalculatorFactory_EmptyMethod_DefaultsToWAC(t *testing.T) {
	calc := appstock.NewCalculator(stubProfile{""}, nil)
	if calc.Name() != stock.CostStrategyWAC {
		t.Errorf("NewCalculator('').Name() = %q, want %q", calc.Name(), stock.CostStrategyWAC)
	}
}

func TestCalculatorFactory_NilProfile_DefaultsToWAC(t *testing.T) {
	calc := appstock.NewCalculator(nil, nil)
	if calc.Name() != stock.CostStrategyWAC {
		t.Errorf("NewCalculator(nil).Name() = %q, want %q", calc.Name(), stock.CostStrategyWAC)
	}
}
