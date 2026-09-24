package main

// End-to-end probes WITH the LLM in the loop (LLM=1): tally's real
// Orchestrator.Chat — real model via newapi, real tools on Postgres, real
// memorus recall/write-back. Frozen before the first LLM run. Runs on the dev
// customer data. Each chat is a fresh conversation (no history), so anything
// it knows about a customer beyond the books came from memory.

type llmCheck struct {
	q         string
	tool      string   // tool that must be called ("" = no requirement)
	mustHave  []string // every one must appear in the answer
	mustNot   []string // none may appear in the answer
	anyOf     []string // at least one must appear (empty = no requirement)
	rationale string
}

// Said to the AI drawer first, one turn each, as a shop owner would.
var llmStatements = []string{
	"张三喜欢少糖，不要冰，记一下",
	"李四只收现金，不走微信",
	"赵六对花生过敏，推荐商品时避开花生",
	"李四现在改用微信付款了",
	"老李说以后买饮料都要塑料袋装",
}

var llmChecks = []llmCheck{
	{q: "老李上次买了什么？", tool: "customer_recent_purchases", mustHave: []string{"可乐"},
		rationale: "alias → 李四; latest bill is the quick checkout QK-D006 (可乐×1)"},
	{q: "张三上次买了什么？", tool: "customer_recent_purchases", mustHave: []string{"矿泉水"}, mustNot: []string{"薯片"},
		rationale: "latest approved bill SL-D009 矿泉水×5; the 薯片 bill is a draft"},
	{q: "张三丰最近买过什么？", tool: "customer_recent_purchases", mustHave: []string{"纸巾"}, mustNot: []string{"矿泉水"},
		rationale: "张三丰's bills only (纸巾, 花生米) — never 张三's"},
	{q: "老张最近买了什么？", tool: "customer_recent_purchases", mustHave: []string{"张三丰"},
		rationale: "two customers surnamed 张: must ask which, naming both"},
	{q: "王五上次买了什么？", tool: "customer_recent_purchases", mustHave: []string{"花生米", "薯片"},
		rationale: "latest bill SL-D007 has both lines"},
	{q: "赵六上次买了什么？", tool: "customer_recent_purchases", mustNot: []string{"矿泉水", "可乐", "薯片", "纸巾"},
		rationale: "赵六 has no purchases; must not invent any"},
	{q: "接待李四要注意什么？", mustHave: []string{"微信", "塑料袋"}, mustNot: []string{"只收现金"},
		rationale: "current payment method + the alias-attributed fact; never the replaced value"},
	{q: "接待张三要注意什么？", mustHave: []string{"少糖"}, mustNot: []string{"过敏", "塑料袋"},
		rationale: "张三's note only, none of 赵六's / 李四's"},
	{q: "赵六来了，给他推荐点零食", anyOf: []string{"过敏", "避开花生", "不含花生"}, mustNot: []string{"推荐花生米"},
		rationale: "the allergy note must shape the recommendation"},
	{q: "孙七最近买了什么？", tool: "customer_recent_purchases", mustHave: []string{"纸巾"},
		rationale: "孙七 only buys 纸巾"},
}

const llmRepeats = 3

// v2 checks, frozen after the first LLM run showed defects the v1 substring
// checks could not see, and before any fix:
//  1. told "记一下", the assistant answered "我无法记录" — while tally had in
//     fact stored it (the prompt never said memory exists);
//  2. it waved remembered notes away ("只是历史备注，别默认按记忆走");
//  3. a note said with an address form ("老李说…") was not tied to 李四 in the
//     prompt, so the assistant doubted it applied to 李四.

// No reply to a statement may deny remembering it.
var llmStatementMustNot = []string{"无法记录", "不能记录", "无法保存", "不能保存", "没有保存", "无法写入", "没有对应的工具", "没有记录客户的"}

// No answer may tell the owner to distrust what they told the assistant.
var llmAnswerMustNot = []string{"只是历史备注", "仅是备注", "别默认按记忆", "无法核实", "别直接套用", "是否有同样要求"}
