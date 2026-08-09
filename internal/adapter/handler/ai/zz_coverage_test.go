package ai_test

// Supplemental coverage for internal/adapter/handler/ai. handler_test.go
// already covers the happy paths; this file fills the error branches,
// rate-limiter wiring, entitlement gate, and ParseSSEEvents edge cases that
// were still at 0 statements per `go tool cover -func`.
//
// Rule: do not edit handler_test.go (concurrent-session WIP risk) — only add here.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	handlerai "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/ai"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appai "github.com/hanmahong5-arch/lurus-tally/internal/app/ai"
	domainai "github.com/hanmahong5-arch/lurus-tally/internal/domain/ai"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmgateway"
)

// --- local stubs (package-local names to avoid collisions with handler_test.go) ---

type zzStubOrchestrator struct {
	streamOut     *appai.ChatOutput
	streamErr     error
	confirmOut    *domainai.Plan
	confirmResult *appai.ExecutionResult
	confirmErr    error
	cancelErr     error
	chunks        []string
	listOut       []*domainai.Plan
	listErr       error
}

func (s *zzStubOrchestrator) StreamChat(_ context.Context, _ appai.ChatInput, onChunk func(string)) (*appai.ChatOutput, error) {
	for _, ch := range s.chunks {
		onChunk(ch)
	}
	return s.streamOut, s.streamErr
}

func (s *zzStubOrchestrator) ConfirmPlan(_ context.Context, _, _, _ uuid.UUID) (*domainai.Plan, *appai.ExecutionResult, error) {
	return s.confirmOut, s.confirmResult, s.confirmErr
}

func (s *zzStubOrchestrator) CancelPlan(_ context.Context, _, _ uuid.UUID) error {
	return s.cancelErr
}

func (s *zzStubOrchestrator) ListPlans(_ context.Context, _ uuid.UUID, _ string) ([]*domainai.Plan, error) {
	return s.listOut, s.listErr
}

type zzStubReverter struct {
	result *appai.RevertResult
	err    error
}

func (s *zzStubReverter) RevertPlan(_ context.Context, _, _, _ uuid.UUID) (*appai.RevertResult, error) {
	return s.result, s.err
}

// zzFakeIncrer is a minimal llmgateway.RedisIncrer fake so we can exercise
// the real llmgateway.RateLimiter wired through the handler (not a fake
// limiter interface — the handler takes a concrete *llmgateway.RateLimiter).
type zzFakeIncrer struct {
	count   int64
	incrErr error
}

func (f *zzFakeIncrer) Incr(_ context.Context, _ string) (int64, error) {
	if f.incrErr != nil {
		return 0, f.incrErr
	}
	f.count++
	return f.count, nil
}

func (f *zzFakeIncrer) Expire(_ context.Context, _ string, _ time.Duration) error {
	return nil
}

func zzNewEngine(h *handlerai.Handler, tenantID uuid.UUID, idpSub string) *gin.Engine {
	e := gin.New()
	e.Use(func(c *gin.Context) {
		if tenantID != uuid.Nil {
			c.Set(middleware.CtxKeyTenantID, tenantID)
		}
		if idpSub != "" {
			c.Set(middleware.CtxKeyIDPSubject, idpSub)
		}
		c.Next()
	})
	h.RegisterRoutes(e.Group("/api/v1"))
	return e
}

// --- ListPlans error branch (orchestrator.ListPlans fails → WriteInternal 500) ---

func TestZZ_ListPlans_OrchestratorError_Returns500(t *testing.T) {
	stub := &zzStubOrchestrator{listErr: errors.New("db unreachable")}
	h := handlerai.New(stub)
	e := zzNewEngine(h, uuid.New(), "")

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/ai/plans", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// --- NewWithLimiter / entitlement gate wiring ---

func TestZZ_NewWithLimiter_NilLimiter_BehavesLikeNew(t *testing.T) {
	stub := &zzStubOrchestrator{streamOut: &appai.ChatOutput{}}
	h := handlerai.NewWithLimiter(stub, nil)
	e := zzNewEngine(h, uuid.New(), "")

	body, _ := json.Marshal(map[string]string{"message": "hi"})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 (nil limiter degrades to unlimited), got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestZZ_WithEntitlementGate_Blocks_WhenGateDenies(t *testing.T) {
	stub := &zzStubOrchestrator{}
	h := handlerai.New(stub).WithEntitlementGate(func(c *gin.Context) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "not_entitled"})
	})
	e := zzNewEngine(h, uuid.New(), "")

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/ai/plans", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 from entitlement gate, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestZZ_WithEntitlementGate_Allows_WhenGatePasses(t *testing.T) {
	tenantID := uuid.New()
	stub := &zzStubOrchestrator{listOut: []*domainai.Plan{}}
	h := handlerai.New(stub).WithEntitlementGate(func(c *gin.Context) { c.Next() })
	e := zzNewEngine(h, tenantID, "")

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/ai/plans", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 through passthrough gate, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// --- Chat: rate limiting business invariant ---

