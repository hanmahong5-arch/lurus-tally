package bill_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	handlerbill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/bill"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

func foreignCreateBody() []byte {
	b, _ := json.Marshal(map[string]any{
		"warehouse_id":  uuid.NewString(),
		"currency":      "USD",
		"exchange_rate": "7.2",
		"shipping_fee":  "5",
		"items":         []map[string]any{{"product_id": uuid.NewString(), "qty": "3", "unit_price": "10", "line_no": 1}},
	})
	return b
}

// TestPurchaseCreate_ClerkForeignCurrencyIsStoredAsCNY verifies that a clerk, who cannot
// see prices, cannot pick a currency or rate either: the draft lands in CNY at rate 1,
// so the boss fills the prices in without an unseen foreign rate multiplying them.
func TestPurchaseCreate_ClerkForeignCurrencyIsStoredAsCNY(t *testing.T) {
	repo := newMockBillRepo()
	r := newRouter(newTestHandler(repo))
	w := asRole(r, "clerk", clerkSub, http.MethodPost, "/api/v1/purchase-bills", foreignCreateBody())
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body %s", w.Code, w.Body.String())
	}
	if len(repo.bills) != 1 {
		t.Fatalf("bills = %d", len(repo.bills))
	}
	for _, h := range repo.bills {
		if h.Currency != "CNY" || !h.ExchangeRateVal.Equal(decimal.NewFromInt(1)) {
			t.Errorf("currency/rate = %q/%s, want CNY/1", h.Currency, h.ExchangeRateVal)
		}
	}
}

// TestPurchaseCreate_ManagerForeignCurrencyIsKept guards the other side: the boss and
// manager still book a foreign-currency purchase.
func TestPurchaseCreate_ManagerForeignCurrencyIsKept(t *testing.T) {
	repo := newMockBillRepo()
	r := newRouter(newTestHandler(repo))
	w := asRole(r, "manager", clerkSub, http.MethodPost, "/api/v1/purchase-bills", foreignCreateBody())
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body %s", w.Code, w.Body.String())
	}
	for _, h := range repo.bills {
		if h.Currency != "USD" || !h.ExchangeRateVal.Equal(decimal.RequireFromString("7.2")) {
			t.Errorf("currency/rate = %q/%s, want USD/7.2", h.Currency, h.ExchangeRateVal)
		}
	}
}

// approvedOnlySummary summarizes like the real repo: count covers approved bills only.
type approvedOnlySummary struct{ *mockBillRepo }

func (m approvedOnlySummary) SummarizeBills(ctx context.Context, f appbill.BillListFilter) (appbill.BillListSummary, error) {
	s, err := m.mockBillRepo.SummarizeBills(ctx, f)
	s.Count = 0
	for _, h := range m.bills {
		if h.Status == domain.StatusApproved {
			s.Count++
		}
	}
	return s, err
}

