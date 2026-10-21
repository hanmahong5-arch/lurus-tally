package permission

import (
	"bytes"
	"encoding/json"
)

// CostKeys are the JSON keys that expose cost, purchase price or margin. A
// person without CostView never receives them.
var CostKeys = map[string]bool{
	"unit_cost":           true,
	"total_cost":          true,
	"cost_strategy":       true,
	"purchase_price":      true,
	"last_purchase_price": true,
	"cost_of_goods":       true,
	"gross_profit":        true,
	"avg_margin":          true,
	"overall_margin":      true,
	"margin":              true,
	"est_amount_cny":      true,
	"inventory_value":     true,
	"amount_cny":          true,
	"total_value_cny":     true,
	"value_cny":           true,
}

// StripCostFields returns v as generic JSON with every CostKeys key removed at
// any depth. Use it on response bodies built from domain structs, so a field
// added later under a known cost name is covered without touching each struct.
func StripCostFields(v any) (any, error) { return StripNamed(v, CostKeys) }

// StripNamed is StripCostFields for an arbitrary key set (the purchase-bill
// amounts a clerk never sees).
func StripNamed(v any, keys map[string]bool) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var generic any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // keep numbers byte-exact; no float round trip
	if err := dec.Decode(&generic); err != nil {
		return nil, err
	}
	return stripKeys(generic, keys), nil
}

// StripGeneric removes CostKeys from already-decoded JSON (maps and slices).
func StripGeneric(v any) any { return stripKeys(v, CostKeys) }

func stripKeys(v any, keys map[string]bool) any {
	switch t := v.(type) {
	case map[string]any:
		for k := range t {
			if keys[k] {
				delete(t, k)
				continue
			}
			t[k] = stripKeys(t[k], keys)
		}
		return t
	case []any:
		for i := range t {
			t[i] = stripKeys(t[i], keys)
		}
		return t
	}
	return v
}
