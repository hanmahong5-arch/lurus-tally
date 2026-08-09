package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	adapternats "github.com/hanmahong5-arch/lurus-tally/internal/adapter/nats"
)

// ---- mapStatus: full status → ErrCode table -------------------------------

func TestMapStatus_Table(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   []byte
		want   ErrCode
	}{
		{"401 unauthorized", http.StatusUnauthorized, nil, ErrCodeUnauthorized},
		{"403 forbidden", http.StatusForbidden, nil, ErrCodeUnauthorized},
		{"404 not found", http.StatusNotFound, nil, ErrCodeNotFound},
		{"402 payment required", http.StatusPaymentRequired, nil, ErrCodeInsufficientBalance},
		{"400 bad request", http.StatusBadRequest, nil, ErrCodeInvalidParameter},
		// 400 is matched by the switch before the body is ever inspected, so an
		// insufficient_balance body on a 400 must NOT flip the code.
		{"400 with insufficient_balance body stays invalid_parameter", http.StatusBadRequest, []byte(`{"error":"insufficient_balance"}`), ErrCodeInvalidParameter},
		{"500 internal error", http.StatusInternalServerError, nil, ErrCodeUnavailable},
		{"502 bad gateway", http.StatusBadGateway, nil, ErrCodeUnavailable},
		{"503 unavailable", http.StatusServiceUnavailable, nil, ErrCodeUnavailable},
		{"599 unavailable ceiling", 599, nil, ErrCodeUnavailable},
		{"409 conflict unknown", http.StatusConflict, nil, ErrCodeUnknown},
		{"409 conflict body-sniffed insufficient_balance", http.StatusConflict, []byte(`{"message":"insufficient_balance: wallet empty"}`), ErrCodeInsufficientBalance},
		{"422 unprocessable no sniff match", http.StatusUnprocessableEntity, []byte(`{"error":"bad plan"}`), ErrCodeUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mapStatus(tc.status, tc.body)
			if got != tc.want {
				t.Errorf("mapStatus(%d, %q) = %s, want %s", tc.status, tc.body, got, tc.want)
			}
		})
	}
}

// ---- extractMessage: message > detail > error precedence ------------------

func TestExtractMessage_Precedence(t *testing.T) {
	cases := []struct {
		name string
		body []byte
		want string
	}{
		{"empty body", nil, ""},
		{"empty slice body", []byte{}, ""},
		{"message wins over detail and error", []byte(`{"message":"m","detail":"d","error":"e"}`), "m"},
		{"detail wins over error when no message", []byte(`{"detail":"d","error":"e"}`), "d"},
		{"error used when only field present", []byte(`{"error":"e"}`), "e"},
		{"all empty fields yields empty string", []byte(`{}`), ""},
		{"malformed json yields empty string", []byte(`not json`), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractMessage(tc.body)
			if got != tc.want {
				t.Errorf("extractMessage(%q) = %q, want %q", tc.body, got, tc.want)
			}
		})
	}
}

// ---- itoa: zero / positive / negative --------------------------------------

func TestItoa_Table(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0"},
		{42, "42"},
		{-7, "-7"},
		{1234567890, "1234567890"},
		{-1234567890, "-1234567890"},
	}
	for _, tc := range cases {
		got := itoa(tc.n)
		want := strconv.FormatInt(tc.n, 10)
		if got != want || got != tc.want {
			t.Errorf("itoa(%d) = %q, want %q", tc.n, got, want)
		}
	}
}

// ---- Error.Error() formatting ------------------------------------------

func TestError_ErrorString(t *testing.T) {
	withMsg := &Error{Code: ErrCodeNotFound, HTTPStatus: 404, Message: "account not found"}
	if got, want := withMsg.Error(), "platform: account not found (not_found, http 404)"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}

	noMsg := &Error{Code: ErrCodeUnavailable, HTTPStatus: 502}
	if got, want := noMsg.Error(), "platform: unavailable (http 502)"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// ---- IsCode -----------------------------------------------------------

func TestIsCode(t *testing.T) {
	pe := &Error{Code: ErrCodeInsufficientBalance}
	if !IsCode(pe, ErrCodeInsufficientBalance) {
		t.Error("expected true for matching *Error code")
	}
	if IsCode(pe, ErrCodeNotFound) {
		t.Error("expected false for mismatched code")
	}
	if IsCode(errors.New("plain error"), ErrCodeNotFound) {
		t.Error("expected false for non-*Error")
	}
	if IsCode(nil, ErrCodeNotFound) {
		t.Error("expected false for nil error")
	}
}

