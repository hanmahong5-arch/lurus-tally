package main

// 熟客记忆 v2 (SCENARIO=v2-dev / SCENARIO=v2-holdout).
//
// FROZEN. Dev and held-out were written together, before any v2 replay, and
// without reading the memory engine's rules. The held-out set is quoted, never
// tuned on: a rule change is judged on v2-dev, then v2-holdout is run once and
// its numbers reported as they come out.
//
// Bigger and harder than v1: 22 customers per set with confusable names
// (name-prefix pairs 王海/王海涛, shared surnames 陈静/陈立, forms of address
// 老黄/高老板/马姐), statements spread over three sessions, attributes that
// change twice (A → B → C, with and without a change word), statements whose
// attribute is unclear, shop rules about no customer, chit-chat, and facts
// about one customer that name another one in passing.
//
// One source per set (v2fact carries the attribute id too); the customer
// replay and the LLM slot replay are both projections of it. Change chains are
// derived from it: all facts of one customer and one attribute, in order, when
// any of them is stale. build() panics on an inconsistent scenario (a key that
// also occurs in another fact's text would make the substring scoring lie).

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/app/ai"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/memorusclient"
)

// v2fact is one owner turn. slot is the remember_customer_fact attribute
// ("a|b" = the attribute is genuinely unclear, either is right); "" for shop
// rules and chit-chat.
type v2fact struct {
	fact
	slot string
}

func cur(owner, slot, text, key string) v2fact {
	return v2fact{fact{owner: owner, text: text, key: key}, slot}
}

func old(owner, slot, text, key string) v2fact {
	return v2fact{fact{owner: owner, text: text, key: key, stale: true}, slot}
}

func rule(text, key string) v2fact { return v2fact{fact: fact{text: text, key: key}} }

func chat(text string) v2fact { return v2fact{fact: q(text)} }

// customerChain: one customer's values of one attribute, oldest → newest; only
// the last is current.
type customerChain struct {
	owner, slot string
	keys        []string
}

type v2scenario struct {
	base     customerScenario // everything but the sessions
	sessions [3][]v2fact
}

func devCustomersV2() customerScenario     { return devV2().customers() }
func holdoutCustomersV2() customerScenario { return holdoutV2().customers() }
func devSlotsV2() slotScenario             { return devV2().slots() }
func holdoutSlotsV2() slotScenario         { return holdoutV2().slots() }

