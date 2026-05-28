// Package llm provides LLM call observability — tracing every inference
// request with structured span attributes (operation, model, prompt/output
// tokens, latency, error state) and optional nested tool-call spans.
//
// The tracer is injected into app/ai; when no Langfuse OTel endpoint is
// configured the no-op implementation is used transparently.
package llm

import "context"

// Tracer creates and manages LLM spans. Implementations must be safe for
// concurrent use. A nil Tracer is not valid — use Noop() instead.
type Tracer interface {
	// StartLLMSpan begins a new LLM inference span. It returns the span and
	// a child context that callers should propagate. op is a short label
	// (e.g. "chat", "stream"); model is the inference model name read from
	// env / config. prompt is the raw prompt text (may be truncated by the
	// implementation to avoid excessive payload size).
	StartLLMSpan(ctx context.Context, op, model, prompt string) (Span, context.Context)
}

// Span represents a single unit of LLM work. Callers must call End exactly
// once after the inference call completes.
type Span interface {
	// End closes the span. output is the final response text (empty string on
	// error path). tokens carries prompt/completion/total token counts from the
	// inference response — zero values are acceptable when tokens are unknown.
	// err, when non-nil, marks the span as failed and records the error message.
	End(output string, tokens TokenCount, err error)

	// AttachToolCall records a nested tool dispatch under this span. name is
	// the tool function name, argsJSON is the raw JSON argument payload, and
	// resultJSON is the tool result returned to the LLM.
	AttachToolCall(name, argsJSON, resultJSON string)
}

// TokenCount carries the token usage from one inference response.
type TokenCount struct {
	Prompt     int
	Completion int
	Total      int
}
