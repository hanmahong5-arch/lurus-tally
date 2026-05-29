package alert_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-tally/internal/alert"
)

var exampleBreaches = []alert.Breach{
	{
		SignalName:      "ks1_onboarding_rate",
		ConsecutiveDays: 14,
		FirstRedDate:    time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	},
}

// TestLogSender_DoesNotError verifies LogSender sends without error.
func TestLogSender_DoesNotError(t *testing.T) {
	s := &alert.LogSender{Logger: slog.Default()}
	if err := s.Send(context.Background(), exampleBreaches); err != nil {
		t.Fatalf("LogSender.Send: %v", err)
	}
}

// TestFeishuSender_SkipsWhenNoURL verifies that an empty WebhookURL is a no-op.
func TestFeishuSender_SkipsWhenNoURL(t *testing.T) {
	s := &alert.FeishuSender{WebhookURL: ""}
	if err := s.Send(context.Background(), exampleBreaches); err != nil {
		t.Fatalf("expected no error for empty URL, got: %v", err)
	}
}

// TestFeishuSender_PostsValidJSON verifies the webhook body is valid JSON
// and contains signal information.
func TestFeishuSender_PostsValidJSON(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		capturedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"StatusCode":0}`))
	}))
	defer srv.Close()

	s := &alert.FeishuSender{
		WebhookURL: srv.URL,
		HTTPClient: srv.Client(),
	}
	if err := s.Send(context.Background(), exampleBreaches); err != nil {
		t.Fatalf("FeishuSender.Send: %v", err)
	}

	// Must be valid JSON.
	var payload map[string]interface{}
	if err := json.Unmarshal(capturedBody, &payload); err != nil {
		t.Fatalf("webhook body not valid JSON: %v\nbody: %s", err, capturedBody)
	}

	// Signal name must appear somewhere in the text content.
	bodyStr := string(capturedBody)
	if !strings.Contains(bodyStr, "ks1_onboarding_rate") {
		t.Errorf("expected signal name in webhook body, got: %s", bodyStr)
	}
}

// TestMultiSender_CallsAllSenders verifies that MultiSender reaches every
// inner sender regardless of order.
func TestMultiSender_CallsAllSenders(t *testing.T) {
	calls := 0
	counter := &countingSender{onSend: func() { calls++ }}

	ms := &alert.MultiSender{
		Senders: []alert.Sender{counter, counter, counter},
	}
	if err := ms.Send(context.Background(), exampleBreaches); err != nil {
		t.Fatalf("MultiSender.Send: %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 sender calls, got %d", calls)
	}
}

// TestMultiSender_ContinuesAfterFailure confirms that one failing sender
// does not prevent the remaining ones from being called.
func TestMultiSender_ContinuesAfterFailure(t *testing.T) {
	calls := 0
	counter := &countingSender{onSend: func() { calls++ }}
	failing := &errorSender{}

	ms := &alert.MultiSender{
		Senders: []alert.Sender{failing, counter, failing, counter},
	}
	err := ms.Send(context.Background(), exampleBreaches)
	if err == nil {
		t.Fatal("expected error from MultiSender when inner senders fail")
	}
	if calls != 2 {
		t.Errorf("expected 2 successful calls, got %d", calls)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

type countingSender struct {
	onSend func()
}

func (c *countingSender) Send(_ context.Context, _ []alert.Breach) error {
	c.onSend()
	return nil
}

type errorSender struct{}

func (e *errorSender) Send(_ context.Context, _ []alert.Breach) error {
	return &senderError{"intentional test failure"}
}

type senderError struct{ msg string }

func (e *senderError) Error() string { return e.msg }
