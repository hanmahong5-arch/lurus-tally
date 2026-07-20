package ai

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	domainai "github.com/hanmahong5-arch/lurus-tally/internal/domain/ai"
	llmobs "github.com/hanmahong5-arch/lurus-tally/internal/observability/llm"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmclient"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmgateway"
)

// PlanStore persists and retrieves Plans (Redis-backed, TTL 1800s).
type PlanStore interface {
	// SavePlan stores a plan. Returns an error on failure.
	SavePlan(ctx context.Context, plan *domainai.Plan) error
	// GetPlan retrieves a plan by ID. Returns nil, nil when not found.
	GetPlan(ctx context.Context, tenantID, planID uuid.UUID) (*domainai.Plan, error)
	// UpdatePlan updates an existing plan's status.
	UpdatePlan(ctx context.Context, plan *domainai.Plan) error
	// ListByTenant returns plans for a tenant. When statusFilter == "" all
	// statuses are returned; otherwise only matching plans.
	ListByTenant(ctx context.Context, tenantID uuid.UUID, statusFilter string) ([]*domainai.Plan, error)
}

// Orchestrator drives the multi-turn tool-calling loop.
// It calls the LLM, dispatches tool calls through the Registry, collects Plans,
// and streams the final text response.
type Orchestrator struct {
	llm       *llmclient.Client
	registry  *Registry
	planStore PlanStore
	executor  PlanExecutor  // nil → ConfirmPlan flips status only (dev / tests)
	audit     AuditWriter   // nil → AI plan executions are not audited
	memory    MemoryClient  // nil when memorus disabled
	tracer    llmobs.Tracer // nil → spans silently skipped
	model     string
}

// NewOrchestrator constructs an Orchestrator.
func NewOrchestrator(llm *llmclient.Client, registry *Registry, planStore PlanStore, model string) *Orchestrator {
	if model == "" {
		model = "deepseek-v4-flash"
	}
	return &Orchestrator{
		llm:       llm,
		registry:  registry,
		planStore: planStore,
		model:     model,
	}
}

// WithMemory attaches a MemoryClient to the orchestrator for recall + write-back.
// Passing nil disables memory (same as default).
func (o *Orchestrator) WithMemory(mc MemoryClient) *Orchestrator {
	o.memory = mc
	return o
}

// WithExecutor attaches a PlanExecutor so ConfirmPlan performs real side effects
// (build PO draft / change prices / adjust stock). Passing nil leaves ConfirmPlan
// as a status-only flip — acceptable for dev/tests where execution is not wired.
func (o *Orchestrator) WithExecutor(ex PlanExecutor) *Orchestrator {
	o.executor = ex
	return o
}

// WithAudit attaches an AuditWriter so each confirmed AI plan execution leaves an
// audit trail (red-line: every AI stock write must be auditable). Passing nil
// disables auditing. Audit failures never block the confirm — the write already
// happened; the adapter is responsible for logging.
func (o *Orchestrator) WithAudit(aw AuditWriter) *Orchestrator {
	o.audit = aw
	return o
}

// WithTracer attaches an LLM observability Tracer so every inference call
// produces a structured span (input/output/model/latency/tokens). Passing nil
// silently disables tracing — callers should pass llmobs.Noop() when they want
// an explicit no-op rather than relying on nil-guard skips.
func (o *Orchestrator) WithTracer(t llmobs.Tracer) *Orchestrator {
	o.tracer = t
	return o
}

// startSpan opens an LLM span when a tracer is attached; otherwise returns
// a nil span and the original context. Callers must check span != nil before
// calling End / AttachToolCall.
func (o *Orchestrator) startSpan(ctx context.Context, op, model, prompt string) (llmobs.Span, context.Context) {
	if o.tracer == nil {
		return nil, ctx
	}
	return o.tracer.StartLLMSpan(ctx, op, model, prompt)
}

