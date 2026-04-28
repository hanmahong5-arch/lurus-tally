package platformclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/platformclient"
)

func newClient(t *testing.T, srv *httptest.Server) *platformclient.Client {
	t.Helper()
	c, err := platformclient.New(platformclient.Config{
		BaseURL: srv.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	return c
}

func TestNew_RejectsEmptyConfig(t *testing.T) {
	if _, err := platformclient.New(platformclient.Config{APIKey: "k"}); err == nil {
		t.Error("expected error when BaseURL empty")
	}
	if _, err := platformclient.New(platformclient.Config{BaseURL: "http://x"}); err == nil {
		t.Error("expected error when APIKey empty")
	}
}

func TestGetAccountByZitadelSub_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("missing bearer; got %q", got)
		}
		if !strings.HasPrefix(r.URL.Path, "/internal/v1/accounts/by-zitadel-sub/") {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(platformclient.Account{ID: 42, ZitadelSub: "sub-abc", Email: "u@x"})
	}))
	defer srv.Close()

	acc, err := newClient(t, srv).GetAccountByZitadelSub(context.Background(), "sub-abc")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if acc.ID != 42 || acc.Email != "u@x" {
		t.Errorf("decoded wrong: %+v", acc)
	}
}

func TestGetAccountByZitadelSub_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"account not found"}`))
	}))
	defer srv.Close()

	_, err := newClient(t, srv).GetAccountByZitadelSub(context.Background(), "missing")
	if !platformclient.IsCode(err, platformclient.ErrCodeNotFound) {
		t.Errorf("expected ErrCodeNotFound, got %v", err)
	}
}

func TestSubscriptionCheckout_WalletActivation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("want POST, got %s", r.Method)
		}
		var body platformclient.SubscriptionCheckoutRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.PaymentMethod != "wallet" {
			t.Errorf("payment_method: %s", body.PaymentMethod)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"subscription": map[string]any{
				"plan_code":  "pro",
				"status":     "active",
				"expires_at": "2027-04-25T00:00:00Z",
			},
		})
	}))
	defer srv.Close()

	resp, err := newClient(t, srv).SubscriptionCheckout(context.Background(), platformclient.SubscriptionCheckoutRequest{
		AccountID:     42,
		ProductID:     "lurus-tally",
		PlanCode:      "pro",
		BillingCycle:  "monthly",
		PaymentMethod: "wallet",
	})
	if err != nil {
		t.Fatalf("checkout err: %v", err)
	}
	if resp.Subscription == nil || resp.Subscription.PlanCode != "pro" {
		t.Errorf("decoded wrong: %+v", resp)
	}
}

func TestSubscriptionCheckout_AlipayReturnsPayURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"order_no": "ORD123",
			"pay_url":  "https://alipay/qr/ORD123",
		})
	}))
	defer srv.Close()

	resp, err := newClient(t, srv).SubscriptionCheckout(context.Background(), platformclient.SubscriptionCheckoutRequest{
		AccountID:     42,
		ProductID:     "lurus-tally",
		PlanCode:      "pro",
		BillingCycle:  "monthly",
		PaymentMethod: "alipay",
	})
	if err != nil {
		t.Fatalf("checkout err: %v", err)
	}
	if resp.PayURL == "" || resp.OrderNo == "" {
		t.Errorf("expected pay_url + order_no, got %+v", resp)
	}
}

func TestSubscriptionCheckout_InsufficientBalance_MapsTo402Code(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"code":"insufficient_balance","message":"Insufficient wallet balance"}`))
	}))
	defer srv.Close()

	_, err := newClient(t, srv).SubscriptionCheckout(context.Background(), platformclient.SubscriptionCheckoutRequest{
		AccountID:     42,
		ProductID:     "lurus-tally",
		PlanCode:      "pro",
		BillingCycle:  "monthly",
		PaymentMethod: "wallet",
	})
	if !platformclient.IsCode(err, platformclient.ErrCodeInsufficientBalance) {
		t.Errorf("expected ErrCodeInsufficientBalance, got %v", err)
	}
}

func TestSubscriptionCheckout_PlatformDown_MapsToUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	_, err := newClient(t, srv).SubscriptionCheckout(context.Background(), platformclient.SubscriptionCheckoutRequest{
		AccountID:     42,
		ProductID:     "lurus-tally",
		PlanCode:      "pro",
		BillingCycle:  "monthly",
		PaymentMethod: "wallet",
	})
	if !platformclient.IsCode(err, platformclient.ErrCodeUnavailable) {
		t.Errorf("expected ErrCodeUnavailable, got %v", err)
	}
}

func TestGetAccountOverview_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("product_id") != "lurus-tally" {
			t.Errorf("missing product_id query: %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"account": map[string]any{"id": 42, "username": "u", "email": "u@x", "vip_tier": "Standard"},
			"wallet":  map[string]any{"available": 100.5, "frozen": 0, "total": 100.5},
			"subscription": map[string]any{
				"plan_code": "free",
				"status":    "active",
			},
		})
	}))
	defer srv.Close()

	ov, err := newClient(t, srv).GetAccountOverview(context.Background(), 42, "lurus-tally")
	if err != nil {
		t.Fatalf("overview err: %v", err)
	}
	if ov.Wallet == nil || ov.Wallet.Available != 100.5 {
		t.Errorf("wallet: %+v", ov.Wallet)
	}
	if ov.Subscription == nil || ov.Subscription.PlanCode != "free" {
		t.Errorf("subscription: %+v", ov.Subscription)
	}
}