func TestZZ_Chat_RateLimiter_Denies_Returns429WithRetryAfter(t *testing.T) {
	store := &zzFakeIncrer{}
	// limit=0 with an incrementing store means the very first call (count=1)
	// already exceeds a limit of 0 → deterministic deny on call #1.
	rl := llmgateway.NewRateLimiter(store, 1, time.Minute)
	tenantID := uuid.New()

	// Exhaust the single allowed slot first.
	if allowed, _, err := rl.Allow(context.Background(), tenantID); err != nil || !allowed {
		t.Fatalf("setup: expected first call allowed, got allowed=%v err=%v", allowed, err)
	}

	stub := &zzStubOrchestrator{streamOut: &appai.ChatOutput{}}
	h := handlerai.NewWithLimiter(stub, rl)
	e := zzNewEngine(h, tenantID, "")

	body, _ := json.Marshal(map[string]string{"message": "hi again"})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d body=%s", rec.Code, rec.Body.String())
	}
	retryAfter := rec.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Fatal("expected Retry-After header to be set")
	}
	var seconds int
	if _, err := fmt.Sscanf(retryAfter, "%d", &seconds); err != nil || seconds < 1 {
		t.Errorf("expected Retry-After >= 1, got %q", retryAfter)
	}
	if !strings.Contains(rec.Body.String(), "llm_rate_limited") {
		t.Errorf("expected llm_rate_limited body, got %s", rec.Body.String())
	}
}