// systemPrompt is the static inventory assistant persona.
// Kept as a constant to maximise prompt-cache hit rate (never put dynamic content here).
const systemPrompt = `You are an expert inventory management assistant for Tally, a Chinese B2B ERP system.
You have deep knowledge of inventory KPIs: ABC classification (Pareto 80/15/5), Re-Order Point (ROP = lead_time × avg_daily_sales + safety_stock), safety stock (z × σ × √lead_time), turnover rate, gross margin, and dead stock analysis.

You can call the provided tools to query real data from the user's inventory system. Always cite the data source in your response (e.g. "Based on 30 days of sales data...").

CRITICAL — tool-first policy: if any available tool can plausibly answer the user's question, you MUST call it in this same turn BEFORE writing any reply text. Never respond with a menu of options, a feature list, or a clarifying question when a tool call would produce a real answer — call the tool first, then explain the result using the returned data. Only ask the user a clarifying question after a tool call has already run and its result was empty or genuinely ambiguous.

Call the SINGLE most specific tool that directly answers the question, and put it FIRST. Do NOT call get_stock_summary as a preliminary survey before a more specific tool: get_stock_summary is only for a genuine overall warehouse overview (库存总体情况 / 仓库概况). For 补货 / 该进多少货 / 算需要补多少 call list_low_stock directly; for 滞销 / 卖不动 call list_dead_stock directly; for 毛利 / 利润率 call gross_margin_summary directly — never precede these with get_stock_summary.

Common Chinese phrasing → tool mapping (resolve intent to a tool call, do not ask which one the user means):
- 补货 / 该进多少货 / 复库 / 缺货预警 / 库存不够 → list_low_stock (then propose_create_purchase_draft if the user wants an order placed)
- 滞销 / 呆滞 / 库存积压 / 卖不动 → list_dead_stock
- 毛利 / 利润率 最低 / 最高 → gross_margin_summary
- 畅销 / 爆款 / 排行 / 卖得最好 → recent_sales_top
- 库存总体情况 / 仓库概况 → get_stock_summary
- ABC分类 / 帕累托 → abc_classify

For DESTRUCTIVE operations (price changes, purchase orders, stock adjustments), you MUST call the propose_* tools. These tools return a plan_id that the user must confirm — you do NOT execute them directly. Inform the user that a confirmation card has been shown.

Respond in Chinese. Be concise and data-driven. When showing lists, limit to 10 items.`

// ChatInput is the input to a single chat turn.
type ChatInput struct {
	TenantID uuid.UUID
	// History is the conversation history (user + assistant turns).
	History []llmclient.Message
	// UserMessage is the new user message.
	UserMessage string
}

// ChatOutput is the result of a single chat turn.
type ChatOutput struct {
	// AssistantText is the final LLM response text (may include plan references).
	AssistantText string
	// Plans contains any Plans generated by destructive tool calls this turn.
	Plans []*domainai.Plan
	// ToolCalls records which tools were called.
	ToolCalls []domainai.ToolCallRecord
}

// maxToolRounds prevents infinite tool-call loops.
const maxToolRounds = 6

