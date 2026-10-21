package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	domainai "github.com/hanmahong5-arch/lurus-tally/internal/domain/ai"
	"github.com/shopspring/decimal"
)

type spokenQty struct {
	said string
	val  string
}

// qtyForms are the ways an owner says a quantity: plain, colloquial, with 半,
// Arabic and mixed.
var qtyForms = []spokenQty{
	{"十棵", "10"}, {"二十棵", "20"}, {"两箱半", "2.5"}, {"一千二百个", "1200"},
	{"一千二个", "1200"}, {"三百五十件", "350"}, {"三百五件", "350"}, {"五包", "5"},
	{"12箱", "12"}, {"3.5公斤", "3.5"}, {"一百零五条", "105"}, {"两万五千根", "25000"},
	{"一万二袋", "12000"}, {"半箱", "0.5"}, {"一个半", "1.5"}, {"三斤半", "3.5"},
	{"八十盒", "80"}, {"两百把", "200"}, {"九百九十九只", "999"}, {"六十桶", "60"},
	{"100个", "100"}, {"2箱半", "2.5"}, {"四十五套", "45"}, {"七台", "7"},
	{"一千零五十张", "1050"},
}

var productNames = []string{"樱花", "桃树", "螺丝", "锤子", "水管", "电缆", "油漆", "手套", "铁钉", "砂纸"}

type specimen struct {
	sentence string
	lines    []CheckLine
	// renamed marks a specimen whose draft names are not in the sentence (the
	// model renamed the product): the only documented exception to the
	// no-miss rule, because a pool number cannot be told from a valid pick.
	renamed bool
}

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func cl(name string, q spokenQty) CheckLine { return CheckLine{ProductName: name, Qty: dec(q.val)} }