func devV2() v2scenario {
	return v2scenario{
		base: customerScenario{
			customers: []customer{
				{"王海", "C201"}, {"王海涛", "C202"}, {"李红", "C203"}, {"李红梅", "C204"},
				{"刘军", "C205"}, {"刘军平", "C206"}, {"陈静", "C207"}, {"陈立", "C208"},
				{"张桂兰", "C209"}, {"张桂芬", "C210"}, {"黄志明", "C211"}, {"马秀英", "C212"},
				{"高建", "C213"}, {"林小燕", "C214"}, {"赵磊", "C215"}, {"周敏", "C216"},
				{"吴刚", "C217"}, {"徐丽", "C218"}, {"孙浩", "C219"}, {"朱勇", "C220"},
				{"胡杰", "C221"}, {"郭芳", "C222"},
			},
			products: map[string]string{
				"矿泉水550ml": "箱", "可乐330ml": "箱", "啤酒500ml": "箱", "方便面": "箱", "食用油5L": "桶",
				"酱油": "瓶", "鸡蛋": "盒", "纸巾": "提", "洗衣粉": "袋",
			},
			bills: []bill{
				{no: "SL-V2D001", customer: "王海", daysAgo: 30, lines: []line{{"啤酒500ml", "5", "40"}}},
				{no: "SL-V2D002", customer: "王海", daysAgo: 3, lines: []line{{"矿泉水550ml", "2", "32"}}},
				{no: "SL-V2D003", customer: "王海涛", daysAgo: 12, lines: []line{{"可乐330ml", "3", "45"}}},
				{no: "SL-V2D004", customer: "王海涛", daysAgo: 1, draft: true, lines: []line{{"啤酒500ml", "2", "40"}}},
				{no: "QK-V2D034", customer: "王海涛", quick: true, daysAgo: 5, lines: []line{{"可乐330ml", "1", "45"}}},
				{no: "SL-V2D005", customer: "李红", daysAgo: 20, lines: []line{{"纸巾", "2", "20"}}},
				{no: "QK-V2D006", customer: "李红", quick: true, daysAgo: 6, lines: []line{{"纸巾", "2", "20"}}},
				{no: "SL-V2D007", customer: "李红梅", daysAgo: 9, lines: []line{{"鸡蛋", "4", "15"}, {"酱油", "2", "9.9"}}},
				{no: "SL-V2D008", customer: "刘军", daysAgo: 15, lines: []line{{"洗衣粉", "3", "18"}}},
				{no: "SL-V2D009", customer: "刘军平", daysAgo: 2, lines: []line{{"啤酒500ml", "10", "40"}}},
				{no: "SL-V2D010", customer: "陈静", daysAgo: 7, lines: []line{{"食用油5L", "1", "79"}}},
				{no: "SL-V2D011", customer: "陈立", daysAgo: 11, lines: []line{{"方便面", "2", "36"}}},
				{no: "SL-V2D012", customer: "张桂兰", daysAgo: 5, lines: []line{{"鸡蛋", "6", "15"}}},
				{no: "SL-V2D013", customer: "张桂芬", daysAgo: 8, lines: []line{{"酱油", "3", "9.9"}}},
				{no: "SL-V2D014", customer: "张桂芬", daysAgo: 4, lines: []line{{"鸡蛋", "1", "15"}}},
				{no: "SL-V2D030", customer: "黄志明", daysAgo: 40, lines: []line{{"矿泉水550ml", "6", "32"}}},
				{no: "SL-V2D031", customer: "黄志明", daysAgo: 35, lines: []line{{"矿泉水550ml", "7", "32"}}},
				{no: "SL-V2D032", customer: "黄志明", daysAgo: 32, lines: []line{{"可乐330ml", "1", "45"}}},
				{no: "SL-V2D033", customer: "黄志明", daysAgo: 28, lines: []line{{"矿泉水550ml", "9", "32"}}},
				{no: "SL-V2D015", customer: "黄志明", daysAgo: 25, lines: []line{{"矿泉水550ml", "10", "32"}}},
				{no: "SL-V2D016", customer: "黄志明", daysAgo: 13, lines: []line{{"矿泉水550ml", "8", "32"}}},
				{no: "SL-V2D017", customer: "黄志明", daysAgo: 6, lines: []line{{"可乐330ml", "2", "45"}}},
				{no: "SL-V2D018", customer: "高建", daysAgo: 10, lines: []line{{"啤酒500ml", "20", "40"}}},
				{no: "SL-V2D019", customer: "高建", daysAgo: 2, lines: []line{{"啤酒500ml", "15", "38"}}},
				{no: "SL-V2D020", customer: "林小燕", daysAgo: 14, lines: []line{{"可乐330ml", "2", "45"}}},
				{no: "SL-V2D021", customer: "赵磊", daysAgo: 18, lines: []line{{"方便面", "4", "36"}, {"矿泉水550ml", "2", "32"}}},
				{no: "SL-V2D022", customer: "周敏", daysAgo: 16, lines: []line{{"纸巾", "5", "20"}}},
				{no: "SL-V2D023", customer: "吴刚", daysAgo: 22, lines: []line{{"食用油5L", "2", "79"}}},
				{no: "QK-V2D024", customer: "徐丽", quick: true, daysAgo: 9, lines: []line{{"可乐330ml", "1", "45"}}},
				{no: "SL-V2D025", customer: "孙浩", daysAgo: 3, lines: []line{{"方便面", "5", "36"}}},
				{no: "SL-V2D026", customer: "朱勇", daysAgo: 17, lines: []line{{"洗衣粉", "3", "18"}}},
				{no: "SL-V2D027", customer: "胡杰", daysAgo: 5, lines: []line{{"酱油", "10", "9.9"}}},
				{no: "SL-V2D028", customer: "胡杰", daysAgo: 0, draft: true, lines: []line{{"酱油", "5", "9.9"}}},
				{no: "SL-V2D029", customer: "郭芳", daysAgo: 7, lines: []line{{"矿泉水550ml", "10", "32"}}},
			},
			decoyCustomer: "王海", decoyProduct: "茅台",
			purchaseProbes: []string{
				"王海", "王海涛", "李红", "李红梅", "刘军", "刘军平", "陈静", "陈立", "张桂兰", "张桂芬", "黄志明",
				"马秀英", "高建", "林小燕", "赵磊", "周敏", "吴刚", "徐丽", "孙浩", "朱勇", "胡杰", "郭芳",
			},
			ambiguousProbe: "王",
			ambiguousWant:  []string{"王海", "王海涛"},
			memoryProbes: []memoryProbe{
				{"接待赵磊要注意什么？", "赵磊"},
				{"王海来了要注意什么？", "王海"},
				{"给王海涛送货要注意什么？", "王海涛"},
				{"李红来了要注意什么？", "李红"},
				{"给李红梅送货要注意什么？", "李红梅"},
				{"刘军的账怎么结？", "刘军"},
				{"接待刘军平要注意什么？", "刘军平"},
				{"给陈静送货找谁收？", "陈静"},
				{"陈立来了要注意什么？", "陈立"},
				{"张桂兰来了要注意什么？", "张桂兰"},
				{"张桂芬喜欢什么口味？", "张桂芬"},
				{"老黄的发票怎么开？", "黄志明"},
				{"马姐那边送货要注意什么？", "马秀英"},
				{"高老板的啤酒现在多少钱一箱？", "高建"},
				{"给林姐送货要注意什么？", "林小燕"},
				{"周敏的货怎么装？", "周敏"},
				{"徐丽的店现在在哪？", "徐丽"},
				{"接待孙浩要注意什么？", "孙浩"},
				{"朱勇什么时候送货？", "朱勇"},
				{"胡杰来了要注意什么？", "胡杰"},
				{"郭芳来了要注意什么？", "郭芳"},
				{"吴刚的发票怎么处理？", "吴刚"},
			},
		},
		sessions: [3][]v2fact{
			{ // session 1
				old("赵磊", "delivery_address", "赵磊的店在城东农贸市场", "城东农贸市场"),
				old("王海", "payment_method", "王海一直付现金", "付现金"),
				old("李红梅", "delivery_time", "李红梅要求每周二上午送货", "周二上午"),
				old("刘军", "settlement", "刘军每个月15号结账", "15号结账"),
				old("陈静", "contact", "陈静那边收货找她老公", "她老公"),
				old("黄志明", "invoice", "黄志明开普通发票就行", "普通发票"),
				old("周敏", "packaging", "周敏的货用纸箱装", "纸箱装"),
				old("王海涛", "delivery_method", "王海涛的货我们开车送过去", "开车送过去"),
				old("高建", "price_terms", "高建的啤酒按每箱40算", "每箱40"),
				old("徐丽", "delivery_address", "徐丽的店在人民路88号", "人民路88号"),
				cur("林小燕", "delivery_address", "林小燕的店在幸福街", "幸福街"),
				cur("孙浩", "payment_method", "孙浩付款用银行转账", "银行转账"),
				cur("朱勇", "delivery_time", "朱勇要求每周一送货", "每周一送货"),
				cur("胡杰", "packaging", "胡杰的货要用泡沫箱", "泡沫箱"),
				cur("郭芳", "regular_items", "郭芳每周固定要十箱矿泉水", "十箱矿泉水"),
				cur("吴刚", "invoice", "吴刚发票开普票", "开普票"),
				cur("马秀英", "delivery_address", "马秀英住在锦绣小区", "锦绣小区"),
				chat("今天生意怎么样？"),
				rule("店里周日下午盘点，不接单", "周日下午盘点"),
				cur("王海", "taste", "王海不喝碳酸饮料", "碳酸饮料"),
				cur("王海涛", "contact", "王海涛店里是他爸收货", "他爸收货"),
				cur("李红", "regular_items", "李红每次都要两提纸巾", "两提纸巾"),
				cur("李红梅", "allergy", "李红梅对芒果过敏", "芒果过敏"),
				cur("刘军平", "delivery_time", "刘军平只在晚上来拿货", "晚上来拿货"),
				cur("陈立", "payment_method", "陈立用支付宝付款", "支付宝付款"),
				cur("张桂芬", "taste", "张桂芬喜欢吃辣", "吃辣"),
				chat("帮我看看这周卖了多少箱啤酒"),
				rule("满五百才包送货", "满五百"),
				cur("赵磊", "contact", "赵磊那边对接的是他老婆", "他老婆"),
				cur("孙浩", "regular_items", "孙浩每次都要五箱方便面", "五箱方便面"),
				cur("吴刚", "settlement", "吴刚每月25号对账", "25号对账"),
				cur("周敏", "allergy", "周敏对花生过敏", "花生过敏"),
				cur("胡杰", "price_terms", "胡杰是朱勇介绍来的，胡杰的酱油给九五折", "九五折"),
				chat("明天天气好像不太好"),
				cur("张桂兰", "regular_items", "张桂兰只要散装鸡蛋，跟她妹妹张桂芬不一样", "散装鸡蛋"),
				cur("高建", "contact", "高建店里有事找他儿子", "找他儿子"),
				cur("黄志明", "delivery_time", "老黄只要上午送货", "只要上午送货"),
				cur("徐丽", "payment_method", "徐丽用对公账户付款", "对公账户"),
				rule("下雨天送货会晚一点", "下雨天"),
				cur("郭芳", "contact", "郭芳说收货只找她本人", "只找她本人"),
			},
			{ // session 2
				old("赵磊", "delivery_address", "赵磊搬到城南了，新店在客运站对面", "客运站对面"),
				old("王海", "payment_method", "王海改用微信转账了", "微信转账"),
				old("李红梅", "delivery_time", "李红梅的送货换到周四下午", "周四下午"),
				old("刘军", "settlement", "刘军的账改成月底结", "月底结"),
				old("陈静", "contact", "陈静店里现在是她侄女收货", "侄女收货"),
				old("黄志明", "invoice", "老黄说以后要开增值税专票", "增值税专票"),
				old("周敏", "packaging", "周敏要求改用编织袋", "编织袋"),
				old("王海涛", "delivery_method", "王海涛以后自己来店里提货", "自己来店里提货"),
				old("高建", "price_terms", "高老板的啤酒价格调到每箱42", "每箱42"),
				old("徐丽", "delivery_address", "徐丽的店在解放路", "解放路"),
				cur("林小燕", "delivery_method|delivery_address", "林小燕的货送到她店门口就行", "店门口就行"),
				cur("孙浩", "settlement|payment_method", "孙浩的钱放月底一起算", "月底一起算"),
				cur("朱勇", "delivery_time|other", "朱勇那边别太早送，他店十点才开门", "十点才开门"),
				chat("哪个客户欠款最多？"),
				rule("谁来都一样，赊账不超过两千，王海也不例外", "不超过两千"),
				cur("李红", "delivery_method", "李红和李红梅不是一家，李红的货单独送", "单独送"),
				cur("陈立", "other", "陈立的货可以跟陈静的一起送，但陈立要单独开单", "单独开单"),
				cur("刘军平", "taste", "刘军平跟刘军不是一个人，刘军平只要冰镇啤酒", "冰镇啤酒"),
				cur("李红", "regular_items", "李红每次都要两提纸巾", "两提纸巾"),
				cur("周敏", "allergy", "周敏对花生过敏", "花生过敏"),
				cur("李红梅", "allergy", "李红梅也对菠萝过敏", "菠萝过敏"),
				cur("张桂芬", "taste", "张桂芬也爱吃酸的", "爱吃酸的"),
				chat("库存里还有多少方便面？"),
				rule("以后啤酒一律先收钱再发货", "先收钱再发货"),
				cur("吴刚", "invoice|delivery_method", "吴刚的发票跟货一起送过去", "发票跟货"),
				cur("郭芳", "price_terms|regular_items", "郭芳要的水按老价格走", "老价格"),
				cur("胡杰", "packaging|other", "胡杰说箱子千万别压", "千万别压"),
				cur("马秀英", "delivery_time|contact", "马姐家下午四点以后才有人", "四点以后才有人"),
				cur("高建", "contact", "高建店里有事找他儿子", "找他儿子"),
				chat("嗯，知道了"),
				cur("张桂兰", "contact", "张桂兰的货让她女婿来拿", "她女婿"),
				cur("孙浩", "taste", "孙浩不要辣味的方便面", "辣味"),
				cur("陈静", "regular_items", "陈静每次要一桶食用油", "一桶食用油"),
				rule("退货要在七天之内", "七天之内"),
			},
			{ // session 3
				cur("赵磊", "delivery_address", "赵磊的店在开发区管委会旁边", "管委会旁边"),
				cur("王海", "payment_method", "王海现在都走支付宝扫码", "支付宝扫码"),
				cur("李红梅", "delivery_time", "李红梅那边周五傍晚送", "周五傍晚"),
				cur("刘军", "settlement", "刘军又改了，以后每半个月结一次", "半个月结一次"),
				cur("陈静", "contact", "陈静那边换了店员阿芳负责收货", "店员阿芳"),
				cur("黄志明", "invoice", "黄志明说现在不要发票了", "不要发票"),
				cur("周敏", "packaging", "周敏的东西装塑料筐", "塑料筐"),
				cur("王海涛", "delivery_method", "王海涛那边改走物流发货", "物流发货"),
				cur("高建", "price_terms", "高建啤酒每箱38", "每箱38"),
				cur("徐丽", "delivery_address", "徐丽又搬了，现在在火车站北广场", "北广场"),
				chat("好的"),
				rule("春节前后十天不送货", "春节前后"),
				cur("王海涛", "contact", "王海涛店里是他爸收货", "他爸收货"),
				cur("朱勇", "regular_items", "朱勇每次要三袋洗衣粉", "三袋洗衣粉"),
				cur("郭芳", "regular_items", "郭芳上次帮徐丽带过货，郭芳要的酱油是生抽", "生抽"),
				chat("最近进货价是不是涨了？"),
				cur("吴刚", "delivery_time", "吴刚要中午十二点前送到", "十二点前"),
				cur("刘军平", "payment_method", "刘军平付款用现金", "付款用现金"),
				cur("马秀英", "taste", "马姐爱喝无糖茶", "无糖茶"),
				cur("高建", "other", "高老板的店周三休息", "周三休息"),
				cur("徐丽", "contact", "徐丽那边收货找店长", "找店长"),
				cur("胡杰", "payment_method", "胡杰用微信付", "用微信付"),
				chat("刚才说到哪了"),
				cur("孙浩", "payment_method", "孙浩付款用银行转账", "银行转账"),
				cur("林小燕", "regular_items", "林小燕每周要两箱可乐", "两箱可乐"),
				cur("陈立", "delivery_time", "陈立要周末送货", "周末送货"),
				cur("赵磊", "contact", "赵磊那边对接的是他老婆", "他老婆"),
				cur("张桂芬", "contact", "张桂芬收货找她儿子", "找她儿子"),
				chat("王海上次来是什么时候？"),
				cur("林小燕", "taste", "林姐爱喝乌龙茶", "乌龙茶"),
			},
		},
	}
}

