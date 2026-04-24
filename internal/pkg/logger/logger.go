// Package logger provides structured JSON logging via the standard library log/slog.
// New creates a logger with automatic service and version attributes on every record,
// registers it as the slog global default, and returns it for direct use.
package logger

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// New creates a JSON slog.Logger that annotates every record with service and version
// attributes, sets it as the global default (slog.SetDefault), and returns it.
// w is the log destination; pass nil to default to os.Stderr.
func New(level, service, version string, w io.Writer) *slog.Logger {
	if w == nil {
		w = os.Stderr
	}

	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	h := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: lvl})
	l := slog.New(h).With(
		slog.String("service", service),
		slog.String("version", version),
	)
	slog.SetDefault(l)
	return l
}
