package llm

import "context"

// noopTracer is a zero-cost Tracer that records nothing. It is the default
// when the OTel / Langfuse endpoint is not configured so callers never need
// to nil-guard the Tracer field.
type noopTracer struct{}

// Noop returns a Tracer that silently discards all span data.
func Noop() Tracer { return noopTracer{} }

func (noopTracer) StartLLMSpan(ctx context.Context, _, _, _ string) (Span, context.Context) {
	return noopSpan{}, ctx
}

// noopSpan silently absorbs all calls.
type noopSpan struct{}

func (noopSpan) End(string, TokenCount, error)         {}
func (noopSpan) AttachToolCall(string, string, string) {}
func (noopSpan) TraceID() string                       { return "" }
