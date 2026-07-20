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
	in  string
}

func (s *stubOverview) Execute(_ context.Context, sub string) (*platformclient.AccountOverview, error) {
	s.in = sub
	return s.out, s.err
}

func newEngine(h *handlerbilling.Handler, sub string) *gin.Engine {
	e := gin.New()
	e.Use(func(c *gin.Context) {
		if sub != "" {
			c.Set(middleware.CtxKeyIDPSubject, sub)
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
	if stub.in.IDPSubject != "sub-abc" {
		t.Errorf("idp subject propagation lost: %+v", stub.in)
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

// TestSubscribe_IgnoresClientIDPSubjectHeader is the regression guard for the
// cross-account billing impersonation fix. A PAT-authenticated request never
// gets CtxKeyIDPSubject set by AuthMiddleware (a PAT is tenant-scoped, carries
// no OIDC subject); newEngine(h, "") reproduces exactly that context. Before
// the fix, callerSub fell back to the attacker-controlled X-IDP-Subject header,
// letting the caller check out a subscription against a victim's platform
// account. Billing identity must come only from the verified OIDC token in
// context, so the spoofed header must be ignored: 401 and the victim subject
// must never reach the use case.
func TestSubscribe_IgnoresClientIDPSubjectHeader(t *testing.T) {
	stub := &stubSubscribe{
		out: &appbilling.SubscribeOutput{
			Subscription: &platformclient.SubscriptionSnapshot{PlanCode: "pro", Status: "active"},
		},
	}
	h := handlerbilling.New(stub, &stubOverview{})
	e := newEngine(h, "") // PAT-auth shape: tenant set upstream, NO idp_subject in ctx

	body, _ := json.Marshal(map[string]string{"plan_code": "pro", "billing_cycle": "monthly", "payment_method": "wallet"})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/billing/subscribe", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-IDP-Subject", "victim-sub") // attacker-controlled
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 (client header must not be honoured), got %d (%s)", rec.Code, rec.Body.String())
	}
	if stub.in.IDPSubject == "victim-sub" {
		t.Fatalf("SECURITY: attacker-controlled X-IDP-Subject header reached the use case: %q", stub.in.IDPSubject)
	}
}

// TestOverview_IgnoresClientIDPSubjectHeader is the read-side twin of the guard
// above: a PAT-auth-shaped request (no ctx subject) with a spoofed header must
// not let the caller read a victim's wallet/subscription snapshot.
func TestOverview_IgnoresClientIDPSubjectHeader(t *testing.T) {
	stub := &stubOverview{out: &platformclient.AccountOverview{}}
	h := handlerbilling.New(&stubSubscribe{}, stub)
	e := newEngine(h, "") // no idp_subject in ctx

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/billing/overview", nil)
	req.Header.Set("X-IDP-Subject", "victim-sub") // attacker-controlled
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 (client header must not be honoured), got %d (%s)", rec.Code, rec.Body.String())
	}
	if stub.in == "victim-sub" {
		t.Fatalf("SECURITY: attacker-controlled X-IDP-Subject header reached the overview use case: %q", stub.in)
	}
}