// ---- New: covered lightly for completeness (baseline already asserted in client_test.go) --

func TestNew_TimeoutDefaultsAndOverride(t *testing.T) {
	c, err := New(Config{BaseURL: "http://example.invalid", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.http.Timeout != defaultTimeout {
		t.Errorf("expected default timeout %v, got %v", defaultTimeout, c.http.Timeout)
	}
}

// ---- do(): malformed JSON / empty body / transport failure -----------------

func TestDo_2xxMalformedJSON_ReturnsDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{not-json`))
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var out Account
	derr := c.do(context.Background(), http.MethodGet, "/whatever", nil, &out)
	if derr == nil {
		t.Fatal("expected decode error, got nil")
	}
	var pe *Error
	if !errors.As(derr, &pe) {
		t.Fatalf("expected *Error, got %#v", derr)
	}
	if pe.Code != ErrCodeUnknown {
		t.Errorf("expected ErrCodeUnknown, got %s", pe.Code)
	}
	if pe.HTTPStatus != http.StatusOK {
		t.Errorf("expected HTTPStatus 200, got %d", pe.HTTPStatus)
	}
}

func TestDo_2xxEmptyBody_OutNonNil_ReturnsNilNoDecode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent) // no body at all
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	out := Account{ID: 999} // sentinel: must remain untouched (no decode attempted)
	if derr := c.do(context.Background(), http.MethodGet, "/whatever", nil, &out); derr != nil {
		t.Fatalf("expected nil error on empty 2xx body, got %v", derr)
	}
	if out.ID != 999 {
		t.Errorf("out was mutated despite empty body: %+v", out)
	}
}

func TestDo_2xxOutNil_ReturnsNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":1}`)) // out is nil, so this must simply be ignored
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if derr := c.do(context.Background(), http.MethodPost, "/whatever", nil, nil); derr != nil {
		t.Fatalf("expected nil error when out is nil, got %v", derr)
	}
}

// TestDo_TransportError_MapsToUnavailable uses a POST (single-shot, no client
// retry per httpx.DefaultConfig RetryMethods) against a server that is closed
// before the call, so the failure is a fast connection error rather than a
// slow, retried one.
func TestDo_TransportError_MapsToUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	baseURL := srv.URL
	srv.Close() // server is now unreachable

	c, err := New(Config{BaseURL: baseURL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	derr := c.do(context.Background(), http.MethodPost, "/whatever", map[string]string{"a": "b"}, nil)
	if !IsCode(derr, ErrCodeUnavailable) {
		t.Fatalf("expected ErrCodeUnavailable, got %v", derr)
	}
}

// ---- doWithIdem: Idempotency-Key header propagation ------------------------

func TestDoWithIdem_ForwardsIdempotencyKeyHeader(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("Idempotency-Key")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if derr := c.doWithIdem(context.Background(), http.MethodPost, "/x", nil, "idem-key-1", nil); derr != nil {
		t.Fatalf("doWithIdem: %v", derr)
	}
	if gotHeader != "idem-key-1" {
		t.Errorf("Idempotency-Key header = %q, want %q", gotHeader, "idem-key-1")
	}
}

func TestDoWithIdem_EmptyKey_OmitsHeader(t *testing.T) {
	var sawHeader bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, sawHeader = r.Header["Idempotency-Key"]
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if derr := c.do(context.Background(), http.MethodGet, "/x", nil, nil); derr != nil {
		t.Fatalf("do: %v", derr)
	}
	if sawHeader {
		t.Error("expected no Idempotency-Key header when key is empty")
	}
}

// ---- UpsertAccount: pre-flight validation + happy path --------------------

func TestUpsertAccount_EmptyIDPSubject_NoHTTPCall(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, uerr := c.UpsertAccount(context.Background(), UpsertAccountRequest{Email: "a@b.c"})
	if !IsCode(uerr, ErrCodeInvalidParameter) {
		t.Fatalf("expected ErrCodeInvalidParameter, got %v", uerr)
	}
	if called {
		t.Error("expected no HTTP call for pre-flight validation failure")
	}
}

func TestUpsertAccount_EmptyEmail_NoHTTPCall(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, uerr := c.UpsertAccount(context.Background(), UpsertAccountRequest{IDPSubject: "sub-1"})
	if !IsCode(uerr, ErrCodeInvalidParameter) {
		t.Fatalf("expected ErrCodeInvalidParameter, got %v", uerr)
	}
	if called {
		t.Error("expected no HTTP call for pre-flight validation failure")
	}
}

func TestUpsertAccount_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("want POST, got %s", r.Method)
		}
		if r.URL.Path != "/internal/v1/accounts/upsert" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":7,"idp_subject":"sub-1","username":"u","email":"a@b.c"}`))
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	acc, uerr := c.UpsertAccount(context.Background(), UpsertAccountRequest{IDPSubject: "sub-1", Email: "a@b.c"})
	if uerr != nil {
		t.Fatalf("UpsertAccount: %v", uerr)
	}
	if acc.ID != 7 || acc.IDPSubject != "sub-1" {
		t.Errorf("decoded wrong: %+v", acc)
	}
}

