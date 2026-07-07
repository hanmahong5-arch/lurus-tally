package ai

import (
	"strings"
	"testing"
)

// tool_first_policy_test.go verifies the fix for the "first-turn no tool call"
// defect: real E2E testing (2026-07-06) showed that computation/ranking-style
// Chinese questions ("帮我算A仓补货", "毛利最低的商品有哪些?") got a menu-style
// reply with zero tool calls, while lookup-style questions ("上月哪些SKU滞销?")
// correctly triggered a tool call. The fix is prompt/description-only (no
// architecture change): an explicit tool-first directive plus a Chinese
// phrase→tool mapping in systemPrompt, and Chinese trigger keywords appended
// to the relevant tool descriptions so tool selection is biased even without
// relying purely on the system prompt.
//
// These are static assertions on the constructed prompt/tool text (no LLM
// call) — they catch regressions where someone edits systemPrompt or
// ToolDefs() and silently drops the directive or a trigger phrase.

// TestSystemPrompt_HasToolFirstDirective verifies the "never ask a clarifying
// question before trying a tool" instruction is present.
func TestSystemPrompt_HasToolFirstDirective(t *testing.T) {
	if !strings.Contains(systemPrompt, "tool-first policy") {
		t.Error("systemPrompt must contain an explicit tool-first policy directive")
	}
	if !strings.Contains(systemPrompt, "MUST call it in this same turn") {
		t.Error("systemPrompt must instruct the model to call a matching tool before replying")
	}
	if !strings.Contains(systemPrompt, "Never respond with a menu of options") {
		t.Error("systemPrompt must explicitly forbid menu-style replies when a tool could answer")
	}
}

// TestSystemPrompt_HasChineseTriggerMapping verifies the Chinese phrase→tool
// mapping table covers the exact failure cases from the 2026-07-06 E2E run:
// "补货" (replenishment) and "毛利最低" (lowest margin) must resolve to a
// specific tool name rather than being left for the model to guess.
func TestSystemPrompt_HasChineseTriggerMapping(t *testing.T) {
	cases := []struct {
		trigger string
		tool    string
	}{
		{"补货", "list_low_stock"},
		{"滞销", "list_dead_stock"},
		{"毛利", "gross_margin_summary"},
		{"畅销", "recent_sales_top"},
	}
	for _, c := range cases {
		if !strings.Contains(systemPrompt, c.trigger) {
			t.Errorf("systemPrompt missing Chinese trigger phrase %q", c.trigger)
		}
		if !strings.Contains(systemPrompt, c.tool) {
			t.Errorf("systemPrompt missing mapped tool name %q for trigger %q", c.tool, c.trigger)
		}
	}
}

// TestToolDefs_DescriptionsCarryChineseTriggerKeywords verifies each
// analytics tool's description embeds the Chinese keywords a user is likely
// to phrase their question with, so the model's semantic tool-selection is
// biased toward the right tool even when the system prompt mapping alone
// isn't enough.
func TestToolDefs_DescriptionsCarryChineseTriggerKeywords(t *testing.T) {
	want := map[string][]string{
		"list_low_stock":       {"补货", "缺货"},
		"list_dead_stock":      {"滞销", "呆滞"},
		"gross_margin_summary": {"毛利", "利润率"},
		"recent_sales_top":     {"畅销", "排行"},
		"abc_classify":         {"ABC分类", "帕累托"},
	}
	tools := ToolDefs()
	descByName := make(map[string]string, len(tools))
	for _, tl := range tools {
		descByName[tl.Function.Name] = tl.Function.Description
	}
	for name, keywords := range want {
		desc, ok := descByName[name]
		if !ok {
			t.Fatalf("tool %s not found in ToolDefs()", name)
		}
		for _, kw := range keywords {
			if !strings.Contains(desc, kw) {
				t.Errorf("tool %s description missing keyword %q: %q", name, kw, desc)
			}
		}
	}
}
