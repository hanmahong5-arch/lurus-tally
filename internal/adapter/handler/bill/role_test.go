package bill_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// ----- purchase bills: a clerk sees and edits only their own drafts, without money -----

var (
	clerkSub    = uuid.New()
	otherSub    = uuid.New()
	moneyFields = []string{"total_amount", "subtotal", "paid_amount", "shipping_fee", "tax_amount", "unpaid_amount", "unit_price", "line_amount", "amount_local", "invoiced_amount"}
)

func seedPurchase(repo *mockBillRepo, creator uuid.UUID, status domain.BillStatus) uuid.UUID {
	tenantID, _ := uuid.Parse(devTenantID)
	id := uuid.New()
	now := time.Now()
	repo.bills[id] = &domain.BillHead{
		ID: id, TenantID: tenantID, BillNo: "PO-" + id.String()[:4],
		BillType: domain.BillTypePurchase, SubType: domain.BillSubTypePurchase, Status: status,
		CreatorID: creator, BillDate: now, CreatedAt: now, UpdatedAt: now,
		Subtotal: decimal.NewFromInt(200), TotalAmount: decimal.NewFromInt(200), PaidAmount: decimal.NewFromInt(50),
	}
	repo.items[id] = []*domain.BillItem{{
		ID: uuid.New(), TenantID: tenantID, HeadID: id, ProductID: uuid.New(), LineNo: 1,
		Qty: decimal.NewFromInt(10), UnitPrice: decimal.NewFromInt(20), LineAmount: decimal.NewFromInt(200),
	}}
	return id
}

func asRole(r http.Handler, role string, sub uuid.UUID, method, path string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", devTenantID)
	req.Header.Set("X-Test-Role", role)
	req.Header.Set("X-Test-Sub", sub.String())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func assertNoMoney(t *testing.T, label, body string) {
	t.Helper()
	for _, k := range moneyFields {
		if strings.Contains(body, `"`+k+`"`) {
			t.Errorf("%s: %s leaked: %s", label, k, body)
		}
	}
}

func updateBody() []byte {
	b, _ := json.Marshal(map[string]any{
		"warehouse_id": uuid.NewString(),
		"items":        []map[string]any{{"product_id": uuid.NewString(), "qty": "4", "unit_price": "9.99"}},
	})
	return b
}

func TestPurchaseUpdate_ClerkCannotEditSomeoneElsesDraft(t *testing.T) {
	repo := newMockBillRepo()
	r := newRouter(newTestHandler(repo))
	bossDraft := seedPurchase(repo, otherSub, domain.StatusDraft)
	before := repo.bills[bossDraft].TotalAmount

	w := asRole(r, "clerk", clerkSub, http.MethodPut, "/api/v1/purchase-bills/"+bossDraft.String(), updateBody())
	if w.Code != http.StatusNotFound && w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 or 404; body %s", w.Code, w.Body.String())
	}
	assertNoMoney(t, "refusal", w.Body.String())
	if !repo.bills[bossDraft].TotalAmount.Equal(before) || len(repo.items[bossDraft]) != 1 || !repo.items[bossDraft][0].Qty.Equal(decimal.NewFromInt(10)) {
		t.Fatal("the boss's draft must stay untouched")
	}
}

func TestPurchaseUpdate_ClerkOwnDraft_OnlyIDAndNo(t *testing.T) {
	repo := newMockBillRepo()
	r := newRouter(newTestHandler(repo))
	mine := seedPurchase(repo, clerkSub, domain.StatusDraft)

	w := asRole(r, "clerk", clerkSub, http.MethodPut, "/api/v1/purchase-bills/"+mine.String(), updateBody())
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", w.Code, w.Body.String())
	}
	var got map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if len(got) != 2 || got["bill_id"] != mine.String() || got["bill_no"] == nil || got["bill_no"] == "" {
		t.Fatalf("response must be exactly bill_id and bill_no: %v", got)
	}
	// the clerk cannot see prices, so a price they send is not written either
	if !repo.items[mine][0].UnitPrice.IsZero() || !repo.bills[mine].TotalAmount.IsZero() {
		t.Fatalf("clerk price must be ignored: price %s total %s", repo.items[mine][0].UnitPrice, repo.bills[mine].TotalAmount)
	}
	if !repo.items[mine][0].Qty.Equal(decimal.NewFromInt(4)) {
		t.Fatalf("quantity must be saved: %s", repo.items[mine][0].Qty)
	}
}

func TestPurchaseUpdate_ManagerEditsAnyDraftAndSeesIt(t *testing.T) {
	repo := newMockBillRepo()
	r := newRouter(newTestHandler(repo))
	d := seedPurchase(repo, otherSub, domain.StatusDraft)
	w := asRole(r, "manager", clerkSub, http.MethodPut, "/api/v1/purchase-bills/"+d.String(), updateBody())
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "total_amount") {
		t.Fatalf("manager: status %d body %s", w.Code, w.Body.String())
	}
	if !repo.items[d][0].UnitPrice.Equal(decimal.RequireFromString("9.99")) {
		t.Fatal("manager price must be saved")
	}
}

