package stock_test

import (
	"testing"

	"github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

func TestMovement_Direction_Valid(t *testing.T) {
	cases := []stock.Direction{
		stock.DirectionIn,
		stock.DirectionOut,
		stock.DirectionAdjust,
	}
	for _, d := range cases {
		if err := d.Validate(); err != nil {
			t.Errorf("Direction %q.Validate() = %v, want nil", d, err)
		}
	}
}

func TestMovement_Direction_Invalid(t *testing.T) {
	invalid := []stock.Direction{"IN", "OUT", "ADJUST", "receive", "ship", ""}
	for _, d := range invalid {
		if err := d.Validate(); err == nil {
			t.Errorf("Direction %q.Validate() = nil, want error", d)
		}
	}
}
