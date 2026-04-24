package bill

import "errors"

// Sentinel errors returned by bill use cases.
var (
	// ErrBillNotFound is returned when the requested bill does not exist.
	ErrBillNotFound = errors.New("bill: not found")

	// ErrInvalidBillStatus is returned when trying to approve/cancel in the wrong state.
	ErrInvalidBillStatus = errors.New("bill: invalid status for operation")

	// ErrCannotCancelApproved is returned when cancelling an already-approved bill.
	ErrCannotCancelApproved = errors.New("bill: cannot cancel an approved bill")

	// ErrBillApprovalConflict is returned when concurrent approval is detected.
	ErrBillApprovalConflict = errors.New("bill: concurrent approval conflict")

	// ErrValidation is returned for input validation failures.
	ErrValidation = errors.New("bill: validation error")
)