func TestPurchaseCreate_ClerkPriceIsIgnored(t *testing.T) {
	repo := newMockBillRepo()
	r := newRouter(newTestHandler(repo))
	w := asRole(r, "clerk", clerkSub, http.MethodPost, "/api/v1/purchase-bills", validCreateBody())
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body %s", w.Code, w.Body.String())
	}
	if len(repo.bills) != 1 {
		t.Fatalf("bills = %d", len(repo.bills))
	}
	for id, h := range repo.bills {
		if !h.TotalAmount.IsZero() || h.CreatorID != clerkSub {
			t.Errorf("bill %s: total %s creator %s", id, h.TotalAmount, h.CreatorID)
		}
		for _, it := range repo.items[id] {
			if !it.UnitPrice.IsZero() {
				t.Errorf("a clerk price was stored: %s", it.UnitPrice)
			}
		}
	}
}

func TestPurchaseList_ClerkSeesOnlyOwnDraftsWithoutMoney(t *testing.T) {
	repo := newMockBillRepo()
	r := newRouter(newTestHandler(repo))
	seedPurchase(repo, clerkSub, domain.StatusDraft)

	w := asRole(r, "clerk", clerkSub, http.MethodGet, "/api/v1/purchase-bills?page=1&size=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", w.Code, w.Body.String())
	}
	assertNoMoney(t, "clerk list", w.Body.String())
	if !strings.Contains(w.Body.String(), "bill_no") {
		t.Fatalf("own draft must be listed: %s", w.Body.String())
	}
	f := repo.lastFilter
	if f.CreatorID == nil || *f.CreatorID != clerkSub || f.Status != nil || f.OwnRecentApprovedSince == nil {
		t.Fatalf("filter must pin creator and the draft-or-recently-approved window: %+v", f)
	}

	// asking for approved ones does not widen it
	asRole(r, "clerk", clerkSub, http.MethodGet, "/api/v1/purchase-bills?status=2", nil)
	f = repo.lastFilter
	if f.CreatorID == nil || *f.CreatorID != clerkSub || f.Status != nil || f.OwnRecentApprovedSince == nil {
		t.Fatalf("status query must not widen the clerk's view: %+v", f)
	}

	// payment and invoice state are hidden, so they cannot be used as filters either
	repo.lastFilter = appbill.BillListFilter{}
	w = asRole(r, "clerk", clerkSub, http.MethodGet, "/api/v1/purchase-bills?payment_status=partial&invoice_status=none", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", w.Code, w.Body.String())
	}
	f = repo.lastFilter
	if f.CreatorID == nil {
		t.Fatalf("list was not reached: %+v", f)
	}
	if f.PaymentStatus != "" || f.InvoiceStatus != "" {
		t.Fatalf("payment or invoice filter must be dropped for the clerk: %+v", f)
	}
}

func TestPurchaseList_ManagerAndFinanceKeepEverything(t *testing.T) {
	repo := newMockBillRepo()
	r := newRouter(newTestHandler(repo))
	seedPurchase(repo, otherSub, domain.StatusApproved)
	for _, role := range []string{"manager", "finance", "owner"} {
		w := asRole(r, role, clerkSub, http.MethodGet, "/api/v1/purchase-bills", nil)
		if !strings.Contains(w.Body.String(), "total_amount") {
			t.Errorf("%s must see amounts: %s", role, w.Body.String())
		}
		if repo.lastFilter.CreatorID != nil {
			t.Errorf("%s must not be narrowed to own bills", role)
		}
	}
}

func TestPurchaseGet_ClerkOwnDraftWithoutMoney_OthersAre404(t *testing.T) {
	repo := newMockBillRepo()
	r := newRouter(newTestHandler(repo))
	mine := seedPurchase(repo, clerkSub, domain.StatusDraft)
	theirs := seedPurchase(repo, otherSub, domain.StatusDraft)
	approvedMine := seedPurchase(repo, clerkSub, domain.StatusApproved)

	w := asRole(r, "clerk", clerkSub, http.MethodGet, "/api/v1/purchase-bills/"+mine.String(), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("own draft: status %d body %s", w.Code, w.Body.String())
	}
	assertNoMoney(t, "clerk detail", w.Body.String())
	if !strings.Contains(w.Body.String(), `"qty"`) {
		t.Fatalf("lines keep product and quantity: %s", w.Body.String())
	}
	for name, id := range map[string]uuid.UUID{"someone else's draft": theirs, "own approved bill": approvedMine} {
		w = asRole(r, "clerk", clerkSub, http.MethodGet, "/api/v1/purchase-bills/"+id.String(), nil)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: status %d body %s", name, w.Code, w.Body.String())
		}
		assertNoMoney(t, name, w.Body.String())
	}
	w = asRole(r, "finance", clerkSub, http.MethodGet, "/api/v1/purchase-bills/"+theirs.String(), nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "unit_price") {
		t.Fatalf("finance sees everything: %d %s", w.Code, w.Body.String())
	}
}

