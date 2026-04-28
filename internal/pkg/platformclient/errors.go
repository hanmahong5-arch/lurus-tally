// Package platformclient is the HTTP client used by Tally to call lurus-platform's
// internal billing/identity API. It mirrors the typed-error contract that platform
// exposes (HTTP status + error_code body) so callers can branch cleanly.
package platformclient

import (
	"errors"
	"fmt"
)

// ErrCode is a stable string returned by the platform for service-to-service errors.
// We keep this list short and only include codes Tally actually reacts to.
type ErrCode string

const (
	// ErrCodeUnauthorized — bearer key rejected by platform.
	ErrCodeUnauthorized ErrCode = "unauthorized"
	// ErrCodeNotFound — account / plan / order not present.
	ErrCodeNotFound ErrCode = "not_found"
	// ErrCodeInsufficientBalance — wallet payment refused (HTTP 402).
	ErrCodeInsufficientBalance ErrCode = "insufficient_balance"
	// ErrCodeInvalidParameter — payment provider/method rejected the request.
	ErrCodeInvalidParameter ErrCode = "invalid_parameter"
	// ErrCodeUnavailable — platform unreachable / 5xx / timeout.
	ErrCodeUnavailable ErrCode = "unavailable"
	// ErrCodeUnknown — fallback when status/body do not map to a known code.
	ErrCodeUnknown ErrCode = "unknown"
)

// Error is the typed error returned by every platformclient call.
// HTTPStatus is the raw status (or 0 if the request never completed).
// Body holds the trimmed response body for diagnostics.
type Error struct {
	Code       ErrCode
	HTTPStatus int
	Message    string
	Body       string
}

// Error implements the error interface with a one-line, log-safe summary.
func (e *Error) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("platform: %s (%s, http %d)", e.Message, e.Code, e.HTTPStatus)
	}
	return fmt.Sprintf("platform: %s (http %d)", e.Code, e.HTTPStatus)
}

// IsCode reports whether err is a *Error with the given code.
func IsCode(err error, code ErrCode) bool {
	var pe *Error
	if !errors.As(err, &pe) {
		return false
	}
	return pe.Code == code
}
