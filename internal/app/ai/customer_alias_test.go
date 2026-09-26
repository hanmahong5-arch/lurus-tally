package ai

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmclient"
)

func TestAliasSurname(t *testing.T) {
	for in, want := range map[string]string{
		"老张": "张", "小王": "王", "阿李": "李", "王老板": "王", "李总": "李", "周姐": "周",
		"孙师傅": "孙", "吴老板娘": "吴",
		"张三": "", "老张三": "", "老": "", "老板": "", "小明明": "", "张": "",
	} {
		if got := aliasSurname(in); got != want {
			t.Errorf("aliasSurname(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAliasSurnamesInText(t *testing.T) {
	got := aliasSurnamesInText("王老板说小赵明天来")
	if want := []string{"王", "赵"}; !reflect.DeepEqual(surnamesOf(got), want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got[0].Form != "王老板" || got[1].Form != "小赵" {
		t.Errorf("forms = %v", got)
	}
	if got := aliasSurnamesInText("今天有哪些订单待发货"); len(got) != 0 {
		t.Errorf("want none, got %v", got)
	}
}

func surnamesOf(ms []aliasMention) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.Surname)
	}
	return out
}

// Everyday words that begin with 老/小/阿 or end in 总/姐/哥 are not address
// forms, even when the character next to the prefix is a real surname
// (顾 时 区 包 麦 高 常 单 米). With exactly one customer of that surname the
// old scan filed the note under them without a word to the owner.
func TestResolveCustomer_EverydayWordsAreNotAddressForms(t *testing.T) {
	gu, shi, qu, bao, mai, gao, chang := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	repo := &aliasRepo{all: []CustomerRef{
		{ID: gu, Name: "顾建国"}, {ID: shi, Name: "时文"}, {ID: qu, Name: "区志强"}, {ID: bao, Name: "包红"},
		{ID: mai, Name: "麦家宝"}, {ID: gao, Name: "高明"}, {ID: chang, Name: "常青"},
	}}
	for _, text := range []string{
		"老顾客一律打九五折",
		"送货按小时收费",
		"小区门口的货放门卫",
		"小包装白糖卖完了",
		"小麦粉进价涨了",
		"这个月要提高总销量",
		"老常客来了先上茶",
		"老板娘说明天休息",
		"小票要打印两联",
		"老样子，照旧送",
		"小时候的事",
		"老地方见",
		"汇总一下本周的单",
	} {
		if got := ResolveCustomer(context.Background(), repo, uuid.New(), text); got != nil {
			t.Errorf("%q was filed under %s", text, got.Name)
		}
	}
	for text, want := range map[string]uuid.UUID{
		"老顾说以后要开发票": gu,
		"高总下午来提货":   gao,
		"麦姐的货放后门":   mai,
		"区老板要专票":    qu,
		"小包，明天来拿货":  bao,
	} {
		got := ResolveCustomer(context.Background(), repo, uuid.New(), text)
		if got == nil || got.ID != want {
			t.Errorf("%q → %v, want the one customer with that surname", text, got)
		}
	}
}

// A note filed by an address form is marked as such, and the model is told
// whom it went to so it can say so ("已记在 顾建国 名下").
func TestChat_AliasAttributionIsMarkedAndAnnounced(t *testing.T) {
	for _, stream := range []bool{false, true} {
		mc := &recordingMemory{}
		o, reqs := factOrchestrator(t, nil, mc, llmclient.Message{Role: "assistant", Content: "好的"})
		o = o.WithCustomerResolver(&aliasRepo{all: []CustomerRef{{ID: liID, Name: "李四"}}})
		in := ChatInput{TenantID: uuid.New(), UserMessage: "老李以后都要塑料袋"}
		var err error
		if stream {
			_, err = o.StreamChat(context.Background(), in, func(string) {})
		} else {
			_, err = o.Chat(context.Background(), in)
		}
		if err != nil {
			t.Fatal(err)
		}
		w := mc.settled()
		if len(w) != 1 || w[0].meta[MemorySubjectKey] != CustomerSubject(liID) ||
			w[0].meta[MemoryAttributionKey] != AttributionAlias || w[0].meta[MemoryAliasKey] != "老李" {
			t.Fatalf("stream=%v: writes = %+v", stream, w)
		}
		var sys strings.Builder
		for _, m := range reqs()[0].Messages {
			if s, ok := m.Content.(string); ok && m.Role == "system" {
				sys.WriteString(s)
			}
		}
		if !strings.Contains(sys.String(), "老李") || !strings.Contains(sys.String(), "李四") {
			t.Errorf("stream=%v: the model is not told 老李 was taken as 李四:\n%s", stream, sys.String())
		}
	}

	// Named in full: no alias mark, no note. A question: nothing is filed, no note.
	for text, writes := range map[string]int{"李四以后都要塑料袋": 1, "老李上次买了什么？": 0} {
		mc := &recordingMemory{}
		o, reqs := factOrchestrator(t, nil, mc, llmclient.Message{Role: "assistant", Content: "好的"})
		o = o.WithCustomerResolver(&aliasRepo{all: []CustomerRef{{ID: liID, Name: "李四"}}})
		if _, err := o.Chat(context.Background(), ChatInput{TenantID: uuid.New(), UserMessage: text}); err != nil {
			t.Fatal(err)
		}
		if w := mc.settled(); len(w) != writes || (writes == 1 && w[0].meta[MemoryAttributionKey] != nil) {
			t.Fatalf("%q: writes = %+v", text, w)
		}
		for _, m := range reqs()[0].Messages {
			if s, ok := m.Content.(string); ok && strings.Contains(s, "taken to mean") {
				t.Errorf("%q needs no attribution note: %s", text, s)
			}
		}
	}
}

func TestRememberCustomerFact_AliasIsMarked(t *testing.T) {
	mc := &recordingMemory{}
	reg := NewRegistry(&cxProductRepo{}, &cxStockRepo{}, &aliasRepo{all: []CustomerRef{{ID: liID, Name: "李四"}}}, &cxExchangeRepo{})
	o := NewOrchestrator(nil, reg, newCxPlanStore(), "m").WithMemory(mc)
	var remembered bool
	res := o.dispatch(context.Background(), uuid.New(),
		factCall(`{"customer":"老李","attribute":"taste","value":"少糖","quote":"老李要少糖"}`).ToolCalls[0], &remembered)
	w := mc.settled()
	if len(w) != 1 || w[0].meta[MemoryAttributionKey] != AttributionAlias || w[0].meta[MemoryAliasKey] != "老李" {
		t.Fatalf("writes = %+v (result %s)", w, res.Content)
	}
}

// aliasRepo answers MatchCustomers by substring, like the SQL repo.
type aliasRepo struct {
	custSaleRepo
	all []CustomerRef
}

func (f *aliasRepo) MatchCustomers(_ context.Context, _ uuid.UUID, name string) ([]CustomerRef, error) {
	var out []CustomerRef
	for _, c := range f.all {
		if strings.Contains(c.Name, name) {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *aliasRepo) MatchCustomersInText(_ context.Context, _ uuid.UUID, text string) ([]CustomerRef, error) {
	var out []CustomerRef
	for _, c := range f.all {
		if strings.Contains(text, c.Name) {
			out = append(out, c)
		}
	}
	return out, nil
}

func aliasFixture() *aliasRepo {
	return &aliasRepo{all: []CustomerRef{
		{ID: zhangID, Name: "张三"}, {ID: zhangfeng, Name: "张三丰"}, {ID: liID, Name: "李四"},
		{Name: "王五"}, // walk-in only: never an alias target
	}}
}

func TestCustomerRecentPurchases_AddressForms(t *testing.T) {
	repo := aliasFixture()
	r := NewRegistry(&cxProductRepo{}, &cxStockRepo{}, repo, &cxExchangeRepo{})

	res := callTool(r, uuid.New(), "customer_recent_purchases", `{"customer":"老李"}`)
	if repo.listed == nil || repo.listed.ID != liID || !strings.Contains(res.Content, `"resolved_from":"老李"`) {
		t.Fatalf("老李 → 李四 expected; listed %v, got %s", repo.listed, res.Content)
	}

	repo.listed = nil
	res = callTool(r, uuid.New(), "customer_recent_purchases", `{"customer":"老张"}`)
	if repo.listed != nil || !strings.Contains(res.Content, `"ambiguous":true`) {
		t.Fatalf("老张 must list 张三/张三丰, got %s", res.Content)
	}

	res = callTool(r, uuid.New(), "customer_recent_purchases", `{"customer":"王老板"}`)
	if !strings.Contains(res.Content, `"found":false`) {
		t.Fatalf("a walk-in name is not an alias target, got %s", res.Content)
	}
}

func TestResolveCustomer_AddressForms(t *testing.T) {
	repo := aliasFixture()
	cases := map[string]uuid.UUID{
		"老李说以后都要塑料袋": liID,
		"李总下午来提货":    liID,
		"老张要发票":      uuid.Nil, // 张三 or 张三丰
		"小时候的事":      uuid.Nil,
		"张三和老李都来了":   zhangID, // a full name wins; the alias is not consulted
		"今天有哪些订单待发货": uuid.Nil,
	}
	for text, want := range cases {
		got := ResolveCustomer(context.Background(), repo, uuid.New(), text)
		var id uuid.UUID
		if got != nil {
			id = got.ID
		}
		if !reflect.DeepEqual(id, want) {
			t.Errorf("%q → %v, want %v", text, got, want)
		}
	}
	// A resolver without MatchCustomers keeps the old behaviour.
	if got := ResolveCustomer(context.Background(), fakeResolver{refs: repo.all}, uuid.New(), "老李来了"); got != nil {
		t.Errorf("want nil without a SurnameMatcher, got %v", got)
	}
}
