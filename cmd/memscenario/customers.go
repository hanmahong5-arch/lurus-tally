package main

// Customer-memory scenarios ("熟客记忆"). Both were written and frozen before
// the first end-to-end replay; the held-out one must not be looked at while
// changing any rule. Fixture data only — expectations for the purchase probes
// are computed from these tables, never read back from the database.

type customer struct {
	name string
	code string
}

type line struct {
	product string
	qty     string
	price   string
}

type bill struct {
	no       string
	customer string // partner name; for a quick-checkout bill, the name written into remark
	quick    bool   // quick checkout: partner_id NULL, customer name in remark
	draft    bool   // status 0 — never counts as a purchase
	daysAgo  int
	lines    []line
}

// fact is one thing the shop owner tells the AI drawer about a customer.
// owner "" = not about any customer. key is a substring that identifies the
// fact in an injected prompt; stale facts are replaced later in the scenario.
type fact struct {
	owner string
	text  string
	key   string
	stale bool
}

type customerScenario struct {
	customers []customer
	products  map[string]string // name → unit
	bills     []bill
	// decoy: another tenant's customer with a colliding name and a bill that
	// must never appear.
	decoyCustomer, decoyProduct string

	session1, session2 []fact
	// session3 and chains are used by the v2 scenarios only (empty in v1).
	session3 []fact
	chains   []customerChain
	// purchaseProbes: names asked about "上次买了什么"; ambiguousProbe must
	// come back as candidates ambiguousWant.
	purchaseProbes []string
	ambiguousProbe string
	ambiguousWant  []string
	// memoryProbes: question → the customer it is about.
	memoryProbes []memoryProbe
}

type memoryProbe struct {
	q     string
	about string
}

func q(s string) fact { return fact{text: s} } // a question or chit-chat, states nothing

func devCustomers() customerScenario {
	return customerScenario{
		customers: []customer{
			{"张三", "C001"}, {"张三丰", "C002"}, {"李四", "C003"},
			{"王五", "C004"}, {"赵六", "C005"}, {"孙七", "C006"},
		},
		products: map[string]string{
			"矿泉水550ml": "箱", "可乐330ml": "箱", "薯片": "包", "花生米": "袋", "纸巾": "提",
		},
		bills: []bill{
			{no: "SL-D001", customer: "张三", daysAgo: 40, lines: []line{{"矿泉水550ml", "2", "32"}}},
			{no: "SL-D005", customer: "张三", daysAgo: 10, lines: []line{{"可乐330ml", "3", "45"}, {"纸巾", "1", "20"}}},
			{no: "SL-D009", customer: "张三", daysAgo: 2, lines: []line{{"矿泉水550ml", "5", "32"}}},
			{no: "SL-D010", customer: "张三", daysAgo: 1, draft: true, lines: []line{{"薯片", "9", "5.5"}}},
			{no: "SL-D002", customer: "张三丰", daysAgo: 30, lines: []line{{"花生米", "10", "8"}}},
			{no: "SL-D008", customer: "张三丰", daysAgo: 3, lines: []line{{"纸巾", "4", "20"}}},
			{no: "SL-D003", customer: "李四", daysAgo: 25, lines: []line{{"薯片", "6", "5.5"}}},
			{no: "QK-D006", customer: "李四", quick: true, daysAgo: 7, lines: []line{{"可乐330ml", "1", "45"}}},
			{no: "SL-D004", customer: "王五", daysAgo: 20, lines: []line{{"矿泉水550ml", "1", "32"}}},
			{no: "SL-D007", customer: "王五", daysAgo: 5, lines: []line{{"花生米", "2", "8"}, {"薯片", "3", "5.5"}}},
			{no: "SL-D011", customer: "孙七", daysAgo: 60, lines: []line{{"纸巾", "1", "20"}}},
			{no: "SL-D012", customer: "孙七", daysAgo: 50, lines: []line{{"纸巾", "2", "20"}}},
			{no: "SL-D013", customer: "孙七", daysAgo: 40, lines: []line{{"纸巾", "3", "20"}}},
			{no: "SL-D014", customer: "孙七", daysAgo: 30, lines: []line{{"纸巾", "4", "20"}}},
			{no: "SL-D015", customer: "孙七", daysAgo: 20, lines: []line{{"纸巾", "5", "20"}}},
			{no: "SL-D016", customer: "孙七", daysAgo: 10, lines: []line{{"纸巾", "6", "20"}}},
		},
		decoyCustomer: "张三", decoyProduct: "茅台",
		session1: []fact{
			{owner: "张三", text: "张三喜欢少糖，不要冰", key: "少糖"},
			{owner: "张三", text: "张三要求送货前一天电话确认", key: "电话确认"},
			{owner: "张三丰", text: "张三丰每次都要开增值税专用发票", key: "专用发票"},
			{owner: "李四", text: "李四只收现金，不走微信", key: "只收现金", stale: true},
			q("今天有哪些订单待发货？"),
			{owner: "王五", text: "王五的店在城东，送货走北门", key: "北门"},
			{owner: "赵六", text: "赵六对花生过敏，推荐商品时避开花生", key: "花生过敏"},
			{text: "周末不安排送货", key: "周末不安排送货"},
		},
		session2: []fact{
			{owner: "李四", text: "李四现在改用微信付款了", key: "微信付款"},
			{owner: "张三", text: "张三要求送货前一天电话确认", key: "电话确认"},
			{owner: "张三丰", text: "张三丰的发票抬头是张记商行", key: "张记商行"},
		},
		purchaseProbes: []string{"张三", "张三丰", "李四", "王五", "赵六", "孙七"},
		ambiguousProbe: "张",
		ambiguousWant:  []string{"张三", "张三丰"},
		memoryProbes: []memoryProbe{
			{"接待张三要注意什么？", "张三"},
			{"接待张三丰要注意什么？", "张三丰"},
			{"李四来了要注意什么？", "李四"},
			{"给王五送货要注意什么？", "王五"},
			{"赵六来买东西要注意什么？", "赵六"},
		},
	}
}

