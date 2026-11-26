package main

import "fmt"

var cities = []string{
	"上海", "广州", "深圳", "杭州", "苏州", "南京", "武汉", "成都", "重庆", "西安",
	"郑州", "济南", "青岛", "长沙", "合肥", "福州", "厦门", "宁波", "东莞", "佛山",
}

var tradeKinds = []string{
	"华东", "华南", "北方", "鑫盛", "恒通", "中汇", "远东", "海晟", "德众", "启航",
	"万邦", "鼎新", "瑞丰", "立信", "宏泰", "嘉禾", "联合", "广源", "正方", "博雅",
}

var goodsKinds = []string{
	"食品", "日化", "五金", "建材", "汽配", "医药", "服饰", "数码", "农资", "百货",
	"包装", "机电", "化工", "文具", "家居", "照明", "安防", "钢材", "塑料", "冷链",
}

func supplierName(i int) string {
	return fmt.Sprintf("%s%s%s%s有限公司", cities[i%len(cities)], tradeKinds[i%len(tradeKinds)],
		goodsKinds[i%len(goodsKinds)], fmt.Sprintf("%02d", (i%50)+10))
}

var custSurnames = []string{
	"王", "李", "张", "刘", "陈", "杨", "赵", "黄", "周", "吴", "徐", "孙", "胡", "朱", "高",
	"林", "何", "郭", "马", "罗", "梁", "宋", "郑", "谢", "韩", "唐", "冯", "于", "董", "萧",
}

var custGiven = []string{
	"建国", "秀英", "伟", "芳", "娜", "敏", "静", "磊", "洋", "艳", "杰", "娟", "涛", "明", "超",
	"霞", "平", "刚", "桂英", "华", "军", "玉兰", "鹏", "慧", "飞", "鑫", "婷", "佳", "倩", "博",
}

var shopKinds = []string{
	"便利", "超市", "餐厅", "药房", "五金", "服装店", "汽修", "花店", "建材", "电商工作室",
}

func customerName(i int) string {
	if i%3 == 0 {
		return fmt.Sprintf("%s%s%s-%03d", cities[i%len(cities)], shopKinds[i%len(shopKinds)],
			fmt.Sprintf("（%s店）", custSurnames[i%len(custSurnames)]), i+1)
	}
	return fmt.Sprintf("%s%s-%03d", custSurnames[i%len(custSurnames)], custGiven[i%len(custGiven)], i+1)
}