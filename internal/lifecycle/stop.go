package lifecycle

import (
	"context"
	"fmt"
	"log/slog"

	llmobs "github.com/hanmahong5-arch/lurus-tally/internal/observability/llm"
)

// Stop drains in-flight requests, stops background workers, and closes the HTTP server.
// ctx controls the shutdown deadline; callers should pass a context with a 5s timeout
// so that the server does not wait indefinitely. A zero or cancelled context causes
// the server to close immediately.
func (a *App) Stop(ctx context.Context) error {
	// Stop the outbox drain worker before the HTTP server so any in-flight
	// drain cycle can finish without new requests racing with shutdown.
	if a.stopOutbox != nil {
		a.stopOutbox()
	}
	// Drain the audit subscriber too — owns its own JetStream consume goroutines.
	if a.auditSub != nil {
		a.auditSub.Stop()
	}

	a.log.Info("shutting down server")
	if err := a.srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("server shutdown: %w", err)
	}
	a.log.Info("server stopped", slog.String("addr", a.srv.Addr))

	// Flush any buffered LLM trace spans (no-op when tracer is no-op).
	if err := llmobs.ShutdownOTelProvider(ctx); err != nil {
		a.log.Warn("llm tracer shutdown failed", slog.String("error", err.Error()))
	}
	return nil
}
