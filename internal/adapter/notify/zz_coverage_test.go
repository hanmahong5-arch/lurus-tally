package notify

// Additional coverage for the branches the hand-written *_test.go files in
// this package don't reach: postJSON's marshal/build-request/transport error
// paths, DingTalkNotifier's signedURL parse-error path, Dispatcher's
// partial-failure fan-out, NewLogNotifier's nil-logger fallback, and the
// remaining isNilNotifier type switch cases. See notify_test.go / lowstock.go
// / dingtalk.go / wecom.go for the code under test.

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- postJSON error branches (wecom.go) -----------------------------------

func TestPostJSON_MarshalError(t *testing.T) {
	// A channel value cannot be JSON-marshaled, so postJSON must surface the
	// marshal error without ever attempting the HTTP request.
	err := postJSON(context.Background(), &http.Client{}, "http://example.invalid", make(chan int), "test")
	if err == nil {
		t.Fatal("expected marshal error, got nil")
	}
	if !strings.Contains(err.Error(), "notify/test: marshal payload") {
		t.Errorf("error = %v, want it to mention marshal payload", err)
	}
}

func TestPostJSON_BuildRequestError(t *testing.T) {
	// A URL containing a raw space fails http.NewRequestWithContext's
	// internal url.Parse before any network I/O happens.
	err := postJSON(context.Background(), &http.Client{}, "http://a b.invalid", struct{}{}, "test")
	if err == nil {
		t.Fatal("expected build-request error, got nil")
	}
	if !strings.Contains(err.Error(), "notify/test: build request") {
		t.Errorf("error = %v, want it to mention build request", err)
	}
}

func TestPostJSON_ClientDoError(t *testing.T) {
	// Bind a TCP listener, then close it immediately: the resulting address
	// is guaranteed to refuse connections, so client.Do fails with a network
	// error (not a status code) — the "send request" branch.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to bind test listener: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("failed to close test listener: %v", err)
	}

	client := &http.Client{Timeout: 2 * time.Second}
	err = postJSON(context.Background(), client, "http://"+addr, struct{}{}, "test")
	if err == nil {
		t.Fatal("expected transport error for closed port, got nil")
	}
	if !strings.Contains(err.Error(), "notify/test: send request") {
		t.Errorf("error = %v, want it to mention send request", err)
	}
}

// --- DingTalkNotifier signedURL parse-error branch (dingtalk.go) ----------

func TestDingTalkNotifier_SignedURL_ParseError(t *testing.T) {
	// A raw space in the host makes url.Parse fail inside signedURL itself
	// (distinct from Send's own error-wrapping branch, exercised below).
	n := NewDingTalkNotifier("http://a b.invalid", "secret")
	if n == nil {
		t.Fatal("expected non-nil notifier for non-empty webhook URL")
	}
	n.now = func() time.Time { return time.UnixMilli(1_700_000_000_000) }

	_, err := n.signedURL()
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	if !strings.Contains(err.Error(), "parse webhook url") {
		t.Errorf("error = %v, want it to mention parse webhook url", err)
	}
}

func TestDingTalkNotifier_Send_SignedURLErrorPropagates(t *testing.T) {
	// Send's own wrapping branch around a failing signedURL() call.
	n := NewDingTalkNotifier("http://a b.invalid", "secret")
	if n == nil {
		t.Fatal("expected non-nil notifier for non-empty webhook URL")
	}

	err := n.Send(context.Background(), "t", "b")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "notify/dingtalk: sign request") {
		t.Errorf("error = %v, want it to mention sign request", err)
	}
}

// --- NewLogNotifier nil-logger fallback (lowstock.go) ---------------------

func TestNewLogNotifier_NilLoggerFallback(t *testing.T) {
	n := NewLogNotifier(nil)
	if n == nil {
		t.Fatal("expected non-nil notifier")
	}
	// Must not panic despite the nil logger argument, and Send always
	// succeeds regardless of which logger backs it.
	if err := n.Send(context.Background(), "title", "body"); err != nil {
		t.Errorf("Send returned %v, want nil", err)
	}
}