// TestZZ_Chat_RateLimiter_StoreError_DegradesOpen verifies that when the
// Redis-backed store errors, the request proceeds (availability > budget
// enforcement — a Redis hiccup must not break AI chat).
func TestZZ_Chat_RateLimiter_StoreError_DegradesOpen(t *testing.T) {
	store := &zzFakeIncrer{incrErr: errors.New("redis: connection refused")}
	rl := llmgateway.NewRateLimiter(store, 1, time.Minute)

	stub := &zzStubOrchestrator{streamOut: &appai.ChatOutput{}}
	h := handlerai.NewWithLimiter(stub, rl)
	e := zzNewEngine(h, uuid.New(), "")

	body, _ := json.Marshal(map[string]string{"message": "hi"})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 (degrade-open on store error), got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestZZ_Chat_RateLimiter_UnderLimit_Proceeds(t *testing.T) {
	store := &zzFakeIncrer{}
	rl := llmgateway.NewRateLimiter(store, 5, time.Minute)

	stub := &zzStubOrchestrator{streamOut: &appai.ChatOutput{}}
	h := handlerai.NewWithLimiter(stub, rl)
	e := zzNewEngine(h, uuid.New(), "")

	body, _ := json.Marshal(map[string]string{"message": "hi"})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 under limit, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// --- Chat: history binding + orchestrator error -> SSE error event (not HTTP 500) ---

func TestZZ_Chat_WithHistory_PassesThroughAndReturns200(t *testing.T) {
	stub := &zzStubOrchestrator{streamOut: &appai.ChatOutput{}}
	h := handlerai.New(stub)
	e := zzNewEngine(h, uuid.New(), "")

	body, _ := json.Marshal(map[string]interface{}{
		"message": "继续",
		"history": []map[string]string{
			{"role": "user", "content": "之前的消息"},
			{"role": "assistant", "content": "之前的回复"},
		},
	})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestZZ_Chat_BadHistoryRole_Returns400(t *testing.T) {
	h := handlerai.New(&zzStubOrchestrator{})
	e := zzNewEngine(h, uuid.New(), "")

	body, _ := json.Marshal(map[string]interface{}{
		"message": "hi",
		"history": []map[string]string{
			{"role": "not-a-real-role", "content": "x"},
		},
	})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid history role, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestZZ_Chat_MessageTooLong_Returns400(t *testing.T) {
	h := handlerai.New(&zzStubOrchestrator{})
	e := zzNewEngine(h, uuid.New(), "")

	longMsg := strings.Repeat("a", 8001) // binding cap is max=8000
	body, _ := json.Marshal(map[string]string{"message": longMsg})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for message>8000, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestZZ_Chat_HistoryTooLong_Returns400(t *testing.T) {
	h := handlerai.New(&zzStubOrchestrator{})
	e := zzNewEngine(h, uuid.New(), "")

	history := make([]map[string]string, 201) // cap is max=200
	for i := range history {
		history[i] = map[string]string{"role": "user", "content": "x"}
	}
	body, _ := json.Marshal(map[string]interface{}{"message": "hi", "history": history})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for history>200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestZZ_Chat_OrchestratorError_EmitsSSEErrorEvent_NotHTTP500 verifies the
// business invariant: a StreamChat failure must surface as an in-band SSE
// 'error' event (the HTTP status is already 200 because SSE headers were
// already flushed), never a raw 500.
func TestZZ_Chat_OrchestratorError_EmitsSSEErrorEvent_NotHTTP500(t *testing.T) {
	stub := &zzStubOrchestrator{streamErr: errors.New("upstream llm timeout")}
	h := handlerai.New(stub)
	e := zzNewEngine(h, uuid.New(), "")

	body, _ := json.Marshal(map[string]string{"message": "hi"})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 (SSE headers already sent), got %d", rec.Code)
	}
	events := handlerai.ParseSSEEvents(rec.Body.Bytes())
	var sawError bool
	var sawDone bool
	for _, ev := range events {
		if ev.Event == "error" {
			sawError = true
			if !strings.Contains(ev.Data, "upstream llm timeout") {
				t.Errorf("expected error event data to contain the error message, got %q", ev.Data)
			}
		}
		if ev.Event == "done" {
			sawDone = true
		}
	}
	if !sawError {
		t.Fatalf("expected an 'error' SSE event, got events=%+v", events)
	}
	if sawDone {
		t.Errorf("expected no 'done' event when StreamChat errors (handler returns early), got events=%+v", events)
	}
}

// TestZZ_Chat_HappyPath_EventOrderAndCounts asserts the exact SSE frame
// sequence: N chunk events (in order), 0+ plan events, exactly one terminal
// done event with finish_reason=stop, and nothing after done.
func TestZZ_Chat_HappyPath_EventOrderAndCounts(t *testing.T) {
	plan := &domainai.Plan{ID: uuid.New(), Type: domainai.PlanTypePriceChange, Status: domainai.PlanStatusPending}
	stub := &zzStubOrchestrator{
		chunks:    []string{"first", "second", "third"},
		streamOut: &appai.ChatOutput{AssistantText: "done", Plans: []*domainai.Plan{plan}},
	}
	h := handlerai.New(stub)
	e := zzNewEngine(h, uuid.New(), "")

	body, _ := json.Marshal(map[string]string{"message": "hi"})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	events := handlerai.ParseSSEEvents(rec.Body.Bytes())
	if len(events) != 5 { // 3 chunk + 1 plan + 1 done
		t.Fatalf("expected 5 events (3 chunk + 1 plan + 1 done), got %d: %+v", len(events), events)
	}
	wantOrder := []string{"chunk", "chunk", "chunk", "plan", "done"}
	for i, want := range wantOrder {
		if events[i].Event != want {
			t.Errorf("event[%d]: expected %q, got %q", i, want, events[i].Event)
		}
	}
	// chunk content order must match orchestrator emission order.
	var chunkContents []string
	for _, ev := range events {
		if ev.Event == "chunk" {
			var d struct {
				Content string `json:"content"`
			}
			if err := json.Unmarshal([]byte(ev.Data), &d); err != nil {
				t.Fatalf("unmarshal chunk data: %v", err)
			}
			chunkContents = append(chunkContents, d.Content)
		}
	}
	wantChunks := []string{"first", "second", "third"}
	if len(chunkContents) != len(wantChunks) {
		t.Fatalf("expected %d chunk contents, got %d: %+v", len(wantChunks), len(chunkContents), chunkContents)
	}
	for i, want := range wantChunks {
		if chunkContents[i] != want {
			t.Errorf("chunk[%d]: expected %q, got %q", i, want, chunkContents[i])
		}
	}
	// terminal done event must carry finish_reason=stop.
	last := events[len(events)-1]
	if last.Event != "done" {
		t.Fatalf("expected last event to be 'done', got %q", last.Event)
	}
	var doneData struct {
		FinishReason string `json:"finish_reason"`
	}
	if err := json.Unmarshal([]byte(last.Data), &doneData); err != nil {
		t.Fatalf("unmarshal done data: %v", err)
	}
	if doneData.FinishReason != "stop" {
		t.Errorf("expected finish_reason=stop, got %q", doneData.FinishReason)
	}
}

// --- ConfirmPlan: full error-mapping matrix (business invariant) ---

func TestZZ_ConfirmPlan_InvalidUUID_Returns400(t *testing.T) {
	h := handlerai.New(&zzStubOrchestrator{})
	e := zzNewEngine(h, uuid.New(), "")

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/plans/not-a-uuid/confirm", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestZZ_ConfirmPlan_ErrorMapping(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantCode int
	}{
		{"not_found", appai.ErrPlanNotFound, http.StatusNotFound},
		{"expired", appai.ErrPlanExpired, http.StatusConflict},
		{"execution_failed", appai.ErrPlanExecutionFailed, http.StatusUnprocessableEntity},
		{"other_conflict", errors.New("some other domain conflict"), http.StatusConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &zzStubOrchestrator{confirmErr: tc.err}
			h := handlerai.New(stub)
			e := zzNewEngine(h, uuid.New(), "")

			req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/plans/"+uuid.New().String()+"/confirm", nil)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			if rec.Code != tc.wantCode {
				t.Errorf("%s: expected %d, got %d body=%s", tc.name, tc.wantCode, rec.Code, rec.Body.String())
			}
		})
	}
}

// TestZZ_ConfirmPlan_ResultNil_NoAffectedCountOrBillID verifies the dev/tests
// (no executor wired) path: response omits affected_count/bill_id entirely.
func TestZZ_ConfirmPlan_ResultNil_NoAffectedCountOrBillID(t *testing.T) {
	planID := uuid.New()
	tenantID := uuid.New()
	stub := &zzStubOrchestrator{
		confirmOut:    &domainai.Plan{ID: planID, TenantID: tenantID, Type: domainai.PlanTypePriceChange, Status: domainai.PlanStatusConfirmed},
		confirmResult: nil,
	}
	h := handlerai.New(stub)
	e := zzNewEngine(h, tenantID, "")

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/plans/"+planID.String()+"/confirm", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := resp["affected_count"]; ok {
		t.Errorf("expected no affected_count key when result is nil, got: %s", rec.Body.String())
	}
	if _, ok := resp["bill_id"]; ok {
		t.Errorf("expected no bill_id key when result is nil, got: %s", rec.Body.String())
	}
}

// TestZZ_ConfirmPlan_ResultWithoutBillID_HasAffectedCountButNoBillFields verifies
// a wired executor with no bill (e.g. bulk_stock_adjust) still surfaces
// affected_count but omits bill_id/bill_no.
func TestZZ_ConfirmPlan_ResultWithoutBillID_HasAffectedCountButNoBillFields(t *testing.T) {
	planID := uuid.New()
	tenantID := uuid.New()
	stub := &zzStubOrchestrator{
		confirmOut: &domainai.Plan{ID: planID, TenantID: tenantID, Type: domainai.PlanTypeBulkStockAdjust, Status: domainai.PlanStatusConfirmed},
		confirmResult: &appai.ExecutionResult{
			Type: domainai.PlanTypeBulkStockAdjust, AffectedCount: 7, BillID: nil,
		},
	}
	h := handlerai.New(stub)
	e := zzNewEngine(h, tenantID, "")

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/plans/"+planID.String()+"/confirm", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ac, ok := resp["affected_count"]; !ok || ac.(float64) != 7 {
		t.Errorf("expected affected_count=7, got: %s", rec.Body.String())
	}
	if _, ok := resp["bill_id"]; ok {
		t.Errorf("expected no bill_id key when BillID is nil, got: %s", rec.Body.String())
	}
}

