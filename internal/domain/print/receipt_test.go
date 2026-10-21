package print

import (
	"flag"
	"os"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "刷新 golden 文件")

func sample() Receipt {
	return Receipt{
		ShopName:   "老王杂货铺",
		Phone:      "13800138000",
		Address:    "杭州市西湖区文三路 100 号一楼临街店面",
		BillNo:     "SO-20261015-0007",
		OccurredAt: time.Date(2026, 10, 15, 9, 30, 0, 0, time.UTC),
		Lines: []Line{
			{Name: "农夫山泉矿泉水 550ml 24 瓶整箱装特惠", Qty: "2", Unit: "箱", Price: "28.00", Amount: "56.00"},
			{Name: "Coke", Qty: "1.5", Unit: "kg", Price: "3.50", Amount: "5.25"},
		},
		Total: "61.25", Paid: "70.00", Change: "8.75", PayMethod: "现金",
		Footer: "谢谢惠顾，欢迎下次光临", Cashier: "小李",
	}
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := "testdata/" + name
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s: %v（加 -update 生成）", path, err)
	}
	if strings.ReplaceAll(string(want), "\r\n", "\n") != got {
		t.Errorf("%s 与排版结果不一致；确认无误后 go test -update", path)
	}
}

func TestRenderGolden(t *testing.T) {
	golden(t, "receipt_58.golden", Render(sample(), Paper58))
	golden(t, "receipt_80.golden", Render(sample(), Paper80))
}

func TestRenderNeverExceedsWidth(t *testing.T) {
	for _, w := range []PaperWidth{Paper58, Paper80} {
		for _, ln := range strings.Split(strings.TrimRight(Render(sample(), w), "\n"), "\n") {
			if StrWidth(ln) > w.Columns() {
				t.Errorf("%d 毫米纸出现超宽行（%d 列）：%q", w, StrWidth(ln), ln)
			}
		}
	}
}

func TestStrWidth(t *testing.T) {
	if got := StrWidth("ab中文，Ａ"); got != 2+4+2+2 {
		t.Errorf("StrWidth = %d", got)
	}
}

func TestRenderFeieMarkup(t *testing.T) {
	out := RenderFeie(sample(), Paper58)
	for _, want := range []string{"<CB>老王杂货铺</CB><BR>", "<B>合计", "<BR>"} {
		if !strings.Contains(out, want) {
			t.Errorf("缺少 %q", want)
		}
	}
	if strings.Contains(out, "\n") {
		t.Error("飞鹅内容不应含换行，换行用 BR 标签")
	}
}

func TestParsePaper(t *testing.T) {
	if ParsePaper("80") != Paper80 || ParsePaper("") != Paper58 || ParsePaper("x") != Paper58 {
		t.Error("ParsePaper 结果不对")
	}
}
