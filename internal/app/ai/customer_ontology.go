package ai

// The retail regular-customer ontology: which attributes of a customer the
// shop owner tells the assistant about, and whether an attribute holds one
// current value (a new one replaces the old) or accumulates.
//
// A free-text note gives memorus nothing to tell "店在城东" and "搬到城南"
// apart from two unrelated facts — they share no word. Naming the attribute
// lets memorus replace the old value of a single-valued slot deterministically
// (memorus metadata `slot` + `slot_single`). The list lives here, not in
// memorus: the engine only knows "same subject, same single-valued slot".
//
// Frozen for the 2026-09-24 evaluation (cmd/memscenario SLOTS=1). Known limit:
// a changed drink (冰美式 → 热拿铁) is `taste`, which is multi-valued, so it is
// not replaced automatically.

// CustomerSlot is one attribute of a customer.
type CustomerSlot struct {
	ID     string
	Label  string
	Single bool
}

// customerSlots in the order the tool schema lists them.
var customerSlots = []CustomerSlot{
	{"payment_method", "付款方式", true},
	{"settlement", "结账方式", true},
	{"delivery_address", "送货地址", true},
	{"delivery_time", "送货时间", true},
	{"delivery_method", "配送方式", true},
	{"packaging", "包装要求", true},
	{"contact", "对接人", true},
	{"invoice", "发票要求", true},
	{"price_terms", "价格约定", true},
	{"allergy", "过敏忌口", false},
	{"taste", "口味偏好", false},
	{"regular_items", "常购商品", false},
	{"other", "其他", false},
}

// Metadata keys of a slot-tagged memory. slot and slot_single are the memorus
// contract; slot_value is tally's own (the value as the model normalised it).
const (
	MemorySlotKey       = "slot"
	MemorySlotSingleKey = "slot_single"
	MemorySlotValueKey  = "slot_value"
)

// customerSlot returns the slot with id, if it is one.
func customerSlot(id string) (CustomerSlot, bool) {
	for _, s := range customerSlots {
		if s.ID == id {
			return s, true
		}
	}
	return CustomerSlot{}, false
}

func customerSlotIDs() []string {
	ids := make([]string, len(customerSlots))
	for i, s := range customerSlots {
		ids[i] = s.ID
	}
	return ids
}

// customerSlotGuide is the attribute list as the tool description shows it.
func customerSlotGuide() string {
	var b []byte
	for _, s := range customerSlots {
		b = append(b, "\n- "+s.ID+" ("+s.Label+")"...)
	}
	return string(b)
}
