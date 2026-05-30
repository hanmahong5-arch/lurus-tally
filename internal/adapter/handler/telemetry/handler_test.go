package telemetry_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/telemetry"
	adapternats "github.com/hanmahong5-arch/lurus-tally/internal/adapter/nats"
)

// fakePublisher records every PublishWebTelemetry call so tests can assert
// the right tenant / event / payload made it through the allow-list gate.
type fakePublisher struct {
	mu    sync.Mutex
	calls []fakeCall
	// publishErr, if non-nil, is returned from PublishWebTelemetry.
	publishErr error
}

type fakeCall struct {
	Tenant  string
	Event   string
	Payload map[string]any
}

func (f *fakePublisher) PublishWebTelemetry(_ context.Context, tenantID, eventName string, payload any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, _ := payload.(map[string]any)
	f.calls = append(f.calls, fakeCall{Tenant: tenantID, Event: eventName, Payload: m})
	return f.publishErr
}

// All other Publisher methods are unused by this handler; return nil.
func (f *fakePublisher) Publish(_ context.Context, _ string, _ any) error { return nil }
func (f *fakePublisher) PublishStockMovementRecorded(_ context.Context, _ string, _ adapternats.StockMovementRecordedPayload) error {
	return nil
}
func (f *fakePublisher) PublishStockSnapshotUpdated(_ context.Context, _ string, _ adapternats.StockSnapshotUpdatedPayload) error {
	return nil
}
func (f *fakePublisher) PublishBillCreated(_ context.Context, _ string, _ adapternats.BillCreatedPayload) error {
	return nil
}
func (f *fakePublisher) PublishBillApproved(_ context.Context, _ string, _ adapternats.BillApprovedPayload) error {
	return nil
}
func (f *fakePublisher) PublishBillRejected(_ context.Context, _ string, _ adapternats.BillRejectedPayload) error {
	return nil
}
func (f *fakePublisher) PublishLowStockAlert(_ context.Context, _ string, _ adapternats.LowStockAlertPayload) error {
	return nil
}
func (f *fakePublisher) Close() error { return nil }

// fakeDAU records every Record call so tests can assert the handler forwards
// (event, userID) exactly when a non-blank user id is present.
type fakeDAU struct {
	mu    sync.Mutex
	calls []dauCall
}

type dauCall struct {
	Event  string
	UserID string
}

func (f *fakeDAU) Record(_ context.Context, event, userID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, dauCall{Event: event, UserID: userID})
	return nil
}

func newRouter(h *telemetry.Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h.Register(r)
	return r
}

