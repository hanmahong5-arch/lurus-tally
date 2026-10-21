package payment

import (
	"errors"
	"testing"
)

func TestResolvePayType_BankAccepted(t *testing.T) {
	pt, err := resolvePayType("bank")
	if err != nil || string(pt) != "bank" {
		t.Fatalf("resolvePayType(bank) = %q, %v", pt, err)
	}
	if _, err := resolvePayType("bitcoin"); !errors.Is(err, ErrInvalidPayType) {
		t.Fatalf("unknown method err = %v", err)
	}
}
