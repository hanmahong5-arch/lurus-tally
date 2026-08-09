package billing_test

// Additional coverage for internal/adapter/handler/billing, focused on the
// writePlatformError status-mapping table (the money-safety contract) and the
// callerSub precedence rules, which handler_test.go does not yet exercise.

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
	handlerbilling "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/billing"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appbilling "github.com/hanmahong5-arch/lurus-tally/internal/app/billing"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/platformclient"
)

// zzStubSubscribe is a second SubscribeExecutor fake local to this file so we
// never touch handler_test.go's stubs (which may carry unrelated concurrent
// session edits).
type zzStubSubscribe struct {
	out *appbilling.SubscribeOutput
	err error
	in  appbilling.SubscribeInput
}

func (s *zzStubSubscribe) Execute(_ context.Context, in appbilling.SubscribeInput) (*appbilling.SubscribeOutput, error) {
	s.in = in
	return s.out, s.err
}

type zzStubOverview struct {
	out *platformclient.AccountOverview
	err error
}

func (s *zzStubOverview) Execute(_ context.Context, _ string) (*platformclient.AccountOverview, error) {
	return s.out, s.err
}

// zzHeaderEngine wires the handler onto a bare gin.Engine with NO auth
// middleware at all, so no idp_subject is ever placed in the context. It is
// used to prove callerSub returns "" here — the X-IDP-Subject header must NOT
// be trusted as an identity source, regardless of what the request carries.
func zzHeaderEngine(h *handlerbilling.Handler) *gin.Engine {
	e := gin.New()
	h.RegisterRoutes(e.Group("/api/v1"))
	return e
}

// zzCtxEngine sets CtxKeyIDPSubject to whatever ctxSub is (including a
// deliberately wrong-typed value to probe the type assertion), independent of
// any X-IDP-Subject header the request may also carry.
func zzCtxEngine(h *handlerbilling.Handler, ctxSub any) *gin.Engine {
	e := gin.New()
	e.Use(func(c *gin.Context) {
		if ctxSub != nil {
			c.Set(middleware.CtxKeyIDPSubject, ctxSub)
		}
		c.Next()
	})
	h.RegisterRoutes(e.Group("/api/v1"))
	return e
}

func zzSubscribeBody(plan, cycle, method string) []byte {
	b, _ := json.Marshal(map[string]string{
		"plan_code":      plan,
		"billing_cycle":  cycle,
		"payment_method": method,
	})
	return b
}

// --- callerSub precedence -------------------------------------------------

func TestZZCallerSub_CtxWinsOverHeader(t *testing.T) {
	stub := &zzStubSubscribe{out: &appbilling.SubscribeOutput{OrderNo: "ORD-CTX"}}
	h := handlerbilling.New(stub, &zzStubOverview{})
	e := zzCtxEngine(h, "ctx-sub")

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/billing/subscribe", bytes.NewReader(zzSubscribeBody("pro", "monthly", "wallet")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-IDP-Subject", "header-sub")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if stub.in.IDPSubject != "ctx-sub" {
		t.Errorf("ctx sub should win over header, got %q", stub.in.IDPSubject)
	}
}

func TestZZCallerSub_IgnoresHeader_WhenCtxAbsent(t *testing.T) {
	// No idp_subject in context (the PAT-auth shape). The X-IDP-Subject header
	// is attacker-controlled and must be ignored — the request is rejected with
	// 401 and the header value must never reach the use case.
	stub := &zzStubSubscribe{out: &appbilling.SubscribeOutput{OrderNo: "ORD-HDR"}}
	h := handlerbilling.New(stub, &zzStubOverview{})
	e := zzHeaderEngine(h) // no ctx middleware at all

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/billing/subscribe", bytes.NewReader(zzSubscribeBody("pro", "monthly", "wallet")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-IDP-Subject", "header-only-sub")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 (header must not be honoured), got %d (%s)", rec.Code, rec.Body.String())
	}
	if stub.in.IDPSubject == "header-only-sub" {
		t.Errorf("SECURITY: client X-IDP-Subject header reached the use case: %q", stub.in.IDPSubject)
	}
}