func holdoutV2() v2scenario {
	return v2scenario{
		base: customerScenario{
			customers: []customer{
				{"杨光", "C301"}, {"杨光明", "C302"}, {"周建", "C303"}, {"周建华", "C304"},
				{"何丽", "C305"}, {"何丽娟", "C306"}, {"郑伟", "C307"}, {"郑斌", "C308"},
				{"冯玉珍", "C309"}, {"冯玉兰", "C310"}, {"蒋国庆", "C311"}, {"韩春梅", "C312"},
				{"唐勇", "C313"}, {"曹淑芬", "C314"}, {"彭飞", "C315"}, {"董磊", "C316"},
				{"袁芳", "C317"}, {"邓涛", "C318"}, {"许静", "C319"}, {"傅军", "C320"},
				{"沈红", "C321"}, {"曾平", "C322"},
			},
			products: map[string]string{
				"大米10kg": "袋", "面粉5kg": "袋", "花生油5L": "桶", "白糖1kg": "包", "挂面": "把",
				"陈醋": "瓶", "卫生纸": "提", "洗洁精": "瓶", "白酒": "瓶",
			},
			bills: []bill{
				{no: "SL-V2H001", customer: "杨光", daysAgo: 20, lines: []line{{"大米10kg", "2", "55"}}},
				{no: "SL-V2H002", customer: "杨光", daysAgo: 4, lines: []line{{"白糖1kg", "3", "8.5"}}},
				{no: "SL-V2H003", customer: "杨光明", daysAgo: 9, lines: []line{{"花生油5L", "1", "89"}}},
				{no: "SL-V2H004", customer: "周建", daysAgo: 14, lines: []line{{"面粉5kg", "5", "26"}}},
				{no: "SL-V2H005", customer: "周建", daysAgo: 2, lines: []line{{"面粉5kg", "5", "26"}}},
				{no: "SL-V2H006", customer: "周建华", daysAgo: 11, lines: []line{{"白糖1kg", "10", "8.5"}}},
				{no: "QK-V2H007", customer: "周建华", quick: true, daysAgo: 3, lines: []line{{"白糖1kg", "2", "8.5"}}},
				{no: "SL-V2H008", customer: "何丽", daysAgo: 6, lines: []line{{"花生油5L", "2", "89"}}},
				{no: "SL-V2H009", customer: "何丽娟", daysAgo: 16, lines: []line{{"挂面", "10", "4.5"}}},
				{no: "SL-V2H010", customer: "何丽娟", daysAgo: 1, draft: true, lines: []line{{"挂面", "5", "4.5"}}},
				{no: "SL-V2H011", customer: "郑伟", daysAgo: 8, lines: []line{{"大米10kg", "4", "55"}}},
				{no: "SL-V2H012", customer: "郑斌", daysAgo: 5, lines: []line{{"面粉5kg", "2", "26"}, {"陈醋", "3", "6.8"}}},
				{no: "SL-V2H013", customer: "冯玉珍", daysAgo: 13, lines: []line{{"大米10kg", "1", "58"}}},
				{no: "SL-V2H014", customer: "冯玉兰", daysAgo: 7, lines: []line{{"白糖1kg", "2", "8.5"}}},
				{no: "SL-V2H015", customer: "蒋国庆", daysAgo: 45, lines: []line{{"白酒", "1", "120"}}},
				{no: "SL-V2H016", customer: "蒋国庆", daysAgo: 38, lines: []line{{"白酒", "2", "120"}}},
				{no: "SL-V2H017", customer: "蒋国庆", daysAgo: 30, lines: []line{{"白酒", "1", "120"}}},
				{no: "SL-V2H018", customer: "蒋国庆", daysAgo: 24, lines: []line{{"白酒", "3", "120"}}},
				{no: "SL-V2H019", customer: "蒋国庆", daysAgo: 17, lines: []line{{"白酒", "1", "120"}}},
				{no: "SL-V2H020", customer: "蒋国庆", daysAgo: 9, lines: []line{{"白酒", "2", "120"}}},
				{no: "SL-V2H021", customer: "蒋国庆", daysAgo: 3, lines: []line{{"白酒", "1", "120"}}},
				{no: "SL-V2H022", customer: "韩春梅", daysAgo: 10, lines: []line{{"洗洁精", "6", "12"}}},
				{no: "SL-V2H023", customer: "唐勇", daysAgo: 21, lines: []line{{"大米10kg", "20", "55"}}},
				{no: "SL-V2H024", customer: "唐勇", daysAgo: 2, lines: []line{{"大米10kg", "10", "52"}}},
				{no: "SL-V2H025", customer: "曹淑芬", daysAgo: 5, lines: []line{{"卫生纸", "3", "22"}}},
				{no: "SL-V2H026", customer: "董磊", daysAgo: 12, lines: []line{{"陈醋", "4", "6.8"}, {"挂面", "20", "4.5"}}},
				{no: "SL-V2H027", customer: "袁芳", daysAgo: 19, lines: []line{{"卫生纸", "2", "22"}}},
				{no: "SL-V2H028", customer: "邓涛", daysAgo: 7, lines: []line{{"大米10kg", "20", "55"}}},
				{no: "QK-V2H029", customer: "许静", quick: true, daysAgo: 6, lines: []line{{"陈醋", "2", "6.8"}}},
				{no: "SL-V2H030", customer: "傅军", daysAgo: 15, lines: []line{{"花生油5L", "1", "89"}}},
				{no: "SL-V2H031", customer: "沈红", daysAgo: 4, lines: []line{{"花生油5L", "2", "89"}}},
				{no: "SL-V2H032", customer: "曾平", daysAgo: 3, lines: []line{{"挂面", "10", "4.5"}}},
				{no: "SL-V2H033", customer: "曾平", daysAgo: 0, draft: true, lines: []line{{"挂面", "10", "4.5"}}},
			},
			decoyCustomer: "杨光", decoyProduct: "茅台",
			purchaseProbes: []string{
				"杨光", "杨光明", "周建", "周建华", "何丽", "何丽娟", "郑伟", "郑斌", "冯玉珍", "冯玉兰", "蒋国庆",
				"韩春梅", "唐勇", "曹淑芬", "彭飞", "董磊", "袁芳", "邓涛", "许静", "傅军", "沈红", "曾平",
			},
			ambiguousProbe: "杨",
			ambiguousWant:  []string{"杨光", "杨光明"},
			memoryProbes: []memoryProbe{
				{"接待董磊要注意什么？", "董磊"},
				{"杨光现在怎么付款？", "杨光"},
				{"给杨光明发货要注意什么？", "杨光明"},
				{"周建的账怎么结？", "周建"},
				{"周建华来了要注意什么？", "周建华"},
				{"何丽来了要注意什么？", "何丽"},
				{"给何丽娟送货要注意什么？", "何丽娟"},
				{"郑伟那边收货找谁？", "郑伟"},
				{"接待郑斌要注意什么？", "郑斌"},
				{"冯玉珍来了要注意什么？", "冯玉珍"},
				{"冯玉兰的发票怎么处理？", "冯玉兰"},
				{"老蒋的发票怎么开？", "蒋国庆"},
				{"给韩姐送货要注意什么？", "韩春梅"},
				{"唐老板的大米现在多少钱一袋？", "唐勇"},
				{"曹姐的货送到哪？", "曹淑芬"},
				{"彭飞来了要注意什么？", "彭飞"},
				{"袁芳的店现在在哪？", "袁芳"},
				{"接待邓涛要注意什么？", "邓涛"},
				{"许静的货怎么包装？", "许静"},
				{"傅军什么时候送货？", "傅军"},
				{"接待沈红要注意什么？", "沈红"},
				{"曾平来了要注意什么？", "曾平"},
			},
		},
		sessions: [3][]v2fact{
			{ // session 1
				old("董磊", "delivery_address", "董磊的店在北关菜场", "北关菜场"),
				old("杨光", "payment_method", "杨光付款都刷银行卡", "刷银行卡"),
				old("何丽娟", "delivery_time", "何丽娟要每周三下午送", "周三下午"),
				old("周建", "settlement", "周建每月10号结账", "10号结账"),
				old("郑伟", "contact", "郑伟那边收货找他媳妇", "他媳妇"),
				old("蒋国庆", "invoice", "蒋国庆的发票开个人抬头", "个人抬头"),
				old("许静", "packaging", "许静的货装麻袋", "装麻袋"),
				old("杨光明", "delivery_method", "杨光明的货让快递寄", "快递寄"),
				old("唐勇", "price_terms", "唐勇的大米按每袋55算", "每袋55"),
				old("袁芳", "delivery_address", "袁芳的店在和平路12号", "和平路12号"),
				cur("曹淑芬", "delivery_address", "曹淑芬住在桂花园小区", "桂花园小区"),
				cur("彭飞", "payment_method", "彭飞用支付宝付", "用支付宝付"),
				cur("傅军", "delivery_time", "傅军要每周四送货", "每周四送货"),
				cur("沈红", "packaging", "沈红的货要用纸箱", "要用纸箱"),
				cur("邓涛", "regular_items", "邓涛每周要二十袋大米", "二十袋大米"),
				cur("冯玉兰", "invoice", "冯玉兰开普通发票", "开普通发票"),
				cur("韩春梅", "delivery_address", "韩春梅的店在光华街", "光华街"),
				chat("今天进了多少货？"),
				rule("每月最后一天盘点", "最后一天盘点"),
				cur("杨光", "taste", "杨光不吃甜的", "不吃甜"),
				cur("杨光明", "contact", "杨光明店里收货是他闺女", "他闺女"),
				cur("周建", "regular_items", "周建每次都要五袋面粉", "五袋面粉"),
				cur("何丽", "other", "何丽不要转基因的油", "转基因"),
				cur("何丽娟", "allergy", "何丽娟对小麦过敏", "小麦过敏"),
				cur("郑斌", "delivery_time", "郑斌只在下午来拿货", "下午来拿货"),
				chat("帮我算一下这个月的利润"),
				rule("订货满三百免运费", "满三百免运费"),
				cur("冯玉珍", "contact", "冯玉珍的货让她孙子来拿", "她孙子"),
				cur("董磊", "contact", "董磊那边对接的是他老丈人", "老丈人"),
				cur("袁芳", "payment_method", "袁芳用对公转账", "对公转账"),
				cur("邓涛", "payment_method", "邓涛付现金", "付现金"),
				cur("郑斌", "price_terms", "郑斌是郑伟的表弟，郑斌的面粉给九八折", "九八折"),
				chat("外面好像要下雨了"),
				cur("冯玉珍", "regular_items", "冯玉珍只要东北大米，别跟冯玉兰的弄混", "东北大米"),
				cur("唐勇", "contact", "唐勇店里有事找他弟弟", "找他弟弟"),
				cur("蒋国庆", "delivery_time", "老蒋只要早上送货", "只要早上送货"),
				cur("许静", "delivery_time", "许静要上午十点前送到", "十点前送到"),
				rule("大雪天不出车", "大雪天"),
				cur("沈红", "regular_items", "沈红每次要两桶花生油", "两桶花生油"),
			},
			{ // session 2
				old("董磊", "delivery_address", "董磊搬去西环路了", "西环路"),
				old("杨光", "payment_method", "杨光现在改成扫微信", "扫微信"),
				old("何丽娟", "delivery_time", "何丽娟改到周一早上送", "周一早上"),
				old("周建", "settlement", "周建的账改成季度结", "季度结"),
				old("郑伟", "contact", "郑伟店里换成他外甥收货", "他外甥"),
				old("蒋国庆", "invoice", "老蒋以后开公司抬头", "公司抬头"),
				old("许静", "packaging", "许静要求改用塑料桶", "塑料桶"),
				old("杨光明", "delivery_method", "杨光明改成我们送货上门", "送货上门"),
				old("唐勇", "price_terms", "唐老板的大米调到每袋58", "每袋58"),
				old("袁芳", "delivery_address", "袁芳的店在建设路", "建设路"),
				cur("曹淑芬", "delivery_method|delivery_address", "曹姐的货放她家门卫室就行", "门卫室"),
				cur("彭飞", "settlement|payment_method", "彭飞的钱等他卖完货再给", "卖完货再给"),
				cur("傅军", "delivery_time|contact", "傅军那边中午没人，别那时候去", "中午没人"),
				chat("哪个商品快卖完了？"),
				rule("不管是谁，退货都要原包装，杨光也一样", "原包装"),
				cur("何丽", "delivery_method", "何丽和何丽娟是两家店，何丽的货要走后门", "走后门"),
				cur("周建华", "regular_items", "周建华跟周建不是一家，周建华只要小包装白糖", "小包装白糖"),
				cur("曾平", "regular_items", "曾平是许静介绍的，曾平每次买十把挂面", "十把挂面"),
				cur("何丽娟", "allergy", "何丽娟也对芝麻过敏", "芝麻过敏"),
				cur("杨光明", "contact", "杨光明店里收货是他闺女", "他闺女"),
				cur("冯玉兰", "taste", "冯玉兰爱吃甜口的", "甜口"),
				cur("韩春梅", "taste", "韩姐爱喝红茶", "红茶"),
				chat("嗯嗯"),
				rule("以后赊账一律要签字", "赊账一律要签字"),
				cur("沈红", "packaging|other", "沈红说袋子要扎紧点", "扎紧"),
				cur("邓涛", "price_terms|regular_items", "邓涛的面粉还按原来的算", "按原来的算"),
				cur("冯玉兰", "invoice|settlement", "冯玉兰的发票月底一起给她", "月底一起给"),
				cur("韩春梅", "delivery_time|other", "韩姐那边晚上七点后才关门", "七点后才关门"),
				cur("唐勇", "contact", "唐勇店里有事找他弟弟", "找他弟弟"),
				cur("曹淑芬", "regular_items", "曹淑芬每周要三提卫生纸", "三提卫生纸"),
				cur("郑斌", "payment_method", "郑斌付款刷卡", "付款刷卡"),
				cur("傅军", "payment_method", "傅军用微信付", "用微信付"),
				chat("行，就这样"),
				rule("早上七点前不接单", "七点前不接单"),
			},
			{ // session 3
				cur("董磊", "delivery_address", "董磊的店在高铁站东边", "高铁站东边"),
				cur("杨光", "payment_method", "杨光以后给现金", "给现金"),
				cur("何丽娟", "delivery_time", "何丽娟那边周六中午送货", "周六中午"),
				cur("周建", "settlement", "周建现在每周结一次", "每周结一次"),
				cur("郑伟", "contact", "郑伟那边收货的是新来的小工阿亮", "小工阿亮"),
				cur("蒋国庆", "invoice", "蒋国庆的发票不用开了", "发票不用开了"),
				cur("许静", "packaging", "许静的货放周转箱里", "周转箱"),
				cur("杨光明", "delivery_method", "杨光明现在自己开三轮来拉", "开三轮来拉"),
				cur("唐勇", "price_terms", "唐勇大米每袋52", "每袋52"),
				cur("袁芳", "delivery_address", "袁芳又搬了，现在在大学城南门", "大学城南门"),
				chat("杨光最近来过吗？"),
				rule("节假日照常营业，但不送货", "节假日照常营业"),
				cur("彭飞", "regular_items", "彭飞上次替傅军捎了货，彭飞自己要的是花生油", "要的是花生油"),
				cur("何丽娟", "allergy", "何丽娟对小麦过敏", "小麦过敏"),
				cur("曾平", "contact", "曾平收货找他老婆", "他老婆"),
				cur("郑伟", "settlement", "郑伟每月20号对账", "20号对账"),
				chat("上个月卖得最好的是什么？"),
				cur("周建华", "delivery_address", "周建华的店在老街口", "老街口"),
				cur("袁芳", "contact", "袁芳那边找她妹妹收货", "她妹妹"),
				cur("蒋国庆", "taste", "老蒋喜欢喝散装白酒", "散装白酒"),
				cur("许静", "regular_items", "许静每次都要两瓶陈醋", "两瓶陈醋"),
				chat("你记得我刚才说什么吗？"),
				cur("董磊", "contact", "董磊那边对接的是他老丈人", "老丈人"),
				cur("曾平", "delivery_time", "曾平要傍晚送货", "傍晚送货"),
				cur("韩春梅", "regular_items", "韩春梅每周要一箱洗洁精", "一箱洗洁精"),
				cur("唐勇", "other", "唐老板的店周一不开门", "周一不开门"),
				chat("这两天怎么这么忙"),
				cur("曹淑芬", "taste", "曹姐不吃香菜", "不吃香菜"),
				cur("邓涛", "contact", "邓涛店里收货的是他表哥", "他表哥"),
				cur("周建华", "payment_method", "周建华用网银付款", "网银付款"),
				cur("沈红", "contact", "沈红那边收货找她老伴", "她老伴"),
			},
		},
	}
}