func postJSON(t *testing.T, r *gin.Engine, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/telemetry/web", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestTelemetry_AllowListedEvent_PublishesToFake(t *testing.T) {
	pub := &fakePublisher{}
	h := telemetry.New(pub, "", "anonymous", nil)
	r := newRouter(h)

	rec := postJSON(t, r, map[string]any{
		"event":     "palette_invocation",
		"tenant_id": "t-1",
		"metadata":  map[string]any{"latency_ms": 123, "action_picked": "navigate"},
	}, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if len(pub.calls) != 1 {
		t.Fatalf("expected 1 publish, got %d", len(pub.calls))
	}
	got := pub.calls[0]
	if got.Tenant != "t-1" || got.Event != "palette_invocation" {
		t.Errorf("publish: tenant=%q event=%q, want t-1/palette_invocation", got.Tenant, got.Event)
	}
	if got.Payload["action_picked"] != "navigate" {
		t.Errorf("payload missing action_picked: %+v", got.Payload)
	}
}

func TestTelemetry_UnknownEvent_Returns400(t *testing.T) {
	pub := &fakePublisher{}
	h := telemetry.New(pub, "", "anonymous", nil)
	r := newRouter(h)

	rec := postJSON(t, r, map[string]any{"event": "totally_bogus"}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if len(pub.calls) != 0 {
		t.Fatalf("publish should not happen on unknown event")
	}
}

func TestTelemetry_MissingTenant_FallsBackToDefault(t *testing.T) {
	pub := &fakePublisher{}
	h := telemetry.New(pub, "", "anonymous", nil)
	r := newRouter(h)

	rec := postJSON(t, r, map[string]any{"event": "ai_drawer_open"}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(pub.calls) != 1 || pub.calls[0].Tenant != "anonymous" {
		t.Fatalf("expected anonymous fallback, got %+v", pub.calls)
	}
}

func TestTelemetry_BearerAuth_RejectsBadKey(t *testing.T) {
	pub := &fakePublisher{}
	h := telemetry.New(pub, "real-secret", "anonymous", nil)
	r := newRouter(h)

	rec := postJSON(t, r, map[string]any{"event": "cmd_z_used"}, map[string]string{
		"Authorization": "Bearer WRONG",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if len(pub.calls) != 0 {
		t.Errorf("should not publish on auth fail")
	}
}

func TestTelemetry_BearerAuth_AcceptsRightKey(t *testing.T) {
	pub := &fakePublisher{}
	h := telemetry.New(pub, "real-secret", "anonymous", nil)
	r := newRouter(h)

	rec := postJSON(t, r, map[string]any{"event": "undo_used", "tenant_id": "t-1"}, map[string]string{
		"Authorization": "Bearer real-secret",
	})
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestTelemetry_PublishFailure_StillReturns200(t *testing.T) {
	pub := &fakePublisher{publishErr: errPub}
	h := telemetry.New(pub, "", "anonymous", nil)
	r := newRouter(h)

	rec := postJSON(t, r, map[string]any{"event": "draft_restore", "tenant_id": "t-1"}, nil)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 even on publish failure", rec.Code)
	}
	if rec.Header().Get("X-Telemetry-Status") != "publish-failed" {
		t.Errorf("expected X-Telemetry-Status=publish-failed, got %q", rec.Header().Get("X-Telemetry-Status"))
	}
}

func TestTelemetry_RecordsDAU_WhenUserIDPresent(t *testing.T) {
	pub := &fakePublisher{}
	rec := &fakeDAU{}
	h := telemetry.New(pub, "", "anonymous", rec)
	r := newRouter(h)

	resp := postJSON(t, r, map[string]any{
		"event":   "palette_invocation",
		"user_id": "user-123",
	}, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", resp.Code, resp.Body.String())
	}
	if len(rec.calls) != 1 {
		t.Fatalf("expected 1 DAU record, got %d", len(rec.calls))
	}
	if rec.calls[0].Event != "palette_invocation" || rec.calls[0].UserID != "user-123" {
		t.Errorf("DAU record = %+v, want palette_invocation/user-123", rec.calls[0])
	}
}

func TestTelemetry_SkipsDAU_WhenUserIDEmpty(t *testing.T) {
	pub := &fakePublisher{}
	rec := &fakeDAU{}
	h := telemetry.New(pub, "", "anonymous", rec)
	r := newRouter(h)

	resp := postJSON(t, r, map[string]any{"event": "ai_drawer_open"}, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d", resp.Code)
	}
	if len(rec.calls) != 0 {
		t.Errorf("expected no DAU record for anonymous event, got %+v", rec.calls)
	}
}

// planAcceptValue reads the current tally_plan_accept_total{accepted=<v>} from
// the default registry. The middleware counter is unexported and cannot be
// Reset() from this package, so tests compare before/after deltas instead.
func planAcceptValue(t *testing.T, accepted string) float64 {
	t.Helper()
	fams, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, mf := range fams {
		if mf.GetName() != "tally_plan_accept_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "accepted" && l.GetValue() == accepted {
					return m.GetCounter().GetValue()
				}
			}
		}
	}
	return 0
}

// TestTelemetry_PlanAcceptRate_IncrementsByOutcome verifies the KS2 wiring:
// a plan_accept_rate event bumps tally_plan_accept_total under the label that
// matches metadata.accepted, and a missing field normalizes to "unknown".
func TestTelemetry_PlanAcceptRate_IncrementsByOutcome(t *testing.T) {
	pub := &fakePublisher{}
	h := telemetry.New(pub, "", "anonymous", nil)
	r := newRouter(h)

	before1 := planAcceptValue(t, "1")
	before0 := planAcceptValue(t, "0")
	beforeU := planAcceptValue(t, "unknown")

	if rec := postJSON(t, r, map[string]any{
		"event":    "plan_accept_rate",
		"metadata": map[string]any{"accepted": "1"},
	}, nil); rec.Code != http.StatusOK {
		t.Fatalf("accepted=1 status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := postJSON(t, r, map[string]any{
		"event":    "plan_accept_rate",
		"metadata": map[string]any{"accepted": "0"},
	}, nil); rec.Code != http.StatusOK {
		t.Fatalf("accepted=0 status = %d", rec.Code)
	}
	// No accepted field at all → unknown.
	if rec := postJSON(t, r, map[string]any{"event": "plan_accept_rate"}, nil); rec.Code != http.StatusOK {
		t.Fatalf("missing-accepted status = %d", rec.Code)
	}

	if got := planAcceptValue(t, "1") - before1; got != 1 {
		t.Errorf(`accepted="1" delta = %v, want 1`, got)
	}
	if got := planAcceptValue(t, "0") - before0; got != 1 {
		t.Errorf(`accepted="0" delta = %v, want 1`, got)
	}
	if got := planAcceptValue(t, "unknown") - beforeU; got != 1 {
		t.Errorf(`accepted="unknown" delta = %v, want 1`, got)
	}
}

// sentinel for the failure test
var errPub = sentinelErr("publish denied")

type sentinelErr string

func (s sentinelErr) Error() string { return string(s) }
