package loghelper_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/loghelper"
)

// captureLogger replaces the global slog default with a JSON logger writing to
// buf, and restores the original default when the test ends.
func captureLogger(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	l := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	old := slog.Default()
	slog.SetDefault(l)
	t.Cleanup(func() { slog.SetDefault(old) })
	return buf
}

func parseLog(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("log output is not valid JSON: %v\nraw: %s", err, buf.String())
	}
	return m
}

// TestInfo_EmitsEvent_WithTenantAndRequestID verifies Info writes a JSON log
// line containing the event, tenant_id, and request_id from context.
func TestInfo_EmitsEvent_WithTenantAndRequestID(t *testing.T) {
	buf := captureLogger(t)

	tenantID := uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	ctx := loghelper.WithTenantID(context.Background(), tenantID)
	ctx = loghelper.WithRequestID(ctx, "req-001")

	loghelper.Info(ctx, "bill_approved", map[string]any{
		"bill_type": "purchase",
	})

	m := parseLog(t, buf)

	if m["msg"] != "bill_approved" {
		t.Errorf("msg = %v, want bill_approved", m["msg"])
	}
	if m["tenant_id"] != tenantID.String() {
		t.Errorf("tenant_id = %v, want %v", m["tenant_id"], tenantID.String())
	}
	if m["request_id"] != "req-001" {
		t.Errorf("request_id = %v, want req-001", m["request_id"])
	}
	if m["bill_type"] != "purchase" {
		t.Errorf("bill_type = %v, want purchase", m["bill_type"])
	}
}

// TestWarn_EmitsWarnLevel verifies Warn writes level=WARN.
func TestWarn_EmitsWarnLevel(t *testing.T) {
	buf := captureLogger(t)

	loghelper.Warn(context.Background(), "idempotency_skipped", map[string]any{
		"reason": "cache_hit",
	})

	m := parseLog(t, buf)
	if m["level"] != "WARN" {
		t.Errorf("level = %v, want WARN", m["level"])
	}
}

// TestError_IncludesErrorMessage verifies Error embeds the error string.
func TestError_IncludesErrorMessage(t *testing.T) {
	buf := captureLogger(t)

	loghelper.Error(context.Background(), "bill_approved", errors.New("db: connection reset"), nil)

	m := parseLog(t, buf)
	if m["error"] != "db: connection reset" {
		t.Errorf("error = %v, want 'db: connection reset'", m["error"])
	}
	if m["level"] != "ERROR" {
		t.Errorf("level = %v, want ERROR", m["level"])
	}
}

// TestInfo_NoTenantOrRequestID_OmitsFields verifies that absent context values
// do not produce empty-string fields in the output.
func TestInfo_NoTenantOrRequestID_OmitsFields(t *testing.T) {
	buf := captureLogger(t)

	loghelper.Info(context.Background(), "outbox_drain_tick", map[string]any{
		"drained": 5,
	})

	m := parseLog(t, buf)
	if _, ok := m["tenant_id"]; ok {
		t.Error("tenant_id should not appear when absent from context")
	}
	if _, ok := m["request_id"]; ok {
		t.Error("request_id should not appear when absent from context")
	}
}