// buildSpecimens makes deterministic dictated sentences in the shapes owners
// really produce (punctuated, unpunctuated, 和-joined, 各 mixed with plain
// numbers, number-first, bare prices) with the correct draft lines for each.
func buildSpecimens() []specimen {
	var out []specimen
	for i := 0; len(out) < 340; i++ {
		a := qtyForms[i%len(qtyForms)]
		b := qtyForms[(i*7+3)%len(qtyForms)]
		c := qtyForms[(i*11+5)%len(qtyForms)]
		p1 := productNames[i%len(productNames)]
		p2 := productNames[(i+3)%len(productNames)]
		p3 := productNames[(i+6)%len(productNames)]
		two := []CheckLine{cl(p1, a), cl(p2, b)}
		three := []CheckLine{cl(p1, a), cl(p2, b), cl(p3, c)}
		each2 := []CheckLine{cl(p1, a), cl(p2, a)}
		switch i % 17 {
		case 0:
			out = append(out, specimen{sentence: fmt.Sprintf("帮我进%s%s", p1, a.said), lines: []CheckLine{cl(p1, a)}})
		case 1:
			out = append(out, specimen{sentence: fmt.Sprintf("进货：%s%s，%s%s", p1, a.said, p2, b.said), lines: two})
		case 2:
			out = append(out, specimen{sentence: fmt.Sprintf("%s和%s各%s", p1, p2, a.said), lines: each2})
		case 3:
			out = append(out, specimen{sentence: fmt.Sprintf("订%s%s，%s%s，还要%s%s，一共五百元", p1, a.said, p2, b.said, p3, c.said), lines: three})
		case 4:
			out = append(out, specimen{sentence: fmt.Sprintf("要%s，%s", a.said, p1), lines: []CheckLine{{ProductName: p1 + "(规格A)", Qty: dec(a.val)}}, renamed: true})
		case 5: // no punctuation at all
			out = append(out, specimen{sentence: fmt.Sprintf("进%s%s%s%s", p1, a.said, p2, b.said), lines: two})
		case 6: // 和 between the lines
			out = append(out, specimen{sentence: fmt.Sprintf("%s%s和%s%s", p1, a.said, p2, b.said), lines: two})
		case 7: // 各 for two products, then a plain number, with a comma
			out = append(out, specimen{sentence: fmt.Sprintf("%s和%s各%s，%s%s", p1, p2, a.said, p3, b.said),
				lines: []CheckLine{cl(p1, a), cl(p2, a), cl(p3, b)}})
		case 8: // same, no punctuation
			out = append(out, specimen{sentence: fmt.Sprintf("进%s和%s各%s%s%s", p1, p2, a.said, p3, b.said),
				lines: []CheckLine{cl(p1, a), cl(p2, a), cl(p3, b)}})
		case 9: // plain number first, then a 各 pair
			out = append(out, specimen{sentence: fmt.Sprintf("%s%s，%s和%s各%s", p1, a.said, p2, p3, b.said),
				lines: []CheckLine{cl(p1, a), cl(p2, b), cl(p3, b)}})
		case 10: // bare price after the quantity
			out = append(out, specimen{sentence: fmt.Sprintf("%s%s每棵三十", p1, a.said), lines: []CheckLine{cl(p1, a)}})
		case 11: // two lines each with a bare price, no punctuation
			out = append(out, specimen{sentence: fmt.Sprintf("进%s%s单价三十%s%s单价四十", p1, a.said, p2, b.said), lines: two})
		case 12: // number first
			out = append(out, specimen{sentence: fmt.Sprintf("进%s%s%s%s", a.said, p1, b.said, p2), lines: two})
		case 14: // draft carries the catalogue name, the sentence a short form
			out = append(out, specimen{sentence: fmt.Sprintf("进%s%s%s%s", p1, a.said, p2, b.said),
				lines: []CheckLine{cl("优质"+p1, a), cl("优质"+p2, b)}})
		case 15: // same, number first
			out = append(out, specimen{sentence: fmt.Sprintf("进%s%s%s%s", a.said, p1, b.said, p2),
				lines: []CheckLine{cl(p1+"(大号)", a), cl(p2+"(大号)", b)}})
		case 16: // per-unit price denominator after the quantity
			out = append(out, specimen{sentence: fmt.Sprintf("%s%s三十块一棵", p1, a.said), lines: []CheckLine{cl(p1, a)}})
		case 13: // 和 with three lines, no punctuation
			out = append(out, specimen{sentence: fmt.Sprintf("%s%s和%s%s和%s%s", p1, a.said, p2, b.said, p3, c.said), lines: three})
		}
	}
	return out
}

func TestCheckSpokenQuantities_CorrectDraftsNotFlagged(t *testing.T) {
	for _, s := range buildSpecimens() {
		if flags := CheckSpokenQuantities(s.sentence, s.lines); len(flags) != 0 {
			t.Errorf("false positive on %q: %+v", s.sentence, flags)
		}
	}
}