func TestZZCallerSub_CtxWrongType_IgnoresHeader(t *testing.T) {
	// ctx value present but not a string -> the type assertion in callerSub
	// fails, so it resolves to "" — it must NOT fall through to the header.
	// The wrong-typed subject yields 401, and the header must be ignored.
	stub := &zzStubSubscribe{out: &appbilling.SubscribeOutput{OrderNo: "ORD-TYPED"}}
	h := handlerbilling.New(stub, &zzStubOverview{})
	e := zzCtxEngine(h, 12345) // int, not string

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/billing/subscribe", bytes.NewReader(zzSubscribeBody("pro", "monthly", "wallet")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-IDP-Subject", "typed-fallback-sub")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on wrong ctx type (header must not be honoured), got %d (%s)", rec.Code, rec.Body.String())
	}
	if stub.in.IDPSubject == "typed-fallback-sub" {
		t.Errorf("SECURITY: client X-IDP-Subject header reached the use case on wrong ctx type: %q", stub.in.IDPSubject)
	}
}

func TestZZCallerSub_BothEmpty_Returns401_NoExecutorCall(t *testing.T) {
	stub := &zzStubSubscribe{out: &appbilling.SubscribeOutput{OrderNo: "SHOULD-NOT-APPEAR"}}
	h := handlerbilling.New(stub, &zzStubOverview{})
	e := zzHeaderEngine(h)

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/billing/subscribe", bytes.NewReader(zzSubscribeBody("pro", "monthly", "wallet")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
	}
	if stub.in.PlanCode != "" {
		t.Errorf("executor must not be invoked on auth gate failure, got in=%+v", stub.in)
	}
}

// --- Idempotency-Key forwarding -------------------------------------------

func TestZZSubscribe_IdempotencyKeyForwarded(t *testing.T) {
	stub := &zzStubSubscribe{out: &appbilling.SubscribeOutput{OrderNo: "ORD-IDEM"}}
	h := handlerbilling.New(stub, &zzStubOverview{})
	e := zzCtxEngine(h, "sub-idem")

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/billing/subscribe", bytes.NewReader(zzSubscribeBody("pro", "monthly", "wallet")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(middleware.HeaderIdempotencyKey, "idem-key-xyz")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if stub.in.IdempotencyKey != "idem-key-xyz" {
		t.Errorf("expected exact idempotency key forwarded, got %q", stub.in.IdempotencyKey)
	}
}

func TestZZSubscribe_IdempotencyKeyAbsent_ForwardsEmpty(t *testing.T) {
	stub := &zzStubSubscribe{out: &appbilling.SubscribeOutput{OrderNo: "ORD-NOIDEM"}}
	h := handlerbilling.New(stub, &zzStubOverview{})
	e := zzCtxEngine(h, "sub-noidem")

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/billing/subscribe", bytes.NewReader(zzSubscribeBody("pro", "monthly", "wallet")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if stub.in.IdempotencyKey != "" {
		t.Errorf("expected empty idempotency key when header absent, got %q", stub.in.IdempotencyKey)
	}
}

// --- subscribeRequest binding: each required field missing -----------------

func TestZZSubscribe_Binding_MissingRequiredFields(t *testing.T) {
	cases := []struct {
		name string
		body []byte
	}{
		{"missing_plan_code", zzSubscribeBody("", "monthly", "wallet")},
		{"missing_billing_cycle", zzSubscribeBody("pro", "", "wallet")},
		{"missing_payment_method", zzSubscribeBody("pro", "monthly", "")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &zzStubSubscribe{out: &appbilling.SubscribeOutput{}}
			h := handlerbilling.New(stub, &zzStubOverview{})
			e := zzCtxEngine(h, "sub-abc")

			req, _ := http.NewRequest(http.MethodPost, "/api/v1/billing/subscribe", bytes.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s: expected 400, got %d (%s)", tc.name, rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "bad_request") {
				t.Errorf("%s: expected bad_request code in body: %s", tc.name, rec.Body.String())
			}
		})
	}
}

