// Package nats provides JetStream event publishing for PSI_EVENTS.
// The Publisher is safe for concurrent use from multiple goroutines.
// Failures never block the main request path — they are logged as ERROR
// and tracked via an internal counter.
package nats

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	defaultNATSURL    = "nats://nats.messaging.svc:4222"
	defaultStreamName = "PSI_EVENTS"
	defaultTimeout    = 3 * time.Second
)

// Config holds Publisher construction parameters.
type Config struct {
	// URL is the NATS server address. Defaults to "nats://nats.messaging.svc:4222" when empty.
	URL string
	// StreamName is the JetStream stream to ensure exists. Defaults to "PSI_EVENTS".
	StreamName string
	// Timeout is the per-operation deadline. Defaults to 3s.
	Timeout time.Duration
	// NoOpFallback when true causes all Publish calls to return nil immediately.
	// Used for local development or environments without a NATS server.
	NoOpFallback bool
}

// Publisher sends PSI_EVENTS to NATS JetStream.
// Obtain one via NewPublisher; always call Close when done.
type Publisher interface {
	// Publish serialises payload as JSON and publishes it to the given subject.
	// subject should be a dot-separated string, e.g. "PSI_EVENTS.project.status_changed".
	// Errors are returned but should not block the caller's main path.
	Publish(ctx context.Context, subject string, payload any) error
	// Close drains and closes the underlying NATS connection.
	Close() error
}

// jsPublisher is the live JetStream implementation.
type jsPublisher struct {
	nc         *nats.Conn
	js         jetstream.JetStream
	streamName string
	timeout    time.Duration
	log        *slog.Logger
	errCount   atomic.Int64
}

// noopPublisher satisfies Publisher when NoOpFallback is set.
type noopPublisher struct{}

func (n *noopPublisher) Publish(_ context.Context, _ string, _ any) error { return nil }
func (n *noopPublisher) Close() error                                     { return nil }

// NewPublisher creates a Publisher connected to NATS, ensuring the PSI_EVENTS
// stream exists. Returns a no-op publisher (not an error) when NoOpFallback is true.
func NewPublisher(cfg Config) (Publisher, error) {
	if cfg.NoOpFallback {
		return &noopPublisher{}, nil
	}

	url := cfg.URL
	if url == "" {
		url = defaultNATSURL
	}
	streamName := cfg.StreamName
	if streamName == "" {
		streamName = defaultStreamName
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	nc, err := nats.Connect(url,
		nats.Timeout(timeout),
		nats.MaxReconnects(5),
		nats.ReconnectWait(1*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("nats publisher: connect to %s: %w", url, err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("nats publisher: create jetstream context: %w", err)
	}

	// Ensure stream exists; idempotent if already present.
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     streamName,
		Subjects: []string{streamName + ".>"},
	})
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("nats publisher: ensure stream %s: %w", streamName, err)
	}

	return &jsPublisher{
		nc:         nc,
		js:         js,
		streamName: streamName,
		timeout:    timeout,
		log:        slog.Default(),
	}, nil
}

// Publish encodes payload as JSON and publishes it to the given subject.
func (p *jsPublisher) Publish(ctx context.Context, subject string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		p.errCount.Add(1)
		return fmt.Errorf("nats publisher: marshal payload for %s: %w", subject, err)
	}

	pubCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	if _, err = p.js.Publish(pubCtx, subject, data); err != nil {
		p.errCount.Add(1)
		p.log.Error("nats publish failed",
			slog.String("subject", subject),
			slog.String("error", err.Error()),
			slog.Int64("total_errors", p.errCount.Load()),
		)
		return fmt.Errorf("nats publisher: publish to %s: %w", subject, err)
	}
	return nil
}

// Close drains the NATS connection gracefully.
func (p *jsPublisher) Close() error {
	if p.nc != nil && !p.nc.IsClosed() {
		p.nc.Close()
	}
	return nil
}
