package audit

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

var actionLabels = map[string]string{
	ActionBillCreated:   "新建",
	ActionBillUpdated:   "修改",
	ActionBillApproved:  "审核",
	ActionBillVoided:    "作废",
	ActionBillRestored:  "恢复",
	ActionBillReturned:  "退货",
	ActionStockNegative: "负库存提醒",
	// Events the audit subscriber mirrors elsewhere; kept so a row of that kind never shows English.
	"bill.rejected": "驳回",
}

var resourceLabels = map[string]string{
	ResourcePurchaseBill: "采购单",
	ResourceSaleBill:     "销售单",
}

// ActionLabel is the shop-facing Chinese name of an action; unknown ones read "其他操作".
func ActionLabel(action string) string {
	if l, ok := actionLabels[action]; ok {
		return l
	}
	return "其他操作"
}

// ResourceLabel is the Chinese name of a resource type; unknown ones read "其他".
func ResourceLabel(resource string) string {
	if l, ok := resourceLabels[resource]; ok {
		return l
	}
	return "其他"
}

var fieldLabels = map[string]string{
	"unit_price": "单价",
	"price":      "单价",
	"qty":        "数量",
	"quantity":   "数量",
	"amount":     "金额",
	"remark":     "备注",
	"bill_date":  "单据日期",
	"discount":   "折扣",
	"reason":     "原因",
}

// Summary writes one Chinese sentence out of a changes object, or "" when there
// is nothing it can say. Two shapes are understood:
//
//	{"unit_price": {"from": "12.00", "to": "10.00"}}  ->  单价 12.00 → 10.00
//	{"reason": "录错了"}                                ->  原因：录错了
//
// Unknown keys are skipped, never echoed as JSON.
func Summary(changes []byte) string {
	if len(changes) == 0 {
		return ""
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(changes, &m); err != nil {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		label, ok := fieldLabels[k]
		if !ok {
			continue
		}
		var pair struct {
			From any `json:"from"`
			To   any `json:"to"`
		}
		if err := json.Unmarshal(m[k], &pair); err == nil && (pair.From != nil || pair.To != nil) {
			parts = append(parts, fmt.Sprintf("%s %s → %s", label, scalar(pair.From), scalar(pair.To)))
			continue
		}
		var s string
		if err := json.Unmarshal(m[k], &s); err == nil && strings.TrimSpace(s) != "" {
			parts = append(parts, label+"："+strings.TrimSpace(s))
		}
	}
	return strings.Join(parts, "；")
}

func scalar(v any) string {
	switch t := v.(type) {
	case nil:
		return "空"
	case string:
		if strings.TrimSpace(t) == "" {
			return "空"
		}
		return t
	case float64:
		return fmt.Sprintf("%g", t)
	}
	return "—"
}
