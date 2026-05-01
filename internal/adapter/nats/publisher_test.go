package nats_test

import (
	"context"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats-server/v2/test"

	adapternats "github.com/hanmahong5-arch/lurus-tally/internal/adapter/nats"
)

// startTestServer starts an in-process NATS server with JetStream enabled.
// The caller is responsible for calling Shutdown.
func startTestServer(t *testing.T) *natsserver.Server {
	t.Helper()
	opts := test.DefaultTestOptions
	opts.Port = -1 // random free port
	opts.JetStream = true
	opts.StoreDir = t.TempDir()
	srv, err := natsserver.NewServer(&opts)
	if err != nil {
		t.Fatalf("create nats server: %v", err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("nats server did not become ready")
	}
	return srv
}

func TestPublisher_NoOpFallback_PublishAlwaysReturnsNil(t *testing.T) {
	pub, err := adapternats.NewPublisher(adapternats.Config{NoOpFallback: true})
	if err != nil {
		t.Fatalf("NewPublisher: %v", err)
	}
	defer func() { _ = pub.Close() }()

	if err := pub.Publish(context.Background(), "PSI_EVENTS.test", map[string]any{"k": "v"}); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestPublisher_NoOpFallback_CloseIsIdempotent(t *testing.T) {
	pub, err := adapternats.NewPublisher(adapternats.Config{NoOpFallback: true})
	if err != nil {
		t.Fatalf("NewPublisher: %v", err)
	}
	if err := pub.Close(); err != nil {
		t.Errorf("first close: %v", err)
	}
	if err := pub.Close(); err != nil {
		t.Errorf("second close: %v", err)
	}
}

func TestPublisher_ConnectBadURL_ReturnsError(t *testing.T) {
	_, err := adapternats.NewPublisher(adapternats.Config{
		URL:     "nats://127.0.0.1:1", // nothing listening
		Timeout: 500 * time.Millisecond,
	})
	if err == nil {
		t.Error("expected error connecting to bad URL")
	}
}

func TestPublisher_Publish_HappyPath(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Shutdown()

	pub, err := adapternats.NewPublisher(adapternats.Config{
		URL:        srv.ClientURL(),
		StreamName: "PSI_EVENTS",
		Timeout:    3 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewPublisher: %v", err)
	}
	defer func() { _ = pub.Close() }()

	type evt struct {
		EventType string `json:"event_type"`
		TenantID  string `json:"tenant_id"`
	}
	payload := evt{EventType: "project.status_changed", TenantID: "tenant-1"}

	if err := pub.Publish(context.Background(), "PSI_EVENTS.project.status_changed", payload); err != nil {
		t.Errorf("Publish: %v", err)
	}
}

func TestPublisher_Publish_StreamAutoCreated(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Shutdown()

	// Use a non-default stream name to verify auto-creation.
	pub, err := adapternats.NewPublisher(adapternats.Config{
		URL:        srv.ClientURL(),
		StreamName: "MY_TEST_STREAM",
		Timeout:    3 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewPublisher: %v", err)
	}
	defer func() { _ = pub.Close() }()

	// Publish to the new stream — should not fail.
	if err := pub.Publish(context.Background(), "MY_TEST_STREAM.any.subject", "hello"); err != nil {
		t.Errorf("Publish after auto-create: %v", err)
	}
}

func TestPublisher_Publish_MarshalErrorPropagated(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Shutdown()

	pub, err := adapternats.NewPublisher(adapternats.Config{
		URL:        srv.ClientURL(),
		StreamName: "PSI_EVENTS",
		Timeout:    3 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewPublisher: %v", err)
	}
	defer func() { _ = pub.Close() }()

	// channel cannot be JSON-marshalled.
	if err := pub.Publish(context.Background(), "PSI_EVENTS.bad", make(chan int)); err == nil {
		t.Error("expected marshal error, got nil")
	}
}
