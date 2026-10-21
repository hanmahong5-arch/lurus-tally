package digest_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	handlerdigest "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/digest"
	apptodo "github.com/hanmahong5-arch/lurus-tally/internal/app/todo"
)

type stubTodo struct {
	s   apptodo.Summary
	err error
}

func (s stubTodo) Execute(context.Context, uuid.UUID) (apptodo.Summary, error) { return s.s, s.err }

func todoEngine(uc handlerdigest.TodoUseCase, tenant uuid.UUID) http.Handler {
	h := handlerdigest.New(&stubUseCase{}).WithTodo(uc)
	return newTestEngine(h, tenant)
}

func TestTodoSummary_ShapeAndFailed(t *testing.T) {
	s := apptodo.Summary{
		DraftSales: apptodo.BillSection{Count: 6, Items: []apptodo.BillItem{{ID: uuid.New(), BillNo: "S1", PartnerName: "老王", TotalAmount: decimal.RequireFromString("12.5"), BillDate: time.Now()}}},
		Total:      6,
		Failed:     []string{apptodo.SrcLowStock},
	}
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/todo/summary", nil)
	todoEngine(stubTodo{s: s}, uuid.New()).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code %d", w.Code)
	}
	var got struct {
		DraftSales struct {
			Count int `json:"count"`
			Items []struct {
				BillNo      string `json:"bill_no"`
				TotalAmount string `json:"total_amount"`
			} `json:"items"`
		} `json:"draft_sales"`
		LowStock struct {
			Items []any `json:"items"`
		} `json:"low_stock"`
		Total  int      `json:"total"`
		Failed []string `json:"failed"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.DraftSales.Count != 6 || got.DraftSales.Items[0].TotalAmount != "12.50" || got.Total != 6 {
		t.Fatalf("body: %s", w.Body.String())
	}
	if len(got.Failed) != 1 || got.Failed[0] != "low_stock" || got.LowStock.Items == nil {
		t.Fatalf("body: %s", w.Body.String())
	}
}

func TestTodoSummary_NoTenant401(t *testing.T) {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/todo/summary", nil)
	todoEngine(stubTodo{}, uuid.Nil).ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("code %d", w.Code)
	}
}

func TestTodoSummary_NotMountedWithoutUseCase(t *testing.T) {
	e := newTestEngine(handlerdigest.New(&stubUseCase{}), uuid.New())
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/todo/summary", nil)
	e.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("code %d", w.Code)
	}
}

func getTodo(t *testing.T, s apptodo.Summary) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/todo/summary", nil)
	todoEngine(stubTodo{s: s}, uuid.New()).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code %d", w.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestTodoSummary_BillDateIsShopCalendarDay(t *testing.T) {
	// 2026-10-09 17:00 UTC is already 10-10 01:00 in Beijing.
	at := time.Date(2026, 10, 9, 17, 0, 0, 0, time.UTC)
	old := at
	s := apptodo.Summary{
		DraftSales:       apptodo.BillSection{Count: 1, Items: []apptodo.BillItem{{ID: uuid.New(), BillDate: at}}},
		OverdueCustomers: apptodo.DebtorSection{Count: 1, Items: []apptodo.DebtorItem{{PartnerName: "老王", OldestBillDate: &old}}},
	}
	got := getTodo(t, s)
	bill := got["draft_sales"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if bill["bill_date"] != "2026-10-10" {
		t.Fatalf("bill_date = %v, want 2026-10-10", bill["bill_date"])
	}
	debtor := got["overdue_customers"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if debtor["oldest_bill_date"] != "2026-10-10" {
		t.Fatalf("oldest_bill_date = %v, want 2026-10-10", debtor["oldest_bill_date"])
	}
}

func TestTodoSummary_OnboardingAndAIFlag(t *testing.T) {
	s := apptodo.Summary{Onboarding: apptodo.Onboarding{HasProduct: true, HasDemo: true}}
	got := getTodo(t, s)
	ob := got["onboarding"].(map[string]any)
	if ob["has_product"] != true || ob["has_purchase"] != false || ob["has_sale"] != false ||
		ob["has_payment"] != false || ob["has_demo"] != true {
		t.Fatalf("onboarding = %v", ob)
	}
	if got["ai_enabled"] != false {
		t.Fatalf("ai_enabled = %v, want false when the LLM is not wired", got["ai_enabled"])
	}
}

func TestTodoSummary_AIEnabledFollowsSwitch(t *testing.T) {
	h := handlerdigest.New(&stubUseCase{}).WithTodo(stubTodo{}).WithAIEnabled(func() bool { return true })
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/todo/summary", nil)
	newTestEngine(h, uuid.New()).ServeHTTP(w, req)
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["ai_enabled"] != true {
		t.Fatalf("ai_enabled = %v, want true", got["ai_enabled"])
	}
}

func TestTodoSummary_NegativeStockSection(t *testing.T) {
	pid := uuid.New()
	got := getTodo(t, apptodo.Summary{
		NegativeStock: apptodo.NegativeStockSection{Count: 1, Items: []apptodo.NegativeStockItem{
			{ProductID: pid, ProductName: "矿泉水", WarehouseName: "主仓", OnHand: "-3"},
		}},
		Total: 1,
	})
	sec := got["negative_stock"].(map[string]any)
	items := sec["items"].([]any)
	it := items[0].(map[string]any)
	if sec["count"].(float64) != 1 || it["product_name"] != "矿泉水" || it["warehouse_name"] != "主仓" ||
		it["on_hand"] != "-3" || it["product_id"] != pid.String() {
		t.Fatalf("body: %v", got)
	}
	empty := getTodo(t, apptodo.Summary{})["negative_stock"].(map[string]any)
	if empty["items"] == nil {
		t.Fatal("empty section must render [] not null")
	}
}
