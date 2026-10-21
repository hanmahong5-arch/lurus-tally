package billing_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	handlerbilling "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/billing"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appbilling "github.com/hanmahong5-arch/lurus-tally/internal/app/billing"
	"github.com/hanmahong5-arch/lurus-tally/internal/app/entitlement"
	"github.com/hanmahong5-arch/lurus-tally/internal/app/subscription"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/platformclient"
)

func init() {
	gin.SetMode(gin.TestMode)
}

var tenantID = uuid.MustParse("22222222-2222-2222-2222-222222222222")

type stubSubscribe struct {
	out   *appbilling.SubscribeOutput
	err   error
	in    appbilling.SubscribeInput
	calls int
}

func (s *stubSubscribe) Execute(_ context.Context, in appbilling.SubscribeInput) (*appbilling.SubscribeOutput, error) {
	s.calls++
	s.in = in
	return s.out, s.err
}

type stubOverview struct {
	out *appbilling.Overview
	err error
	in  appbilling.Caller
}

func (s *stubOverview) Execute(_ context.Context, c appbilling.Caller) (*appbilling.Overview, error) {
	s.in = c
	return s.out, s.err
}

type stubOrder struct {
	out     *appbilling.OrderStatus
	err     error
	orderNo string
}

func (s *stubOrder) Execute(_ context.Context, _ appbilling.Caller, orderNo string) (*appbilling.OrderStatus, error) {
	s.orderNo = orderNo
	return s.out, s.err
}

// newEngine mimics AuthMiddleware: it injects the verified subject and tenant.
func newEngine(sub *stubSubscribe, ov *stubOverview, ord *stubOrder, idpSub string) *gin.Engine {
	if sub == nil {
		sub = &stubSubscribe{}
	}
	if ov == nil {
		ov = &stubOverview{}
	}
	if ord == nil {
		ord = &stubOrder{}
	}
	e := gin.New()
	e.Use(func(c *gin.Context) {
		if idpSub != "" {
			c.Set(middleware.CtxKeyIDPSubject, idpSub)
		}
		c.Set(middleware.CtxKeyTenantID, tenantID)
		c.Next()
	})
	handlerbilling.New(sub, ov, ord).RegisterRoutes(e.Group("/api/v1"))
	return e
}

// engineFor mounts an already-built handler behind the same auth shim as newEngine.
func engineFor(h *handlerbilling.Handler, idpSub string) *gin.Engine {
	e := gin.New()
	e.Use(func(c *gin.Context) {
		if idpSub != "" {
			c.Set(middleware.CtxKeyIDPSubject, idpSub)
		}
		c.Set(middleware.CtxKeyTenantID, tenantID)
		c.Next()
	})
	h.RegisterRoutes(e.Group("/api/v1"))
	return e
}

func postSubscribe(e *gin.Engine, body map[string]string, hdr map[string]string) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/billing/subscribe", bytes.NewReader(raw))
	req.Host = "tally.lurus.cn"
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func walletBody() map[string]string {
	return map[string]string{"plan_code": "pro", "billing_cycle": "monthly", "payment_method": "wallet"}
}

func TestSubscribe_WalletActivation(t *testing.T) {
	stub := &stubSubscribe{out: &appbilling.SubscribeOutput{
		Subscription: &platformclient.SubscriptionSnapshot{PlanCode: "pro", Status: "active"},
	}}
	rec := postSubscribe(newEngine(stub, nil, nil, "sub-owner"), walletBody(),
		map[string]string{middleware.HeaderIdempotencyKey: "idem-9", "User-Agent": "UA-test"})
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body)
	}
	if stub.in.IDPSubject != "sub-owner" || stub.in.TenantID != tenantID {
		t.Errorf("caller not propagated: %+v", stub.in.Caller)
	}
	if stub.in.IdempotencyKey != "idem-9" || stub.in.UserAgent != "UA-test" {
		t.Errorf("idempotency key / UA not forwarded: %+v", stub.in)
	}
	if !strings.Contains(rec.Body.String(), `"plan_code":"pro"`) || strings.Contains(rec.Body.String(), "pay_url") {
		t.Errorf("unexpected body %s", rec.Body)
	}
}

func TestSubscribe_ReturnURLIsAbsolutizedOnRequestOrigin(t *testing.T) {
	stub := &stubSubscribe{out: &appbilling.SubscribeOutput{OrderNo: "LO1", PayURL: "https://openapi.alipay.com/x"}}
	body := walletBody()
	body["payment_method"] = "alipay"
	body["return_url"] = "/subscription"
	rec := postSubscribe(newEngine(stub, nil, nil, "sub-owner"), body, map[string]string{"X-Forwarded-Proto": "https"})
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body)
	}
	if stub.in.ReturnURL != "https://tally.lurus.cn/subscription" {
		t.Errorf("return url = %q", stub.in.ReturnURL)
	}
	if !strings.Contains(rec.Body.String(), `"pay_url":"https://openapi.alipay.com/x"`) {
		t.Errorf("pay_url missing: %s", rec.Body)
	}
}

