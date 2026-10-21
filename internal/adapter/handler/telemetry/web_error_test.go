package telemetry_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/telemetry"
)

func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func webErrBody(tenant, msg string) map[string]any {
	return map[string]any{
		"event":     "web_error",
		"tenant_id": tenant,
		"metadata": map[string]any{
			"route": "/sales", "message": msg, "kind": "api", "request_id": "req-123",
			"stack": "at x",
		},
	}
}

func TestWebError_LogsWarnWithTenant(t *testing.T) {
	buf := captureSlog(t)
	pub := &fakePublisher{}
	r := newRouter(telemetry.New(pub, "", "anonymous", nil))

	rec := postJSON(t, r, webErrBody("tenant-a", "boom"), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	out := buf.String()
	for _, want := range []string{"level=WARN", "msg=web_error", "tenant=tenant-a", "route=/sales", "message=boom", "request_id=req-123"} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q: %s", want, out)
		}
	}
	if strings.Contains(out, "at x") {
		t.Errorf("stack must not be in the log line: %s", out)
	}
	if len(pub.calls) != 1 {
		t.Errorf("publish calls = %d, want 1", len(pub.calls))
	}
}

func TestWebError_RateLimitedPerTenant(t *testing.T) {
	buf := captureSlog(t)
	pub := &fakePublisher{}
	h := telemetry.New(pub, "", "anonymous", nil)
	r := newRouter(h)

	for i := 0; i < 35; i++ {
		postJSON(t, r, webErrBody("tenant-a", "boom"), nil)
	}
	if got := strings.Count(buf.String(), "msg=web_error"); got != 30 {
		t.Errorf("logged = %d, want 30", got)
	}
	if len(pub.calls) != 30 {
		t.Errorf("published = %d, want 30", len(pub.calls))
	}
	if h.DroppedWebErrors() != 5 {
		t.Errorf("dropped = %d, want 5", h.DroppedWebErrors())
	}
	// another tenant is unaffected
	postJSON(t, r, webErrBody("tenant-b", "boom"), nil)
	if len(pub.calls) != 31 {
		t.Errorf("tenant-b should still pass, published = %d", len(pub.calls))
	}
}

func TestOtherEvents_NoWebErrorLog(t *testing.T) {
	buf := captureSlog(t)
	r := newRouter(telemetry.New(&fakePublisher{}, "", "anonymous", nil))
	postJSON(t, r, map[string]any{"event": "undo_used", "tenant_id": "t"}, nil)
	if strings.Contains(buf.String(), "web_error") {
		t.Errorf("unexpected log: %s", buf.String())
	}
}

func TestWebError_SkipsDAU(t *testing.T) {
	captureSlog(t)
	dau := &fakeDAU{}
	r := newRouter(telemetry.New(&fakePublisher{}, "", "anonymous", dau))
	body := webErrBody("tenant-a", "boom")
	body["user_id"] = "u1"
	if rec := postJSON(t, r, body, nil); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(dau.calls) != 0 {
		t.Errorf("web_error must not record DAU, got %v", dau.calls)
	}
}
