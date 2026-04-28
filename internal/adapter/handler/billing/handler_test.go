package billing_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	handlerbilling "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/billing"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appbilling "github.com/hanmahong5-arch/lurus-tally/internal/app/billing"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/platformclient"
)

func init() {
	gin.SetMode(gin.TestMode)
}

type stubSubscribe struct {
	out *appbilling.SubscribeOutput
	err error
	in  appbilling.SubscribeInput
}

func (s *stubSubscribe) Execute(_ context.Context, in appbilling.SubscribeInput) (*appbilling.SubscribeOutput, error) {
	s.in = in
	return s.out, s.err
}

type stubOverview struct {
	out *platformclient.AccountOverview
	err error
}

func (s *stubOverview) Execute(_ context.Context, _ string) (*platformclient.AccountOverview, error) {
	return s.out, s.err
}

func newEngine(h *handlerbilling.Handler, sub string) *gin.Engine {
	e := gin.New()
	e.Use(func(c *gin.Context) {
		if sub != "" {
			c.Set(middleware.CtxKeyZitadelSub, sub)
		}
		c.Next()
	})
	h.RegisterRoutes(e.Group("/api/v1"))
	return e
}

func TestSubscribe_Unauthenticated_Returns401(t *testing.T) {
	h := handlerbilling.New(&stubSubscribe{}, &stubOverview{})
	e := newEngine(h, "")

	body, _ := json.Marshal(map[string]string{"plan_code": "pro", "billing_cycle": "monthly", "payment_method": "wallet"})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/billing/subscribe", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestSubscribe_HappyWalletPath_Returns200WithSubscription(t *testing.T) {
	stub := &stubSubscribe{
		out: &appbilling.SubscribeOutput{
			Subscription: &platformclient.SubscriptionSnapshot{PlanCode: "pro", Status: "active"},
		},
	}
	h := handlerbilling.New(stub, &stubOverview{})
	e := newEngine(h, "sub-abc")

	body, _ := json.Marshal(map[string]string{"plan_code": "pro", "billing_cycle": "monthly", "payment_method": "wallet"})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/billing/subscribe", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"plan_code":"pro"`) {
		t.Errorf("expected subscription in body, got %s", rec.Body.String())
	}
	if stub.in.ZitadelSub != "sub-abc" {
		t.Errorf("zitadel sub propagation lost: %+v", stub.in)
	}
}

func TestSubscribe_AlipayReturnsPayURL(t *testing.T) {
	stub := &stubSubscribe{
		out: &appbilling.SubscribeOutput{OrderNo: "ORD42", PayURL: "https://alipay/qr/ORD42"},
	}
	h := handlerbilling.New(stub, &stubOverview{})
	e := newEngine(h, "sub-abc")

	body, _ := json.Marshal(map[string]string{"plan_code": "pro", "billing_cycle": "monthly", "payment_method": "alipay"})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/billing/subscribe", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"pay_url":"https://alipay/qr/ORD42"`) {
		t.Errorf("missing pay_url in body: %s", rec.Body.String())
	}
}

func TestSubscribe_InsufficientBalance_Returns402(t *testing.T) {
	stub := &stubSubscribe{
		err: &platformclient.Error{Code: platformclient.ErrCodeInsufficientBalance, HTTPStatus: 402, Message: "broke"},
	}
	h := handlerbilling.New(stub, &stubOverview{})
	e := newEngine(h, "sub-abc")

	body, _ := json.Marshal(map[string]string{"plan_code": "pro", "billing_cycle": "monthly", "payment_method": "wallet"})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/billing/subscribe", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusPaymentRequired {
		t.Errorf("expected 402, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "insufficient_balance") {
		t.Errorf("expected insufficient_balance code in body: %s", rec.Body.String())
	}
}

func TestSubscribe_PlatformDown_Returns502(t *testing.T) {
	stub := &stubSubscribe{
		err: &platformclient.Error{Code: platformclient.ErrCodeUnavailable, HTTPStatus: 502, Message: "boom"},
	}
	h := handlerbilling.New(stub, &stubOverview{})
	e := newEngine(h, "sub-abc")

	body, _ := json.Marshal(map[string]string{"plan_code": "pro", "billing_cycle": "monthly", "payment_method": "wallet"})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/billing/subscribe", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestSubscribe_BadRequest_Returns400(t *testing.T) {
	h := handlerbilling.New(&stubSubscribe{}, &stubOverview{})
	e := newEngine(h, "sub-abc")

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/billing/subscribe",
		bytes.NewReader([]byte(`{"plan_code":""}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestOverview_HappyPath_Returns200(t *testing.T) {
	ov := &platformclient.AccountOverview{}
	ov.Account.ID = 7
	ov.Account.Email = "u@x"
	ov.Wallet = &platformclient.Wallet{Available: 50}
	ov.Subscription = &platformclient.SubscriptionSnapshot{PlanCode: "free", Status: "active"}

	h := handlerbilling.New(&stubSubscribe{}, &stubOverview{out: ov})
	e := newEngine(h, "sub-abc")

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/billing/overview", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"plan_code":"free"`) {
		t.Errorf("expected subscription block: %s", rec.Body.String())
	}
}

func TestOverview_Unauthenticated_Returns401(t *testing.T) {
	h := handlerbilling.New(&stubSubscribe{}, &stubOverview{})
	e := newEngine(h, "")
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/billing/overview", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}