func TestSubscribe_RejectsOffSiteReturnURL(t *testing.T) {
	for _, bad := range []string{"https://evil.example/x", "//evil.example/x", "subscription", "/\\evil.example"} {
		stub := &stubSubscribe{}
		body := walletBody()
		body["return_url"] = bad
		rec := postSubscribe(newEngine(stub, nil, nil, "sub-owner"), body, nil)
		if rec.Code != http.StatusBadRequest || stub.calls != 0 {
			t.Errorf("%q: want 400 without checkout, got %d (calls=%d)", bad, rec.Code, stub.calls)
		}
	}
}

func TestSubscribe_QRCodeIsReturnedSeparately(t *testing.T) {
	stub := &stubSubscribe{out: &appbilling.SubscribeOutput{OrderNo: "LO2", QRCode: "weixin://wxpay/bizpayurl?pr=1"}}
	rec := postSubscribe(newEngine(stub, nil, nil, "sub-owner"), walletBody(), nil)
	if !strings.Contains(rec.Body.String(), `"qr_code":"weixin://wxpay/bizpayurl?pr=1"`) {
		t.Errorf("qr_code missing: %s", rec.Body)
	}
}

func TestSubscribe_ErrorMapping(t *testing.T) {
	cases := []struct {
		err      error
		status   int
		wantCode string
	}{
		{appbilling.ErrOwnerOnly, http.StatusForbidden, "owner_only"},
		{appbilling.ErrNotLinked, http.StatusConflict, "not_linked"},
		{appbilling.ErrUnauthenticated, http.StatusUnauthorized, "unauthorized"},
		{&platformclient.Error{Code: platformclient.ErrCodeInsufficientBalance, HTTPStatus: 402}, http.StatusPaymentRequired, "insufficient_balance"},
		{&platformclient.Error{Code: platformclient.ErrCodeInvalidParameter, HTTPStatus: 400, Body: `{"error":"method_not_available"}`}, http.StatusBadRequest, "method_not_available"},
		{&platformclient.Error{Code: platformclient.ErrCodeUnauthorized, HTTPStatus: 401}, http.StatusBadGateway, "platform_auth_failed"},
		{&platformclient.Error{Code: platformclient.ErrCodeUnavailable}, http.StatusBadGateway, "platform_unavailable"},
		{errors.New("boom"), http.StatusInternalServerError, ""},
	}
	for _, c := range cases {
		rec := postSubscribe(newEngine(&stubSubscribe{err: c.err}, nil, nil, "sub-owner"), walletBody(), nil)
		if rec.Code != c.status {
			t.Errorf("%v: want %d, got %d", c.err, c.status, rec.Code)
		}
		if c.wantCode != "" && !strings.Contains(rec.Body.String(), `"error":"`+c.wantCode+`"`) {
			t.Errorf("%v: want code %s, body %s", c.err, c.wantCode, rec.Body)
		}
	}
}

func TestSubscribe_BadBodyIs400(t *testing.T) {
	stub := &stubSubscribe{}
	rec := postSubscribe(newEngine(stub, nil, nil, "sub-owner"), map[string]string{"plan_code": "pro"}, nil)
	if rec.Code != http.StatusBadRequest || stub.calls != 0 {
		t.Fatalf("want 400 without checkout, got %d", rec.Code)
	}
}

// Billing identity must come only from the verified token in context. Before a
// past fix the handler fell back to the client-controlled X-IDP-Subject header,
// letting a caller act on a victim's account. PAT-authenticated requests carry
// no subject and must get 401.
func TestSubscribe_IgnoresClientIDPSubjectHeader(t *testing.T) {
	stub := &stubSubscribe{}
	rec := postSubscribe(newEngine(stub, nil, nil, ""), walletBody(), map[string]string{"X-IDP-Subject": "victim-sub"})
	if rec.Code != http.StatusUnauthorized || stub.calls != 0 {
		t.Fatalf("SECURITY: want 401 without reaching the use case, got %d (calls=%d)", rec.Code, stub.calls)
	}
}