// TestCheckSpokenQuantities_WrongQuantityNeverMissed: for 300+ dictated
// sentences, corrupt each line's quantity in several ways (x10, /10, +1, x2,
// off-by-digit, and a swap with another line) and require every corruption to
// be flagged on that line. Landing on another number said in the sentence gets
// no exemption; the single exception is a draft whose product name is not in
// the sentence at all, and those are counted and bounded.
func TestCheckSpokenQuantities_WrongQuantityNeverMissed(t *testing.T) {
	mutations := []func(decimal.Decimal) decimal.Decimal{
		func(d decimal.Decimal) decimal.Decimal { return d.Mul(dec("10")) },
		func(d decimal.Decimal) decimal.Decimal { return d.Div(dec("10")) },
		func(d decimal.Decimal) decimal.Decimal { return d.Add(dec("1")) },
		func(d decimal.Decimal) decimal.Decimal { return d.Mul(dec("2")) },
		func(d decimal.Decimal) decimal.Decimal { return d.Add(dec("0.5")) },
	}
	specimens := buildSpecimens()
	if len(specimens) < 200 {
		t.Fatalf("want >= 200 specimens, got %d", len(specimens))
	}
	checked, missed, exempt := 0, 0, 0
	flaggedLine := func(sentence string, lines []CheckLine, li int) bool {
		for _, f := range CheckSpokenQuantities(sentence, lines) {
			if f.Line == li {
				return true
			}
		}
		return false
	}
	for _, s := range specimens {
		said := map[string]bool{}
		for _, q := range ExtractQuantities(s.sentence) {
			said[q.Value.String()] = true
		}
		for li := range s.lines {
			for mi, m := range mutations {
				lines := append([]CheckLine(nil), s.lines...)
				lines[li].Qty = m(lines[li].Qty)
				checked++
				if flaggedLine(s.sentence, lines, li) {
					continue
				}
				if s.renamed && said[lines[li].Qty.String()] {
					exempt++
					continue
				}
				missed++
				t.Errorf("missed: %q line %d mutation %d -> qty %s", s.sentence, li, mi, lines[li].Qty)
			}
			// Swap with each other line holding a different quantity.
			for lj := range s.lines {
				if lj == li || s.lines[lj].Qty.Equal(s.lines[li].Qty) {
					continue
				}
				lines := append([]CheckLine(nil), s.lines...)
				lines[li].Qty, lines[lj].Qty = s.lines[lj].Qty, s.lines[li].Qty
				checked++
				if !flaggedLine(s.sentence, lines, li) {
					missed++
					t.Errorf("missed swap: %q lines %d<->%d", s.sentence, li, lj)
				}
			}
		}
	}
	if missed != 0 {
		t.Fatalf("%d of %d wrong quantities slipped through", missed, checked)
	}
	if exempt*20 > checked {
		t.Fatalf("too many renamed-product exemptions: %d of %d", exempt, checked)
	}
	t.Logf("checked %d corruptions over %d sentences, %d renamed-product exemptions", checked, len(specimens), exempt)
}

// TestCheckSpokenQuantities_ReviewerProbes pins the shapes that once slipped
// through: unpunctuated dictation, 和-joined lines, 各 scoped to its own
// clause, and a bare price after the quantity.
func TestCheckSpokenQuantities_ReviewerProbes(t *testing.T) {
	l := func(pairs ...string) []CheckLine {
		var out []CheckLine
		for i := 0; i < len(pairs); i += 2 {
			out = append(out, CheckLine{ProductName: pairs[i], Qty: dec(pairs[i+1])})
		}
		return out
	}
	cases := []struct {
		said  string
		lines []CheckLine
		want  []int // line indexes that must be flagged
	}{
		{"进可乐三箱雪碧五箱", l("可乐", "5", "雪碧", "3"), []int{0, 1}},
		{"进可乐三箱雪碧五箱", l("可乐", "3", "雪碧", "5"), nil},
		{"可乐三箱和雪碧五箱", l("可乐", "5", "雪碧", "3"), []int{0, 1}},
		{"可乐三箱和雪碧五箱", l("可乐", "3", "雪碧", "5"), nil},
		{"苹果和梨各十箱，香蕉五箱", l("苹果", "10", "梨", "10", "香蕉", "10"), []int{2}},
		{"苹果和梨各十箱，香蕉五箱", l("苹果", "10", "梨", "10", "香蕉", "5"), nil},
		{"樱花十棵每棵三十", l("樱花", "30"), []int{0}},
		{"樱花十棵每棵三十", l("樱花", "10"), nil},
		{"进三箱可乐五箱雪碧", l("可乐", "5", "雪碧", "3"), []int{0, 1}},
		{"进三箱可乐五箱雪碧", l("可乐", "3", "雪碧", "5"), nil},
		{"M8螺丝一百个", l("M8螺丝", "100"), nil},
		{"M8螺丝一百个", l("M8螺丝", "8"), []int{0}},
		// Draft carries the catalogue name, the sentence a short form.
		{"进可乐三箱雪碧五箱", l("可口可乐", "5", "雪碧", "3"), []int{0, 1}},
		{"进可乐三箱雪碧五箱", l("可口可乐", "3", "雪碧", "5"), nil},
		{"樱花十棵桃树二十棵", l("樱花树", "20", "桃树", "10"), []int{0, 1}},
		{"樱花十棵桃树二十棵", l("樱花树", "10", "桃树", "20"), nil},
		{"进三箱可乐五箱雪碧", l("可口可乐(330ml)", "5", "雪碧", "3"), []int{0, 1}},
		// A draft line the sentence never names must not let a leading number be read as number-first.
		{"进可乐三箱雪碧五箱", l("汽水", "5", "雪碧", "3"), []int{0, 1}},
		// Per-unit price denominators are not quantities.
		{"樱花十棵三十块一棵", l("樱花", "1"), []int{0}},
		{"樱花十棵三十块一棵", l("樱花", "10"), nil},
		{"螺丝两千个三毛钱一个", l("螺丝", "1"), []int{0}},
		{"螺丝两千个三毛钱一个", l("螺丝", "2000"), nil},
		{"螺丝两千个一个三毛钱", l("螺丝", "1"), []int{0}},
		// Counting words are not quantities; a colon starts a new clause.
		{"帮我进两种货：可乐三箱，雪碧五箱", l("可乐", "2", "雪碧", "5"), []int{0}},
		{"帮我进两种货：可乐三箱，雪碧五箱", l("可乐", "3", "雪碧", "5"), nil},
		{"进第二批货可乐三箱雪碧五箱", l("可乐", "2", "雪碧", "3"), []int{0, 1}},
		{"进第二批货可乐三箱雪碧五箱", l("可乐", "3", "雪碧", "5"), nil},
		// A list separator does not end the scope of 各.
		{"樱花、桃树各十棵", l("樱花", "10", "桃树", "10"), nil},
		{"樱花、桃树各十棵", l("樱花", "5", "桃树", "10"), []int{0}},
	}
	for _, c := range cases {
		got := map[int]bool{}
		for _, f := range CheckSpokenQuantities(c.said, c.lines) {
			got[f.Line] = true
		}
		want := map[int]bool{}
		for _, w := range c.want {
			want[w] = true
		}
		ok := len(got) == len(want)
		for w := range want {
			ok = ok && got[w]
		}
		if !ok {
			t.Errorf("%q %+v: flagged %v want %v", c.said, c.lines, got, want)
		}
	}
}

