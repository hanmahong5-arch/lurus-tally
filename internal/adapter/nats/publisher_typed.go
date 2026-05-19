package nats

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// nowFunc and newUUIDFunc are package-level seams so tests can pin time/UUID.
// Production code never reassigns these; tests restore the original on cleanup.
var (
	nowFunc     = func() time.Time { return time.Now().UTC() }
	newUUIDFunc = func() string { return uuid.NewString() }
)

// buildEvent assembles a fully-populated Event envelope.
// tenantID must be non-empty; payload must JSON-marshal successfully.
//
// Returns:
//   - serialised JSON bytes ready for js.Publish
//   - the assembled Event (for logging / test introspection)
//   - error iff tenantID is empty or payload marshal fails
func buildEvent(eventType, tenantID string, payload any) ([]byte, Event, error) {
	if tenantID == "" {
		return nil, Event{}, fmt.Errorf("nats publisher: tenant_id is required for event %q (caller must pass a non-empty tenant scope)", eventType)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, Event{}, fmt.Errorf("nats publisher: marshal payload for %q (caller must supply a JSON-encodable struct): %w", eventType, err)
	}
	evt := Event{
		EventID:    newUUIDFunc(),
		EventType:  eventType,
		TenantID:   tenantID,
		OccurredAt: nowFunc(),
		Source:     Source,
		Payload:    raw,
	}
	data, err := json.Marshal(evt)
	if err != nil {
		// Should be unreachable: every Event field is JSON-safe by construction.
		return nil, evt, fmt.Errorf("nats publisher: marshal envelope for %q: %w", eventType, err)
	}
	return data, evt, nil
}

// publishEnvelope is the shared path for every typed PublishX method.
// It serialises the envelope and forwards the raw bytes to JetStream
// using the same timeout / error-counter as Publish().
func (p *jsPublisher) publishEnvelope(ctx context.Context, eventType, tenantID string, payload any) error {
	data, _, err := buildEvent(eventType, tenantID, payload)
	if err != nil {
		p.errCount.Add(1)
		return err
	}
	subject := SubjectFor(eventType)

	pubCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	if _, err := p.js.Publish(pubCtx, subject, data); err != nil {
		p.errCount.Add(1)
		p.log.Error("nats publish failed",
			"subject", subject,
			"event_type", eventType,
			"tenant_id", tenantID,
			"error", err.Error(),
			"total_errors", p.errCount.Load(),
		)
		return fmt.Errorf("nats publisher: publish %q to %s: %w", eventType, subject, err)
	}
	return nil
}

// PublishStockMovementRecorded — see EventTypeStockMovementRecorded.
func (p *jsPublisher) PublishStockMovementRecorded(ctx context.Context, tenantID string, payload StockMovementRecordedPayload) error {
	return p.publishEnvelope(ctx, EventTypeStockMovementRecorded, tenantID, payload)
}

// PublishStockSnapshotUpdated — see EventTypeStockSnapshotUpdated.
func (p *jsPublisher) PublishStockSnapshotUpdated(ctx context.Context, tenantID string, payload StockSnapshotUpdatedPayload) error {
	return p.publishEnvelope(ctx, EventTypeStockSnapshotUpdated, tenantID, payload)
}

// PublishBillCreated — see EventTypeBillCreated.
func (p *jsPublisher) PublishBillCreated(ctx context.Context, tenantID string, payload BillCreatedPayload) error {
	return p.publishEnvelope(ctx, EventTypeBillCreated, tenantID, payload)
}

// PublishBillApproved — see EventTypeBillApproved.
func (p *jsPublisher) PublishBillApproved(ctx context.Context, tenantID string, payload BillApprovedPayload) error {
	return p.publishEnvelope(ctx, EventTypeBillApproved, tenantID, payload)
}

// PublishBillRejected — see EventTypeBillRejected.
func (p *jsPublisher) PublishBillRejected(ctx context.Context, tenantID string, payload BillRejectedPayload) error {
	return p.publishEnvelope(ctx, EventTypeBillRejected, tenantID, payload)
}

// PublishLowStockAlert — see EventTypeAlertLowStock.
func (p *jsPublisher) PublishLowStockAlert(ctx context.Context, tenantID string, payload LowStockAlertPayload) error {
	return p.publishEnvelope(ctx, EventTypeAlertLowStock, tenantID, payload)
}

// PublishWebTelemetry — see SubjectWebTelemetry. Goes to PSI_TELEMETRY.web.*
// rather than PSI_EVENTS.*, so it bypasses publishEnvelope's subject helper.
func (p *jsPublisher) PublishWebTelemetry(ctx context.Context, tenantID, eventName string, payload any) error {
	if _, ok := AllowedWebTelemetryEvents[eventName]; !ok {
		return fmt.Errorf("nats publisher: web telemetry event %q not in allow-list", eventName)
	}
	data, _, err := buildEvent("web."+eventName, tenantID, payload)
	if err != nil {
		p.errCount.Add(1)
		return err
	}
	subject := SubjectWebTelemetry(eventName)
	pubCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	if _, err := p.js.Publish(pubCtx, subject, data); err != nil {
		p.errCount.Add(1)
		p.log.Error("nats publish failed (telemetry)",
			"subject", subject,
			"event_name", eventName,
			"tenant_id", tenantID,
			"error", err.Error(),
			"total_errors", p.errCount.Load(),
		)
		return fmt.Errorf("nats publisher: publish telemetry %q to %s: %w", eventName, subject, err)
	}
	return nil
}

// --- noopPublisher implementations (NoOpFallback path) ---

func (n *noopPublisher) PublishWebTelemetry(_ context.Context, _ string, _ string, _ any) error {
	return nil
}

func (n *noopPublisher) PublishStockMovementRecorded(_ context.Context, _ string, _ StockMovementRecordedPayload) error {
	return nil
}
func (n *noopPublisher) PublishStockSnapshotUpdated(_ context.Context, _ string, _ StockSnapshotUpdatedPayload) error {
	return nil
}
func (n *noopPublisher) PublishBillCreated(_ context.Context, _ string, _ BillCreatedPayload) error {
	return nil
}
func (n *noopPublisher) PublishBillApproved(_ context.Context, _ string, _ BillApprovedPayload) error {
	return nil
}
func (n *noopPublisher) PublishBillRejected(_ context.Context, _ string, _ BillRejectedPayload) error {
	return nil
}
func (n *noopPublisher) PublishLowStockAlert(_ context.Context, _ string, _ LowStockAlertPayload) error {
	return nil
}