// all returns every turn in order.
func (v v2scenario) all() []v2fact {
	var out []v2fact
	for _, s := range v.sessions {
		out = append(out, s...)
	}
	return out
}

func plain(fs []v2fact) []fact {
	out := make([]fact, len(fs))
	for i, f := range fs {
		out[i] = f.fact
	}
	return out
}

// chains: per customer and attribute, the distinct values in the order said,
// for every attribute that has a stale value.
func (v v2scenario) chains() []customerChain {
	type gk struct{ owner, slot string }
	var order []gk
	keys := map[gk][]string{}
	hasStale := map[gk]bool{}
	for _, f := range v.all() {
		if f.owner == "" || f.key == "" {
			continue
		}
		g := gk{f.owner, f.slot}
		if _, ok := keys[g]; !ok {
			order = append(order, g)
		}
		ks := keys[g]
		if len(ks) == 0 || ks[len(ks)-1] != f.key {
			ks = append(ks, f.key)
		}
		keys[g] = ks
		hasStale[g] = hasStale[g] || f.stale
	}
	var out []customerChain
	for _, g := range order {
		if hasStale[g] {
			out = append(out, customerChain{g.owner, g.slot, keys[g]})
		}
	}
	return out
}

func (v v2scenario) customers() customerScenario {
	v.validate()
	sc := v.base
	sc.session1, sc.session2, sc.session3 = plain(v.sessions[0]), plain(v.sessions[1]), plain(v.sessions[2])
	sc.chains = v.chains()
	return sc
}