func TestOverview_PassesCallerAndRendersReadModel(t *testing.T) {
	ov := &stubOverview{out: &appbilling.Overview{
		AccountID: 7, CanManage: true,
		Wallet:       appbilling.OverviewWallet{Available: 10, Total: 10},
		Entitlements: map[string]string{"plan_code": "pro"},
	}}
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/billing/overview", nil)
	req.Header.Set("X-IDP-Subject", "victim-sub")
	rec := httptest.NewRecorder()
	newEngine(nil, ov, nil, "sub-member").ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if ov.in.IDPSubject != "sub-member" || ov.in.TenantID != tenantID {
		t.Errorf("caller must come from context only: %+v", ov.in)
	}
	for _, want := range []string{`"can_manage":true`, `"available":10`, `"plan_code":"pro"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("body missing %s: %s", want, rec.Body)
		}
	}
}

func TestOverview_UnauthenticatedIs401(t *testing.T) {
	rec := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/billing/overview", nil)
	newEngine(nil, nil, nil, "").ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestOrderStatus_Route(t *testing.T) {
	ord := &stubOrder{out: &appbilling.OrderStatus{OrderNo: "LO7", Status: "paid", Paid: true, Final: true}}
	rec := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/billing/orders/LO7", nil)
	newEngine(nil, nil, ord, "sub-owner").ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || ord.orderNo != "LO7" || !strings.Contains(rec.Body.String(), `"paid":true`) {
		t.Fatalf("got %d %s (order=%q)", rec.Code, rec.Body, ord.orderNo)
	}

	notFound := &stubOrder{err: &platformclient.Error{Code: platformclient.ErrCodeNotFound, HTTPStatus: 404}}
	rec = httptest.NewRecorder()
	newEngine(nil, nil, notFound, "sub-owner").ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown order: want 404, got %d", rec.Code)
	}
}

func TestOverview_LockedFlag(t *testing.T) {
	ov := &appbilling.Overview{}
	ov.Subscription = &platformclient.SubscriptionSnapshot{PlanCode: "pro", Status: "expired"}

	cases := []struct {
		name     string
		lock     subscription.Checker
		wantLock string
	}{
		{"locked", subscription.Fixed{Locked: true}, `"locked":true`},
		{"open", subscription.Never{}, `"locked":false`},
		{"no checker", nil, `"locked":false`},
	}
	for _, tc := range cases {
		h := handlerbilling.New(&stubSubscribe{}, &stubOverview{out: ov}, &stubOrder{})
		if tc.lock != nil {
			h = h.WithLock(tc.lock)
		}
		e := engineFor(h, "sub-abc")
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/billing/overview", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		body := rec.Body.String()
		if rec.Code != http.StatusOK || !strings.Contains(body, tc.wantLock) || !strings.Contains(body, `"plan_code":"pro"`) {
			t.Errorf("%s: %d %s", tc.name, rec.Code, body)
		}
		if tc.name == "locked" && !strings.Contains(body, "套餐已到期") {
			t.Errorf("locked: missing lock_message: %s", body)
		}
	}
}

type lockStub struct{ locked bool }

func (l lockStub) State(context.Context, uuid.UUID) subscription.State {
	return subscription.State{Locked: l.locked}
}

func TestLockState_ReportsLockWithoutPlatform(t *testing.T) {
	for _, tc := range []struct {
		locked bool
		want   string
	}{{true, `"locked":true`}, {false, `"locked":false`}} {
		h := handlerbilling.New(nil, nil, nil).WithLock(lockStub{locked: tc.locked})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/subscription/state", nil)
		engineFor(h, "").ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), tc.want) {
			t.Errorf("locked=%v: %d %s", tc.locked, rec.Code, rec.Body.String())
		}
	}
}

type limitsStub struct{}

func (limitsStub) Status(context.Context, uuid.UUID) map[string]entitlement.LimitStatus {
	return map[string]entitlement.LimitStatus{
		"max_users": {Limit: 3, Used: 2, Enforced: true},
		"max_skus":  {Limit: -1, Used: 7, Enforced: true},
	}
}
func (limitsStub) Mode() entitlement.Mode { return entitlement.ModeEnforce }

func TestLockState_CarriesPlanLimits(t *testing.T) {
	h := handlerbilling.New(nil, nil, nil).WithLock(lockStub{}).WithLimits(limitsStub{})
	rec := httptest.NewRecorder()
	engineFor(h, "").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/subscription/state", nil))
	var body struct {
		Limits map[string]entitlement.LimitStatus `json:"limits"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if got := body.Limits["max_users"]; got != (entitlement.LimitStatus{Limit: 3, Used: 2, Enforced: true}) {
		t.Fatalf("limits = %+v (%s)", body.Limits, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"limit":-1`) {
		t.Fatalf("unlimited must serialise as -1: %s", rec.Body.String())
	}
}

func TestLockState_NoLimitsWithoutGuard(t *testing.T) {
	rec := httptest.NewRecorder()
	engineFor(handlerbilling.New(nil, nil, nil).WithLock(lockStub{}), "").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/subscription/state", nil))
	if strings.Contains(rec.Body.String(), "limits") {
		t.Fatalf("no guard, no limits: %s", rec.Body.String())
	}
}
