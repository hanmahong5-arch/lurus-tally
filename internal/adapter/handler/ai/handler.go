// Package ai implements the HTTP handlers for the Tally AI assistant.
//
// Endpoints:
//
//	POST /api/v1/ai/chat     — SSE streaming chat (tool-calling orchestration)
//	POST /api/v1/ai/plans/:plan_id/confirm — confirm a destructive plan
//	POST /api/v1/ai/plans/:plan_id/cancel  — cancel a destructive plan
package ai

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appai "github.com/hanmahong5-arch/lurus-tally/internal/app/ai"
	domainai "github.com/hanmahong5-arch/lurus-tally/internal/domain/ai"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmclient"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmgateway"
)

// ChatOrchestrator is the surface the handler uses from the AI orchestrator.
type ChatOrchestrator interface {
	StreamChat(ctx context.Context, in appai.ChatInput, onChunk func(string)) (*appai.ChatOutput, error)
	ConfirmPlan(ctx context.Context, tenantID, actorID, planID uuid.UUID) (*domainai.Plan, *appai.ExecutionResult, error)
	CancelPlan(ctx context.Context, tenantID, planID uuid.UUID) error
	ListPlans(ctx context.Context, tenantID uuid.UUID, statusFilter string) ([]*domainai.Plan, error)
}

// Handler groups the AI HTTP endpoints.
type Handler struct {
	orchestrator ChatOrchestrator
	limiter      *llmgateway.RateLimiter // nil → no rate limiting (dev / tests)
}

// New constructs an AI Handler with no rate limiting. Production callers
// should use NewWithLimiter to attach a per-tenant budget.
func New(orchestrator ChatOrchestrator) *Handler {
	return &Handler{orchestrator: orchestrator}
}

// NewWithLimiter constructs an AI Handler that enforces the given rate limiter
// on POST /chat (the only LLM-spending endpoint). Pass nil to disable.
func NewWithLimiter(orchestrator ChatOrchestrator, limiter *llmgateway.RateLimiter) *Handler {
	return &Handler{orchestrator: orchestrator, limiter: limiter}
}

// RegisterRoutes mounts AI endpoints onto the given router group.
// The group must already be guarded by AuthMiddleware so tenant_id is present.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	ai := rg.Group("/ai")
	{
		ai.POST("/chat", h.Chat)
		ai.GET("/plans", h.ListPlans)
		ai.POST("/plans/:plan_id/confirm", h.ConfirmPlan)
		ai.POST("/plans/:plan_id/cancel", h.CancelPlan)
	}
}

// ListPlans handles GET /api/v1/ai/plans?status=pending.
// status query param is optional — when omitted returns plans of all statuses.
func (h *Handler) ListPlans(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "detail": "tenant_id required"})
		return
	}
	status := c.Query("status")
	plans, err := h.orchestrator.ListPlans(c.Request.Context(), tenantID, status)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "detail": err.Error()})
		return
	}
	if plans == nil {
		plans = []*domainai.Plan{}
	}
	c.JSON(http.StatusOK, gin.H{"items": plans, "count": len(plans)})
}

// chatRequest is the body of POST /api/v1/ai/chat.
type chatRequest struct {
	// Message is the user's new message.
	Message string `json:"message" binding:"required,max=8000"`
	// History is the previous conversation turns (optional; omit for first turn).
	// Hard-capped at 200 turns: longer histories blow prompt budget faster than
	// they improve answer quality and are usually a runaway client bug.
	History []historyTurn `json:"history" binding:"max=200,dive"`
}

// historyTurn is a single turn in the conversation history.
type historyTurn struct {
	Role    string `json:"role"    binding:"required,oneof=user assistant tool system"`
	Content string `json:"content" binding:"max=16000"`
}

// SSE event types.
const (
	sseEventChunk = "chunk"
	sseEventPlan  = "plan"
	sseEventDone  = "done"
	sseEventError = "error"
)

// Chat handles POST /api/v1/ai/chat.
// Response is an SSE stream:
//
//	event: chunk  data: {"content":"..."}
//	event: plan   data: {Plan JSON}
//	event: done   data: {"finish_reason":"stop"}
//	event: error  data: {"error":"..."}
func (h *Handler) Chat(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "detail": "tenant_id required"})
		return
	}

	if h.limiter != nil {
		allowed, retryAfter, lerr := h.limiter.Allow(c.Request.Context(), tenantID)
		if lerr == nil && !allowed {
			llmgateway.RecordDropped(tenantID)
			seconds := int(retryAfter.Seconds())
			if seconds < 1 {
				seconds = 1
			}
			c.Header("Retry-After", strconv.Itoa(seconds))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":   "llm_rate_limited",
				"detail":  "per-tenant LLM call budget exceeded; retry after window resets",
				"retry_s": seconds,
			})
			return
		}
		// lerr != nil → degrade open (Redis hiccup must not break AI).
	}

	var req chatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request", "detail": err.Error()})
		return
	}

	// Build history for orchestrator.
	history := make([]llmclient.Message, 0, len(req.History))
	for _, h := range req.History {
		history = append(history, llmclient.Message{
			Role:    h.Role,
			Content: h.Content,
		})
	}

	// Set up SSE headers before writing anything.
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no") // disable nginx buffering

	flusher, canFlush := c.Writer.(http.Flusher)

	writeSSE := func(event, data string) {
		_, _ = fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, data)
		if canFlush {
			flusher.Flush()
		}
	}

	// Start streaming.
	input := appai.ChatInput{
		TenantID:    tenantID,
		History:     history,
		UserMessage: req.Message,
	}

	out, err := h.orchestrator.StreamChat(c.Request.Context(), input, func(chunk string) {
		b, _ := json.Marshal(map[string]string{"content": chunk})
		writeSSE(sseEventChunk, string(b))
	})
	if err != nil {
		b, _ := json.Marshal(map[string]string{"error": err.Error()})
		writeSSE(sseEventError, string(b))
		return
	}

	// Emit any plan cards.
	for _, plan := range out.Plans {
		b, _ := json.Marshal(plan)
		writeSSE(sseEventPlan, string(b))
	}

	b, _ := json.Marshal(map[string]string{"finish_reason": "stop"})
	writeSSE(sseEventDone, string(b))

	// Drain the response writer.
	if rw, ok := c.Writer.(interface{ WriteHeader(int) }); ok {
		_ = rw
	}
}