// slots: the customer statements, in order, for the LLM slot replay.
// Shop rules and chit-chat are left out (the slot replay scores tool calls on
// customer statements only).
func (v v2scenario) slots() slotScenario {
	v.validate()
	var sc slotScenario
	for i, s := range v.sessions {
		for _, f := range s {
			if f.owner == "" {
				continue
			}
			st := slotStatement{f.owner, f.text, f.slot}
			if i == 0 {
				sc.before = append(sc.before, st)
			} else {
				sc.after = append(sc.after, st)
			}
		}
	}
	last := map[string]bool{}
	for _, c := range v.chains() {
		sc.chains = append(sc.chains, slotChain{c.owner, c.slot, c.keys})
		last[c.owner+"\x00"+c.keys[len(c.keys)-1]] = true
	}
	seen := map[string]bool{}
	for _, f := range v.all() {
		k := f.owner + "\x00" + f.key
		if f.owner == "" || f.stale || last[k] || seen[k] {
			continue
		}
		seen[k] = true
		why := "another attribute of the customer"
		switch {
		case strings.Contains(f.slot, "|"):
			why = "unclear attribute (" + f.slot + ")"
		case v.namesOther(f.fact):
			why = "names another customer"
		}
		sc.keep = append(sc.keep, slotKeep{f.owner, f.key, why})
	}
	return sc
}

