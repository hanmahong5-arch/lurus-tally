package lifecycle

import (
	"context"
	"fmt"
	"log/slog"
)

// Stop drains in-flight requests and closes the HTTP server.
// ctx controls the shutdown deadline; callers should pass a context with a 5s timeout
// so that the server does not wait indefinitely. A zero or cancelled context causes
// the server to close immediately.
func (a *App) Stop(ctx context.Context) error {
	a.log.Info("shutting down server")
	if err := a.srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("server shutdown: %w", err)
	}
	a.log.Info("server stopped", slog.String("addr", a.srv.Addr))
	return nil
}
