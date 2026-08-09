package platform

import (
	"context"
	"errors"
	"testing"
)

// TestNilClient_UpsertAccount_DegradesNotPanics is the regression guard for the
// onboarding 500: a nil *Client is the "platform integration disabled" sentinel
// (PLATFORM_INTERNAL_KEY unset). When it is passed as an interface, the caller's
// `== nil` guard cannot see the typed nil, so UpsertAccount is invoked on a nil
// receiver. It must return an ErrCodeUnavailable error (documented degrade path)
// rather than panic — a panic here surfaces as a 500 on every new-tenant
// registration.
func TestNilClient_UpsertAccount_DegradesNotPanics(t *testing.T) {
	var c *Client // typed nil — the disabled-integration sentinel

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil client panicked instead of degrading: %v", r)
		}
	}()

	_, err := c.UpsertAccount(context.Background(), UpsertAccountRequest{
		IDPSubject:  "dev-user-1",
		Email:       "dev-user-1@dev.local",
		DisplayName: "Dev User",
	})
	if err == nil {
		t.Fatal("expected an error from nil client, got nil")
	}
	var pe *Error
	if !errors.As(err, &pe) || pe.Code != ErrCodeUnavailable {
		t.Fatalf("expected *Error{Code: unavailable}, got %#v", err)
	}
}

// TestNilClient_Do_NoPanic covers the shared funnel directly: every *Client
// method routes through do → doWithIdem, so the nil-receiver guard there
// protects all of them (GetAccountByIDPSubject, subscription checkout, etc.).
func TestNilClient_Do_NoPanic(t *testing.T) {
	var c *Client

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil client do() panicked: %v", r)
		}
	}()

	err := c.do(context.Background(), "GET", "/internal/v1/anything", nil, nil)
	if !IsCode(err, ErrCodeUnavailable) {
		t.Fatalf("expected ErrCodeUnavailable, got %v", err)
	}
}
