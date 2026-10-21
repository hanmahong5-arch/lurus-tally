package ai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	domainai "github.com/hanmahong5-arch/lurus-tally/internal/domain/ai"
)

type previewProducts struct{}

func (previewProducts) SearchProducts(context.Context, uuid.UUID, string) ([]ProductRow, error) {
	return []ProductRow{{ID: uuid.New(), Name: "樱花"}}, nil
}
func (previewProducts) ListAllProducts(context.Context, uuid.UUID) ([]ProductRow, error) {
	return nil, nil
}

func startsASCIILetter(s string) bool {
	return s != "" && (s[0] >= 'a' && s[0] <= 'z' || s[0] >= 'A' && s[0] <= 'Z')
}

// The plan card shows these strings to the shopkeeper as-is: none may open
// with an English word, a bracketed placeholder or a symbol.
func TestProposeTools_PlanCardTextIsChinese(t *testing.T) {
	reg := NewRegistry(previewProducts{}, nil, histSales{}, nil)
	cases := map[string]string{
		"price": `{"filter":"樱花","action":"+10%"}`,
		"stock": `{"filter":"樱花","delta":5}`,
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			var (
				plan   interface{}
				desc   string
				rows   [][3]string
				result string
				err    error
			)
			if name == "price" {
				p, r, e := reg.proposePriceChange(context.Background(), uuid.New(), args)
				plan, result, err = p, r, e
				desc = p.Preview.Description
				for _, sr := range p.Preview.SampleRows {
					rows = append(rows, [3]string{sr.Name, sr.Before, sr.After})
				}
			} else {
				p, r, e := reg.proposeBulkStockAdjust(context.Background(), uuid.New(), args)
				plan, result, err = p, r, e
				desc = p.Preview.Description
				for _, sr := range p.Preview.SampleRows {
					rows = append(rows, [3]string{sr.Name, sr.Before, sr.After})
				}
			}
			if err != nil || plan == nil {
				t.Fatalf("err=%v", err)
			}
			if startsASCIILetter(desc) || desc == "" {
				t.Fatalf("description=%q", desc)
			}
			if len(rows) == 0 {
				t.Fatal("no sample rows")
			}
			for _, r := range rows {
				for _, v := range r[1:] {
					if startsASCIILetter(v) || v == "" || v[0] == '(' || v[0] == '\xce' {
						t.Fatalf("row cell %q", v)
					}
				}
			}
			var m map[string]interface{}
			if err := json.Unmarshal([]byte(result), &m); err != nil {
				t.Fatal(err)
			}
			if msg, _ := m["message"].(string); startsASCIILetter(msg) || msg == "" {
				t.Fatalf("message=%q", msg)
			}
		})
	}
}

func TestProposeCreatePurchaseDraft_PreviewIsChinese(t *testing.T) {
	reg := NewRegistry(previewProducts{}, nil, histSales{}, nil)
	p, _, err := reg.proposeCreatePurchaseDraft(context.Background(), uuid.New(), `{"items":[{"product_name":"樱花","qty":10}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if p.Preview.Description != "新建采购草稿，共 1 行" {
		t.Fatalf("description=%q", p.Preview.Description)
	}
	if len(p.Preview.SampleRows) != 1 || p.Preview.SampleRows[0].After != "数量 10.00" {
		t.Fatalf("rows=%+v", p.Preview.SampleRows)
	}
}

func TestProposePriceChange_ActionIsWordedInChinese(t *testing.T) {
	reg := NewRegistry(previewProducts{}, nil, histSales{}, nil)
	for action, want := range map[string]string{"+10%": "涨价 10%", "-5%": "降价 5%", "=199.00": "改为 199.00 元"} {
		p, _, err := reg.proposePriceChange(context.Background(), uuid.New(), `{"filter":"樱花","action":"`+action+`"}`)
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		if got := p.Preview.SampleRows[0].After; got != want {
			t.Fatalf("%s: after=%q want %q", action, got, want)
		}
	}
	if _, _, err := reg.proposePriceChange(context.Background(), uuid.New(), `{"filter":"樱花","action":"raise ten percent"}`); err == nil {
		t.Fatal("free-text action must be refused")
	}
}

func TestCheckSpokenQuantities_AddWordQuotesWhatWasSaid(t *testing.T) {
	fl := CheckSpokenQuantities("可乐三箱，再来两箱", []CheckLine{{ProductName: "可乐", Qty: dec("5")}})
	if len(fl) != 1 || !strings.Contains(fl[0].Message, "「再来」") {
		t.Fatalf("flags=%+v", fl)
	}
}

type noProducts struct{ previewProducts }

func (noProducts) SearchProducts(context.Context, uuid.UUID, string) ([]ProductRow, error) {
	return nil, nil
}

func TestPropose_ZeroMatchesRaisesNoCard(t *testing.T) {
	reg := NewRegistry(noProducts{}, nil, histSales{}, nil)
	for name, call := range map[string]func() (*domainai.Plan, string, error){
		"price": func() (*domainai.Plan, string, error) {
			return reg.proposePriceChange(context.Background(), uuid.New(), `{"filter":"樱花","action":"+10%"}`)
		},
		"stock": func() (*domainai.Plan, string, error) {
			return reg.proposeBulkStockAdjust(context.Background(), uuid.New(), `{"filter":"樱花","delta":5}`)
		},
	} {
		p, res, err := call()
		if err != nil || p != nil || !strings.Contains(res, "没找到") {
			t.Fatalf("%s: plan=%v res=%s err=%v", name, p, res, err)
		}
	}
}

func TestProposePriceChange_LooseActionSpellings(t *testing.T) {
	reg := NewRegistry(previewProducts{}, nil, histSales{}, nil)
	for action, want := range map[string]string{"10%": "涨价 10%", "+ 10%": "涨价 10%", "- 5 %": "降价 5%"} {
		p, _, err := reg.proposePriceChange(context.Background(), uuid.New(), `{"filter":"樱花","action":"`+action+`"}`)
		if err != nil || p.Preview.SampleRows[0].After != want {
			t.Fatalf("%q: err=%v plan=%+v", action, err, p)
		}
	}
}

func TestStockDeltaText(t *testing.T) {
	for d, want := range map[float64]string{5: "增加 5", -2.5: "减少 2.5"} {
		if got := stockDeltaText(d); got != want {
			t.Fatalf("%v: %q", d, got)
		}
	}
}