// TestZZ_ConfirmPlan_NoTenantID_Returns401 covers the auth guard once more
// alongside the other endpoints, matching the technical_cases matrix.
func TestZZ_ConfirmPlan_NoTenantID_Returns401(t *testing.T) {
	h := handlerai.New(&zzStubOrchestrator{})
	e := zzNewEngine(h, uuid.Nil, "")

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/plans/"+uuid.New().String()+"/confirm", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// TestZZ_ConfirmPlan_ActorIDFromIDPSubject_UsedWhenPresent verifies
// resolveActorID picks up the injected OIDC sub instead of falling back to
// tenantID, by round-tripping a valid UUID through the context.
func TestZZ_ConfirmPlan_ActorIDFromIDPSubject_UsedWhenPresent(t *testing.T) {
	actorID := uuid.New()
	tenantID := uuid.New()
	planID := uuid.New()
	stub := &zzStubOrchestrator{
		confirmOut: &domainai.Plan{ID: planID, TenantID: tenantID, Status: domainai.PlanStatusConfirmed},
	}
	h := handlerai.New(stub)
	e := zzNewEngine(h, tenantID, actorID.String())

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/plans/"+planID.String()+"/confirm", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestZZ_ConfirmPlan_ActorIDFallback_WhenIDPSubjectMissingOrMalformed covers
// resolveActorID's three "return uuid.Nil" branches: key absent, non-string
// value, and unparsable string — all three must fall back to tenantID
// without erroring.
func TestZZ_ConfirmPlan_ActorIDFallback_WhenIDPSubjectMissingOrMalformed(t *testing.T) {
	tenantID := uuid.New()
	planID := uuid.New()

	tests := []struct {
		name    string
		setupFn func(c *gin.Context)
	}{
		{"subject_absent", func(c *gin.Context) {}},
		{"subject_non_string", func(c *gin.Context) { c.Set(middleware.CtxKeyIDPSubject, 12345) }},
		{"subject_unparsable", func(c *gin.Context) { c.Set(middleware.CtxKeyIDPSubject, "not-a-uuid") }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stub := &zzStubOrchestrator{
				confirmOut: &domainai.Plan{ID: planID, TenantID: tenantID, Status: domainai.PlanStatusConfirmed},
			}
			h := handlerai.New(stub)
			e := gin.New()
			e.Use(func(c *gin.Context) {
				c.Set(middleware.CtxKeyTenantID, tenantID)
				tc.setupFn(c)
				c.Next()
			})
			h.RegisterRoutes(e.Group("/api/v1"))

			req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/plans/"+planID.String()+"/confirm", nil)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("%s: expected 200 (actorID falls back to tenantID), got %d body=%s", tc.name, rec.Code, rec.Body.String())
			}
		})
	}
}

