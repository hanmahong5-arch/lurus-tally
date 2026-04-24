package logger_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/logger"
)

func TestLogger_JSONOutput_ContainsRequiredFields(t *testing.T) {
	var buf bytes.Buffer
	l := logger.New("info", "lurus-tally", "v1.0.0", &buf)

	l.Info("test message")

	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatal("expected log output, got empty string")
	}

	var entry map[string]any
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		t.Fatalf("log output is not valid JSON: %v\noutput: %s", err, line)
	}

	for _, field := range []string{"time", "level", "service", "version"} {
		if _, ok := entry[field]; !ok {
			t.Errorf("expected field %q in log entry, not found. entry: %v", field, entry)
		}
	}
}

func TestLogger_LevelFilter_DebugSuppressedAtInfo(t *testing.T) {
	var buf bytes.Buffer
	l := logger.New("info", "lurus-tally", "test", &buf)

	l.Debug("this should be suppressed")

	if buf.Len() > 0 {
		t.Errorf("expected no output for DEBUG at INFO level, got: %s", buf.String())
	}

	l.Info("this should appear")
	if buf.Len() == 0 {
		t.Error("expected INFO message to appear")
	}
}

func TestLogger_SetDefault_IsCallable(t *testing.T) {
	// Verify that New also sets the global default logger without panicking.
	var buf bytes.Buffer
	logger.New("info", "lurus-tally", "test", &buf)
	slog.Info("global logger test") // must not panic
}