// ---- GetAccountByIDPSubject: pre-flight validation -------------------------

func TestGetAccountByIDPSubject_EmptySub_NoHTTPCall(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, gerr := c.GetAccountByIDPSubject(context.Background(), "")
	if !IsCode(gerr, ErrCodeInvalidParameter) {
		t.Fatalf("expected ErrCodeInvalidParameter, got %v", gerr)
	}
	if called {
		t.Error("expected no HTTP call")
	}
}

// ---- GetAccountOverview: pre-flight validation ------------------------------

func TestGetAccountOverview_InvalidArgs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c, err := New(Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, gerr := c.GetAccountOverview(context.Background(), 0, "tally"); !IsCode(gerr, ErrCodeInvalidParameter) {
		t.Errorf("accountID=0: expected ErrCodeInvalidParameter, got %v", gerr)
	}
	if _, gerr := c.GetAccountOverview(context.Background(), -5, "tally"); !IsCode(gerr, ErrCodeInvalidParameter) {
		t.Errorf("accountID<0: expected ErrCodeInvalidParameter, got %v", gerr)
	}
	if _, gerr := c.GetAccountOverview(context.Background(), 42, ""); !IsCode(gerr, ErrCodeInvalidParameter) {
		t.Errorf("empty productID: expected ErrCodeInvalidParameter, got %v", gerr)
	}
}

// TestGetAccountOverview_PlatformError_PropagatesUnwrapped covers the
// `if err := c.do(...); err != nil { return nil, err }` branch — a valid
// request that the platform rejects must surface the *Error unchanged.
func TestGetAccountOverview_PlatformError_PropagatesUnwrapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"account not found"}`))
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, gerr := c.GetAccountOverview(context.Background(), 42, "tally")
	if !IsCode(gerr, ErrCodeNotFound) {
		t.Fatalf("expected ErrCodeNotFound, got %v", gerr)
	}
}

// ---- doWithIdem: payload encode error / request build error ---------------

func TestDoWithIdem_PayloadMarshalError_ReturnsEncodeError(t *testing.T) {
	c, err := New(Config{BaseURL: "http://example.invalid", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// A channel value is never JSON-marshalable, forcing the json.Marshal(payload)
	// branch to fail before any request is built.
	derr := c.doWithIdem(context.Background(), http.MethodPost, "/x", make(chan int), "", nil)
	if !IsCode(derr, ErrCodeUnknown) {
		t.Fatalf("expected ErrCodeUnknown, got %v", derr)
	}
}

func TestDoWithIdem_BuildRequestError_ReturnsUnknown(t *testing.T) {
	c, err := New(Config{BaseURL: "http://example.invalid", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// A method containing a control character is rejected by
	// http.NewRequestWithContext at the "build request" step.
	derr := c.doWithIdem(context.Background(), "BAD\nMETHOD", "/x", nil, "", nil)
	if !IsCode(derr, ErrCodeUnknown) {
		t.Fatalf("expected ErrCodeUnknown, got %v", derr)
	}
}

// ---- GetEntitlements: pre-flight validation + happy path -------------------

func TestGetEntitlements_InvalidArgs(t *testing.T) {
	c, err := New(Config{BaseURL: "http://example.invalid", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, gerr := c.GetEntitlements(context.Background(), 0, "tally"); !IsCode(gerr, ErrCodeInvalidParameter) {
		t.Errorf("accountID=0: expected ErrCodeInvalidParameter, got %v", gerr)
	}
	if _, gerr := c.GetEntitlements(context.Background(), 42, ""); !IsCode(gerr, ErrCodeInvalidParameter) {
		t.Errorf("empty productID: expected ErrCodeInvalidParameter, got %v", gerr)
	}
}

func TestGetEntitlements_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/internal/v1/accounts/42/entitlements/tally"
		if r.URL.Path != wantPath {
			t.Errorf("wrong path: got %s want %s", r.URL.Path, wantPath)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"plan_code":"pro"}`))
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ents, gerr := c.GetEntitlements(context.Background(), 42, "tally")
	if gerr != nil {
		t.Fatalf("GetEntitlements: %v", gerr)
	}
	if ents["plan_code"] != "pro" {
		t.Errorf("decoded wrong: %+v", ents)
	}
}