// Chat executes one user turn (non-streaming). Builds the full message sequence,
// runs tool-call rounds, and returns the final text + any plans.
func (o *Orchestrator) Chat(ctx context.Context, in ChatInput) (*ChatOutput, error) {
	ctx = llmgateway.WithTenant(ctx, in.TenantID.String())
	// Memory recall: augment user message with relevant past context.
	userID := in.TenantID.String()
	augmented := AugmentMessagesWithMemoryOrFallback(o.memory, ctx, userID, in.UserMessage)
	inAugmented := in
	inAugmented.UserMessage = augmented

	messages := buildMessages(inAugmented)
	tools := ToolDefs()

	var plans []*domainai.Plan
	var toolCalls []domainai.ToolCallRecord

	for round := 0; round < maxToolRounds; round++ {
		// Open an LLM span for this inference call. span is nil when no tracer
		// is attached; all span.* calls are guarded by the nil check below.
		span, spanCtx := o.startSpan(ctx, "chat", o.model, in.UserMessage)
		resp, err := o.llm.Chat(spanCtx, o.model, messages, tools)
		if err != nil {
			if span != nil {
				span.End("", llmobs.TokenCount{}, err)
			}
			return nil, fmt.Errorf("orchestrator: llm chat: %w", err)
		}
		if len(resp.Choices) == 0 {
			if span != nil {
				span.End("", llmobs.TokenCount{}, fmt.Errorf("no choices in response"))
			}
			return nil, fmt.Errorf("orchestrator: no choices in response")
		}

		choice := resp.Choices[0]

		// If no tool calls, we have the final answer.
		if len(choice.Message.ToolCalls) == 0 {
			content, _ := extractContent(choice.Message.Content)
			if span != nil {
				span.End(content, llmobs.TokenCount{
					Prompt:     resp.Usage.PromptTokens,
					Completion: resp.Usage.CompletionTokens,
					Total:      resp.Usage.TotalTokens,
				}, nil)
			}
			// Async write-back: summarise this turn to memorus (non-blocking).
			summary := BuildMemorySummary(in.TenantID, in.UserMessage, content)
			AsyncWriteMemory(o.memory, userID, summary, map[string]any{"source": "tally-ai"})
			return &ChatOutput{
				AssistantText: content,
				Plans:         plans,
				ToolCalls:     toolCalls,
			}, nil
		}

		// Append the full assistant message verbatim (trap 4: reasoning_content must survive).
		messages = append(messages, choice.Message)

		// Dispatch each tool call.
		for _, tc := range choice.Message.ToolCalls {
			result := o.registry.Dispatch(ctx, in.TenantID, tc)
			if span != nil {
				span.AttachToolCall(tc.Function.Name, tc.Function.Arguments, result.Content)
			}
			toolCalls = append(toolCalls, domainai.ToolCallRecord{
				ToolName:   tc.Function.Name,
				ArgsJSON:   tc.Function.Arguments,
				ResultJSON: result.Content,
			})

			// Persist plan if this was a destructive tool.
			if result.Plan != nil {
				if span != nil {
					result.Plan.TraceID = span.TraceID()
				}
				if err := o.planStore.SavePlan(ctx, result.Plan); err != nil {
					// Non-fatal: log and continue; plan won't be confirmable.
					toolCalls[len(toolCalls)-1].Error = err
				} else {
					plans = append(plans, result.Plan)
				}
			}

			// Append the tool result message for the next LLM turn.
			messages = append(messages, llmclient.Message{
				Role:       "tool",
				Content:    result.Content,
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
			})
		}
		// Close the span for this round before the next tool-call round starts.
		if span != nil {
			span.End("", llmobs.TokenCount{
				Prompt:     resp.Usage.PromptTokens,
				Completion: resp.Usage.CompletionTokens,
				Total:      resp.Usage.TotalTokens,
			}, nil)
		}
	}

	return nil, fmt.Errorf("orchestrator: exceeded %d tool rounds without final answer", maxToolRounds)
}

// StreamChat executes one user turn and streams the response via onChunk.
// Tool calls are executed synchronously before streaming begins.
func (o *Orchestrator) StreamChat(ctx context.Context, in ChatInput, onChunk func(string)) (*ChatOutput, error) {
	ctx = llmgateway.WithTenant(ctx, in.TenantID.String())
	// Memory recall: augment user message with relevant past context.
	userID := in.TenantID.String()
	augmented := AugmentMessagesWithMemoryOrFallback(o.memory, ctx, userID, in.UserMessage)
	inAugmented := in
	inAugmented.UserMessage = augmented

	messages := buildMessages(inAugmented)
	tools := ToolDefs()

	var plans []*domainai.Plan
	var toolCalls []domainai.ToolCallRecord

	// First do any tool-call rounds (non-streaming).
	for round := 0; round < maxToolRounds; round++ {
		span, spanCtx := o.startSpan(ctx, "stream", o.model, in.UserMessage)
		resp, err := o.llm.Chat(spanCtx, o.model, messages, tools)
		if err != nil {
			if span != nil {
				span.End("", llmobs.TokenCount{}, err)
			}
			return nil, fmt.Errorf("orchestrator: stream pre-tool chat: %w", err)
		}
		if len(resp.Choices) == 0 {
			if span != nil {
				span.End("", llmobs.TokenCount{}, fmt.Errorf("no choices"))
			}
			return nil, fmt.Errorf("orchestrator: no choices")
		}
		choice := resp.Choices[0]

		if len(choice.Message.ToolCalls) == 0 {
			// No more tools — this Chat response already IS the final answer.
			// Emit it directly instead of re-requesting with stream=true: a
			// second inference on the same prompt would run the LLM twice and
			// report usage twice (P1 double-billing defect — every simple Q&A
			// cost 2x tokens + 2x usage_events). The streaming contract is still
			// honoured: the handler wraps each onChunk call in an SSE `chunk`
			// event, so the client receives the answer as a (single) chunk.
			finalText, _ := extractContent(choice.Message.Content)
			if finalText != "" {
				onChunk(finalText)
			}
			if span != nil {
				span.End(finalText, llmobs.TokenCount{
					Prompt:     resp.Usage.PromptTokens,
					Completion: resp.Usage.CompletionTokens,
					Total:      resp.Usage.TotalTokens,
				}, nil)
			}
			// Async write-back: summarise this turn to memorus (non-blocking).
			summary := BuildMemorySummary(in.TenantID, in.UserMessage, finalText)
			AsyncWriteMemory(o.memory, userID, summary, map[string]any{"source": "tally-ai"})
			return &ChatOutput{
				AssistantText: finalText,
				Plans:         plans,
				ToolCalls:     toolCalls,
			}, nil
		}

		// Has tool calls — execute them first (non-streaming).
		messages = append(messages, choice.Message)
		for _, tc := range choice.Message.ToolCalls {
			result := o.registry.Dispatch(ctx, in.TenantID, tc)
			if span != nil {
				span.AttachToolCall(tc.Function.Name, tc.Function.Arguments, result.Content)
			}
			toolCalls = append(toolCalls, domainai.ToolCallRecord{
				ToolName:   tc.Function.Name,
				ArgsJSON:   tc.Function.Arguments,
				ResultJSON: result.Content,
			})
			if result.Plan != nil {
				if err := o.planStore.SavePlan(ctx, result.Plan); err != nil {
					toolCalls[len(toolCalls)-1].Error = err
				} else {
					plans = append(plans, result.Plan)
				}
			}
			messages = append(messages, llmclient.Message{
				Role:       "tool",
				Content:    result.Content,
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
			})
		}
		if span != nil {
			span.End("", llmobs.TokenCount{
				Prompt:     resp.Usage.PromptTokens,
				Completion: resp.Usage.CompletionTokens,
				Total:      resp.Usage.TotalTokens,
			}, nil)
		}
	}

	return nil, fmt.Errorf("orchestrator: exceeded %d tool rounds", maxToolRounds)
}

