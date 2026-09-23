package ai

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/memorusclient"
	"github.com/shopspring/decimal"
)

var (
	zhangID   = uuid.MustParse("00000000-0000-0000-0000-00000000000a")
	zhangfeng = uuid.MustParse("00000000-0000-0000-0000-00000000000b")
	liID      = uuid.MustParse("00000000-0000-0000-0000-00000000000c")
)

// --- customer_recent_purchases ---

type custSaleRepo struct {
	cxSaleRepo
	cands  []CustomerRef
	bills  []CustomerBill
	listed *CustomerRef
}

func (f *custSaleRepo) MatchCustomers(_ context.Context, _ uuid.UUID, _ string) ([]CustomerRef, error) {
	return f.cands, nil
}

func (f *custSaleRepo) ListCustomerSales(_ context.Context, _ uuid.UUID, c CustomerRef, _ int) ([]CustomerBill, error) {
	f.listed = &c
	return f.bills, nil
}

func TestPickCustomer(t *testing.T) {
	zhang := CustomerRef{ID: zhangID, Name: "张三"}
	feng := CustomerRef{ID: zhangfeng, Name: "张三丰"}
	dupZhang := CustomerRef{ID: liID, Name: "张三"}
	walkIn := CustomerRef{Name: "王五"}
	cases := []struct {
		name      string
		q         string
		cands     []CustomerRef
		want      *CustomerRef
		ambiguous int
	}{
		{"exact beats containing", "张三", []CustomerRef{zhang, feng}, &zhang, 0},
		{"single partial match", "三丰", []CustomerRef{feng}, &feng, 0},
		{"several partial matches", "张", []CustomerRef{zhang, feng}, nil, 2},
		{"two partners share the exact name", "张三", []CustomerRef{zhang, dupZhang, feng}, nil, 2},
		{"walk-in only", "王五", []CustomerRef{walkIn}, &walkIn, 0},
		{"partner beats walk-in of same name", "张三", []CustomerRef{{Name: "张三"}, zhang}, &zhang, 0},
		{"nothing", "赵六", nil, nil, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, amb := pickCustomer(c.q, c.cands)
			if len(amb) != c.ambiguous {
				t.Fatalf("ambiguous = %v, want %d", amb, c.ambiguous)
			}
			if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestCustomerRecentPurchases_ReturnsBillsOfTheResolvedCustomer(t *testing.T) {
	sales := &custSaleRepo{
		cands: []CustomerRef{{ID: zhangID, Name: "张三", Code: "C001"}, {ID: zhangfeng, Name: "张三丰"}},
		bills: []CustomerBill{{
			BillNo: "SL-0002", BillDate: time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC),
			Total: decimal.RequireFromString("64"),
			Lines: []CustomerBillLine{{ProductName: "矿泉水", Unit: "箱", Qty: decimal.NewFromInt(2),
				UnitPrice: decimal.NewFromInt(32), Amount: decimal.NewFromInt(64)}},
		}},
	}
	r := NewRegistry(&cxProductRepo{}, &cxStockRepo{}, sales, &cxExchangeRepo{})
	res := callTool(r, uuid.New(), "customer_recent_purchases", `{"customer":"张三"}`)
	if sales.listed == nil || sales.listed.ID != zhangID {
		t.Fatalf("listed %v, want 张三", sales.listed)
	}
	for _, want := range []string{`"found":true`, `"SL-0002"`, `"2026-09-20"`, `"矿泉水"`, `"unit_price":"32.00"`, `"amount":"64.00"`} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("result lacks %s: %s", want, res.Content)
		}
	}
}

func TestCustomerRecentPurchases_AmbiguousNameListsCandidates(t *testing.T) {
	sales := &custSaleRepo{cands: []CustomerRef{{ID: zhangID, Name: "张三"}, {ID: zhangfeng, Name: "张三丰"}}}
	r := NewRegistry(&cxProductRepo{}, &cxStockRepo{}, sales, &cxExchangeRepo{})
	res := callTool(r, uuid.New(), "customer_recent_purchases", `{"customer":"张"}`)
	if sales.listed != nil {
		t.Fatalf("must not pick a customer, listed %v", sales.listed)
	}
	if !strings.Contains(res.Content, `"ambiguous":true`) || !strings.Contains(res.Content, "张三丰") {
		t.Fatalf("want candidates, got %s", res.Content)
	}
}

func TestCustomerRecentPurchases_RequiresCustomer(t *testing.T) {
	r := NewRegistry(&cxProductRepo{}, &cxStockRepo{}, &custSaleRepo{}, &cxExchangeRepo{})
	res := callTool(r, uuid.New(), "customer_recent_purchases", `{"customer":"  "}`)
	if !strings.Contains(res.Content, "error") {
		t.Fatalf("want error, got %s", res.Content)
	}
}

// --- customer attribution of memories ---

type fakeResolver struct {
	refs []CustomerRef
	err  error
}

func (f fakeResolver) MatchCustomersInText(_ context.Context, _ uuid.UUID, text string) ([]CustomerRef, error) {
	var out []CustomerRef
	for _, c := range f.refs {
		if strings.Contains(text, c.Name) {
			out = append(out, c)
		}
	}
	return out, f.err
}

