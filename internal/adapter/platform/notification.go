package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	adapternats "github.com/hanmahong5-arch/lurus-tally/internal/adapter/nats"
)

// PSIEvent is the canonical event envelope published to NATS stream PSI_EVENTS
// or forwarded via platform notification HTTP.
// Contract: doc/coord/contracts.md § PSI_EVENTS
type PSIEvent struct {
	// EventID is a UUID v4 assigned by the publisher.
	EventID string `json:"event_id"`
	// EventType is a dot-separated taxonomy, e.g. "project.status_changed".
	EventType string `json:"event_type"`
	// TenantID scopes the event to a specific tenant.
	TenantID string `json:"tenant_id"`
	// OccurredAt is the wall-clock time the business event happened.
	OccurredAt time.Time `json:"occurred_at"`
	// Source identifies the originating service ("tally").
	Source string `json:"source"`
	// Payload carries event-specific fields. Shape is defined per EventType in contracts.md.
	Payload map[string]any `json:"payload"`
}

// NotifyRequest is the body for POST /internal/v1/notify (sync HTTP path).
type NotifyRequest struct {
	// AccountID is the recipient's platform account ID.
	AccountID int64 `json:"account_id"`
	// Type is a short tag, e.g. "payment.received".
	Type string `json:"type"`
	// Title is the notification headline (shown in the bell icon).
	Title string `json:"title"`
	// Body is the notification detail text.
	Body string `json:"body,omitempty"`
	// Metadata is an optional map of extra data.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// NotificationClient sends Tally events two ways:
//   - Async (default): NATS PSI_EVENTS — notification svc consumes
//   - Sync HTTP: POST /internal/v1/notify — for immediate user-facing feedback
//
// Construct via NewNotificationClient. Always call Close when the process exits.
type NotificationClient struct {
	natsPub    adapternats.Publisher
	notifyURL  string
	apiKey     string
	httpClient *http.Client
	log        *slog.Logger
}

// NotificationConfig holds the wiring parameters for NotificationClient.
type NotificationConfig struct {
	// NATSPublisher is the NATS publisher used for async events. Required.
	NATSPublisher adapternats.Publisher
	// NotifyURL is the base URL for POST /internal/v1/notify.
	// Defaults to "http://notification.lurus-platform.svc:18900".
	NotifyURL string
	// APIKey is the bearer token for the notification service.
	APIKey string
	// HTTPTimeout overrides the default 5s per-request deadline.
	HTTPTimeout time.Duration
}

const defaultNotifyURL = "http://notification.lurus-platform.svc:18900"
const defaultHTTPTimeout = 5 * time.Second

// NewNotificationClient constructs a NotificationClient.
func NewNotificationClient(cfg NotificationConfig) *NotificationClient {
	notifyURL := cfg.NotifyURL
	if notifyURL == "" {
		notifyURL = defaultNotifyURL
	}
	timeout := cfg.HTTPTimeout
	if timeout <= 0 {
		timeout = defaultHTTPTimeout
	}
	return &NotificationClient{
		natsPub:    cfg.NATSPublisher,
		notifyURL:  notifyURL,
		apiKey:     cfg.APIKey,
		httpClient: &http.Client{Timeout: timeout},
		log:        slog.Default(),
	}
}

// subjectFor builds the NATS subject from the event type.
// e.g. EventType "project.status_changed" → subject "PSI_EVENTS.project.status_changed"
func subjectFor(evt PSIEvent) string {
	return "PSI_EVENTS." + evt.EventType
}

// NotifyAsync publishes evt to NATS JetStream. Errors are logged and returned
// but should not block the caller — fire-and-forget semantics are expected.
func (c *NotificationClient) NotifyAsync(ctx context.Context, evt PSIEvent) error {
	if err := c.natsPub.Publish(ctx, subjectFor(evt), evt); err != nil {
		c.log.Error("notification async publish failed",
			slog.String("event_type", evt.EventType),
			slog.String("tenant_id", evt.TenantID),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("notification async: %w", err)
	}
	return nil
}

// NotifySync calls POST /internal/v1/notify on the platform notification service
// and waits for an HTTP 2xx response. Use for user-visible notifications that
// need to appear immediately (e.g. payment confirmation).
func (c *NotificationClient) NotifySync(ctx context.Context, req NotifyRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("notification sync: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.notifyURL+"/internal/v1/notify", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("notification sync: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("notification sync: http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("notification sync: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// Close releases the NATS publisher connection.
func (c *NotificationClient) Close() error {
	if c.natsPub != nil {
		return c.natsPub.Close()
	}
	return nil
}
