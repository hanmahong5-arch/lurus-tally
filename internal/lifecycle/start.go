package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
)

// Start runs database migrations (when MigrateOnBoot is true), then begins listening for HTTP
// connections in a background goroutine. Migration runs synchronously before the server starts
// so that the readiness probe only passes after the schema is up-to-date.
// Returns a non-zero error if migration fails; the caller (main.go) should os.Exit(1) on error.
func (a *App) Start(ctx context.Context) error {
	if ctx.Err() != nil {
		return fmt.Errorf("context already cancelled before Start: %w", ctx.Err())
	}

	// Run database migrations before accepting traffic so the schema is always current.
	if a.cfg.MigrateOnBoot {
		if err := RunMigrations(ctx, a.cfg.DatabaseDSN, a.log); err != nil {
			a.log.Error("migration failed", slog.String("error", err.Error()))
			return err
		}
	}

	errCh := make(chan error, 1)
	go func() {
		if err := a.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http server error: %w", err)
		}
	}()

	// Check briefly for immediate startup errors (e.g. port already in use).
	select {
	case err := <-errCh:
		return err
	default:
	}

	a.log.Info("server started",
		slog.String("addr", a.srv.Addr),
		slog.String("service", "lurus-tally"),
		slog.String("version", a.cfg.ServiceVersion),
	)
	return nil
}
