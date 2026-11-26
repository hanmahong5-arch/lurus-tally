package main

import "fmt"

type industrySpec struct {
	code   string
	label  string
	unit   string
	brands []string
	items  []string
}

type productRow struct {
	industry, code, name, unit, purchase, retail string
}

var industries = []industrySpec{
	{code: "RT", label: "零售", unit: "件", brands: []string{"康师傅", "统一", "农夫山泉", "金龙鱼", "海天", "乐事", "百事", "云南白药"},
		items: []string{"饮用水", "薯片", "酱油", "牙膏", "纸巾", "牛奶", "面包", "电池", "洗洁精", "雨伞", "暖宝宝", "口香糖"}},
	{code: "WS", label: "批发", unit: "箱", brands: []string{"娃哈哈", "蒙牛", "鲁花", "雪花", "青岛", "洽洽", "双汇", "立白"},
		items: []string{"矿泉水", "洗衣液", "食用油", "啤酒", "坚果", "火腿肠", "方便面", "卫生纸", "奶粉", "罐头", "瓜子", "味精"}},
	{code: "MF", label: "制造", unit: "件", brands: []string{"精工", "华强", "沪工", "博世", "牧田", "德力西", "正泰", "亚太"},
		items: []string{"轴承", "螺丝", "电机", "钻头", "砂轮", "焊条", "密封圈", "联轴器", "钢板", "铝型材", "皮带", "继电器"}},
	{code: "RS", label: "餐饮", unit: "千克", brands: []string{"李锦记", "太太乐", "恒顺", "海天", "安琪", "王守义", "老干妈", "家乐"},
		items: []string{"生抽", "料酒", "陈醋", "辣椒粉", "酵母", "火锅底料", "淀粉", "冰糖", "花椒", "八角", "冰糖橘", "椰浆"}},
	{code: "PH", label: "药店", unit: "盒", brands: []string{"同仁堂", "三九", "东阿", "拜耳", "华润", "江中", "云南白药", "片仔癀"},
		items: []string{"感冒灵", "维生素C", "创可贴", "体温计", "口罩", "碘伏", "钙片", "止咳糖浆", "酒精棉", "艾条", "板蓝根", "纱布"}},
	{code: "AR", label: "汽修", unit: "件", brands: []string{"米其林", "壳牌", "美孚", "博世", "3M", "嘉实多", "瓦尔塔", "马勒"},
		items: []string{"机油", "机滤", "空气滤", "火花塞", "刹车片", "防冻液", "雨刮", "轮胎", "蓄电池", "变速箱油", "玻璃水", "灯泡"}},
	{code: "BM", label: "建材", unit: "袋", brands: []string{"海螺", "立邦", "多乐士", "圣戈班", "东方雨虹", "马可波罗", "箭牌", "九牧"},
		items: []string{"水泥", "瓷砖胶", "防水涂料", "乳胶漆", "石膏板", "PVC管", "电线", "开关插座", "美缝剂", "腻子粉", "红砖", "玻璃胶"}},
	{code: "AP", label: "服装", unit: "件", brands: []string{"优衣库", "安踏", "李宁", "波司登", "七匹狼", "海澜之家", "太平鸟", "森马"},
		items: []string{"T恤", "牛仔裤", "羽绒服", "衬衫", "运动裤", "卫衣", "半身裙", "袜子", "领带", "围巾", "皮鞋", "帽子"}},
	{code: "HO", label: "苗木", unit: "株", brands: []string{"绿源", "森禾", "康德", "天香", "香樟苑", "锦艺", "常青", "青云"},
		items: []string{"香樟", "桂花", "红叶石楠", "银杏", "茶花", "爬山虎", "罗汉松", "女贞", "海桐", "金森", "黄杨", "紫玉兰"}},
	{code: "EC", label: "电商", unit: "件", brands: []string{"小米", "华为", "罗技", "绿联", "漫步者", "飞利浦", "九阳", "美的"},
		items: []string{"数据线", "蓝牙耳机", "键盘", "充电宝", "鼠标垫", "加湿器", "电饭煲", "电动牙刷", "台灯", "摄像头", "路由器", "手环"}},
}

func productRows() []productRow {
	var out []productRow
	for _, ind := range industries {
		n := 0
		for _, brand := range ind.brands {
			for _, item := range ind.items {
				n++
				if n > 42 {
					break
				}
				code := fmt.Sprintf("ACC-%s-%03d", ind.code, n)
				name := brand + item
				if ind.code == "PH" {
					name = brand + " " + item + " 0.25g×24粒"
				}
				if ind.code == "BM" && item == "水泥" {
					name = brand + " 42.5级水泥 50kg"
				}
				purchase, retail := pricePair(ind.code, n)
				out = append(out, productRow{ind.label, code, name, ind.unit, purchase, retail})
			}
			if n >= 42 {
				break
			}
		}
	}
	return out
}

func pricePair(industry string, n int) (purchase, retail string) {
	base := 8.0 + float64((n*7)%500)
	switch industry {
	case "AR", "EC":
		base = 35.0 + float64((n*13)%8000)
	case "BM":
		base = 18.0 + float64((n*11)%600)
	case "AP":
		base = 45.0 + float64((n*5)%900)
	case "HO":
		base = 12.0 + float64((n*17)%2000)
	case "WS":
		base = 55.0 + float64((n*9)%1200)
	}
	return fmt.Sprintf("%.2f", base), fmt.Sprintf("%.2f", base*1.28)
}