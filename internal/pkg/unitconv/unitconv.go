// Package unitconv provides unit conversion arithmetic using decimal strings.
// All inputs and outputs are NUMERIC-compatible strings to avoid float64 precision loss.
// The conversion_factor stored in product_unit is always relative to the base unit:
//
//	base_quantity = user_quantity * conversion_factor
//	user_quantity = base_quantity / conversion_factor
package unitconv

import (
	"fmt"
	"math/big"
)

const decimalPlaces = 6

// ConvertToBase converts a quantity expressed in a non-base unit to the base unit quantity.
// quantity and factor must be valid decimal strings (no NaN, no Inf).
// factor must be > 0; a zero or negative factor is a domain violation.
// Returns the result rounded to 6 decimal places as a string.
func ConvertToBase(quantity, factor string) (string, error) {
	q, ok := new(big.Float).SetPrec(128).SetString(quantity)
	if !ok {
		return "", fmt.Errorf("unitconv: invalid quantity %q: expected a decimal number", quantity)
	}

	f, ok := new(big.Float).SetPrec(128).SetString(factor)
	if !ok {
		return "", fmt.Errorf("unitconv: invalid conversion factor %q: expected a decimal number", factor)
	}

	if f.Sign() <= 0 {
		return "", fmt.Errorf(
			"unitconv: conversion factor must be > 0; got %s: "+
				"a zero or negative factor indicates a data integrity error in product_unit",
			factor,
		)
	}

	result := new(big.Float).SetPrec(128).Mul(q, f)
	return formatDecimal(result), nil
}

// ConvertFromBase converts a base-unit quantity to a non-base unit quantity.
// factor must be > 0 (division by zero is guarded).
func ConvertFromBase(baseQuantity, factor string) (string, error) {
	q, ok := new(big.Float).SetPrec(128).SetString(baseQuantity)
	if !ok {
		return "", fmt.Errorf("unitconv: invalid base quantity %q: expected a decimal number", baseQuantity)
	}

	f, ok := new(big.Float).SetPrec(128).SetString(factor)
	if !ok {
		return "", fmt.Errorf("unitconv: invalid conversion factor %q: expected a decimal number", factor)
	}

	if f.Sign() <= 0 {
		return "", fmt.Errorf(
			"unitconv: conversion factor must be > 0; got %s: "+
				"a zero or negative factor causes division by zero",
			factor,
		)
	}

	result := new(big.Float).SetPrec(128).Quo(q, f)
	return formatDecimal(result), nil
}

// formatDecimal formats a big.Float to a fixed-point string with exactly 6 decimal places.
func formatDecimal(f *big.Float) string {
	// Convert to string with enough digits, then parse as rat for exact rounding.
	rat, _ := f.Rat(nil)
	// Scale: multiply by 10^6, round, then format.
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(decimalPlaces), nil)
	num := new(big.Int).Mul(rat.Num(), scale)
	// Integer division with rounding (round half up).
	q, r := new(big.Int).DivMod(num, rat.Denom(), new(big.Int))
	// Round half up: if 2*r >= denom, increment q.
	twoR := new(big.Int).Mul(r, big.NewInt(2))
	if twoR.Abs(twoR).Cmp(rat.Denom()) >= 0 {
		if num.Sign() < 0 {
			q.Sub(q, big.NewInt(1))
		} else {
			q.Add(q, big.NewInt(1))
		}
	}

	// Format: split integer and fractional parts.
	neg := q.Sign() < 0
	if neg {
		q.Neg(q)
	}

	scaleStr := q.String()
	// Pad to at least decimalPlaces+1 digits.
	for len(scaleStr) <= decimalPlaces {
		scaleStr = "0" + scaleStr
	}

	intPart := scaleStr[:len(scaleStr)-decimalPlaces]
	fracPart := scaleStr[len(scaleStr)-decimalPlaces:]

	result := intPart + "." + fracPart
	if neg {
		result = "-" + result
	}
	return result
}
