package platform_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/platform"
)

// fakePublisher is a test double for adapternats.Publisher.
type fakePublisher struct {
	published []fakeMsg
	err       error
}

type fakeMsg struct {
	subject string
	payload any
}

func (f *fakePublisher) Publish(_ context.Context, subject string, payload any) error {
	if f.err != nil {
		return f.err
	}
	f.published = append(f.published, fakeMsg{subject: subject, payload: payload})
	return nil
}

func (f *fakePublisher) Close() error { return nil }

func TestNotifyAsync_PublishesToCorrectSubject(t *testing.T) {
	pub := &fakePublisher{}
	nc := platform.NewNotificationClient(platform.NotificationConfig{
		NATSPublisher: pub,
	})

	evt := platform.PSIEvent{
		EventID:    "evt-001",
		EventType:  "project.status_changed",
		TenantID:   "tenant-42",
		OccurredAt: time.Now(),
		Source:     "tally",
		Payload:    map[string]any{"old": "draft", "new": "active"},
	}

	if err := nc.NotifyAsync(context.Background(), evt); err != nil {
		t.Fatalf("NotifyAsync: %v", err)
	}

	if len(pub.published) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(pub.published))
	}
	if pub.published[0].subject != "PSI_EVENTS.project.status_changed" {
		t.Errorf("wrong subject: %s", pub.published[0].subject)
	}
}

func TestNotifyAsync_PaymentReceivedSubject(t *testing.T) {
	pub := &fakePublisher{}
	nc := platform.NewNotificationClient(platform.NotificationConfig{
		NATSPublisher: pub,
	})

	evt := platform.PSIEvent{
		EventType: "payment.received",
		TenantID:  "tenant-1",
		Source:    "tally",
		Payload:   map[string]any{"amount": 100.0},
	}

	if err := nc.NotifyAsync(context.Background(), evt); err != nil {
		t.Fatalf("NotifyAsync: %v", err)
	}
	if pub.published[0].subject != "PSI_EVENTS.payment.received" {
		t.Errorf("subject mismatch: %s", pub.published[0].subject)
	}
}

func TestNotifyAsync_NATSError_ReturnedToCallerWithLog(t *testing.T) {
	pub := &fakePublisher{err: errFakeNATS}
	nc := platform.NewNotificationClient(platform.NotificationConfig{
		NATSPublisher: pub,
	})

	err := nc.NotifyAsync(context.Background(), platform.PSIEvent{
		EventType: "project.status_changed",
		TenantID:  "t",
		Source:    "tally",
	})
	if err == nil {
		t.Error("expected error from failed NATS publish")
	}
}

func TestNotifySync_HappyPath_Returns2xx(t *testing.T) {
	var gotBody NotifyRequestBody
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/internal/v1/notify" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("wrong auth: %s", auth)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	pub := &fakePublisher{}
	nc := platform.NewNotificationClient(platform.NotificationConfig{
		NATSPublisher: pub,
		NotifyURL:     srv.URL,
		APIKey:        "test-key",
	})

	req := platform.NotifyRequest{
		AccountID: 7,
		Type:      "payment.received",
		Title:     "Payment received",
		Body:      "Your payment of ¥100 was received.",
	}
	if err := nc.NotifySync(context.Background(), req); err != nil {
		t.Fatalf("NotifySync: %v", err)
	}
	if gotBody.AccountID != 7 {
		t.Errorf("account_id not propagated: %+v", gotBody)
	}
}

func TestNotifySync_Non2xx_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	pub := &fakePublisher{}
	nc := platform.NewNotificationClient(platform.NotificationConfig{
		NATSPublisher: pub,
		NotifyURL:     srv.URL,
	})

	err := nc.NotifySync(context.Background(), platform.NotifyRequest{AccountID: 1, Type: "t", Title: "T"})
	if err == nil {
		t.Error("expected error on 500 response")
	}
}

// NotifyRequestBody mirrors platform.NotifyRequest for test assertion.
type NotifyRequestBody struct {
	AccountID int64  `json:"account_id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
}

// errFakeNATS is a sentinel error for tests.
type natsErr struct{}

func (natsErr) Error() string { return "fake nats error" }

var errFakeNATS = natsErr{}
