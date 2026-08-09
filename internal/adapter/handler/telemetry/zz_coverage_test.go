package telemetry_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/telemetry"
)

// zzFakeDAUErr always returns an error from Record, so tests can assert the
// X-DAU-Status header is set on failure while the response still succeeds
// (telemetry — and DAU counting — must never block the caller).
type zzFakeDAUErr struct{}

func (zzFakeDAUErr) Record(_ context.Context, _, _ string) error {
	return sentinelErr("redis down")
}

// zzPostRaw posts a raw body (not JSON-marshaled) so malformed-JSON cases can
// be exercised directly.
func zzPostRaw(t *testing.T, r *gin.Engine, raw string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/telemetry/web", bytes.NewReader([]byte(raw)))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestZZ_BearerAuth_TableDriven covers the three internal-auth branches: no
// Authorization header at all, a wrong bearer token, and the correct token —
// with expectedKey non-empty in all three so the constant-time compare path
// actually runs.
func TestZZ_BearerAuth_TableDriven(t *testing.T) {
	cases := []struct {
		name       string
		headers    map[string]string
		wantStatus int
	}{
		{
			name:       "missing Authorization header",
			headers:    nil,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "wrong bearer token",
			headers:    map[string]string{"Authorization": "Bearer nope"},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "correct bearer token",
			headers:    map[string]string{"Authorization": "Bearer real-secret"},
			wantStatus: http.StatusOK,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pub := &fakePublisher{}
			h := telemetry.New(pub, "real-secret", "anonymous", nil)
			r := newRouter(h)

			rec := postJSON(t, r, map[string]any{"event": "palette_invocation"}, tc.headers)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantStatus == http.StatusUnauthorized && len(pub.calls) != 0 {
				t.Errorf("should not publish on auth failure, got %+v", pub.calls)
			}
		})
	}
}

// TestZZ_AuthDisabled_WhenExpectedKeyEmpty locks in the dev-mode bypass: with
// expectedKey=="" the constant-time-compare branch is skipped entirely, even
// though no Authorization header is sent.
func TestZZ_AuthDisabled_WhenExpectedKeyEmpty(t *testing.T) {
	pub := &fakePublisher{}
	h := telemetry.New(pub, "", "anonymous", nil)
	r := newRouter(h)

	rec := postJSON(t, r, map[string]any{"event": "palette_invocation"}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with auth disabled; body=%s", rec.Code, rec.Body.String())
	}
	if len(pub.calls) != 1 {
		t.Fatalf("expected 1 publish, got %d", len(pub.calls))
	}
}

// TestZZ_InvalidJSONBody_Returns400 exercises the ShouldBindJSON error path.
func TestZZ_InvalidJSONBody_Returns400(t *testing.T) {
	pub := &fakePublisher{}
	h := telemetry.New(pub, "", "anonymous", nil)
	r := newRouter(h)

	rec := zzPostRaw(t, r, `{"event": not-valid-json`, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() == "" {
		t.Fatal("expected a body on invalid JSON")
	}
	if len(pub.calls) != 0 {
		t.Errorf("should not publish on invalid JSON, got %+v", pub.calls)
	}
}

// TestZZ_MissingEventField_Returns400 exercises the body.Event == "" branch,
// distinct from the allow-list-miss branch below.
func TestZZ_MissingEventField_Returns400(t *testing.T) {
	pub := &fakePublisher{}
	h := telemetry.New(pub, "", "anonymous", nil)
	r := newRouter(h)

	rec := postJSON(t, r, map[string]any{"tenant_id": "t-1"}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if len(pub.calls) != 0 {
		t.Errorf("should not publish when event is missing, got %+v", pub.calls)
	}
}

// TestZZ_UnknownEvent_EchoesEventInBody asserts the allow-list-miss branch
// echoes the offending event name back in the JSON body (distinct assertion
// from the pre-existing TestTelemetry_UnknownEvent_Returns400, which only
// checks status).
func TestZZ_UnknownEvent_EchoesEventInBody(t *testing.T) {
	pub := &fakePublisher{}
	h := telemetry.New(pub, "", "anonymous", nil)
	r := newRouter(h)

	rec := postJSON(t, r, map[string]any{"event": "not_a_real_event"}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"not_a_real_event"`)) {
		t.Errorf("expected body to echo the rejected event name, got %s", rec.Body.String())
	}
}

// TestZZ_DAURecordFailure_SetsHeaderButStillReturns200 exercises the
// dau.Record error branch: X-DAU-Status is set to record-failed, but the
// call still succeeds (DAU counting is best-effort, never blocking).
func TestZZ_DAURecordFailure_SetsHeaderButStillReturns200(t *testing.T) {
	pub := &fakePublisher{}
	h := telemetry.New(pub, "", "anonymous", zzFakeDAUErr{})
	r := newRouter(h)

	rec := postJSON(t, r, map[string]any{
		"event":   "palette_invocation",
		"user_id": "user-err",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 even on DAU record failure; body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-DAU-Status") != "record-failed" {
		t.Errorf("expected X-DAU-Status=record-failed, got %q", rec.Header().Get("X-DAU-Status"))
	}
	// Publish must still happen — DAU failure is independent of the NATS path.
	if len(pub.calls) != 1 {
		t.Errorf("expected publish to still occur, got %d calls", len(pub.calls))
	}
}

// TestZZ_New_EmptyDefaultTenant_NormalizesToAnonymous is a non-self-validating
// check of New's normalization: passing "" for defaultTenant must behave
// identically (tenant fallback == "anonymous") to passing "anonymous"
// explicitly, because New hard-codes that default per its doc comment.
func TestZZ_New_EmptyDefaultTenant_NormalizesToAnonymous(t *testing.T) {
	pub := &fakePublisher{}
	h := telemetry.New(pub, "", "", nil) // defaultTenant: "" -> must normalize to "anonymous"
	r := newRouter(h)

	rec := postJSON(t, r, map[string]any{"event": "palette_invocation"}, nil) // no tenant_id in body
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(pub.calls) != 1 || pub.calls[0].Tenant != "anonymous" {
		t.Fatalf("expected fallback tenant 'anonymous' from New(\"\"), got %+v", pub.calls)
	}
}

// TestZZ_PlanAcceptRate_NonStringAccepted_NormalizesToUnknown covers the type
// assertion `body.Metadata["accepted"].(string)` failing (a JSON number, not
// a string) — the ", _" discard must fall through to the zero value "" and
// IncPlanAccept must record it under the "unknown" label, mirroring the
// missing-field case already covered by the pre-existing plan_accept_rate
// test but hitting the type-assertion-false branch specifically.
func TestZZ_PlanAcceptRate_NonStringAccepted_NormalizesToUnknown(t *testing.T) {
	pub := &fakePublisher{}
	h := telemetry.New(pub, "", "anonymous", nil)
	r := newRouter(h)

	beforeU := planAcceptValue(t, "unknown")

	rec := postJSON(t, r, map[string]any{
		"event":    "plan_accept_rate",
		"metadata": map[string]any{"accepted": 1}, // JSON number, not a string
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	if got := planAcceptValue(t, "unknown") - beforeU; got != 1 {
		t.Errorf(`accepted=<number> delta on "unknown" = %v, want 1`, got)
	}
	if len(pub.calls) != 1 || pub.calls[0].Event != "plan_accept_rate" {
		t.Fatalf("expected publish to still occur for plan_accept_rate, got %+v", pub.calls)
	}
}