func (v v2scenario) namesOther(f fact) bool { return namesOther(v.base.customers, f) }

// namesOther: a customer fact whose text also names another customer (longest
// names first, so 李红梅 is not also read as 李红).
func namesOther(customers []customer, f fact) bool {
	if f.owner == "" {
		return false
	}
	names := make([]string, 0, len(customers))
	for _, c := range customers {
		names = append(names, c.name)
	}
	sort.Slice(names, func(i, j int) bool { return len([]rune(names[i])) > len([]rune(names[j])) })
	rest := f.text
	for _, n := range names {
		if strings.Contains(rest, n) {
			if n != f.owner {
				return true
			}
			rest = strings.ReplaceAll(rest, n, "\x00")
		}
	}
	return false
}

// validate panics on a scenario the substring scoring cannot score honestly.
func (v v2scenario) validate() {
	names := map[string]bool{}
	for _, c := range v.base.customers {
		names[c.name] = true
	}
	all := v.all()
	type meta struct {
		owner string
		stale bool
	}
	byKey := map[string]meta{}
	for _, f := range all {
		if f.owner != "" && !names[f.owner] {
			panic(fmt.Sprintf("v2: owner %q is not a customer", f.owner))
		}
		if f.key == "" {
			if f.owner != "" {
				panic(fmt.Sprintf("v2: customer fact without key: %q", f.text))
			}
			continue
		}
		if !strings.Contains(f.text, f.key) {
			panic(fmt.Sprintf("v2: key %q not in its text %q", f.key, f.text))
		}
		if m, ok := byKey[f.key]; ok && (m.owner != f.owner || m.stale != f.stale) {
			panic(fmt.Sprintf("v2: key %q used by two different facts", f.key))
		}
		byKey[f.key] = meta{f.owner, f.stale}
	}
	for k := range byKey {
		for _, g := range all {
			if g.key != k && strings.Contains(g.text, k) {
				panic(fmt.Sprintf("v2: key %q also occurs in %q", k, g.text))
			}
		}
	}
	for _, c := range v.chains() {
		for i, k := range c.keys {
			if byKey[k].stale != (i < len(c.keys)-1) {
				panic(fmt.Sprintf("v2: chain %s/%s %v: only the last value may be current", c.owner, c.slot, c.keys))
			}
		}
	}
	for _, p := range v.base.memoryProbes {
		if !names[p.about] {
			panic(fmt.Sprintf("v2: probe %q about unknown customer %q", p.q, p.about))
		}
	}
}

