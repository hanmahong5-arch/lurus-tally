package main

// How shop owners actually name customers: 老张 / 小王 / 王老板 / 李总 / 周姐,
// not the full name on the customer record (PRD US-9.4's own example is
// "老张上次来买了什么"). Written and frozen before alias resolution was
// implemented. Runs on top of the base scenarios' data (ALIASES=1).

type aliasProbe struct {
	alias string
	want  string   // the one customer it must resolve to ("" = none)
	cands []string // or: the candidates it must list (ambiguous)
}

type aliasScenario struct {
	purchases []aliasProbe
	// Statements that name a customer only by alias, written after the base
	// sessions; each must be attributed to `about`.
	session3 []fact
	memory   []memoryProbe
}

func devAliases() aliasScenario {
	return aliasScenario{
		purchases: []aliasProbe{
			{alias: "老李", want: "李四"},
			{alias: "王老板", want: "王五"},
			{alias: "小赵", want: "赵六"},
			{alias: "孙总", want: "孙七"},
			{alias: "老张", cands: []string{"张三", "张三丰"}},
			{alias: "老刘"},
		},
		session3: []fact{
			{owner: "李四", text: "老李说以后买饮料都要塑料袋装", key: "塑料袋装"},
			{owner: "赵六", text: "小赵下次要订两箱无糖可乐", key: "无糖可乐"},
		},
		memory: []memoryProbe{
			{"老李来了要注意什么？", "李四"},
			{"小赵来了要注意什么？", "赵六"},
			{"王老板那边送货要注意什么？", "王五"},
		},
	}
}

func holdoutAliases() aliasScenario {
	return aliasScenario{
		purchases: []aliasProbe{
			{alias: "老刘", want: "刘二"},
			{alias: "周姐", want: "周八"},
			{alias: "吴老板", want: "吴九"},
			{alias: "郑总", want: "郑十"},
			{alias: "老陈", cands: []string{"陈一", "陈一鸣"}},
			{alias: "老王"},
		},
		session3: []fact{
			{owner: "周八", text: "周姐说她女儿下周来帮她取货", key: "女儿下周"},
			{owner: "吴九", text: "吴老板要的油以后都放后门", key: "放后门"},
		},
		memory: []memoryProbe{
			{"周姐来了要注意什么？", "周八"},
			{"老刘什么时候结账？", "刘二"},
			{"吴老板来买油要注意什么？", "吴九"},
		},
	}
}