// TestCheckSpokenQuantities_AmendmentProbes pins the re-statements that once
// slipped through: a draft that kept the old number after "错了" / "算了还是",
// a product re-said in a later message, and "再加" where either the total or the
// increment could be in the draft.
func TestCheckSpokenQuantities_AmendmentProbes(t *testing.T) {
	cases := []struct {
		said string
		qty  string
		want bool // flagged
	}{
		{"可乐三箱，错了，五箱", "3", true},
		{"可乐三箱，错了，五箱", "5", false},
		{"可乐三箱，算了，还是五箱", "3", true},
		{"可乐三箱，算了，还是五箱", "5", false},
		{"可乐三箱\n可乐要十箱", "3", true},
		{"可乐三箱\n可乐要十箱", "10", false},
		{"可乐三箱，再加两箱", "2", true},
		{"可乐三箱，再加两箱", "5", true},
		{"可乐三箱，再加雪碧两箱", "3", false},
	}
	for _, c := range cases {
		lines := []CheckLine{{ProductName: "可乐", Qty: dec(c.qty)}}
		if strings.Contains(c.said, "雪碧") {
			lines = append(lines, CheckLine{ProductName: "雪碧", Qty: dec("2")})
		}
		got := false
		for _, f := range CheckSpokenQuantities(c.said, lines) {
			if f.Line == 0 {
				got = true
			}
		}
		if got != c.want {
			t.Errorf("%q draft %s: flagged=%v want %v", c.said, c.qty, got, c.want)
		}
	}
}

func TestCheckSpokenQuantities_SwappedQuantitiesFlagged(t *testing.T) {
	lines := []CheckLine{{ProductName: "樱花", Qty: dec("20")}, {ProductName: "桃树", Qty: dec("10")}}
	flags := CheckSpokenQuantities("樱花十棵，桃树二十棵", lines)
	if len(flags) != 2 {
		t.Fatalf("swap must flag both lines, got %+v", flags)
	}
}

