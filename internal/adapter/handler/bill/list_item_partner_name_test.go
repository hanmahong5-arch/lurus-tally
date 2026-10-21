package bill

import (
	"encoding/json"
	"testing"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// A walk-in sale has no partner: the list row must still carry partner_name
// (empty string), because the embedded BillHead field is omitempty.
func TestNewListItem_PartnerNameKeyAlwaysPresent(t *testing.T) {
	raw, err := json.Marshal(newListItem(domain.BillHead{}))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	v, ok := m["partner_name"]
	if !ok {
		t.Fatalf("partner_name key missing: %s", raw)
	}
	if v != "" {
		t.Errorf("partner_name = %v, want empty string", v)
	}
}