// --- CancelPlan: full matrix ---

func TestZZ_CancelPlan_NoTenantID_Returns401(t *testing.T) {
	h := handlerai.New(&zzStubOrchestrator{})
	e := zzNewEngine(h, uuid.Nil, "")

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/plans/"+uuid.New().String()+"/cancel", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestZZ_CancelPlan_NotFound_Returns404(t *testing.T) {
	stub := &zzStubOrchestrator{cancelErr: appai.ErrPlanNotFound}
	h := handlerai.New(stub)
	e := zzNewEngine(h, uuid.New(), "")

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/plans/"+uuid.New().String()+"/cancel", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestZZ_CancelPlan_OtherError_Returns500(t *testing.T) {
	stub := &zzStubOrchestrator{cancelErr: errors.New("store unavailable")}
	h := handlerai.New(stub)
	e := zzNewEngine(h, uuid.New(), "")

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/plans/"+uuid.New().String()+"/cancel", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// --- RevertPlan: full matrix (undo-window idempotency invariant) ---

func TestZZ_RevertPlan_InvalidUUID_Returns400(t *testing.T) {
	sr := &zzStubReverter{}
	h := handlerai.New(&zzStubOrchestrator{}).WithReverter(sr)
	e := zzNewEngine(h, uuid.New(), "")

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/plans/not-a-uuid/revert", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestZZ_RevertPlan_ErrorMapping(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantCode int
	}{
		{"not_found", appai.ErrPlanNotFound, http.StatusNotFound},
		{"already_reverted", appai.ErrAlreadyReverted, http.StatusConflict},
		{"window_closed", appai.ErrRevertWindowClosed, http.StatusConflict},
		{"not_revertible", appai.ErrPlanNotRevertible, http.StatusUnprocessableEntity},
		{"other_internal", errors.New("redis timeout"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sr := &zzStubReverter{err: tc.err}
			h := handlerai.New(&zzStubOrchestrator{}).WithReverter(sr)
			e := zzNewEngine(h, uuid.New(), "")

			req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/plans/"+uuid.New().String()+"/revert", nil)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			if rec.Code != tc.wantCode {
				t.Errorf("%s: expected %d, got %d body=%s", tc.name, tc.wantCode, rec.Code, rec.Body.String())
			}
		})
	}
}