func v2ByLabel(label string) (v2scenario, bool) {
	switch label {
	case "v2-dev":
		return devV2(), true
	case "v2-holdout":
		return holdoutV2(), true
	}
	return v2scenario{}, false
}

// printV2Scores adds two v2-only lines after the unchanged SCORE line:
//   - SCORE-CHAINS: per (probe, change chain of the probed customer), the
//     newest value injected and none of the older ones;
//   - SCORE-ATTRIBUTION, read from memorus' stored rows (GET /memories), not
//     from the prompt: every row holding a customer fact carries that
//     customer's subject_id, and no row holding a shop rule carries any.
//     A fact with no row at all counts as wrong.
func printV2Scores(sc customerScenario, label string, noAttr bool, m memoryScore, partners map[string]uuid.UUID, user string) {
	fmt.Printf("SCORE-CHAINS scenario=%s noattr=%v | latest_only=%d/%d latest_found=%d/%d older_values_injected=%d\n",
		label, noAttr, m.chainOK, m.chainTotal, m.chainLatest, m.chainTotal, m.chainOlder)

	asOfProbe(sc, label, m.sessionEnds, partners, user)

	rows := listMemories(user)
	type tally struct{ ok, n int }
	score := map[string]*tally{"customer": {}, "names_other": {}, "shop_rule": {}}
	seen := map[string]bool{}
	for _, f := range append(append(append([]fact{}, sc.session1...), sc.session2...), sc.session3...) {
		if f.key == "" || seen[f.key] {
			continue
		}
		seen[f.key] = true
		kind, want := "shop_rule", ""
		if f.owner != "" {
			kind, want = "customer", ai.CustomerSubject(partners[f.owner])
			if namesOther(sc.customers, f) {
				kind = "names_other"
			}
		}
		var subjects []string
		ok := true
		for _, r := range rows {
			if !r.mentions(f.key) {
				continue
			}
			subjects = append(subjects, r.meta(ai.MemorySubjectKey))
			ok = ok && r.meta(ai.MemorySubjectKey) == want
		}
		ok = ok && len(subjects) > 0
		if ok {
			score[kind].ok++
		}
		score[kind].n++
		fmt.Printf("ATTR ok=%v kind=%s owner=%s key=%s rows=%d subjects=%v\n", ok, kind, f.owner, f.key, len(subjects), subjects)
	}
	fmt.Printf("SCORE-ATTRIBUTION scenario=%s noattr=%v | customer_facts=%d/%d names_another_customer=%d/%d shop_rules_untagged=%d/%d rows=%d\n",
		label, noAttr, score["customer"].ok, score["customer"].n, score["names_other"].ok, score["names_other"].n,
		score["shop_rule"].ok, score["shop_rule"].n, len(rows))
}

