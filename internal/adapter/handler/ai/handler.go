// Package ai implements the HTTP handlers for the Tally AI assistant.
//
// Endpoints:
//   POST /api/v1/ai/chat     — SSE streaming chat (tool-calling orchestration)
//   POST /api/v1/ai/plans/:plan_id/confirm — confirm a destructive plan
//   POST /api/v1/ai/plans/:plan_id/cancel  — cancel a destructive plan
package ai

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	appai "github.com/hanmahong5-arch/lurus-tally/internal/app/ai"
	domainai "github.com/hanmahong5-arch/lurus-tally/internal/domain/ai"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmclient"
)

// ChatOrchestrator is the surface the handler uses from the AI orchestrator.
type ChatOrchestrator interface {
	StreamChat(ctx context.Context, in appai.ChatInput, onChunk func(string)) (*appai.ChatOutput, error)
	ConfirmPlan(ctx context.Context, tenantID, planID uuid.UUID) (*domainai.Plan, error)
	CancelPlan(ctx context.Context, tenantID, planID uuid.UUID) error
}

// Handler groups the AI HTTP endpoints.
type Handler struct {
	orchestrator ChatOrchestrator
}

// New constructs an AI Handler.
func New(orchestrator ChatOrchestrator) *Handler {
	return &Handler{orchestrator: orchestrator}
}

// RegisterRoutes mounts AI endpoints onto the given router group.
// The group must already be guarded by AuthMiddleware so tenant_id is present.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	ai := rg.Group("/ai")
	{
		ai.POST("/chat", h.Chat)
		ai.POST("/plans/:plan_id/confirm", h.ConfirmPlan)
		ai.POST("/plans/:plan_id/cancel", h.CancelPlan)
	}
}

// chatRequest is the body of POST /api/v1/ai/chat.
type chatRequest struct {
	// Message is the user's new message.
	Message string `json:"message" binding:"required"`
	// History is the previous conversation turns (optional; omit for first turn).
	History []historyTurn `json:"history"`
}

// historyTurn is a single turn in the conversation history.
type historyTurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
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
//   event: chunk  data: {"content":"..."}
//   event: plan   data: {Plan JSON}
//   event: done   data: {"finish_reason":"stop"}
//   event: error  data: {"error":"..."}
func (h *Handler) Chat(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	if tenantID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "detail": "tenant_id required"})
		return
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
		fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, data)
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

	plan, err := h.orchestrator.ConfirmPlan(c.Request.Context(), tenantID, planID)
	if err != nil {
		if err == appai.ErrPlanNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "detail": "plan not found or expired"})
			return
		}
		c.JSON(http.StatusConflict, gin.H{"error": "conflict", "detail": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"plan_id": plan.ID,
		"status":  plan.Status,
		"type":    plan.Type,
	})
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