// ----- sale return: a cashier may only return a sale they rang up today -----

func cashierReturn(f *returnHTTPFixture, sub uuid.UUID, saleID uuid.UUID, body map[string]any) *httptest.ResponseRecorder {
	return returnAs(f, "cashier", sub, saleID, body)
}

func returnAs(f *returnHTTPFixture, role string, sub uuid.UUID, saleID uuid.UUID, body map[string]any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sale-bills/"+saleID.String()+"/return", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantHeader(shTenantID))
	req.Header.Set("X-Test-Role", role)
	req.Header.Set("X-Test-Sub", sub.String())
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}

const notYoursToday = "这张单不是你今天开的，请找老板或店长"

func TestSaleReturn_CashierOwnSaleToday_Allowed(t *testing.T) {
	f := newReturnHTTPFixture()
	head := f.repo.heads[f.saleID]
	head.CreatorID, head.BillDate = clerkSub, time.Now()
	w := cashierReturn(f, clerkSub, f.saleID, f.line(f.lineID, "1"))
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body %s", w.Code, w.Body.String())
	}
}

func TestSaleReturn_CashierSomeoneElsesSale_403Chinese(t *testing.T) {
	f := newReturnHTTPFixture()
	head := f.repo.heads[f.saleID]
	head.CreatorID, head.BillDate = otherSub, time.Now()
	w := cashierReturn(f, clerkSub, f.saleID, f.line(f.lineID, "1"))
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), notYoursToday) {
		t.Fatalf("status = %d body %s", w.Code, w.Body.String())
	}
	if f.stockUC.calls != 0 {
		t.Fatal("nothing may move")
	}
}

func TestSaleReturn_CashierOwnSaleFromYesterday_403(t *testing.T) {
	f := newReturnHTTPFixture()
	head := f.repo.heads[f.saleID]
	head.CreatorID, head.BillDate = clerkSub, time.Now().Add(-30*time.Hour)
	w := cashierReturn(f, clerkSub, f.saleID, f.line(f.lineID, "1"))
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), notYoursToday) {
		t.Fatalf("status = %d body %s", w.Code, w.Body.String())
	}
	// refund-only bookkeeping goes through the same door
	w = cashierReturn(f, clerkSub, f.saleID, map[string]any{"refund_only": true, "refund_amount": "10", "return_bill_id": uuid.NewString()})
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), notYoursToday) {
		t.Fatalf("refund-only: status = %d body %s", w.Code, w.Body.String())
	}
}

func TestSaleReturn_CashierDayIsBeijingDay(t *testing.T) {
	f := newReturnHTTPFixture()
	head := f.repo.heads[f.saleID]
	head.CreatorID = clerkSub
	cst := time.FixedZone("CST", 8*3600)
	now := time.Now().In(cst)
	// 00:00:01 Beijing time today is still today; the last second of yesterday is not
	head.BillDate = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 1, 0, cst).UTC()
	if now.Before(head.BillDate) {
		t.Skip("clock sits in the first second of the day")
	}
	if w := cashierReturn(f, clerkSub, f.saleID, f.line(f.lineID, "1")); w.Code != http.StatusCreated {
		t.Fatalf("early today: status = %d body %s", w.Code, w.Body.String())
	}
	head.BillDate = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, cst).UTC().Add(-time.Second)
	if w := cashierReturn(f, clerkSub, f.saleID, f.line(f.lineID, "1")); w.Code != http.StatusForbidden {
		t.Fatalf("last second of yesterday: status = %d body %s", w.Code, w.Body.String())
	}
}

func TestSaleReturn_ManagerAndOwnerReturnAnyOldSale(t *testing.T) {
	f := newReturnHTTPFixture()
	head := f.repo.heads[f.saleID]
	head.CreatorID, head.BillDate = otherSub, time.Now().Add(-90*24*time.Hour)
	for _, role := range []string{"manager", "owner"} {
		w := returnAs(f, role, clerkSub, f.saleID, f.line(f.lineID, "1"))
		if w.Code != http.StatusCreated {
			t.Fatalf("%s: status = %d body %s", role, w.Code, w.Body.String())
		}
	}
}

func TestSaleReturn_CashierUnknownSale_404(t *testing.T) {
	f := newReturnHTTPFixture()
	w := cashierReturn(f, clerkSub, uuid.New(), f.line(f.lineID, "1"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d body %s", w.Code, w.Body.String())
	}
}