func TestCheckSpokenQuantities_NoSpokenNumber_NotChecked(t *testing.T) {
	lines := []CheckLine{{ProductName: "樱花", Qty: dec("37")}}
	if flags := CheckSpokenQuantities("帮我补货", lines); len(flags) != 0 {
		t.Fatalf("computed quantities must not be flagged: %+v", flags)
	}
}

func TestCheckSpokenQuantities_NonPositiveFlagged(t *testing.T) {
	for _, q := range []string{"0", "-3"} {
		flags := CheckSpokenQuantities("帮我补货", []CheckLine{{ProductName: "樱花", Qty: dec(q)}})
		if len(flags) != 1 || flags[0].Code != FlagQtyInvalid {
			t.Fatalf("qty %s: %+v", q, flags)
		}
	}
}

func TestCheckHistoryQty(t *testing.T) {
	hist := map[string]decimal.Decimal{"樱花": dec("10")}
	cases := []struct {
		qty  string
		flag bool
	}{{"30", false}, {"30.01", true}, {"300", true}, {"1", false}}
	for _, c := range cases {
		flags := CheckHistoryQty([]CheckLine{{ProductName: "樱花", Qty: dec(c.qty)}}, hist)
		if (len(flags) == 1) != c.flag {
			t.Errorf("qty %s: flags=%+v want flagged=%v", c.qty, flags, c.flag)
		}
	}
	// No history for the product: never flagged.
	if f := CheckHistoryQty([]CheckLine{{ProductName: "桃树", Qty: dec("99999")}}, hist); len(f) != 0 {
		t.Fatalf("no history must not flag: %+v", f)
	}
}

func TestCheckPriceDeviation(t *testing.T) {
	last := map[string]decimal.Decimal{"樱花": dec("20")}
	price := func(s string) *decimal.Decimal { d := dec(s); return &d }
	cases := []struct {
		price string
		flag  bool
	}{{"30", false}, {"10", false}, {"30.01", true}, {"9.99", true}, {"20", false}}
	for _, c := range cases {
		flags := CheckPriceDeviation([]CheckLine{{ProductName: "樱花", Qty: dec("1"), UnitPrice: price(c.price)}}, last)
		if (len(flags) == 1) != c.flag {
			t.Errorf("price %s: flags=%+v want flagged=%v", c.price, flags, c.flag)
		}
	}
	if f := CheckPriceDeviation([]CheckLine{{ProductName: "樱花", Qty: dec("1")}}, last); len(f) != 0 {
		t.Fatalf("unpriced line must be skipped: %+v", f)
	}
}

func TestCheckTotalAndSpokenTotal(t *testing.T) {
	p := func(s string) *decimal.Decimal { d := dec(s); return &d }
	lines := []CheckLine{
		{ProductName: "樱花", Qty: dec("10"), UnitPrice: p("30")},
		{ProductName: "桃树", Qty: dec("5"), UnitPrice: p("40")},
	}
	total, ok := SpokenTotal("樱花十棵三十块，桃树五棵四十块，一共五百元")
	if !ok || !total.Equal(dec("500")) {
		t.Fatalf("SpokenTotal=%s ok=%v", total, ok)
	}
	if f := CheckTotal(lines, total); len(f) != 0 {
		t.Fatalf("matching total flagged: %+v", f)
	}
	if f := CheckTotal(lines, dec("450")); len(f) != 1 || f[0].Line != -1 || f[0].Code != FlagTotalMismatch {
		t.Fatalf("mismatch not flagged: %+v", f)
	}
	lines[1].UnitPrice = nil
	if f := CheckTotal(lines, dec("450")); len(f) != 0 {
		t.Fatalf("unpriced lines must skip the total check: %+v", f)
	}
	if _, ok := SpokenTotal("樱花十棵"); ok {
		t.Fatal("no total said")
	}
}

