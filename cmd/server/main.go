// Package main is the entry point for lurus-tally.
// It loads config, wires dependencies via lifecycle.NewApp, starts the HTTP server,
// waits for a SIGTERM/SIGINT signal, then gracefully shuts down.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hanmahong5-arch/lurus-tally/internal/lifecycle"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		// Print error to stderr before exiting; slog not yet initialised.
		slog.Error("configuration error",
			slog.String("error", err.Error()),
			slog.String("action", "set the missing environment variable and restart"),
		)
		os.Exit(1)
	}

	app, err := lifecycle.NewApp(cfg)
	if err != nil {
		slog.Error("failed to initialise application",
			slog.String("error", err.Error()),
		)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	if err := app.Start(ctx); err != nil {
		slog.Error("failed to start server", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Block until signal received.
	<-ctx.Done()
	stop() // release signal resources

	slog.Info("shutdown signal received")

	shutdownTimeout := 5 * time.Second
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := app.Stop(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
