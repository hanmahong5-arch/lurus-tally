package bill_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	handlerbill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/bill"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	apppayment "github.com/hanmahong5-arch/lurus-tally/internal/app/payment"
)

// dupRepo reports every client_ref as already booked under a fixed bill.
type dupRepo struct {
	*shMockBillRepo
	id uuid.UUID
	no string
}

func (r *dupRepo) FindBillByClientRef(_ context.Context, _ uuid.UUID, _ string) (uuid.UUID, string, bool, error) {
	return r.id, r.no, true, nil
}

func offlineHandler(repo appbill.BillRepo) *handlerbill.SaleHandler {
	approveUC := appbill.NewApproveSaleUseCase(repo, &shMockStockUC{}, &shMockUnitRepo{}, &shMockPaymentRecorder{})
	return handlerbill.NewSaleHandler(
		appbill.NewCreateSaleUseCase(repo), approveUC, appbill.NewCancelPurchaseUseCase(repo),
		appbill.NewListPurchasesUseCase(repo), repo, appbill.NewQuickCheckoutUseCase(repo, approveUC),
		apppayment.NewListPaymentsUseCase(&shMockPaymentRepo{}))
}

// 重复的 client_ref：409 checkout_already_exists，带已入账单据的 id / bill_no。
func TestSaleHandler_QuickCheckout_DuplicateClientRef_Returns409(t *testing.T) {
	existing := uuid.New()
	repo := &dupRepo{shMockBillRepo: newSHMockBillRepo(), id: existing, no: "SL-20261015-0009"}
	r := newSaleRouter(offlineHandler(repo))

	w := postQuickCheckout(r, quickCheckoutBody("80", map[string]any{"client_ref": "till-1-0001", "source": "pos_offline", "occurred_at": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)}))
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["error"] != "checkout_already_exists" || resp["id"] != existing.String() || resp["bill_no"] != "SL-20261015-0009" {
		t.Fatalf("unexpected body: %v", resp)
	}
	if len(repo.heads) != 0 {
		t.Fatal("a replayed checkout must not create a second bill")
	}
}

func TestSaleHandler_QuickCheckout_OfflineFields(t *testing.T) {
	repo := newSHMockBillRepo()
	r := newSaleRouter(buildSaleHandlerForTest(repo, &shMockStockUC{}, &shMockPaymentRecorder{}, &shMockPaymentRepo{}))

	at := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	w := postQuickCheckout(r, quickCheckoutBody("80", map[string]any{
		"client_ref": "till-1-0002", "source": "pos_offline", "occurred_at": at.Format(time.RFC3339),
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	var saved bool
	for _, h := range repo.heads {
		saved = true
		if h.ClientRef != "till-1-0002" || h.Source != "pos_offline" || h.OccurredAt == nil || !h.OccurredAt.Equal(at) || time.Since(h.BillDate) > time.Minute {
			t.Fatalf("offline fields not stored: %+v", h)
		}
	}
	if !saved {
		t.Fatal("no bill stored")
	}
}

func TestSaleHandler_QuickCheckout_BadOfflineFields_Return400(t *testing.T) {
	r := newSaleRouter(buildSaleHandlerForTest(newSHMockBillRepo(), &shMockStockUC{}, &shMockPaymentRecorder{}, &shMockPaymentRepo{}))
	for name, extra := range map[string]map[string]any{
		"unparsable time": {"occurred_at": "yesterday"},
		"future time":     {"occurred_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)},
		"too old":         {"occurred_at": time.Now().Add(-31 * 24 * time.Hour).UTC().Format(time.RFC3339)},
		"long client_ref": {"client_ref": strings.Repeat("x", 65)},
		"bad source":      {"source": "shopify"},
	} {
		if w := postQuickCheckout(r, quickCheckoutBody("80", extra)); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400; body: %s", name, w.Code, w.Body.String())
		}
	}
}
