package ai

import (
	"fmt"
	"strings"

	domainai "github.com/hanmahong5-arch/lurus-tally/internal/domain/ai"
)

// flagMark is appended to the After cell of a flagged preview sample row.
const flagMark = "  ⚠ 需核对"

// descSuffixTail terminates the item-verification count suffix on a plan description.
const descSuffixTail = " 项需要逐行核对）"

// attachDraftFlags records flags on a plan and updates its preview rows and description.
func attachDraftFlags(plan *domainai.Plan, flags []DraftFlag) {
	if plan == nil || len(flags) == 0 {
		return
	}
	existing, _ := plan.Payload["flags"].([]map[string]interface{})
	existing = append(existing, FlagsPayload(flags)...)
	plan.Payload["flags"] = existing
	plan.Payload["require_line_confirm"] = true

	for _, f := range flags {
		if f.Line >= 0 && f.Line < len(plan.Preview.SampleRows) {
			row := &plan.Preview.SampleRows[f.Line]
			if !strings.HasSuffix(row.After, flagMark) {
				row.After += flagMark
			}
		}
	}
	marked := 0
	for _, row := range plan.Preview.SampleRows {
		if strings.HasSuffix(row.After, flagMark) {
			marked++
		}
	}
	desc := plan.Preview.Description
	if i := strings.LastIndex(desc, "（"); i >= 0 && strings.HasSuffix(desc, descSuffixTail) {
		desc = desc[:i]
	}
	plan.Preview.Description = desc + fmt.Sprintf("（%d%s", max(marked, 1), descSuffixTail)
}

// checkPlanAgainstSpoken runs CheckSpokenQuantities on a proposed purchase draft plan.
func checkPlanAgainstSpoken(plan *domainai.Plan, spoken string) {
	if plan == nil || plan.Type != domainai.PlanTypeCreatePurchase {
		return
	}
	items, ok := plan.Payload["items"].([]purchaseDraftItem)
	if !ok {
		return
	}
	attachDraftFlags(plan, CheckSpokenQuantities(spoken, checkLines(items)))
}

// crossCheckExtracted compares a purchase draft plan against independent LLM extraction lines.
func crossCheckExtracted(plan *domainai.Plan, extracted []ExtractedLine, spoken string) {
	if plan == nil || plan.Type != domainai.PlanTypeCreatePurchase || len(extracted) == 0 {
		return
	}
	items, ok := plan.Payload["items"].([]purchaseDraftItem)
	if !ok {
		return
	}
	var flags []DraftFlag
	for i, ln := range checkLines(items) {
		name := strings.TrimSpace(ln.ProductName)
		if name == "" || !ln.Qty.IsPositive() || hasFlag(plan, i, FlagQtyMismatch) {
			continue
		}
		var same []ExtractedLine
		for _, e := range extracted {
			if strings.Contains(e.ProductName, name) || strings.Contains(name, e.ProductName) {
				same = append(same, e)
			}
		}
		if len(same) == 0 {
			continue
		}
		match := false
		for _, e := range same {
			if e.Qty.Equal(ln.Qty) {
				match = true
				break
			}
		}
		if !match {
			flags = append(flags, mismatch(i, ln))
		}
	}
	if total, ok := SpokenTotal(spoken); ok {
		priced := make([]CheckLine, 0, len(extracted))
		for _, e := range extracted {
			priced = append(priced, CheckLine(e))
		}
		flags = append(flags, CheckTotal(priced, total)...)
	}
	attachDraftFlags(plan, flags)
}

// hasFlag reports whether plan already carries a flag with code on line.
func hasFlag(plan *domainai.Plan, line int, code string) bool {
	fs, _ := plan.Payload["flags"].([]map[string]interface{})
	for _, f := range fs {
		if f["line"] == line && f["code"] == code {
			return true
		}
	}
	return false
}

// flagsAcknowledged reports whether all required line flags on plan have been acknowledged.
func flagsAcknowledged(plan *domainai.Plan, acknowledged []int) bool {
	if req, _ := plan.Payload["require_line_confirm"].(bool); !req {
		return true
	}
	n := 0
	switch fs := plan.Payload["flags"].(type) {
	case []map[string]interface{}:
		n = len(fs)
	case []interface{}:
		n = len(fs)
	}
	if n == 0 {
		return false
	}
	got := make(map[int]bool, len(acknowledged))
	for _, i := range acknowledged {
		got[i] = true
	}
	for i := 0; i < n; i++ {
		if !got[i] {
			return false
		}
	}
	return true
}