func TestGetEntitlements_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"account not found"}`))
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, gerr := c.GetEntitlements(context.Background(), 42, "tally")
	if !IsCode(gerr, ErrCodeNotFound) {
		t.Fatalf("expected ErrCodeNotFound, got %v", gerr)
	}
}

// ---- SubscriptionCheckout: pre-flight validation ---------------------------

func TestSubscriptionCheckout_InvalidArgs(t *testing.T) {
	c, err := New(Config{BaseURL: "http://example.invalid", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	cases := []SubscriptionCheckoutRequest{
		{AccountID: 0, ProductID: "tally", PlanCode: "pro", BillingCycle: "monthly", PaymentMethod: "wallet"},
		{AccountID: 42, ProductID: "", PlanCode: "pro", BillingCycle: "monthly", PaymentMethod: "wallet"},
		{AccountID: 42, ProductID: "tally", PlanCode: "", BillingCycle: "monthly", PaymentMethod: "wallet"},
		{AccountID: 42, ProductID: "tally", PlanCode: "pro", BillingCycle: "", PaymentMethod: "wallet"},
		{AccountID: 42, ProductID: "tally", PlanCode: "pro", BillingCycle: "monthly", PaymentMethod: ""},
	}
	for i, req := range cases {
		if _, cerr := c.SubscriptionCheckout(context.Background(), req, "idem"); !IsCode(cerr, ErrCodeInvalidParameter) {
			t.Errorf("case %d: expected ErrCodeInvalidParameter, got %v", i, cerr)
		}
	}
}

// ---- ReportUsageEvent: append-only metering, no wallet debit ---------------

func TestReportUsageEvent_InvalidArgs(t *testing.T) {
	c, err := New(Config{BaseURL: "http://example.invalid", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if rerr := c.ReportUsageEvent(context.Background(), UsageEventRequest{ProductID: "tally", Metric: "llm_tokens"}); !IsCode(rerr, ErrCodeInvalidParameter) {
		t.Errorf("accountID missing: expected ErrCodeInvalidParameter, got %v", rerr)
	}
	if rerr := c.ReportUsageEvent(context.Background(), UsageEventRequest{AccountID: 1, Metric: "llm_tokens"}); !IsCode(rerr, ErrCodeInvalidParameter) {
		t.Errorf("productID missing: expected ErrCodeInvalidParameter, got %v", rerr)
	}
	if rerr := c.ReportUsageEvent(context.Background(), UsageEventRequest{AccountID: 1, ProductID: "tally"}); !IsCode(rerr, ErrCodeInvalidParameter) {
		t.Errorf("metric missing: expected ErrCodeInvalidParameter, got %v", rerr)
	}
}

// TestReportUsageEvent_PostsToUsageEventsEndpoint_NoWalletDebit asserts the
// business invariant that usage reporting is a pure metering POST to
// /internal/v1/usage/events — never a billing/wallet endpoint — and that
// platform errors flow back verbatim so shadow-mode callers can drop them
// non-fatally.
func TestReportUsageEvent_PostsToUsageEventsEndpoint_NoWalletDebit(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody UsageEventRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK) // no body: append-only ack, nothing to decode
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	req := UsageEventRequest{AccountID: 7, ProductID: "tally", Metric: "llm_tokens", Quantity: 100}
	if rerr := c.ReportUsageEvent(context.Background(), req); rerr != nil {
		t.Fatalf("ReportUsageEvent: %v", rerr)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/internal/v1/usage/events" {
		t.Errorf("path = %s, want /internal/v1/usage/events", gotPath)
	}
	if gotBody.AccountID != 7 || gotBody.Quantity != 100 {
		t.Errorf("body not forwarded: %+v", gotBody)
	}
}

func TestReportUsageEvent_PlatformError_ReturnedVerbatim(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"message":"platform overloaded"}`))
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rerr := c.ReportUsageEvent(context.Background(), UsageEventRequest{AccountID: 1, ProductID: "tally", Metric: "llm_tokens"})
	if !IsCode(rerr, ErrCodeUnavailable) {
		t.Fatalf("expected ErrCodeUnavailable, got %v", rerr)
	}
	var pe *Error
	if !errors.As(rerr, &pe) || pe.Message != "platform overloaded" {
		t.Fatalf("expected message passthrough, got %#v", rerr)
	}
}

