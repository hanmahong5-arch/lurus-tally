package customer_test

import (
	"testing"

	"github.com/hanmahong5-arch/lurus-tally/internal/domain/customer"
)

func TestCustomer_Validate_RejectsEmptyName(t *testing.T) {
	c := &customer.Customer{}
	err := c.Validate()
	if err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
	if err.Error() != "name is required" {
		t.Errorf("expected 'name is required', got %q", err.Error())
	}
}

func TestCustomer_Validate_RejectsBlankName(t *testing.T) {
	c := &customer.Customer{Name: "   "}
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for whitespace-only name, got nil")
	}
}

func TestCustomer_Validate_AcceptsValidName(t *testing.T) {
	c := &customer.Customer{Name: "杭州客户甲"}
	if err := c.Validate(); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestCustomerPartnerTypes_CoverCustomerBothMember(t *testing.T) {
	want := map[string]bool{"customer": true, "both": true, "member": true}
	if len(customer.ListPartnerTypes) != len(want) {
		t.Fatalf("ListPartnerTypes = %v, want exactly %v", customer.ListPartnerTypes, want)
	}
	for _, pt := range customer.ListPartnerTypes {
		if !want[pt] {
			t.Errorf("unexpected partner type %q in ListPartnerTypes", pt)
		}
	}
	if customer.PartnerType != "customer" {
		t.Errorf("PartnerType = %q, want customer", customer.PartnerType)
	}
}