func TestZZSubscribe_MalformedJSON_Returns400(t *testing.T) {
	stub := &zzStubSubscribe{}
	h := handlerbilling.New(stub, &zzStubOverview{})
	e := zzCtxEngine(h, "sub-abc")

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/billing/subscribe", bytes.NewReader([]byte(`{not-json`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
}

// --- Subscribe success: exact DTO mapping, omitempty ----------------------

func TestZZSubscribe_Success_OnlyNonEmptyFieldsSerialized(t *testing.T) {
	// out has OrderNo+PayURL but no Subscription -> subscription key must be
	// entirely absent (omitempty), not "subscription":null.
	stub := &zzStubSubscribe{out: &appbilling.SubscribeOutput{OrderNo: "ORD-OMIT", PayURL: "https://pay/omit"}}
	h := handlerbilling.New(stub, &zzStubOverview{})
	e := zzCtxEngine(h, "sub-abc")

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/billing/subscribe", bytes.NewReader(zzSubscribeBody("pro", "monthly", "wallet")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if _, present := got["subscription"]; present {
		t.Errorf("expected subscription key absent under omitempty, got body: %s", rec.Body.String())
	}
	if got["order_no"] != "ORD-OMIT" || got["pay_url"] != "https://pay/omit" {
		t.Errorf("unexpected order_no/pay_url mapping: %s", rec.Body.String())
	}
}

// --- executor-returned appbilling sentinel errors --------------------------

func TestZZSubscribe_ExecutorReturnsErrUnauthenticated_Returns401(t *testing.T) {
	stub := &zzStubSubscribe{err: appbilling.ErrUnauthenticated}
	h := handlerbilling.New(stub, &zzStubOverview{})
	e := zzCtxEngine(h, "sub-abc") // auth gate passes; executor itself rejects

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/billing/subscribe", bytes.NewReader(zzSubscribeBody("pro", "monthly", "wallet")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "unauthorized") {
		t.Errorf("expected unauthorized code: %s", rec.Body.String())
	}
}

func TestZZSubscribe_ExecutorReturnsErrInvalidInput_Returns400(t *testing.T) {
	stub := &zzStubSubscribe{err: appbilling.ErrInvalidInput}
	h := handlerbilling.New(stub, &zzStubOverview{})
	e := zzCtxEngine(h, "sub-abc")

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/billing/subscribe", bytes.NewReader(zzSubscribeBody("pro", "monthly", "wallet")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "bad_request") {
		t.Errorf("expected bad_request code: %s", rec.Body.String())
	}
}

// --- writePlatformError: full *platformclient.Error status-mapping table ---

func TestZZWritePlatformError_StatusMappingTable(t *testing.T) {
	cases := []struct {
		name       string
		code       platformclient.ErrCode
		wantStatus int
		wantBody   string
	}{
		{"insufficient_balance", platformclient.ErrCodeInsufficientBalance, http.StatusPaymentRequired, "insufficient_balance"},
		{"not_found", platformclient.ErrCodeNotFound, http.StatusNotFound, "not_found"},
		{"invalid_parameter", platformclient.ErrCodeInvalidParameter, http.StatusBadRequest, "bad_request"},
		{"unauthorized", platformclient.ErrCodeUnauthorized, http.StatusBadGateway, "platform_auth_failed"},
		{"unavailable", platformclient.ErrCodeUnavailable, http.StatusBadGateway, "platform_unavailable"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &zzStubSubscribe{err: &platformclient.Error{Code: tc.code, Message: "detail-" + tc.name}}
			h := handlerbilling.New(stub, &zzStubOverview{})
			e := zzCtxEngine(h, "sub-abc")

			req, _ := http.NewRequest(http.MethodPost, "/api/v1/billing/subscribe", bytes.NewReader(zzSubscribeBody("pro", "monthly", "wallet")))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Errorf("%s: expected %d, got %d (%s)", tc.name, tc.wantStatus, rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Errorf("%s: expected body to contain %q, got %s", tc.name, tc.wantBody, rec.Body.String())
			}
		})
	}
}

