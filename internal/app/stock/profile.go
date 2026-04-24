package stock

// Profile is the minimal interface this package needs from the tenant profile.
// The full profile struct lives in Story 2.1; this avoids a circular import.
type Profile interface {
	// InventoryMethod returns "fifo" or "wac" (lowercase). Empty string → treat as "wac".
	InventoryMethod() string
}
