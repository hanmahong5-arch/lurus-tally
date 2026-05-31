package llm

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const (
	// tracerName scopes all spans produced by this package.
	tracerName = "lurus-tally/llm"

	// envHost is the OTel / Langfuse OTLP HTTP collector endpoint.
	// Example: https://langfuse.example.internal
	envHost = "LANGFUSE_HOST"
	// envPublicKey is the Langfuse public key (used as OTLP basic-auth username).
	envPublicKey = "LANGFUSE_PUBLIC_KEY"
	// envSecretKey is the Langfuse secret key (used as OTLP basic-auth password).
	envSecretKey = "LANGFUSE_SECRET_KEY"

	// envSampleRatio controls the fraction of LLM spans that are exported.
	// Valid range: 0.0 (none) to 1.0 (all). Values outside [0, 1] or
	// non-numeric strings are silently treated as 1.0 (AlwaysSample).
	envSampleRatio = "LLM_TRACE_SAMPLE_RATIO"

	// maxAttrBytes is the maximum number of bytes stored for prompt/output
	// span attributes. Langfuse persists these; keeping them bounded avoids
	// oversized OTLP payloads on long conversations.
	maxAttrBytes = 4096
)

// providerOnce ensures the TracerProvider is initialised exactly once even
// under concurrent calls (e.g. test harnesses that call NewOTelTracer multiple
// times in parallel).
var (
	providerMu     sync.Mutex
	globalProvider *sdktrace.TracerProvider
)

// otelTracer is a Tracer backed by an OTel TracerProvider that exports spans
// to the Langfuse self-hosted OTLP HTTP endpoint.
type otelTracer struct {
	t trace.Tracer
}

// NewOTelTracer builds an OTel-backed Tracer that exports to a Langfuse
// self-hosted OTLP HTTP endpoint.  Configuration is read from:
//
//	LANGFUSE_HOST        — base URL, e.g. https://langfuse.internal
//	LANGFUSE_PUBLIC_KEY  — Langfuse public key  (basic-auth username)
//	LANGFUSE_SECRET_KEY  — Langfuse secret key  (basic-auth password)
//
// If any of the three env vars is absent or empty, Noop() is returned
// silently — LLM observability is always optional and must never block
// service startup or business operations.
func NewOTelTracer(serviceName, serviceVersion string) Tracer {
	host := os.Getenv(envHost)
	pubKey := os.Getenv(envPublicKey)
	secKey := os.Getenv(envSecretKey)

	if host == "" || pubKey == "" || secKey == "" {
		return Noop()
	}

	providerMu.Lock()
	defer providerMu.Unlock()

	if globalProvider == nil {
		tp, err := buildProvider(host, pubKey, secKey, serviceName, serviceVersion)
		if err != nil {
			// Initialisation failed — degrade silently; business path unaffected.
			return Noop()
		}
		globalProvider = tp
		otel.SetTracerProvider(globalProvider)
	}

	return &otelTracer{t: globalProvider.Tracer(tracerName)}
}

// buildProvider constructs an SDK TracerProvider with an OTLP HTTP exporter.
//
// Langfuse exposes OTLP at:  POST <host>/api/public/otel/v1/traces
// Authentication:  HTTP Basic  (username=publicKey, password=secretKey)
func buildProvider(host, pubKey, secKey, serviceName, serviceVersion string) (*sdktrace.TracerProvider, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Langfuse OTLP HTTP path: /api/public/otel  (exporter appends /v1/traces)
	endpoint := stripScheme(host)

	exp, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithURLPath("/api/public/otel/v1/traces"),
		otlptracehttp.WithHeaders(map[string]string{
			"Authorization": "Basic " + base64.StdEncoding.EncodeToString(
				[]byte(pubKey+":"+secKey),
			),
		}),
		// TLS is handled by the ingress in front of Langfuse on R6.
		// Flip to WithTLSClientConfig when direct mTLS is required.
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("llm tracer: build OTLP exporter: %w", err)
	}

	res, _ := resource.New(ctx,
		resource.WithAttributes(
			attribute.String("service.name", serviceName),
			attribute.String("service.version", serviceVersion),
		),
	)
	if res == nil {
		res = resource.Empty()
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(parseSampler()),
	)
	return tp, nil
}