func TestAttachDraftFlags_MarksRowsAndDescription(t *testing.T) {
	plan := &domainai.Plan{
		Type:    domainai.PlanTypeCreatePurchase,
		Payload: map[string]interface{}{"items": []purchaseDraftItem{{ProductName: "樱花", Qty: 20}, {ProductName: "桃树", Qty: 10}}},
		Preview: domainai.PlanPreview{
			Description: "Create purchase draft with 2 line items",
			SampleRows:  []domainai.SampleRow{{Name: "樱花", After: "qty: 20.00"}, {Name: "桃树", After: "qty: 10.00"}},
		},
	}
	checkPlanAgainstSpoken(plan, "樱花十棵，桃树十棵")
	if plan.Payload["require_line_confirm"] != true {
		t.Fatalf("flagged plan must require line confirm: %+v", plan.Payload)
	}
	flags, _ := plan.Payload["flags"].([]map[string]interface{})
	if len(flags) != 1 || flags[0]["line"] != 0 {
		t.Fatalf("flags=%+v", plan.Payload["flags"])
	}
	if !strings.HasSuffix(plan.Preview.SampleRows[0].After, flagMark) || strings.HasSuffix(plan.Preview.SampleRows[1].After, flagMark) {
		t.Fatalf("rows=%+v", plan.Preview.SampleRows)
	}
	if !strings.Contains(plan.Preview.Description, "1 项需要逐行核对") {
		t.Fatalf("desc=%q", plan.Preview.Description)
	}
	// Idempotent marker: a second pass must not stack suffixes.
	attachDraftFlags(plan, []DraftFlag{{Line: 0, Code: FlagQtyOverHist, Message: "x"}})
	if strings.Count(plan.Preview.SampleRows[0].After, "⚠") != 1 {
		t.Fatalf("row marked twice: %q", plan.Preview.SampleRows[0].After)
	}
	if strings.Count(plan.Preview.Description, "（") != 1 {
		t.Fatalf("desc stacked: %q", plan.Preview.Description)
	}
}

func TestCheckPlanAgainstSpoken_CleanPlanUntouched(t *testing.T) {
	plan := &domainai.Plan{
		Type:    domainai.PlanTypeCreatePurchase,
		Payload: map[string]interface{}{"items": []purchaseDraftItem{{ProductName: "樱花", Qty: 10}}},
		Preview: domainai.PlanPreview{Description: "Create purchase draft with 1 line items", SampleRows: []domainai.SampleRow{{Name: "樱花", After: "qty: 10.00"}}},
	}
	checkPlanAgainstSpoken(plan, "樱花十棵")
	if _, has := plan.Payload["flags"]; has || plan.Preview.Description != "Create purchase draft with 1 line items" {
		t.Fatalf("clean plan modified: %+v", plan)
	}
}

func TestProposePurchaseDraft_FlagsQtyOverHistory(t *testing.T) {
	reg := NewRegistry(nil, nil, histSales{rows: []SaleRow{{ProductName: "樱花", Qty: decimal.NewFromInt(10)}}}, nil)
	plan, _, err := reg.proposeCreatePurchaseDraft(context.Background(), uuid.New(),
		`{"items":[{"product_name":"樱花","qty":100},{"product_name":"桃树","qty":100}]}`)
	if err != nil {
		t.Fatal(err)
	}
	flags, _ := plan.Payload["flags"].([]map[string]interface{})
	if len(flags) != 1 || flags[0]["code"] != FlagQtyOverHist || flags[0]["line"] != 0 {
		t.Fatalf("flags=%+v", plan.Payload["flags"])
	}
}

type histSales struct{ rows []SaleRow }

func (h histSales) ListRecentSaleLines(context.Context, uuid.UUID, int) ([]SaleRow, error) {
	return h.rows, nil
}
func (histSales) MatchCustomers(context.Context, uuid.UUID, string) ([]CustomerRef, error) {
	return nil, nil
}
func (histSales) ListCustomerSales(context.Context, uuid.UUID, CustomerRef, int) ([]CustomerBill, error) {
	return nil, nil
}