// ConfirmPlan executes a confirmed plan's real side effects (build PO draft,
// change prices, adjust stock) via the attached PlanExecutor and returns the
// updated plan plus an ExecutionResult.
//
// Idempotency: the plan is flipped to Confirmed *before* execution, so a
// concurrent second click sees a non-pending plan and is rejected. On execution
// failure the plan is moved to the terminal Failed state (NOT back to Pending):
// only the purchase-draft path is fully transactional; bulk price/stock changes
// are not atomic across products, so a partial failure may already have changed
// some rows and an immediate re-confirm could double-apply them. The user must
// cancel and request a fresh suggestion.
//
// Returns ErrPlanNotFound when the plan is missing, ErrPlanExpired when
// ExpiresAt has passed, or a wrapped error for any other failure.
// actorID is the acting user (for bill creator + audit attribution).
func (o *Orchestrator) ConfirmPlan(ctx context.Context, tenantID, actorID, planID uuid.UUID) (*domainai.Plan, *ExecutionResult, error) {
	plan, err := o.planStore.GetPlan(ctx, tenantID, planID)
	if err != nil {
		return nil, nil, fmt.Errorf("confirm plan: get: %w", err)
	}
	if plan == nil {
		return nil, nil, ErrPlanNotFound
	}
	if !plan.ExpiresAt.IsZero() && time.Now().After(plan.ExpiresAt) {
		// Mark expired in store so subsequent GETs see the terminal state.
		// Best-effort: a transient store error here doesn't change the answer
		// to the caller — the plan is still expired.
		if plan.Status == domainai.PlanStatusPending {
			plan.Status = domainai.PlanStatusExpired
			_ = o.planStore.UpdatePlan(ctx, plan)
		}
		return nil, nil, ErrPlanExpired
	}
	if plan.Status != domainai.PlanStatusPending {
		return nil, nil, fmt.Errorf("confirm plan: plan is %s, cannot confirm", plan.Status)
	}

	// Flip to Confirmed first — acts as a lock against concurrent double-clicks.
	plan.Status = domainai.PlanStatusConfirmed
	if err := o.planStore.UpdatePlan(ctx, plan); err != nil {
		return nil, nil, fmt.Errorf("confirm plan: update: %w", err)
	}

	// No executor wired (dev/tests): status flip only, no side effects.
	if o.executor == nil {
		return plan, nil, nil
	}

	result, execErr := o.executor.Execute(ctx, actorID, plan)
	if execErr != nil {
		o.recordAudit(ctx, actorID, plan, nil, execErr)
		// Mark as Failed — a terminal state. Execution may have produced partial
		// side effects (e.g. price rows already changed before a later row errors),
		// so reverting to Pending and allowing an immediate retry is unsafe: a
		// re-confirm could double-apply the successfully-executed portion.
		// The user must cancel this plan and request a fresh suggestion.
		plan.Status = domainai.PlanStatusFailed
		if markErr := o.planStore.UpdatePlan(ctx, plan); markErr != nil {
			return nil, nil, fmt.Errorf("confirm plan: execute failed (%v) and mark-failed: %w", execErr, markErr)
		}
		return nil, nil, fmt.Errorf("confirm plan: %w: %w", ErrPlanExecutionFailed, execErr)
	}
	o.recordAudit(ctx, actorID, plan, result, nil)
	return plan, result, nil
}

