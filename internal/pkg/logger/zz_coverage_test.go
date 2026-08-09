package logger_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/logger"
)

// TestLogger_LevelMapping_FiltersAsExpected drives every branch of the level
// switch in New (debug/warn/warning/error/default) and asserts the resulting
// *slog.Logger actually filters Debug-level records accordingly. This is the
// real observable behaviour of the level mapping, not just an enum check.
func TestLogger_LevelMapping_FiltersAsExpected(t *testing.T) {
	cases := []struct {
		name         string
		level        string
		debugEmitted bool // whether a Debug() call should produce output
		infoEmitted  bool // whether an Info() call should produce output
		warnEmitted  bool // whether a Warn() call should produce output
		errorEmitted bool // whether an Error() call should produce output
	}{
		{name: "debug lowercase", level: "debug", debugEmitted: true, infoEmitted: true, warnEmitted: true, errorEmitted: true},
		{name: "DEBUG uppercase", level: "DEBUG", debugEmitted: true, infoEmitted: true, warnEmitted: true, errorEmitted: true},
		{name: "warn", level: "warn", debugEmitted: false, infoEmitted: false, warnEmitted: true, errorEmitted: true},
		{name: "warning alias", level: "warning", debugEmitted: false, infoEmitted: false, warnEmitted: true, errorEmitted: true},
		{name: "WARN uppercase", level: "WARN", debugEmitted: false, infoEmitted: false, warnEmitted: true, errorEmitted: true},
		{name: "error", level: "error", debugEmitted: false, infoEmitted: false, warnEmitted: false, errorEmitted: true},
		{name: "ERROR uppercase", level: "ERROR", debugEmitted: false, infoEmitted: false, warnEmitted: false, errorEmitted: true},
		{name: "empty string defaults to info", level: "", debugEmitted: false, infoEmitted: true, warnEmitted: true, errorEmitted: true},
		{name: "info explicit", level: "info", debugEmitted: false, infoEmitted: true, warnEmitted: true, errorEmitted: true},
		{name: "garbage value defaults to info", level: "not-a-real-level", debugEmitted: false, infoEmitted: true, warnEmitted: true, errorEmitted: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			l := logger.New(tc.level, "lurus-tally", "v1.0.0", &buf)

			buf.Reset()
			l.Debug("debug probe")
			gotDebug := buf.Len() > 0
			if gotDebug != tc.debugEmitted {
				t.Errorf("level=%q: Debug emitted=%v, want %v (output: %q)", tc.level, gotDebug, tc.debugEmitted, buf.String())
			}

			buf.Reset()
			l.Info("info probe")
			gotInfo := buf.Len() > 0
			if gotInfo != tc.infoEmitted {
				t.Errorf("level=%q: Info emitted=%v, want %v (output: %q)", tc.level, gotInfo, tc.infoEmitted, buf.String())
			}

			buf.Reset()
			l.Warn("warn probe")
			gotWarn := buf.Len() > 0
			if gotWarn != tc.warnEmitted {
				t.Errorf("level=%q: Warn emitted=%v, want %v (output: %q)", tc.level, gotWarn, tc.warnEmitted, buf.String())
			}

			buf.Reset()
			l.Error("error probe")
			gotError := buf.Len() > 0
			if gotError != tc.errorEmitted {
				t.Errorf("level=%q: Error emitted=%v, want %v (output: %q)", tc.level, gotError, tc.errorEmitted, buf.String())
			}
		})
	}
}

// TestLogger_RecordCarriesServiceAndVersion asserts every emitted record
// carries the exact service and version values passed to New, and that the
// output is valid JSON (confirms the JSON handler is wired, not text).
func TestLogger_RecordCarriesServiceAndVersion(t *testing.T) {
	var buf bytes.Buffer
	const wantService = "lurus-tally-svc"
	const wantVersion = "v2.3.4-rc1"

	l := logger.New("info", wantService, wantVersion, &buf)
	l.Info("carries attrs")

	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatal("expected log output, got empty string")
	}
	if !json.Valid([]byte(line)) {
		t.Fatalf("log output is not valid JSON: %s", line)
	}

	var entry map[string]any
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		t.Fatalf("failed to unmarshal log line: %v", err)
	}

	if got, ok := entry["service"]; !ok || got != wantService {
		t.Errorf("service = %v (present=%v), want %q", got, ok, wantService)
	}
	if got, ok := entry["version"]; !ok || got != wantVersion {
		t.Errorf("version = %v (present=%v), want %q", got, ok, wantVersion)
	}
}

// TestLogger_NilWriter_DefaultsToStderr exercises the w == nil branch. We
// cannot easily capture os.Stderr writes here, so we assert the documented
// fallback behaviour: New must not panic and must return a usable logger.
func TestLogger_NilWriter_DefaultsToStderr(t *testing.T) {
	l := logger.New("info", "lurus-tally", "v1.0.0", nil)
	if l == nil {
		t.Fatal("expected non-nil *slog.Logger when w is nil")
	}

	// Logging through it must not panic even though output goes to os.Stderr.
	l.Info("nil-writer probe, should not panic")
}

// TestLogger_SetDefault_AnnotatesGlobalLogger asserts that after New runs,
// slog.Default() is the annotated logger: a subsequent package-level
// slog.Info call must carry the service attribute passed to New.
func TestLogger_SetDefault_AnnotatesGlobalLogger(t *testing.T) {
	var buf bytes.Buffer
	const wantService = "lurus-tally-default-check"

	logger.New("info", wantService, "v9.9.9", &buf)

	// Use the package-level slog function, not the returned logger, to prove
	// SetDefault actually took effect globally.
	slog.Info("via global default")

	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatal("expected slog.Default() to write through to buf after New")
	}

	var entry map[string]any
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		t.Fatalf("global default output is not valid JSON: %v\noutput: %s", err, line)
	}
	if got, ok := entry["service"]; !ok || got != wantService {
		t.Errorf("global default service = %v (present=%v), want %q", got, ok, wantService)
	}
}
