package onboarding

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/app/replenish"
)

// allDemoSKUs returns every persona's catalogue flattened, for table tests.
func allDemoSKUs() []demoSKU {
	var out []demoSKU
	for _, p := range []Persona{PersonaCrossBorder, PersonaRetail, PersonaHorticulture} {
		out = append(out, demoCatalogue(p)...)
	}
	return out
}

// TestCalibration_AlertSetEqualsLowStockIntent locks the calibration invariant:
// computed through the REAL replenish.Forecast + ReorderPoint (lead 7, the seed's
// default), a SKU alerts (ROP > qtyOnHand) iff it was tagged lowStock. Driving it
// through Forecast means it cannot drift if the ROP constants change.
func TestCalibration_AlertSetEqualsLowStockIntent(t *testing.T) {
	thirty := decimal.NewFromInt(30)
	for _, sku := range allDemoSKUs() {
		avgDaily := sku.monthlySales.Div(thirty)
		f := replenish.Forecast(replenish.RawRow{
			AvailableQty:  sku.qtyOnHand,
			AvgDailySales: avgDaily,
			LeadTimeDays:  defaultSeedLeadTimeDays,
		}, replenish.DefaultWeeks)

		alerts := replenish.ReorderPoint(f).GreaterThan(sku.qtyOnHand)
		if alerts != sku.lowStock {
			t.Errorf("%s: alerts=%v (ROP=%s, onHand=%s) but lowStock intent=%v",
				sku.code, alerts, replenish.ReorderPoint(f), sku.qtyOnHand, sku.lowStock)
		}
	}
}

// TestCalibration_ExactlyOneAlertPerPersona confirms each persona shows exactly
// one urgent SKU (1 alert + 2 healthy) — the intended demo 观感.
func TestCalibration_ExactlyOneAlertPerPersona(t *testing.T) {
	for _, p := range []Persona{PersonaCrossBorder, PersonaRetail, PersonaHorticulture} {
		low := 0
		for _, sku := range demoCatalogue(p) {
			if sku.lowStock {
				low++
			}
		}
		if low != 1 {
			t.Errorf("persona %s: want exactly 1 low-stock SKU, got %d", p, low)
		}
	}
}

// defaultSeedLeadTimeDays mirrors tally.product.lead_time_days default (7) — the
// lead time a freshly-seeded demo SKU carries, against which the ROP is computed.
const defaultSeedLeadTimeDays = 7

// TestSalesSchedule_IntegerTotals_IntegerPartsExactSumWindow verifies the pure
// scheduler for whole-number totals (every demo SKU): K parts, integer
// quantities summing exactly to total, every occurredAt strictly inside
// (now−30d, now).
func TestSalesSchedule_IntegerTotals_IntegerPartsExactSumWindow(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	lower := now.Add(-30 * 24 * time.Hour)

	// Cover every catalogue total plus a couple of edge totals.
	totals := []int64{150, 96, 90, 72, 54, 66, 36, 24, 1, 7, 100}
	for _, total := range totals {
		tot := decimal.NewFromInt(total)
		sched := salesSchedule(tot, now)

		if len(sched) != demoSalesParts {
			t.Errorf("total %d: want %d parts, got %d", total, demoSalesParts, len(sched))
		}

		sum := decimal.Zero
		for _, s := range sched {
			// Integer quantity.
			if !s.qty.Equal(s.qty.Truncate(0)) {
				t.Errorf("total %d: non-integer part qty %s", total, s.qty)
			}
			sum = sum.Add(s.qty)
			// Strictly within (now−30d, now).
			if !s.occurredAt.After(lower) || !s.occurredAt.Before(now) {
				t.Errorf("total %d: occurredAt %v not strictly within (%v, %v)",
					total, s.occurredAt, lower, now)
			}
		}
		if !sum.Equal(tot) {
			t.Errorf("total %d: parts sum to %s, want %s", total, sum, tot)
		}
	}
}

// TestSalesSchedule_FractionalTotal_SumsExactly guards the defense-in-depth
// invariant: even a fractional total (no demo SKU has one today) sums to EXACTLY
// total — the last part absorbs the sub-unit remainder, so end-state on-hand can
// never silently drift if a future SKU carries a fractional monthlySales.
func TestSalesSchedule_FractionalTotal_SumsExactly(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	for _, str := range []string{"66.5", "10.25", "99.999", "0.1"} {
		tot, err := decimal.NewFromString(str)
		if err != nil {
			t.Fatalf("parse %s: %v", str, err)
		}
		sum := decimal.Zero
		for _, s := range salesSchedule(tot, now) {
			sum = sum.Add(s.qty)
		}
		if !sum.Equal(tot) {
			t.Errorf("fractional total %s: parts sum to %s, want exact", str, sum)
		}
	}
}