// v2Counts is printed at the start of a v2 run.
func (v v2scenario) counts() string {
	facts, chat, rules, unclear, traps, chains3 := 0, 0, 0, 0, 0, 0
	for _, f := range v.all() {
		facts++
		switch {
		case f.key == "":
			chat++
		case f.owner == "":
			rules++
		case strings.Contains(f.slot, "|"):
			unclear++
		}
		if v.namesOther(f.fact) {
			traps++
		}
	}
	chains := v.chains()
	for _, c := range chains {
		if len(c.keys) >= 3 {
			chains3++
		}
	}
	about := map[string]bool{}
	for _, p := range v.base.memoryProbes {
		about[p.about] = true
	}
	return fmt.Sprintf("customers=%d statements=%d (s1=%d s2=%d s3=%d) change_chains=%d (3-value=%d) unclear_attribute=%d shop_rules=%d chit_chat=%d names_another_customer=%d memory_probes=%d (customers=%d)",
		len(v.base.customers), facts, len(v.sessions[0]), len(v.sessions[1]), len(v.sessions[2]),
		len(chains), chains3, unclear, rules, chat, traps, len(v.base.memoryProbes), len(about))
}

// asOfProbe asks memorus, for every three-step change chain, what held right
// after session 1 and after session 2 (GET /memories/search?as_of=): the
// value stated then must be there, the later ones must not. This is the
// memorus side of 「三月时李四怎么付款」 on real replay data, without a model.
func asOfProbe(sc customerScenario, label string, ends []time.Time, partners map[string]uuid.UUID, user string) {
	if len(ends) < 2 {
		return
	}
	c, err := memorusclient.New(memorusclient.Config{BaseURL: os.Getenv("MEMORUS_URL"), APIKey: "probekey"})
	if err != nil {
		panic(fmt.Sprint("memorus client: ", err))
	}
	ok, total := 0, 0
	for _, ch := range sc.chains {
		if len(ch.keys) != 3 {
			continue
		}
		for r := 0; r < 2; r++ {
			hits, err := c.SearchAsOf(context.Background(), user, ch.owner, 20,
				map[string]string{ai.MemorySubjectKey: ai.CustomerSubject(partners[ch.owner])}, ends[r])
			var got []string
			for _, h := range hits {
				got = append(got, h.Content)
			}
			joined := strings.Join(got, " | ")
			good := err == nil && strings.Contains(joined, ch.keys[r])
			for _, later := range ch.keys[r+1:] {
				good = good && !strings.Contains(joined, later)
			}
			total++
			if good {
				ok++
			}
			fmt.Printf("ASOF ok=%v %s %s after_session=%d want=%s notes=%q\n", good, ch.owner, ch.slot, r+1, ch.keys[r], got)
		}
	}
	fmt.Printf("SCORE-ASOF scenario=%s | held_then=%d/%d\n", label, ok, total)
}
