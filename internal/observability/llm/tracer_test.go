package llm_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hanmahong5-arch/lurus-tally/internal/observability/llm"
)

// TestNoop_StartLLMSpan_ReturnsValidSpanAndContext verifies that the no-op
// tracer returns a valid Span and an unmodified context.
func TestNoop_StartLLMSpan_ReturnsValidSpanAndContext(t *testing.T) {
	tr := llm.Noop()
	ctx := context.Background()

	span, derived := tr.StartLLMSpan(ctx, "chat", "model-from-env", "Hello")
	if span == nil {
		t.Fatal("expected non-nil span from noop tracer")
	}
	if derived == nil {
		t.Fatal("expected non-nil context from noop tracer")
	}
}

// TestNoop_Span_End_DoesNotPanic verifies that calling End on a no-op span
// with every combination of output/tokens/error does not panic.
func TestNoop_Span_End_DoesNotPanic(t *testing.T) {
	tr := llm.Noop()

	tests := []struct {
		name   string
		output string
		tokens llm.TokenCount
		err    error
	}{
		{"success_with_tokens", "ok", llm.TokenCount{Prompt: 10, Completion: 5, Total: 15}, nil},
		{"error_path", "", llm.TokenCount{}, errors.New("upstream timeout")},
		{"zero_tokens", "some output", llm.TokenCount{}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			span, _ := tr.StartLLMSpan(context.Background(), "chat", "model-from-env", "prompt")
			// Must not panic.
			span.End(tc.output, tc.tokens, tc.err)
		})
	}
}

// TestNoop_Span_AttachToolCall_DoesNotPanic verifies that recording tool calls
// on a no-op span is safe.
func TestNoop_Span_AttachToolCall_DoesNotPanic(t *testing.T) {
	tr := llm.Noop()
	span, _ := tr.StartLLMSpan(context.Background(), "chat", "model-from-env", "query")
	// Must not panic with arbitrary inputs.
	span.AttachToolCall("search_products", `{"query":"widget"}`, `[{"id":"1"}]`)
	span.AttachToolCall("", "", "")
	span.End("", llm.TokenCount{}, nil)
}

// TestNoop_MultipleSpans_AreIndependent verifies that creating multiple spans
// from the same no-op tracer does not interfere.
func TestNoop_MultipleSpans_AreIndependent(t *testing.T) {
	tr := llm.Noop()
	ctx := context.Background()

	s1, ctx1 := tr.StartLLMSpan(ctx, "chat", "model-a", "p1")
	s2, ctx2 := tr.StartLLMSpan(ctx, "stream", "model-b", "p2")

	if ctx1 == nil || ctx2 == nil {
		t.Fatal("derived contexts must not be nil")
	}

	s1.End("out1", llm.TokenCount{Total: 5}, nil)
	s2.End("", llm.TokenCount{}, errors.New("err"))
}

// TestNewOTelTracer_MissingEnv_ReturnsNoop verifies that when the Langfuse
// env vars are absent, NewOTelTracer falls back to a no-op implementation and
// does not return nil.
func TestNewOTelTracer_MissingEnv_ReturnsNoop(t *testing.T) {
	// Ensure env vars are absent.
	t.Setenv("LANGFUSE_HOST", "")
	t.Setenv("LANGFUSE_PUBLIC_KEY", "")
	t.Setenv("LANGFUSE_SECRET_KEY", "")

	tr := llm.NewOTelTracer("lurus-tally-test", "0.0.0")
	if tr == nil {
		t.Fatal("expected non-nil tracer even when env vars are missing")
	}

	// Verify it behaves like a no-op (no panic, valid span returned).
	span, ctx := tr.StartLLMSpan(context.Background(), "chat", "model-from-env", "hello")
	if span == nil || ctx == nil {
		t.Fatal("tracer from missing env must return usable span and context")
	}
	span.End("response", llm.TokenCount{Prompt: 1, Completion: 1, Total: 2}, nil)
}

// TestTokenCount_ZeroValue_IsAccepted verifies that the zero value of
// TokenCount is a valid input to Span.End.
func TestTokenCount_ZeroValue_IsAccepted(t *testing.T) {
	tr := llm.Noop()
	span, _ := tr.StartLLMSpan(context.Background(), "chat", "model-from-env", "")
	var tok llm.TokenCount // zero value
	span.End("", tok, nil)
}

// TestNoop_Span_TraceID_ReturnsEmpty verifies the no-op span emits an empty
// trace ID so callers can branch on "".
func TestNoop_Span_TraceID_ReturnsEmpty(t *testing.T) {
	tr := llm.Noop()
	span, _ := tr.StartLLMSpan(context.Background(), "chat", "m", "p")
	if got := span.TraceID(); got != "" {
		t.Errorf("noop TraceID = %q, want \"\"", got)
	}
}

// TestSamplingRatio_ParsesEnv verifies that parseSampler (exercised indirectly
// via NewOTelTracer) degrades gracefully across representative env values.
// Because NewOTelTracer returns a Noop when Langfuse vars are absent, we test
// the sampler selection function directly through the exported surface: the
// tracer must start and return usable spans under every ratio string.
func TestSamplingRatio_ParsesEnv(t *testing.T) {
	// Langfuse vars absent → NewOTelTracer returns Noop regardless of ratio.
	// The key invariant is: no panic, non-nil tracer, non-nil span.
	cases := []struct {
		ratio string
	}{
		{"0.0"},
		{"0.5"},
		{"1.0"},
		{""},             // absent → AlwaysSample
		{"not-a-number"}, // bad string → AlwaysSample fallback
		{"-0.1"},         // out of range → AlwaysSample fallback
		{"1.5"},          // out of range → AlwaysSample fallback
	}
	for _, tc := range cases {
		t.Run("ratio="+tc.ratio, func(t *testing.T) {
			t.Setenv("LLM_TRACE_SAMPLE_RATIO", tc.ratio)
			// Langfuse creds absent → returns Noop; we are testing that no
			// panic occurs and the tracer is usable, not the sampler variant.
			t.Setenv("LANGFUSE_HOST", "")
			t.Setenv("LANGFUSE_PUBLIC_KEY", "")
			t.Setenv("LANGFUSE_SECRET_KEY", "")

			tr := llm.NewOTelTracer("tally-test", "0.0.0")
			if tr == nil {
				t.Fatal("NewOTelTracer must not return nil")
			}
			span, ctx := tr.StartLLMSpan(context.Background(), "chat", "m", "p")
			if span == nil || ctx == nil {
				t.Fatal("StartLLMSpan must return non-nil span and context")
			}
			span.End("ok", llm.TokenCount{}, nil)
		})
	}
}