func TestZZWritePlatformError_UnknownCode_FallsThroughTo500(t *testing.T) {
	// ErrCodeUnknown (or any code not in the explicit switch) has no case, so
	// it must fall through the switch and hit httperr.WriteInternal's generic
	// 500 arm — same as a non-typed error.
	stub := &zzStubSubscribe{err: &platformclient.Error{Code: platformclient.ErrCodeUnknown, Message: "mystery"}}
	h := handlerbilling.New(stub, &zzStubOverview{})
	e := zzCtxEngine(h, "sub-abc")

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/billing/subscribe", bytes.NewReader(zzSubscribeBody("pro", "monthly", "wallet")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for unmapped ErrCode, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "internal_error") {
		t.Errorf("expected internal_error code in body: %s", rec.Body.String())
	}
	// The typed platform error's Message must never leak into a 5xx body.
	if strings.Contains(rec.Body.String(), "mystery") {
		t.Errorf("5xx body must not leak internal message, got: %s", rec.Body.String())
	}
}

func TestZZWritePlatformError_GenericError_DefaultArm_Returns500(t *testing.T) {
	// A plain, non-typed error (neither the appbilling sentinels nor
	// *platformclient.Error) must fall through every branch to the default
	// httperr.WriteInternal(c, err) 500 arm.
	stub := &zzStubSubscribe{err: errors.New("boom: something unexpected broke")}
	h := handlerbilling.New(stub, &zzStubOverview{})
	e := zzCtxEngine(h, "sub-abc")

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/billing/subscribe", bytes.NewReader(zzSubscribeBody("pro", "monthly", "wallet")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "internal_error") {
		t.Errorf("expected internal_error code: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Errorf("5xx body must not leak the raw internal error text, got: %s", rec.Body.String())
	}
}

// --- Overview: auth gate, success pass-through, and error mapping ----------

func TestZZOverview_ExecutorReturnsErrUnauthenticated_Returns401(t *testing.T) {
	h := handlerbilling.New(&zzStubSubscribe{}, &zzStubOverview{err: appbilling.ErrUnauthenticated})
	e := zzCtxEngine(h, "sub-abc")

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/billing/overview", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestZZOverview_PlatformNotFound_Returns404(t *testing.T) {
	h := handlerbilling.New(&zzStubSubscribe{}, &zzStubOverview{
		err: &platformclient.Error{Code: platformclient.ErrCodeNotFound, Message: "account missing"},
	})
	e := zzCtxEngine(h, "sub-abc")

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/billing/overview", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not_found") {
		t.Errorf("expected not_found code: %s", rec.Body.String())
	}
}

func TestZZOverview_GenericError_Returns500(t *testing.T) {
	h := handlerbilling.New(&zzStubSubscribe{}, &zzStubOverview{err: errors.New("db exploded")})
	e := zzCtxEngine(h, "sub-abc")

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/billing/overview", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d (%s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "db exploded") {
		t.Errorf("5xx body must not leak the raw internal error text, got: %s", rec.Body.String())
	}
}

func TestZZOverview_AsIsPassthrough(t *testing.T) {
	// Overview must pass platformclient.AccountOverview through unmodified —
	// no reshaping/filtering by the handler.
	ov := &platformclient.AccountOverview{}
	ov.Account.ID = 99
	ov.Account.Email = "zz@example.com"
	ov.Wallet = &platformclient.Wallet{Available: 1234}
	ov.Subscription = &platformclient.SubscriptionSnapshot{PlanCode: "enterprise", Status: "trialing"}

	h := handlerbilling.New(&zzStubSubscribe{}, &zzStubOverview{out: ov})
	e := zzCtxEngine(h, "sub-abc")

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/billing/overview", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var got platformclient.AccountOverview
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got.Account.ID != 99 || got.Account.Email != "zz@example.com" {
		t.Errorf("account fields not passed through as-is: %+v", got.Account)
	}
	if got.Wallet == nil || got.Wallet.Available != 1234 {
		t.Errorf("wallet not passed through as-is: %+v", got.Wallet)
	}
	if got.Subscription == nil || got.Subscription.PlanCode != "enterprise" || got.Subscription.Status != "trialing" {
		t.Errorf("subscription not passed through as-is: %+v", got.Subscription)
	}
}
