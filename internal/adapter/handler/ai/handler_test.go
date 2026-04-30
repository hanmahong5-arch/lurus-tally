package ai_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	streamOut *appai.ChatOutput
	streamErr error
	confirmOut *domainai.Plan
	confirmErr error
	cancelErr  error
	chunks     []string
}

func (s *stubOrchestrator) StreamChat(_ context.Context, _ appai.ChatInput, onChunk func(string)) (*appai.ChatOutput, error) {
	for _, ch := range s.chunks {
		onChunk(ch)
	}
	return s.streamOut, s.streamErr
}

func (s *stubOrchestrator) ConfirmPlan(_ context.Context, _, _ uuid.UUID) (*domainai.Plan, error) {
	return s.confirmOut, s.confirmErr
}

func (s *stubOrchestrator) CancelPlan(_ context.Context, _, _ uuid.UUID) error {
	return s.cancelErr
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
