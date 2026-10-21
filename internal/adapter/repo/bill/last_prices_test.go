package bill

import (
	"strings"
	"testing"
)

func TestLastPricesQuery(t *testing.T) {
	q := lastPricesQuery()
	for _, frag := range []string{
		"DISTINCT ON (i.product_id)",
		"h.status = 2",
		"h.partner_id = $2",
		"h.sub_type = $3",
		"i.product_id = ANY($4::uuid[])",
		"h.deleted_at IS NULL",
		"i.deleted_at IS NULL",
		"i.unit_price IS NOT NULL",
		"COALESCE(h.currency, 'CNY') = 'CNY'",
		"tally.product_unit pu",
		"pu.conversion_factor",
		"ORDER BY i.product_id, h.bill_date DESC, h.created_at DESC",
	} {
		if !strings.Contains(q, frag) {
			t.Errorf("missing %q", frag)
		}
	}
}

func TestBaseUnitNamesQuery(t *testing.T) {
	q := baseUnitNamesQuery()
	for _, frag := range []string{"tally.unit_def ud", "ud.id = p.default_unit_id", "p.tenant_id = $1", "p.id = ANY($2::uuid[])"} {
		if !strings.Contains(q, frag) {
			t.Errorf("missing %q", frag)
		}
	}
}
