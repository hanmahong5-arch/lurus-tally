package lifecycle

import (
	appai "github.com/hanmahong5-arch/lurus-tally/internal/app/ai"
	llmobs "github.com/hanmahong5-arch/lurus-tally/internal/observability/llm"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/config"
)

// BuildTracer constructs an LLM observability Tracer from the service config.
//
// When the three Langfuse env vars (LANGFUSE_HOST / LANGFUSE_PUBLIC_KEY /
// LANGFUSE_SECRET_KEY) are all present, an OTel-backed tracer that exports to
// the self-hosted Langfuse instance on R6 is returned.  If any env var is
// absent or empty, a no-op Tracer is returned — LLM calls proceed unchanged
// and no error is surfaced.
//
// Usage in main wiring (after the Orchestrator is constructed):
//
//	tracer := lifecycle.BuildTracer(cfg)
//	lifecycle.WireTracer(orchestrator, tracer)
func BuildTracer(cfg *config.Config) llmobs.Tracer {
	return llmobs.NewOTelTracer("lurus-tally", cfg.ServiceVersion)
}

// WireTracer attaches a Tracer to the given Orchestrator. It is a separate
// function so main.go can call it after the Orchestrator is constructed
// without this package needing to import the full lifecycle.App graph.
//
// Calling WireTracer with a nil tracer is safe — the Orchestrator skips
// tracing silently.
func WireTracer(o *appai.Orchestrator, t llmobs.Tracer) {
	if o == nil {
		return
	}
	o.WithTracer(t)
}