// ConfirmPlan handles POST /api/v1/ai/plans/:plan_id/confirm.
func (h *Handler) ConfirmPlan(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	planID, err := uuid.Parse(c.Param("plan_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request", "detail": "invalid plan_id"})
		return
	}

	// Acting user for bill creator + audit attribution. Falls back to tenantID
	// when no per-user identity is present (single-operator dev deployments).
	actorID := resolveActorID(c)
	if actorID == uuid.Nil {
		actorID = tenantID
	}

	plan, result, err := h.orchestrator.ConfirmPlan(c.Request.Context(), tenantID, actorID, planID)
	if err != nil {
		switch err {
		case appai.ErrPlanNotFound:
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "detail": "plan not found"})
			return
		case appai.ErrPlanExpired:
			c.JSON(http.StatusConflict, gin.H{"error": "plan_expired", "detail": "plan TTL elapsed; ask AI again to generate a fresh plan"})
			return
		}
		c.JSON(http.StatusConflict, gin.H{"error": "conflict", "detail": err.Error()})
		return
	}

	resp := gin.H{
		"plan_id": plan.ID,
		"status":  plan.Status,
		"type":    plan.Type,
	}
	// result is nil only when no executor is wired (dev/tests).
	if result != nil {
		resp["affected_count"] = result.AffectedCount
		// North-star + funnel metrics: count every confirmed AI plan, and count
		// a Weekly Active Decision whenever the plan produced a purchase draft.
		middleware.IncAIPlanExecuted(string(result.Type), tenantID.String())
		if result.BillID != nil {
			resp["bill_id"] = result.BillID
			resp["bill_no"] = result.BillNo
			middleware.IncWAD(tenantID.String())
		}
	}
	c.JSON(http.StatusOK, resp)
}

// resolveActorID extracts the acting user's UUID from the Zitadel subject or the
// X-User-ID header. Returns uuid.Nil when neither is present.
func resolveActorID(c *gin.Context) uuid.UUID {
	if sub, ok := c.Get(middleware.CtxKeyZitadelSub); ok {
		if s, ok := sub.(string); ok {
			if id, err := uuid.Parse(s); err == nil {
				return id
			}
		}
	}
	if raw := c.GetHeader("X-User-ID"); raw != "" {
		if id, err := uuid.Parse(raw); err == nil {
			return id
		}
	}
	return uuid.Nil
}

// CancelPlan handles POST /api/v1/ai/plans/:plan_id/cancel.
func (h *Handler) CancelPlan(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	planID, err := uuid.Parse(c.Param("plan_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request", "detail": "invalid plan_id"})
		return
	}

	if err := h.orchestrator.CancelPlan(c.Request.Context(), tenantID, planID); err != nil {
		if err == appai.ErrPlanNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "detail": "plan not found or expired"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "detail": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "cancelled"})
}

// --- SSE helpers (used by tests) ---

// ParseSSEEvents parses a raw SSE response body into a slice of {event, data} pairs.
func ParseSSEEvents(body []byte) []SSEEvent {
	var events []SSEEvent
	scanner := bufio.NewScanner(
		&byteReader{data: body},
	)

	var curEvent, curData string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if curEvent != "" || curData != "" {
				events = append(events, SSEEvent{Event: curEvent, Data: curData})
				curEvent, curData = "", ""
			}
			continue
		}
		if len(line) > 7 && line[:7] == "event: " {
			curEvent = line[7:]
		} else if len(line) > 6 && line[:6] == "data: " {
			curData = line[6:]
		}
	}
	if curEvent != "" || curData != "" {
		events = append(events, SSEEvent{Event: curEvent, Data: curData})
	}
	return events
}

// SSEEvent is a parsed SSE event.
type SSEEvent struct {
	Event string
	Data  string
}

type byteReader struct {
	data   []byte
	offset int
}

func (b *byteReader) Read(p []byte) (int, error) {
	if b.offset >= len(b.data) {
		return 0, fmt.Errorf("EOF")
	}
	n := copy(p, b.data[b.offset:])
	b.offset += n
	return n, nil
}