func TestAttachDraftFlags_KeepsFullWidthParenInOriginalDescription(t *testing.T) {
	plan := &domainai.Plan{
		Payload: map[string]interface{}{},
		Preview: domainai.PlanPreview{Description: "补货（含样品）", SampleRows: []domainai.SampleRow{{Name: "樱花", After: "qty: 20.00"}}},
	}
	attachDraftFlags(plan, []DraftFlag{{Line: 0, Code: FlagQtyMismatch, Message: "x"}})
	attachDraftFlags(plan, []DraftFlag{{Line: 0, Code: FlagQtyOverHist, Message: "y"}})
	if plan.Preview.Description != "补货（含样品）（1 项需要逐行核对）" {
		t.Fatalf("desc=%q", plan.Preview.Description)
	}
}

func TestProposePurchaseDraft_HistoryComparesWithTotalSold(t *testing.T) {
	// Single sale lines of 2 pieces, 40 sold in all: restocking 50 is normal,
	// restocking 500 is not.
	var rows []SaleRow
	for i := 0; i < 20; i++ {
		rows = append(rows, SaleRow{ProductName: "樱花", Qty: decimal.NewFromInt(2)})
	}
	reg := NewRegistry(nil, nil, histSales{rows: rows}, nil)
	plan, _, err := reg.proposeCreatePurchaseDraft(context.Background(), uuid.New(),
		`{"items":[{"product_name":"樱花","qty":50}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, has := plan.Payload["flags"]; has {
		t.Fatalf("normal restock flagged: %+v", plan.Payload["flags"])
	}
	plan, _, _ = reg.proposeCreatePurchaseDraft(context.Background(), uuid.New(),
		`{"items":[{"product_name":"樱花","qty":500}]}`)
	if _, has := plan.Payload["flags"]; !has {
		t.Fatal("500 against 40 sold must be flagged")
	}
}

func TestCrossCheckExtracted_TotalMismatchFlagsDocument(t *testing.T) {
	p := func(s string) *decimal.Decimal { d := dec(s); return &d }
	plan := &domainai.Plan{
		Type:    domainai.PlanTypeCreatePurchase,
		Payload: map[string]interface{}{"items": []purchaseDraftItem{{ProductName: "樱花", Qty: 10}}},
		Preview: domainai.PlanPreview{Description: "d", SampleRows: []domainai.SampleRow{{Name: "樱花", After: "qty: 10.00"}}},
	}
	crossCheckExtracted(plan, []ExtractedLine{{ProductName: "樱花", Qty: dec("10"), UnitPrice: p("30")}}, "樱花十棵三十块，一共五百元")
	flags, _ := plan.Payload["flags"].([]map[string]interface{})
	if len(flags) != 1 || flags[0]["code"] != FlagTotalMismatch || flags[0]["line"] != -1 {
		t.Fatalf("flags=%+v", flags)
	}
}

// corpusCase is one hand-written dictation from testdata/spoken_corpus.json:
// the draft lines are "name:qty" and flag lists the lines that must be flagged
// (decided by reading the sentence, not by running the checker).
type corpusCase struct {
	Said  string   `json:"said"`
	Lines []string `json:"lines"`
	Flag  []int    `json:"flag"`
}

func TestCheckSpokenQuantities_HandWrittenCorpus(t *testing.T) {
	raw, err := os.ReadFile("testdata/spoken_corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []corpusCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	distinct := map[string]bool{}
	for _, c := range cases {
		distinct[c.Said] = true
	}
	if len(distinct) < 80 {
		t.Fatalf("corpus must hold >= 80 distinct sentences, got %d", len(distinct))
	}
	for _, c := range cases {
		var lines []CheckLine
		for _, l := range c.Lines {
			i := strings.LastIndex(l, ":")
			lines = append(lines, CheckLine{ProductName: l[:i], Qty: dec(l[i+1:])})
		}
		got := []int{}
		seen := map[int]bool{}
		for _, f := range CheckSpokenQuantities(c.Said, lines) {
			if !seen[f.Line] {
				seen[f.Line] = true
				got = append(got, f.Line)
			}
		}
		sort.Ints(got)
		want := append([]int{}, c.Flag...)
		sort.Ints(want)
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%q %v: flagged %v want %v", c.Said, c.Lines, got, want)
		}
	}
}

// TestCheckSpokenQuantities_PoolFallbackFlagsAllPooledLines: when two or more
// lines can only draw from the leftover pool and the pool holds different
// numbers, which line owns which cannot be told, so all of them are flagged.
func TestCheckSpokenQuantities_PoolFallbackFlagsAllPooledLines(t *testing.T) {
	lines := []CheckLine{{ProductName: "汽水", Qty: dec("5")}, {ProductName: "果汁", Qty: dec("3")}}
	got := map[int]bool{}
	for _, f := range CheckSpokenQuantities("进三箱，五箱", lines) {
		got[f.Line] = true
	}
	if !got[0] || !got[1] {
		t.Fatalf("both pooled lines must be flagged, got %v", got)
	}
}

// The plan card shows the description and rows to the shopkeeper as-is.
func TestProposePurchaseDraft_PreviewIsChinese(t *testing.T) {
	reg := NewRegistry(nil, nil, histSales{}, nil)
	plan, _, err := reg.proposeCreatePurchaseDraft(context.Background(), uuid.New(),
		`{"items":[{"product_name":"樱花","qty":10},{"product_name":"桃树","qty":5}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if want := "新建采购草稿，共 2 行"; plan.Preview.Description != want {
		t.Fatalf("description=%q want %q", plan.Preview.Description, want)
	}
	if got := plan.Preview.SampleRows[0].After; got != "数量 10.00" {
		t.Fatalf("after=%q", got)
	}
}

func TestCheckSpokenQuantities_AddWordGetsItsOwnMessage(t *testing.T) {
	flags := CheckSpokenQuantities("可乐三箱，再加两箱", []CheckLine{{ProductName: "可乐", Qty: dec("5")}})
	want := "你说了「再加」，请确认「可乐」的 5 是加完后的总数"
	if len(flags) != 1 || flags[0].Message != want {
		t.Fatalf("flags=%+v want message %q", flags, want)
	}
}

func TestCheckSpokenQuantities_CancelWithoutNewNumberFlagsPreviousLine(t *testing.T) {
	flags := CheckSpokenQuantities("可乐十箱，不要了", []CheckLine{{ProductName: "可乐", Qty: dec("10")}})
	if len(flags) != 1 || flags[0].Line != 0 || flags[0].Code != FlagQtyMismatch {
		t.Fatalf("flags=%+v", flags)
	}
	if !strings.Contains(flags[0].Message, "不要了") {
		t.Fatalf("message=%q", flags[0].Message)
	}
}

func TestCheckSpokenQuantities_CancelProbes(t *testing.T) {
	two := []CheckLine{{ProductName: "可乐", Qty: dec("10")}, {ProductName: "雪碧", Qty: dec("5")}}
	for name, tc := range map[string]struct {
		text string
		want []int
	}{
		"earlier cancel word is not a name": {"可乐十箱，雪碧五箱，雪碧不要了，可乐也不要了", []int{0, 1}},
		"都不要了 sweeps every line":            {"可乐十箱，雪碧五箱，都不要了", []int{0, 1}},
		"全部不要了":                             {"可乐十箱，雪碧五箱，全部不要了", []int{0, 1}},
		"不用了":                               {"可乐十箱，雪碧五箱，雪碧不用了", []int{1}},
		"去掉 after name":                     {"可乐十箱，雪碧五箱，雪碧去掉", []int{1}},
		"删了":                                {"可乐十箱，雪碧五箱，可乐删了", []int{0}},
		"都不要了 but renewed":                  {"可乐十箱，雪碧五箱，都不要了，可乐十箱", []int{1}},
	} {
		got := []int{}
		for _, f := range CheckSpokenQuantities(tc.text, two) {
			got = append(got, f.Line)
		}
		if !reflect.DeepEqual(got, tc.want) && !(len(got) == 0 && len(tc.want) == 0) {
			t.Fatalf("%s: lines=%v want %v", name, got, tc.want)
		}
	}
}
