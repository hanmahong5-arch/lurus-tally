// Package platformclient is a compatibility shim that re-exports types from
// internal/adapter/platform. New code should import the adapter package directly.
// This file exists so that callers not yet updated to the new import path continue
// to compile without modification.
package platformclient

import "github.com/hanmahong5-arch/lurus-tally/internal/adapter/platform"

// Type aliases — each maps the old exported name to the new package.
// Using type aliases (= syntax) preserves assignability: values of the old type
// and the new type are interchangeable without conversion.

type Client = platform.Client
type Config = platform.Config
type ErrCode = platform.ErrCode
type Error = platform.Error

type Account = platform.Account
type AccountOverview = platform.AccountOverview
type SubscriptionSnapshot = platform.SubscriptionSnapshot
type SubscriptionCheckoutRequest = platform.SubscriptionCheckoutRequest
type SubscriptionCheckoutResponse = platform.SubscriptionCheckoutResponse
type UpsertAccountRequest = platform.UpsertAccountRequest
type Wallet = platform.Wallet

// ErrCode constants re-exported for callers that reference platformclient.ErrCode*.
const (
	ErrCodeUnauthorized        = platform.ErrCodeUnauthorized
	ErrCodeNotFound            = platform.ErrCodeNotFound
	ErrCodeInsufficientBalance = platform.ErrCodeInsufficientBalance
	ErrCodeInvalidParameter    = platform.ErrCodeInvalidParameter
	ErrCodeUnavailable         = platform.ErrCodeUnavailable
	ErrCodeUnknown             = platform.ErrCodeUnknown
)

// New constructs a Client — thin wrapper so callers can keep using platformclient.New.
func New(cfg Config) (*Client, error) {
	return platform.New(cfg)
}

// IsCode reports whether err is a *Error with the given code.
func IsCode(err error, code ErrCode) bool {
	return platform.IsCode(err, code)
}