// ---- Concurrency: Client is documented safe for concurrent use ------------

func TestClient_ConcurrentCalls_RaceSafe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"plan_code":"free"}`))
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_, gerr := c.GetEntitlements(context.Background(), int64(id+1), "tally")
			errs <- gerr
		}(i)
	}
	wg.Wait()
	close(errs)
	for gerr := range errs {
		if gerr != nil {
			t.Errorf("concurrent GetEntitlements failed: %v", gerr)
		}
	}
}

// ---- NotificationClient.NotifySync: remaining error branches --------------

func TestNotifySync_MarshalError_ReturnsError(t *testing.T) {
	nc := NewNotificationClient(NotificationConfig{NATSPublisher: &zzFakePublisher{}})
	// Metadata carrying a channel value is never JSON-marshalable, forcing the
	// json.Marshal(req) branch to fail before any request is built.
	req := NotifyRequest{
		AccountID: 1,
		Type:      "t",
		Title:     "T",
		Metadata:  map[string]any{"bad": make(chan int)},
	}
	if err := nc.NotifySync(context.Background(), req); err == nil {
		t.Error("expected marshal error, got nil")
	}
}

func TestNotifySync_BuildRequestError_ReturnsError(t *testing.T) {
	// notifyURL + "/internal/v1/notify" containing a control character makes
	// http.NewRequestWithContext fail at the "build request" step.
	nc := NewNotificationClient(NotificationConfig{
		NATSPublisher: &zzFakePublisher{},
		NotifyURL:     "http://\x7f\ninvalid",
	})
	if err := nc.NotifySync(context.Background(), NotifyRequest{AccountID: 1, Type: "t", Title: "T"}); err == nil {
		t.Error("expected build-request error, got nil")
	}
}

func TestNotifySync_TransportError_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	notifyURL := srv.URL
	srv.Close() // now unreachable: httpClient.Do must fail

	nc := NewNotificationClient(NotificationConfig{
		NATSPublisher: &zzFakePublisher{},
		NotifyURL:     notifyURL,
	})
	if err := nc.NotifySync(context.Background(), NotifyRequest{AccountID: 1, Type: "t", Title: "T"}); err == nil {
		t.Error("expected transport error, got nil")
	}
}

// ---- NotificationClient.Close --------------------------------------------

func TestNotificationClient_Close_ClosesPublisher(t *testing.T) {
	pub := &zzFakePublisher{}
	nc := NewNotificationClient(NotificationConfig{NATSPublisher: pub})
	if err := nc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !pub.closed {
		t.Error("expected underlying publisher Close to be invoked")
	}
}

func TestNotificationClient_Close_NilPublisher_ReturnsNil(t *testing.T) {
	nc := NewNotificationClient(NotificationConfig{})
	if err := nc.Close(); err != nil {
		t.Fatalf("expected nil error with nil publisher, got %v", err)
	}
}

// zzFakePublisher is a minimal adapternats.Publisher double, local to this
// file so it does not collide with the fakePublisher already defined in
// notification_test.go (different package: platform vs platform_test).
type zzFakePublisher struct {
	closed bool
}

func (f *zzFakePublisher) Publish(_ context.Context, _ string, _ any) error { return nil }
func (f *zzFakePublisher) Close() error                                    { f.closed = true; return nil }
func (f *zzFakePublisher) PublishStockMovementRecorded(_ context.Context, _ string, _ adapternats.StockMovementRecordedPayload) error {
	return nil
}
func (f *zzFakePublisher) PublishStockSnapshotUpdated(_ context.Context, _ string, _ adapternats.StockSnapshotUpdatedPayload) error {
	return nil
}
func (f *zzFakePublisher) PublishBillCreated(_ context.Context, _ string, _ adapternats.BillCreatedPayload) error {
	return nil
}
func (f *zzFakePublisher) PublishBillApproved(_ context.Context, _ string, _ adapternats.BillApprovedPayload) error {
	return nil
}
func (f *zzFakePublisher) PublishBillRejected(_ context.Context, _ string, _ adapternats.BillRejectedPayload) error {
	return nil
}
func (f *zzFakePublisher) PublishLowStockAlert(_ context.Context, _ string, _ adapternats.LowStockAlertPayload) error {
	return nil
}
func (f *zzFakePublisher) PublishWebTelemetry(_ context.Context, _ string, _ string, _ any) error {
	return nil
}