func TestResolveCustomer(t *testing.T) {
	cr := fakeResolver{refs: []CustomerRef{
		{ID: zhangID, Name: "张三"}, {ID: zhangfeng, Name: "张三丰"}, {ID: liID, Name: "李四"},
	}}
	cases := []struct {
		text string
		want uuid.UUID // uuid.Nil = no customer
	}{
		{"张三喜欢少糖", zhangID},
		{"张三丰每次都要发票", zhangfeng},
		{"张三丰和张三是亲戚", uuid.Nil},
		{"张三和李四都喜欢少糖", uuid.Nil},
		{"今天有哪些订单待发货", uuid.Nil},
	}
	for _, c := range cases {
		got := ResolveCustomer(context.Background(), cr, uuid.New(), c.text)
		if (got == nil && c.want != uuid.Nil) || (got != nil && got.ID != c.want) {
			t.Errorf("%q → %v, want %v", c.text, got, c.want)
		}
	}
	dup := fakeResolver{refs: []CustomerRef{{ID: zhangID, Name: "张三"}, {ID: liID, Name: "张三"}}}
	if got := ResolveCustomer(context.Background(), dup, uuid.New(), "张三要发票"); got != nil {
		t.Errorf("two customers named 张三 must not resolve, got %v", got)
	}
	if got := ResolveCustomer(context.Background(), fakeResolver{err: errors.New("db down")}, uuid.New(), "张三"); got != nil {
		t.Errorf("error must resolve to nil, got %v", got)
	}
	if got := ResolveCustomer(context.Background(), nil, uuid.New(), "张三"); got != nil {
		t.Errorf("nil resolver must resolve to nil, got %v", got)
	}
}

func TestMemoryWriteMeta_TagsTheCustomer(t *testing.T) {
	tenant := uuid.New()
	meta := MemoryWriteMeta(tenant, &CustomerRef{ID: zhangID})
	if meta[MemorySubjectKey] != CustomerSubject(zhangID) {
		t.Fatalf("meta = %v", meta)
	}
	if _, ok := MemoryWriteMeta(tenant, nil)[MemorySubjectKey]; ok {
		t.Fatal("untagged write must not carry a subject")
	}
}

type filteringMemory struct {
	similar  []memorusclient.Memory
	own      []memorusclient.Memory
	filtered map[string]string
}

func (f *filteringMemory) Search(context.Context, string, string, int) ([]memorusclient.Memory, error) {
	return f.similar, nil
}

func (f *filteringMemory) Add(context.Context, string, string, map[string]any) (*memorusclient.Memory, error) {
	return nil, nil
}

func (f *filteringMemory) SearchWithFilter(_ context.Context, _, _ string, _ int, metaEq map[string]string) ([]memorusclient.Memory, error) {
	f.filtered = metaEq
	return f.own, nil
}

func hit(id, content string, score float64, subject string, superseded bool) memorusclient.Memory {
	meta := map[string]any{}
	if subject != "" {
		meta["metadata"] = map[string]any{MemorySubjectKey: subject}
	}
	if superseded {
		meta["superseded_by"] = "newer"
	}
	return memorusclient.Memory{ID: id, Content: content, Score: score, Metadata: meta}
}

func TestAugmentWithCustomerMemory_KeepsOtherCustomersOut(t *testing.T) {
	zhang, li := CustomerSubject(zhangID), CustomerSubject(liID)
	mc := &filteringMemory{
		similar: []memorusclient.Memory{
			hit("l1", "李四喜欢全糖", 0.9, li, false),
			hit("z1", "张三喜欢少糖", 0.8, zhang, false),
			hit("g1", "周末不安排送货", 0.7, "", false),
		},
		own: []memorusclient.Memory{
			hit("z1", "张三喜欢少糖", 0.8, zhang, false),
			hit("z2", "张三只用微信付款", 0.1, zhang, false),
			hit("z0", "张三只用现金付款", 0.2, zhang, true),
		},
	}
	out := AugmentWithCustomerMemory(mc, context.Background(), "u", "接待张三要注意什么", &CustomerRef{ID: zhangID, Name: "张三"})
	if mc.filtered[MemorySubjectKey] != zhang {
		t.Fatalf("own memories not fetched by subject: %v", mc.filtered)
	}
	for _, want := range []string{"张三喜欢少糖", "张三只用微信付款", "周末不安排送货"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	for _, bad := range []string{"李四", "现金"} {
		if strings.Contains(out, bad) {
			t.Errorf("must not contain %q:\n%s", bad, out)
		}
	}
	if strings.Count(out, "张三喜欢少糖") != 1 {
		t.Errorf("duplicate hit injected twice:\n%s", out)
	}
	if strings.Index(out, "张三") > strings.Index(out, "周末") {
		t.Errorf("the customer's own facts go first:\n%s", out)
	}
}

func TestAugmentWithCustomerMemory_NoCustomerIsUnchanged(t *testing.T) {
	mc := &filteringMemory{similar: []memorusclient.Memory{hit("l1", "李四喜欢全糖", 0.9, CustomerSubject(liID), false)}}
	out := AugmentWithCustomerMemory(mc, context.Background(), "u", "糖", nil)
	if mc.filtered != nil {
		t.Fatal("no filtered search without a customer")
	}
	if !strings.Contains(out, "李四喜欢全糖") {
		t.Fatalf("unfiltered recall must be as before:\n%s", out)
	}
}
