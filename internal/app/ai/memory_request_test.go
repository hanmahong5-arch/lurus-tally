package ai

import (
	"testing"

	"github.com/google/uuid"
)

// Requests and questions state no fact. Stored anyway, "赵六来了，给他推荐点零食"
// came back in later prompts as a note, and the assistant said "他之前提到是买
// 零食的" (measured in the LLM replay, cmd/memscenario LLM=1).
func TestBuildMemorySummary_SkipsRequestsKeepsFacts(t *testing.T) {
	keep := []string{
		"张三喜欢少糖，不要冰，记一下",
		"李四只收现金，不走微信",
		"赵六对花生过敏，推荐商品时避开花生",
		"老李说以后买饮料都要塑料袋装",
		"以后给他打九五折",
		"客户张三要求每次送货前一天电话确认",
		"矿泉水 550ml 的进价调整为每箱 35 元",
	}
	skip := []string{
		"赵六来了，给他推荐点零食",
		"帮我查一下张三上次买了什么",
		"张三上次买了什么",
		"库存预警阈值是多少",
		"李四还欠我钱吗",
		"看看最近哪个卖得好",
		"今天有哪些订单待发货？",
	}
	for _, s := range keep {
		if BuildMemorySummary(uuid.Nil, s, "") == "" {
			t.Errorf("fact dropped: %q", s)
		}
	}
	for _, s := range skip {
		if got := BuildMemorySummary(uuid.Nil, s, ""); got != "" {
			t.Errorf("request stored as a fact: %q", s)
		}
	}
}

func TestBuildMemorySummary_NarratedHelpIsAFact(t *testing.T) {
	if BuildMemorySummary(uuid.Nil, "周姐说她女儿下周来帮她取货", "") == "" {
		t.Fatal("帮她 inside a clause narrates; it is not a request")
	}
}