// recordAudit writes one audit row for an AI plan execution. Best-effort: a
// failure here is never surfaced to the user because the side effect already
// committed — the AuditWriter implementation is responsible for logging.
func (o *Orchestrator) recordAudit(ctx context.Context, actorID uuid.UUID, plan *domainai.Plan, result *ExecutionResult, execErr error) {
	if o.audit == nil {
		return
	}
	payload := map[string]any{
		"plan_id":     plan.ID.String(),
		"type":        string(plan.Type),
		"description": plan.Preview.Description,
		"sample_rows": plan.Preview.SampleRows, // before/after diff
	}
	rec := AuditRecord{
		TenantID:   plan.TenantID,
		ActorID:    actorID,
		Action:     "ai.plan.executed",
		TargetKind: "ai_plan",
		TargetID:   plan.ID.String(),
		Payload:    payload,
	}
	if execErr != nil {
		rec.Action = "ai.plan.failed"
		payload["error"] = execErr.Error()
		_ = o.audit.Write(ctx, rec)
		return
	}
	if result != nil {
		payload["affected_count"] = result.AffectedCount
		if result.BillID != nil {
			payload["bill_id"] = result.BillID.String()
			payload["bill_no"] = result.BillNo
			rec.TargetKind = "bill"
			rec.TargetID = result.BillID.String()
		}
	}
	_ = o.audit.Write(ctx, rec)
}

// CancelPlan marks a plan as cancelled.
func (o *Orchestrator) CancelPlan(ctx context.Context, tenantID, planID uuid.UUID) error {
	plan, err := o.planStore.GetPlan(ctx, tenantID, planID)
	if err != nil {
		return fmt.Errorf("cancel plan: get: %w", err)
	}
	if plan == nil {
		return ErrPlanNotFound
	}
	if plan.Status != domainai.PlanStatusPending {
		return nil // already resolved — idempotent
	}
	plan.Status = domainai.PlanStatusCancelled
	return o.planStore.UpdatePlan(ctx, plan)
}

// ListPlans returns plans for tenantID, optionally filtered by status.
// Backs the GET /api/v1/ai/plans endpoint used by both the web UI and the
// tally-mcp tally://ai/plans/pending resource (ADR-0011 Phase 3b).
func (o *Orchestrator) ListPlans(ctx context.Context, tenantID uuid.UUID, statusFilter string) ([]*domainai.Plan, error) {
	plans, err := o.planStore.ListByTenant(ctx, tenantID, statusFilter)
	if err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	return plans, nil
}

// ErrPlanNotFound is returned when a plan cannot be found in the store.
var ErrPlanNotFound = fmt.Errorf("plan not found")

// ErrPlanExpired is returned when ConfirmPlan sees a plan whose ExpiresAt has
// passed. The handler maps this to 409 Conflict so the UI can prompt the user
// to start a new turn rather than retrying blindly.
var ErrPlanExpired = fmt.Errorf("plan expired")

// ErrPlanExecutionFailed is returned when ConfirmPlan's executor call fails.
// The plan is marked Failed (terminal); the handler maps this to 422 so the UI
// knows not to offer a retry button — the user must cancel and start over.
var ErrPlanExecutionFailed = fmt.Errorf("plan execution failed")

// buildMessages assembles the full message list for the LLM.
func buildMessages(in ChatInput) []llmclient.Message {
	msgs := make([]llmclient.Message, 0, 1+len(in.History)+1)
	msgs = append(msgs, llmclient.Message{Role: "system", Content: systemPrompt})
	msgs = append(msgs, in.History...)
	msgs = append(msgs, llmclient.Message{Role: "user", Content: in.UserMessage})
	return msgs
}

// extractContent handles both string and []interface{} content formats.
func extractContent(content interface{}) (string, bool) {
	switch v := content.(type) {
	case string:
		return v, true
	case nil:
		return "", false
	default:
		return fmt.Sprintf("%v", v), true
	}
}