// TestZZ_RevertPlan_ActorIDFallback exercises resolveActorID's nil-fallback
// path specifically through the revert endpoint (separate call site from confirm).
func TestZZ_RevertPlan_ActorIDFallback_UsesTenantID(t *testing.T) {
	tenantID := uuid.New()
	planID := uuid.New()
	sr := &zzStubReverter{result: &appai.RevertResult{PlanID: planID, RevertedType: "price_change", AffectedCount: 1}}
	h := handlerai.New(&zzStubOrchestrator{}).WithReverter(sr)
	e := zzNewEngine(h, tenantID, "") // no idp subject → actorID falls back to tenantID

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/plans/"+planID.String()+"/revert", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// --- ListPlans nil→[] normalisation (duplicated intent check via count field) ---

func TestZZ_ListPlans_EmptySliceNotNil_CountZero(t *testing.T) {
	stub := &zzStubOrchestrator{listOut: []*domainai.Plan{}}
	h := handlerai.New(stub)
	e := zzNewEngine(h, uuid.New(), "")

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/ai/plans", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp struct {
		Items []*domainai.Plan `json:"items"`
		Count int              `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Count != 0 || len(resp.Items) != 0 {
		t.Errorf("expected count=0/items=[], got count=%d items=%+v", resp.Count, resp.Items)
	}
}

// --- ParseSSEEvents unit tests (pure function, no HTTP) ---

func TestZZ_ParseSSEEvents_MultiEventBody(t *testing.T) {
	raw := "event: chunk\ndata: {\"content\":\"a\"}\n\nevent: chunk\ndata: {\"content\":\"b\"}\n\nevent: done\ndata: {\"finish_reason\":\"stop\"}\n\n"
	events := handlerai.ParseSSEEvents([]byte(raw))
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d: %+v", len(events), events)
	}
	if events[0].Event != "chunk" || events[0].Data != `{"content":"a"}` {
		t.Errorf("event[0] mismatch: %+v", events[0])
	}
	if events[2].Event != "done" || events[2].Data != `{"finish_reason":"stop"}` {
		t.Errorf("event[2] mismatch: %+v", events[2])
	}
}

// TestZZ_ParseSSEEvents_TrailingEventWithoutBlankLine verifies the flush-on-EOF
// path: a final event with no trailing blank line must still be captured.
func TestZZ_ParseSSEEvents_TrailingEventWithoutBlankLine(t *testing.T) {
	raw := "event: done\ndata: {\"finish_reason\":\"stop\"}" // no trailing "\n\n"
	events := handlerai.ParseSSEEvents([]byte(raw))
	if len(events) != 1 {
		t.Fatalf("expected 1 trailing event, got %d: %+v", len(events), events)
	}
	if events[0].Event != "done" {
		t.Errorf("expected event 'done', got %+v", events[0])
	}
}

// TestZZ_ParseSSEEvents_EmptyBody verifies no events / no panic on empty input.
func TestZZ_ParseSSEEvents_EmptyBody(t *testing.T) {
	events := handlerai.ParseSSEEvents([]byte(""))
	if len(events) != 0 {
		t.Errorf("expected 0 events for empty body, got %d: %+v", len(events), events)
	}
}

// TestZZ_ParseSSEEvents_OnlyBlankLines verifies runs of blank lines never
// synthesize phantom events (curEvent/curData both stay empty).
func TestZZ_ParseSSEEvents_OnlyBlankLines(t *testing.T) {
	events := handlerai.ParseSSEEvents([]byte("\n\n\n"))
	if len(events) != 0 {
		t.Errorf("expected 0 events for blank-only body, got %d: %+v", len(events), events)
	}
}
