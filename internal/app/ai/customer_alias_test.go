package ai

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
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
	for _, s := range []string{"王", "板", "赵"} {
		found := false
		for _, g := range got {
			found = found || g == s
		}
		if !found {
			t.Errorf("missing %q in %v", s, got)
		}
	}
	if got := aliasSurnamesInText("今天有哪些订单待发货"); len(got) != 0 {
		t.Errorf("want none, got %v", got)
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