func holdoutCustomers() customerScenario {
	return customerScenario{
		customers: []customer{
			{"陈一", "C101"}, {"陈一鸣", "C102"}, {"刘二", "C103"},
			{"周八", "C104"}, {"吴九", "C105"}, {"郑十", "C106"},
		},
		products: map[string]string{
			"大米10kg": "袋", "食用油5L": "桶", "酱油": "瓶", "鸡蛋": "盒",
		},
		bills: []bill{
			{no: "SL-H001", customer: "陈一", daysAgo: 15, lines: []line{{"大米10kg", "2", "58"}}},
			{no: "SL-H006", customer: "陈一", daysAgo: 4, lines: []line{{"鸡蛋", "3", "15"}, {"酱油", "2", "9.9"}}},
			{no: "SL-H002", customer: "陈一鸣", daysAgo: 12, lines: []line{{"食用油5L", "1", "79"}}},
			{no: "QK-H003", customer: "刘二", quick: true, daysAgo: 9, lines: []line{{"大米10kg", "1", "58"}}},
			{no: "SL-H007", customer: "刘二", daysAgo: 1, lines: []line{{"酱油", "6", "9.9"}}},
			{no: "SL-H004", customer: "周八", daysAgo: 8, lines: []line{{"鸡蛋", "10", "15"}}},
			{no: "SL-H008", customer: "周八", daysAgo: 0, draft: true, lines: []line{{"大米10kg", "1", "58"}}},
			{no: "SL-H005", customer: "吴九", daysAgo: 6, lines: []line{{"食用油5L", "2", "79"}}},
		},
		decoyCustomer: "陈一", decoyProduct: "茅台",
		session1: []fact{
			{owner: "陈一", text: "陈一只要散装鸡蛋，不要盒装", key: "散装鸡蛋"},
			{owner: "陈一鸣", text: "陈一鸣的货都送到他女儿家", key: "女儿家"},
			{owner: "刘二", text: "刘二每月 25 号统一结账", key: "25 号", stale: true},
			q("哪个商品库存最少？"),
			{owner: "周八", text: "周八腿脚不方便，货要送上楼", key: "送上楼"},
			{owner: "吴九", text: "吴九只买金龙鱼牌的油", key: "金龙鱼"},
			{text: "节假日照常营业", key: "节假日照常营业"},
		},
		session2: []fact{
			{owner: "刘二", text: "刘二的结账日改到每月 5 号", key: "每月 5 号"},
			{owner: "周八", text: "周八腿脚不方便，货要送上楼", key: "送上楼"},
			{owner: "陈一鸣", text: "陈一鸣说以后都开电子发票", key: "电子发票"},
		},
		purchaseProbes: []string{"陈一", "陈一鸣", "刘二", "周八", "吴九", "郑十"},
		ambiguousProbe: "陈",
		ambiguousWant:  []string{"陈一", "陈一鸣"},
		memoryProbes: []memoryProbe{
			{"陈一来了要注意什么？", "陈一"},
			{"给陈一鸣送货要注意什么？", "陈一鸣"},
			{"刘二什么时候结账？", "刘二"},
			{"接待周八要注意什么？", "周八"},
			{"吴九来买油要注意什么？", "吴九"},
		},
	}
}
