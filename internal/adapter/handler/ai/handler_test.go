package ai_test

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
)

func init() {
	gin.SetMode(gin.TestMode)
}

// --- stubs ---

type stubOrchestrator struct {
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

func (s *stubOrchestrator) StreamChat(_ context.Context, _ appai.ChatInput, onChunk func(string)) (*appai.ChatOutput, error) {
	for _, ch := range s.chunks {
		onChunk(ch)
	}
	return s.streamOut, s.streamErr
}

func (s *stubOrchestrator) ConfirmPlan(_ context.Context, _, _, _ uuid.UUID) (*domainai.Plan, *appai.ExecutionResult, error) {
	return s.confirmOut, s.confirmResult, s.confirmErr
}

func (s *stubOrchestrator) CancelPlan(_ context.Context, _, _ uuid.UUID) error {
	return s.cancelErr
}

func (s *stubOrchestrator) ListPlans(_ context.Context, _ uuid.UUID, _ string) ([]*domainai.Plan, error) {
	return s.listOut, s.listErr
}

func newTestEngine(h *handlerai.Handler, tenantID uuid.UUID) *gin.Engine {
	e := gin.New()
	e.Use(func(c *gin.Context) {
		if tenantID != uuid.Nil {
			c.Set(middleware.CtxKeyTenantID, tenantID)
		}
		c.Next()
	})
	h.RegisterRoutes(e.Group("/api/v1"))
	return e
}

// TestAIHandler_Chat_NoTenantID_Returns401 verifies auth guard on chat endpoint.
func TestAIHandler_Chat_NoTenantID_Returns401(t *testing.T) {
	h := handlerai.New(&stubOrchestrator{})
	e := newTestEngine(h, uuid.Nil)

	body, _ := json.Marshal(map[string]string{"message": "low stock?"})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestAIHandler_Chat_BadBody_Returns400 verifies validation on chat endpoint.
func TestAIHandler_Chat_BadBody_Returns400(t *testing.T) {
	h := handlerai.New(&stubOrchestrator{})
	e := newTestEngine(h, uuid.New())

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/chat",
		bytes.NewReader([]byte(`{}`))) // missing required message
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestAIHandler_Chat_HappyPath_StreamsChunksAndDone verifies SSE streaming response.
func TestAIHandler_Chat_HappyPath_StreamsChunksAndDone(t *testing.T) {
	stub := &stubOrchestrator{
		chunks:    []string{"低库存", "商品共 3 个"},
		streamOut: &appai.ChatOutput{AssistantText: "低库存商品共 3 个"},
	}
	h := handlerai.New(stub)
	e := newTestEngine(h, uuid.New())

	body, _ := json.Marshal(map[string]string{"message": "低库存"})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// Content-Type must be SSE.
	ct := rec.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %s", ct)
	}

	// Parse SSE events.
	events := handlerai.ParseSSEEvents(rec.Body.Bytes())
	if len(events) == 0 {
		t.Fatal("expected SSE events, got none")
	}

	// Verify done event is present.
	var hasDone bool
	for _, ev := range events {
		if ev.Event == "done" {
			hasDone = true
		}
	}
	if !hasDone {
		t.Errorf("expected 'done' event, got events: %+v", events)
	}
}

// TestAIHandler_Chat_WithPlan_EmitsPlanEvent verifies plan cards are emitted as SSE events.
func TestAIHandler_Chat_WithPlan_EmitsPlanEvent(t *testing.T) {
	planID := uuid.New()
	tenantID := uuid.New()
	plan := &domainai.Plan{
		ID:       planID,
		TenantID: tenantID,
		Type:     domainai.PlanTypePriceChange,
		Status:   domainai.PlanStatusPending,
		Preview: domainai.PlanPreview{
			Description:   "Change 5 products by +5%",
			AffectedCount: 5,
		},
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(30 * time.Minute),
	}
	stub := &stubOrchestrator{
		streamOut: &appai.ChatOutput{
			AssistantText: "已生成调价计划",
			Plans:         []*domainai.Plan{plan},
		},
	}
	h := handlerai.New(stub)
	e := newTestEngine(h, tenantID)

	body, _ := json.Marshal(map[string]string{"message": "所有商品涨价5%"})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	events := handlerai.ParseSSEEvents(rec.Body.Bytes())
	var planEvents int
	for _, ev := range events {
		if ev.Event == "plan" {
			planEvents++
		}
	}
	if planEvents != 1 {
		t.Errorf("expected 1 plan event, got %d events=%+v", planEvents, events)
	}
}

// TestAIHandler_ConfirmPlan_NoTenantID_Returns401 verifies confirm auth guard.
func TestAIHandler_ConfirmPlan_NoTenantID_Returns401(t *testing.T) {
	h := handlerai.New(&stubOrchestrator{})
	e := newTestEngine(h, uuid.Nil)

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/plans/"+uuid.New().String()+"/confirm", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// TestAIHandler_ConfirmPlan_HappyPath_Returns200 verifies successful confirm.
func TestAIHandler_ConfirmPlan_HappyPath_Returns200(t *testing.T) {
	planID := uuid.New()
	tenantID := uuid.New()
	confirmedPlan := &domainai.Plan{
		ID:       planID,
		TenantID: tenantID,
		Type:     domainai.PlanTypePriceChange,
		Status:   domainai.PlanStatusConfirmed,
	}
	stub := &stubOrchestrator{confirmOut: confirmedPlan}
	h := handlerai.New(stub)
	e := newTestEngine(h, tenantID)

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/plans/"+planID.String()+"/confirm", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("confirmed")) {
		t.Errorf("expected 'confirmed' in body: %s", rec.Body.String())
	}
}

// TestAIHandler_ConfirmPlan_WithResult_ReturnsBillRef verifies that a confirmed
// purchase-draft plan surfaces bill_id/bill_no so the PlanCard can deep-link.
func TestAIHandler_ConfirmPlan_WithResult_ReturnsBillRef(t *testing.T) {
	planID := uuid.New()
	tenantID := uuid.New()
	billID := uuid.New()
	stub := &stubOrchestrator{
		confirmOut: &domainai.Plan{ID: planID, TenantID: tenantID, Type: domainai.PlanTypeCreatePurchase, Status: domainai.PlanStatusConfirmed},
		confirmResult: &appai.ExecutionResult{
			Type: domainai.PlanTypeCreatePurchase, AffectedCount: 3, BillID: &billID, BillNo: "PO-20260522-0007",
		},
	}
	h := handlerai.New(stub)
	e := newTestEngine(h, tenantID)

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/plans/"+planID.String()+"/confirm", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		BillID        string `json:"bill_id"`
		BillNo        string `json:"bill_no"`
		AffectedCount int    `json:"affected_count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.BillID != billID.String() || resp.BillNo != "PO-20260522-0007" || resp.AffectedCount != 3 {
		t.Errorf("unexpected response: %+v", resp)
	}
}

// TestAIHandler_ConfirmPlan_NotFound_Returns404 verifies not-found response.
func TestAIHandler_ConfirmPlan_NotFound_Returns404(t *testing.T) {
	stub := &stubOrchestrator{confirmErr: appai.ErrPlanNotFound}
	h := handlerai.New(stub)
	e := newTestEngine(h, uuid.New())

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/plans/"+uuid.New().String()+"/confirm", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// TestAIHandler_CancelPlan_HappyPath_Returns200 verifies successful cancel.
func TestAIHandler_CancelPlan_HappyPath_Returns200(t *testing.T) {
	h := handlerai.New(&stubOrchestrator{})
	e := newTestEngine(h, uuid.New())

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/plans/"+uuid.New().String()+"/cancel", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestAIHandler_CancelPlan_InvalidPlanID_Returns400 verifies bad plan_id rejected.
func TestAIHandler_CancelPlan_InvalidPlanID_Returns400(t *testing.T) {
	h := handlerai.New(&stubOrchestrator{})
	e := newTestEngine(h, uuid.New())

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/plans/not-a-uuid/cancel", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

// TestAIHandler_ListPlans_ReturnsItems verifies GET /api/v1/ai/plans returns
// the orchestrator's list and the response carries items + count.
func TestAIHandler_ListPlans_ReturnsItems(t *testing.T) {
	tenantID := uuid.New()
	plan := &domainai.Plan{ID: uuid.New(), TenantID: tenantID, Status: domainai.PlanStatusPending}
	stub := &stubOrchestrator{listOut: []*domainai.Plan{plan}}
	h := handlerai.New(stub)
	e := newTestEngine(h, tenantID)

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/ai/plans?status=pending", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []*domainai.Plan `json:"items"`
		Count int              `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Count != 1 || len(resp.Items) != 1 {
		t.Errorf("count = %d, items len = %d, want 1/1", resp.Count, len(resp.Items))
	}
	if resp.Items[0].ID != plan.ID {
		t.Errorf("returned plan id = %s, want %s", resp.Items[0].ID, plan.ID)
	}
}

// TestAIHandler_ListPlans_NoTenant_Returns401 verifies auth guard.
func TestAIHandler_ListPlans_NoTenant_Returns401(t *testing.T) {
	h := handlerai.New(&stubOrchestrator{})
	e := newTestEngine(h, uuid.Nil)

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/ai/plans", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// TestAIHandler_ListPlans_NilSlice_ReturnsEmpty verifies a nil orchestrator
// result is normalised to [] (never JSON null) so MCP clients can iterate safely.
func TestAIHandler_ListPlans_NilSlice_ReturnsEmpty(t *testing.T) {
	stub := &stubOrchestrator{listOut: nil}
	h := handlerai.New(stub)
	e := newTestEngine(h, uuid.New())

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/ai/plans", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"items":[]`) {
		t.Errorf("expected items: [] in body, got: %s", body)
	}
}

// --- revert handler tests ---

type stubReverter struct {
	result *appai.RevertResult
	err    error
}

func (s *stubReverter) RevertPlan(_ context.Context, _, _, _ uuid.UUID) (*appai.RevertResult, error) {
	return s.result, s.err
}

// TestAIHandler_RevertPlan_NoReverterWired_Returns501 verifies that when the reverter
// is not wired (nil), the endpoint returns 501 Not Implemented.
func TestAIHandler_RevertPlan_NoReverterWired_Returns501(t *testing.T) {
	h := handlerai.New(&stubOrchestrator{})
	e := newTestEngine(h, uuid.New())

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/plans/"+uuid.New().String()+"/revert", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Errorf("expected 501, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestAIHandler_RevertPlan_HappyPath_Returns200 verifies successful revert.
func TestAIHandler_RevertPlan_HappyPath_Returns200(t *testing.T) {
	planID := uuid.New()
	tenantID := uuid.New()
	sr := &stubReverter{result: &appai.RevertResult{PlanID: planID, RevertedType: "bulk_stock_adjust", AffectedCount: 2}}
	h := handlerai.New(&stubOrchestrator{}).WithReverter(sr)
	e := newTestEngine(h, tenantID)

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/plans/"+planID.String()+"/revert", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp appai.RevertResult
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.AffectedCount != 2 || resp.RevertedType != "bulk_stock_adjust" {
		t.Errorf("unexpected result: %+v", resp)
	}
}

// TestAIHandler_RevertPlan_AlreadyReverted_Returns409 verifies idempotency error response.
func TestAIHandler_RevertPlan_AlreadyReverted_Returns409(t *testing.T) {
	sr := &stubReverter{err: appai.ErrAlreadyReverted}
	h := handlerai.New(&stubOrchestrator{}).WithReverter(sr)
	e := newTestEngine(h, uuid.New())

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/plans/"+uuid.New().String()+"/revert", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestAIHandler_RevertPlan_WindowClosed_Returns409 verifies undo window error response.
func TestAIHandler_RevertPlan_WindowClosed_Returns409(t *testing.T) {
	sr := &stubReverter{err: appai.ErrRevertWindowClosed}
	h := handlerai.New(&stubOrchestrator{}).WithReverter(sr)
	e := newTestEngine(h, uuid.New())

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/plans/"+uuid.New().String()+"/revert", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestAIHandler_RevertPlan_NoTenantID_Returns401 verifies auth guard on revert.
func TestAIHandler_RevertPlan_NoTenantID_Returns401(t *testing.T) {
	sr := &stubReverter{}
	h := handlerai.New(&stubOrchestrator{}).WithReverter(sr)
	e := newTestEngine(h, uuid.Nil)

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/plans/"+uuid.New().String()+"/revert", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// ackOrchestrator is a stubOrchestrator that also takes acknowledged flags.
type ackOrchestrator struct {
	stubOrchestrator
	gotAck []int
	calls  int
}

func (a *ackOrchestrator) ConfirmPlanAck(_ context.Context, _, _, _ uuid.UUID, ack []int) (*domainai.Plan, *appai.ExecutionResult, error) {
	a.calls++
	a.gotAck = ack
	if a.confirmErr != nil {
		return nil, nil, a.confirmErr
	}
	return a.confirmOut, a.confirmResult, nil
}

func postConfirm(e http.Handler, planID uuid.UUID, body string) *httptest.ResponseRecorder {
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/plans/"+planID.String()+"/confirm", strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// A flagged draft confirmed without acknowledged_flags is refused with 422.
func TestAIHandler_ConfirmPlan_LineConfirmRequired_Returns422(t *testing.T) {
	tenantID, planID := uuid.New(), uuid.New()
	stub := &ackOrchestrator{stubOrchestrator: stubOrchestrator{confirmErr: appai.ErrLineConfirmRequired}}
	e := newTestEngine(handlerai.New(stub), tenantID)

	for _, body := range []string{"", "{}", `{"acknowledged_flags":[]}`} {
		rec := postConfirm(e, planID, body)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("body %q: want 422, got %d %s", body, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "line_confirm_required") {
			t.Errorf("body %q: missing stable error code: %s", body, rec.Body.String())
		}
	}
}

// The plain ConfirmPlan path (an orchestrator without the ack method) maps the
// same sentinel to 422 too.
func TestAIHandler_ConfirmPlan_PlainOrchestratorLineConfirmRequired_Returns422(t *testing.T) {
	tenantID, planID := uuid.New(), uuid.New()
	e := newTestEngine(handlerai.New(&stubOrchestrator{confirmErr: appai.ErrLineConfirmRequired}), tenantID)
	if rec := postConfirm(e, planID, ""); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d %s", rec.Code, rec.Body.String())
	}
}

// acknowledged_flags reaches the orchestrator and an acknowledged draft confirms.
func TestAIHandler_ConfirmPlan_PassesAcknowledgedFlags(t *testing.T) {
	tenantID, planID := uuid.New(), uuid.New()
	stub := &ackOrchestrator{stubOrchestrator: stubOrchestrator{
		confirmOut: &domainai.Plan{ID: planID, TenantID: tenantID, Type: domainai.PlanTypeCreatePurchase, Status: domainai.PlanStatusConfirmed},
	}}
	e := newTestEngine(handlerai.New(stub), tenantID)
	rec := postConfirm(e, planID, `{"acknowledged_flags":[0,1]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	if len(stub.gotAck) != 2 || stub.gotAck[0] != 0 || stub.gotAck[1] != 1 {
		t.Fatalf("ack not passed through: %v", stub.gotAck)
	}
}

func TestAIHandler_ConfirmPlan_MalformedBody_Returns400(t *testing.T) {
	tenantID, planID := uuid.New(), uuid.New()
	stub := &ackOrchestrator{}
	e := newTestEngine(handlerai.New(stub), tenantID)
	if rec := postConfirm(e, planID, `{"acknowledged_flags":"x"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d %s", rec.Code, rec.Body.String())
	}
	if stub.calls != 0 {
		t.Fatal("a malformed body must not reach the orchestrator")
	}
}

// Confirm failures reach a shopkeeper's card: the detail is Chinese and never
// carries the internal error text.
func TestAIHandler_ConfirmPlan_ErrorDetailsAreChinese(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code int
	}{
		{"not_found", appai.ErrPlanNotFound, http.StatusNotFound},
		{"expired", appai.ErrPlanExpired, http.StatusConflict},
		{"execution_failed", fmt.Errorf("confirm plan: %w: %w", appai.ErrPlanExecutionFailed, errors.New("pq: duplicate key value")), http.StatusUnprocessableEntity},
		{"other", fmt.Errorf("plan is confirmed: %w", appai.ErrPlanNotPending), http.StatusConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tenantID := uuid.New()
			e := newTestEngine(handlerai.New(&stubOrchestrator{confirmErr: tc.err}), tenantID)
			rec := postConfirm(e, uuid.New(), "")
			if rec.Code != tc.code {
				t.Fatalf("code=%d want %d body=%s", rec.Code, tc.code, rec.Body.String())
			}
			var body map[string]string
			_ = json.Unmarshal(rec.Body.Bytes(), &body)
			detail := body["detail"]
			if detail == "" {
				t.Fatalf("no detail: %s", rec.Body.String())
			}
			for _, r := range detail {
				if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
					t.Fatalf("detail has ASCII letters: %q", detail)
				}
			}
		})
	}
}