func TestLogNotifier_Send_LogsTitleAndBody(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	n := NewLogNotifier(logger)

	if err := n.Send(context.Background(), "my-title", "my-body"); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("log line not JSON: %q: %v", buf.String(), err)
	}
	if rec["title"] != "my-title" {
		t.Errorf("logged title = %v, want my-title", rec["title"])
	}
	if rec["body"] != "my-body" {
		t.Errorf("logged body = %v, want my-body", rec["body"])
	}
}

// --- isNilNotifier remaining type-switch cases (lowstock.go) -------------

// fakeNotifier is a minimal Notifier used only to drive isNilNotifier's
// default case (a concrete type the switch doesn't special-case) and to
// exercise Dispatcher's partial-failure fan-out below. Safe for concurrent
// Send calls, per the Notifier contract.
type fakeNotifier struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (f *fakeNotifier) Send(ctx context.Context, title, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.err
}

func (f *fakeNotifier) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestNewDispatcher_DropsNilDingTalkNotifier_AndKeepsUnknownType(t *testing.T) {
	// NewDingTalkNotifier("", ...) returns a typed-nil *DingTalkNotifier;
	// isNilNotifier's *DingTalkNotifier case must catch it (it currently has
	// no other test coverage). fakeNotifier is not one of the four named
	// types, so it must fall through to isNilNotifier's default case and be
	// kept as live regardless.
	fn := &fakeNotifier{}
	d := NewDispatcher(slog.Default(), NewDingTalkNotifier("", "secret"), fn)

	if !d.Configured() {
		t.Fatal("dispatcher should be configured: fakeNotifier is a live, non-nil notifier")
	}
	if len(d.notifiers) != 1 {
		t.Fatalf("notifiers = %d, want exactly 1 (nil DingTalk dropped, fake kept)", len(d.notifiers))
	}
}

// --- Dispatcher.DispatchLowStock partial-failure fan-out (lowstock.go) ---

func TestDispatcher_DispatchLowStock_PartialFailureContinuesFanOut(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	failing := &fakeNotifier{err: context.DeadlineExceeded}
	succeeding := &fakeNotifier{}

	d := NewDispatcher(logger, failing, succeeding)
	d.DispatchLowStock(context.Background(), "scope", sampleLines())

	// Both notifiers must have been attempted: a failure must never abort
	// the fan-out to the remaining notifiers.
	if failing.callCount() != 1 {
		t.Errorf("failing notifier calls = %d, want 1", failing.callCount())
	}
	if succeeding.callCount() != 1 {
		t.Errorf("succeeding notifier calls = %d, want 1", succeeding.callCount())
	}

	var sawError, sawInfo bool
	for _, ln := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if ln == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(ln), &rec); err != nil {
			t.Fatalf("log line not JSON: %q: %v", ln, err)
		}
		switch rec["msg"] {
		case "notify: low-stock alert send failed":
			sawError = true
			if rec["error"] != context.DeadlineExceeded.Error() {
				t.Errorf("logged error = %v, want %v", rec["error"], context.DeadlineExceeded.Error())
			}
		case "notify: low-stock alert sent":
			sawInfo = true
		}
	}
	if !sawError {
		t.Errorf("no error log for failing notifier\nlog:\n%s", buf.String())
	}
	if !sawInfo {
		t.Errorf("no success log for succeeding notifier\nlog:\n%s", buf.String())
	}
}

func TestDispatcher_DispatchLowStock_ZeroLinesNoOp(t *testing.T) {
	fn := &fakeNotifier{}
	d := NewDispatcher(slog.Default(), fn)
	d.DispatchLowStock(context.Background(), "scope", nil)
	if fn.callCount() != 0 {
		t.Errorf("calls = %d, want 0: zero lines must be a no-op", fn.callCount())
	}
}
