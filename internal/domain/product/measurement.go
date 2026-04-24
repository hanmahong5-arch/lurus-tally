// Package product contains domain entities and value objects for the product catalogue.
package product

import "fmt"

// MeasurementStrategy defines how a product's quantity is measured and tracked.
// Values are locked in DL-3 (decision-lock.md) and stored in product.measurement_strategy.
type MeasurementStrategy string

const (
	// StrategyIndividual is the default — whole-unit items with discrete count.
	StrategyIndividual MeasurementStrategy = "individual"
	// StrategyWeight is for bulk items sold by weight (e.g. screws, grain).
	StrategyWeight MeasurementStrategy = "weight"
	// StrategyLength is for items sold by linear measure (e.g. pipe, cable).
	StrategyLength MeasurementStrategy = "length"
	// StrategyVolume is for liquids or gases sold by volume.
	StrategyVolume MeasurementStrategy = "volume"
	// StrategyBatch groups items under a lot number (food, pharma).
	StrategyBatch MeasurementStrategy = "batch"
	// StrategySerial assigns a unique serial number per item (electronics).
	StrategySerial MeasurementStrategy = "serial"
)

// validStrategies is the exhaustive set accepted by the DB CHECK constraint.
var validStrategies = map[MeasurementStrategy]struct{}{
	StrategyIndividual: {},
	StrategyWeight:     {},
	StrategyLength:     {},
	StrategyVolume:     {},
	StrategyBatch:      {},
	StrategySerial:     {},
}

// IsValid reports whether s is one of the six defined strategies.
func (s MeasurementStrategy) IsValid() bool {
	_, ok := validStrategies[s]
	return ok
}

// Validate returns a descriptive error when s is not one of the six defined values.
// Use at API boundaries to give callers a clear message about what is expected.
func (s MeasurementStrategy) Validate() error {
	if !s.IsValid() {
		return fmt.Errorf(
			"invalid measurement_strategy %q: must be one of individual|weight|length|volume|batch|serial",
			string(s),
		)
	}
	return nil
}