// parseSampler reads LLM_TRACE_SAMPLE_RATIO and returns the corresponding
// sdktrace.Sampler. Invalid or absent values fall back to AlwaysSample so
// the service starts safely without explicit configuration.
//
// Recommended values:
//
//	unset / "1.0"  — AlwaysSample (default; good for low-volume deployments)
//	"0.1"          — sample 10 % of traces (high-throughput staging)
//	"0.0"          — NeverSample  (disable tracing without removing env vars)
func parseSampler() sdktrace.Sampler {
	raw := os.Getenv(envSampleRatio)
	if raw == "" {
		return sdktrace.AlwaysSample()
	}
	ratio, err := strconv.ParseFloat(raw, 64)
	if err != nil || ratio < 0 || ratio > 1 {
		// Silent fallback: misconfigured ratio must not break the service.
		return sdktrace.AlwaysSample()
	}
	switch {
	case ratio >= 1.0:
		return sdktrace.AlwaysSample()
	case ratio <= 0.0:
		return sdktrace.NeverSample()
	default:
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))
	}
}

// ShutdownOTelProvider flushes and shuts down the global TracerProvider.
// It should be called from the lifecycle Stop path. Safe to call when the
// provider was never initialised.
func ShutdownOTelProvider(ctx context.Context) error {
	providerMu.Lock()
	defer providerMu.Unlock()
	if globalProvider == nil {
		return nil
	}
	return globalProvider.Shutdown(ctx)
}

// StartLLMSpan begins a new client span for one LLM inference call.
func (o *otelTracer) StartLLMSpan(ctx context.Context, op, model, prompt string) (Span, context.Context) {
	ctx, s := o.t.Start(ctx, "llm."+op,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("llm.operation", op),
			attribute.String("llm.model", model),
			attribute.String("llm.prompt", Redact(truncate(prompt, maxAttrBytes))),
		),
	)
	return &otelSpan{s: s, tracer: o.t}, ctx
}

// otelSpan wraps an OTel span with LLM-specific attribute helpers.
type otelSpan struct {
	s      trace.Span
	tracer trace.Tracer
}

// End records output, token counts, and error state, then closes the span.
func (sp *otelSpan) End(output string, tok TokenCount, err error) {
	if err != nil {
		sp.s.SetAttributes(
			attribute.Bool("llm.error", true),
			attribute.String("llm.error.message", err.Error()),
		)
	} else {
		sp.s.SetAttributes(
			attribute.String("llm.output", Redact(truncate(output, maxAttrBytes))),
			attribute.Int("llm.tokens.prompt", tok.Prompt),
			attribute.Int("llm.tokens.completion", tok.Completion),
			attribute.Int("llm.tokens.total", tok.Total),
		)
	}
	sp.s.End()
}

// TraceID returns the 32-hex OTel trace ID. Empty string when the span is
// invalid (e.g. recording disabled or no provider).
func (sp *otelSpan) TraceID() string {
	sc := sp.s.SpanContext()
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}

// AttachToolCall creates a synchronous child span under the LLM span that
// records one tool dispatch (name, arguments, result).
func (sp *otelSpan) AttachToolCall(name, argsJSON, resultJSON string) {
	// Parent context carrying the active span so the child is properly linked.
	parent := trace.ContextWithSpan(context.Background(), sp.s)
	_, child := sp.tracer.Start(parent, "llm.tool."+name,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("llm.tool.name", name),
			attribute.String("llm.tool.args", RedactJSON(truncate(argsJSON, maxAttrBytes))),
			attribute.String("llm.tool.result", RedactJSON(truncate(resultJSON, maxAttrBytes))),
		),
	)
	child.End()
}

// truncate returns the first n bytes of s. Mid-rune cuts are acceptable for
// observability attributes.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// stripScheme removes a leading "https://" or "http://" prefix so the result
// can be passed to otlptracehttp.WithEndpoint, which expects host[:port].
func stripScheme(u string) string {
	for _, pfx := range []string{"https://", "http://"} {
		if len(u) > len(pfx) && u[:len(pfx)] == pfx {
			return u[len(pfx):]
		}
	}
	return u
}
