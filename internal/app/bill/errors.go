package bill

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

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

	// ErrNegativeFee is returned when shipping_fee or tax_amount is negative.
	// Callers must reject the request rather than silently clamping to zero.
	ErrNegativeFee = errors.New("bill: fee must be non-negative")

	// ErrWarehouseRequired is returned on approval when the bill names no warehouse
	// and the tenant has no default warehouse to fall back to.
	ErrWarehouseRequired = errors.New("bill: warehouse required")

	// ErrPaidExceedsTotal is returned when the paid amount on approval is larger than
	// the bill total. Change must be handled by the caller; it is never booked.
	ErrPaidExceedsTotal = errors.New("bill: paid amount exceeds bill total")

	// ErrBillAlreadyApproved is returned when an already-approved bill is approved
	// again with a positive paid amount: the approval is idempotent but the payment
	// would be silently dropped, so the caller is told instead.
	ErrBillAlreadyApproved = errors.New("bill: already approved, paid amount not recorded")

	// ErrInvalidPayMethod is returned when the payment method is not one the system
	// accepts. An unknown value is rejected, never silently booked as cash.
	ErrInvalidPayMethod = errors.New("bill: unsupported payment method")

	// ErrZeroPriceLines is returned when a draft purchase has lines with a zero unit
	// price and the caller did not confirm they are gifts. A forgotten price would
	// otherwise book stock at zero cost and understate the inventory value.
	ErrZeroPriceLines = errors.New("bill: purchase has zero-price lines")
)

// ZeroPriceLine identifies one zero-priced line of a purchase draft.
type ZeroPriceLine struct {
	LineNo    int
	ProductID uuid.UUID
}

// ZeroPriceLinesError carries the offending lines; it matches ErrZeroPriceLines.
type ZeroPriceLinesError struct {
	Lines []ZeroPriceLine
}

func (e *ZeroPriceLinesError) Error() string {
	return fmt.Sprintf("%s: %d line(s)", ErrZeroPriceLines.Error(), len(e.Lines))
}

// Is lets errors.Is(err, ErrZeroPriceLines) match.
func (e *ZeroPriceLinesError) Is(target error) bool { return target == ErrZeroPriceLines }