// TestPurchaseList_ClerkSummaryCountEqualsTotal verifies that the clerk's "my drafts"
// list reports the same count in summary as the number of drafts it lists: the summary
// counts approved bills only, which would show 0 next to a full list.
func TestPurchaseList_ClerkSummaryCountEqualsTotal(t *testing.T) {
	repo := newMockBillRepo()
	h := handlerbill.New(
		appbill.NewCreatePurchaseDraftUseCase(repo),
		appbill.NewUpdatePurchaseDraftUseCase(repo),
		appbill.NewApprovePurchaseUseCase(repo, nil, nil),
		appbill.NewCancelPurchaseUseCase(repo),
		appbill.NewListPurchasesUseCase(approvedOnlySummary{repo}),
		appbill.NewGetPurchaseUseCase(repo),
		appbill.NewRestorePurchaseUseCase(repo),
	)
	r := newRouter(h)
	seedPurchase(repo, clerkSub, domain.StatusDraft)
	seedPurchase(repo, clerkSub, domain.StatusDraft)

	w := asRole(r, "clerk", clerkSub, http.MethodGet, "/api/v1/purchase-bills", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", w.Code, w.Body.String())
	}
	var got struct {
		Total   int `json:"total"`
		Summary struct {
			Count int `json:"count"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v; %s", err, w.Body.String())
	}
	if got.Total != 2 || got.Summary.Count != got.Total {
		t.Fatalf("total=%d summary.count=%d, want both 2; %s", got.Total, got.Summary.Count, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "total_amount") {
		t.Errorf("amounts leaked: %s", w.Body.String())
	}
}

// capturingListRepo records the filter the list use case hands to the repo.
type capturingListRepo struct {
	*mockBillRepo
	got *appbill.BillListFilter
}

func (m capturingListRepo) ListBills(ctx context.Context, f appbill.BillListFilter) ([]domain.BillHead, int64, error) {
	*m.got = f
	return m.mockBillRepo.ListBills(ctx, f)
}

// TestPurchaseList_ClerkKeepsRecentApproved verifies that the clerk's list is the
// caller's own bills only, drafts plus the ones approved in the last 7 days.
func TestPurchaseList_ClerkKeepsRecentApproved(t *testing.T) {
	repo := newMockBillRepo()
	var got appbill.BillListFilter
	h := handlerbill.New(
		appbill.NewCreatePurchaseDraftUseCase(repo),
		appbill.NewUpdatePurchaseDraftUseCase(repo),
		appbill.NewApprovePurchaseUseCase(repo, nil, nil),
		appbill.NewCancelPurchaseUseCase(repo),
		appbill.NewListPurchasesUseCase(capturingListRepo{repo, &got}),
		appbill.NewGetPurchaseUseCase(repo),
		appbill.NewRestorePurchaseUseCase(repo),
	)
	r := newRouter(h)
	w := asRole(r, "clerk", clerkSub, http.MethodGet, "/api/v1/purchase-bills?status=0", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", w.Code, w.Body.String())
	}
	if got.CreatorID == nil || *got.CreatorID != clerkSub {
		t.Errorf("CreatorID = %v, want the caller", got.CreatorID)
	}
	if got.Status != nil {
		t.Errorf("Status = %v, want unset (draft or recently approved)", *got.Status)
	}
	if got.OwnRecentApprovedSince == nil {
		t.Fatalf("OwnRecentApprovedSince unset")
	}
	age := time.Since(*got.OwnRecentApprovedSince)
	if age < 7*24*time.Hour-time.Minute || age > 7*24*time.Hour+time.Minute {
		t.Errorf("window = %v, want 7 days", age)
	}
}

func seedApproved(repo *mockBillRepo, creator uuid.UUID, approvedAgo time.Duration) uuid.UUID {
	id := seedPurchase(repo, creator, domain.StatusApproved)
	at := time.Now().Add(-approvedAgo)
	repo.bills[id].ApprovedAt = &at
	return id
}

// TestPurchaseGet_ClerkSeesOwnRecentApprovedWithoutMoney verifies the detail of a
// clerk's own bill approved within 7 days opens, with no price or amount in it; an
// older one, or someone else's, stays hidden.
func TestPurchaseGet_ClerkSeesOwnRecentApprovedWithoutMoney(t *testing.T) {
	repo := newMockBillRepo()
	r := newRouter(newTestHandler(repo))
	recent := seedApproved(repo, clerkSub, 24*time.Hour)
	old := seedApproved(repo, clerkSub, 8*24*time.Hour)
	other := seedApproved(repo, uuid.New(), time.Hour)

	w := asRole(r, "clerk", clerkSub, http.MethodGet, "/api/v1/purchase-bills/"+recent.String(), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("recent: status = %d body %s", w.Code, w.Body.String())
	}
	for _, k := range []string{"total_amount", "paid_amount", "unit_price", "line_amount"} {
		if strings.Contains(w.Body.String(), k) {
			t.Errorf("%s leaked: %s", k, w.Body.String())
		}
	}
	for name, id := range map[string]uuid.UUID{"old": old, "other": other} {
		w := asRole(r, "clerk", clerkSub, http.MethodGet, "/api/v1/purchase-bills/"+id.String(), nil)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", name, w.Code)
		}
	}
}
