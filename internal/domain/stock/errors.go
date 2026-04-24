package stock

import "errors"

// Sentinel errors for the stock domain.
var (
	// ErrInvalidDirection is returned when a Direction string is not one of in/out/adjust.
	ErrInvalidDirection = errors.New("stock: direction must be 'in', 'out', or 'adjust'")

	// ErrInsufficientStock is returned when an out/adjust movement would push on_hand_qty below zero.
	ErrInsufficientStock = errors.New("stock: insufficient stock")
)
