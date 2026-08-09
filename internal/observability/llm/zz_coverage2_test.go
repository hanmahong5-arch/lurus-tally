package llm

import (
	"context"
	"testing"
	"time"

	oteltrace "go.opentelemetry.io/otel/trace"
)

// This file closes the two remaining *reachable* coverage gaps left after
// zz_coverage_test.go:
//
//  1. otel.go:217-223 TraceID()'s "invalid SpanContext" branch (line 219-221)
//     was never hit because every span produced by a real TracerProvider
//     (even non-recording, NeverSample ones) inherits a valid, randomly
//     generated trace ID from the root. To hit the `!sc.IsValid()` branch we
//     construct an otelSpan directly (white-box, same package) around
//     oteltrace.SpanFromContext(context.Background()), which per the
//     go.opentelemetry.io/otel/trace API returns a noop Span whose
//     SpanContext() is the zero value (IsValid() == false).
//
//  2. otel.go:169-176 ShutdownOTelProvider's non-nil-provider path (line 175,
//     globalProvider.Shutdown(ctx)) was only exercised via the nil-provider
//     no-op branch in zz_coverage_test.go. Here we build a real provider
//     (NeverSample, invalid host, matching the brief's "no live collector"
//     rule — no span is ever queued for export so Shutdown never dials out)
//     and then shut it down, asserting it returns promptly with no error.
//
// The remaining otel.go gaps (buildProvider's otlptracehttp.New error path,
// the resource.New nil-result defensive check, and NewOTelTracer's wrapping
// of a buildProvider error) are NOT reachable from this package's exported
// surface: otlptracehttp/otlptrace's Start only fails if the internally
// constructed context is already Done (buildProvider builds its own 10s
// timeout context with no injection seam), and sdk/resource.New always
// returns a non-nil *Resource for the WithAttributes-only option used here.
// Hitting those branches would require either a live network failure or an
// injectable seam in otel.go itself — out of scope for a test-only change.

// TestOtelSpan_TraceID_InvalidSpanContext_ReturnsEmpty pins the "no valid
// SpanContext" branch of otelSpan.TraceID (otel.go:217-223): TraceID() must
// return "" rather than panicking or fabricating an ID.
func TestOtelSpan_TraceID_InvalidSpanContext_ReturnsEmpty(t *testing.T) {
	sp := &otelSpan{s: oteltrace.SpanFromContext(context.Background())}

	if sc := sp.s.SpanContext(); sc.IsValid() {
		t.Fatalf("test setup bug: expected an invalid SpanContext from a noop span, got %v", sc)
	}

	if got := sp.TraceID(); got != "" {
		t.Errorf("otelSpan.TraceID() with invalid SpanContext = %q, want \"\"", got)
	}
}

// TestShutdownOTelProvider_RealProvider_ShutsDownWithoutDialing exercises the
// non-nil-provider branch of ShutdownOTelProvider. LLM_TRACE_SAMPLE_RATIO=0.0
// (NeverSample) means no span is ever recorded/queued, so the batch
// processor's drain-and-shutdown never has anything to export and the
// exporter's Stop() only closes an internal channel (see
// otlptracehttp@v1.24.0 client.Stop) — no network dial occurs.
func TestShutdownOTelProvider_RealProvider_ShutsDownWithoutDialing(t *testing.T) {
	t.Setenv(envHost, "https://langfuse.invalid.test")
	t.Setenv(envPublicKey, "pk-test")
	t.Setenv(envSecretKey, "sk-test")
	t.Setenv(envSampleRatio, "0.0")

	resetGlobalProviderForTest(t)
	t.Cleanup(func() { resetGlobalProviderForTest(t) })

	tr := NewOTelTracer("lurus-tally-test-shutdown", "0.0.0")
	if _, ok := tr.(*otelTracer); !ok {
		t.Fatalf("expected *otelTracer when env fully configured, got %T", tr)
	}

	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		done <- ShutdownOTelProvider(ctx)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("ShutdownOTelProvider() on a real, never-exported-to provider error = %v, want nil", err)
		}
	case <-time.After(9 * time.Second):
		t.Fatal("ShutdownOTelProvider() did not return within 9s; it must not attempt a network dial when nothing was ever queued for export")
	}
}
