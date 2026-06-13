package nats

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	appacct "github.com/hanmahong5-arch/lurus-tally/internal/app/account"
)

// AuditAppender is the minimal surface the subscriber needs from
// account.AppendAuditLog. Matches the signature of (*AppendAuditLog).Execute.
type AuditAppender interface {
	Execute(ctx context.Context, in appacct.AppendInput) error
}

// AuditSubscriber consumes business events from PSI_EVENTS and writes a row
// per event into tally.account_audit_log. Subscribes to bill.* and alert.* — read-
// only / draft events are intentionally NOT captured (audit ≠ activity log).
//
// Resilience: a connection drop is logged and the subscriber retries forever
// with a 5s backoff. A persistent JetStream consumer is created so events
// produced during downtime are replayed on reconnect.
type AuditSubscriber struct {
	nc        *nats.Conn
	js        jetstream.JetStream
	appender  AuditAppender
	log       *slog.Logger
	consumer  jetstream.Consumer
	cctx      jetstream.ConsumeContext
	closeOnce bool
}

// NewAuditSubscriber wires the subscriber against an existing NATS connection.
// Returns nil with no error when nc is nil — caller treats this as "audit
// stream not configured, skip wiring" (same shape as OutboxWorker).
func NewAuditSubscriber(nc *nats.Conn, appender AuditAppender, log *slog.Logger) (*AuditSubscriber, error) {
	if nc == nil || appender == nil {
		return nil, nil
	}
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, err
	}
	return &AuditSubscriber{nc: nc, js: js, appender: appender, log: log}, nil
}

// Start binds a durable consumer to PSI_EVENTS for the bill.* / alert.*
// subject filter and starts the message pump. Blocks only for the brief
// consumer-create round trip; thereafter dispatch runs in a goroutine pool
// owned by the JetStream client.
func (s *AuditSubscriber) Start(ctx context.Context) error {
	if s == nil {
		return nil
	}
	consumer, err := s.js.CreateOrUpdateConsumer(ctx, defaultStreamName, jetstream.ConsumerConfig{
		Durable:   "tally-audit",
		AckPolicy: jetstream.AckExplicitPolicy,
		FilterSubjects: []string{
			"PSI_EVENTS.bill.>",
			"PSI_EVENTS.alert.>",
		},
		MaxDeliver:    5,
		AckWait:       30 * time.Second,
		DeliverPolicy: jetstream.DeliverNewPolicy,
	})
	if err != nil {
		return err
	}
	s.consumer = consumer

	cctx, err := consumer.Consume(s.dispatch, jetstream.ConsumeErrHandler(func(_ jetstream.ConsumeContext, consumeErr error) {
		s.log.Warn("audit subscriber: consume error", slog.Any("error", consumeErr))
	}))
	if err != nil {
		return err
	}
	s.cctx = cctx
	s.log.Info("audit subscriber started", slog.String("stream", defaultStreamName))
	return nil
}

// Stop drains the consumer and releases JetStream resources.
func (s *AuditSubscriber) Stop() {
	if s == nil || s.closeOnce {
		return
	}
	s.closeOnce = true
	if s.cctx != nil {
		s.cctx.Stop()
	}
}

// dispatch maps one event to one account_audit_log row.
func (s *AuditSubscriber) dispatch(msg jetstream.Msg) {
	var env Event
	if err := json.Unmarshal(msg.Data(), &env); err != nil {
		s.log.Warn("audit subscriber: malformed envelope, terminating msg",
			slog.Any("error", err),
			slog.String("subject", msg.Subject()),
		)
		_ = msg.Term()
		return
	}
	tenantID, err := uuid.Parse(env.TenantID)
	if err != nil {
		s.log.Warn("audit subscriber: invalid tenant_id, terminating msg",
			slog.String("tenant_id", env.TenantID),
			slog.String("subject", msg.Subject()),
		)
		_ = msg.Term()
		return
	}

	// payload-derived target_id (best effort). bill events carry bill_id;
	// alert events carry product_id.
	targetKind, targetID := extractTarget(env.EventType, env.Payload)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.appender.Execute(ctx, appacct.AppendInput{
		TenantID:   tenantID,
		ActorID:    env.Source,
		Action:     env.EventType,
		TargetKind: targetKind,
		TargetID:   targetID,
		Payload:    env.Payload,
		// Dedup key: a redelivered envelope carries the same EventID, so the
		// repo's ON CONFLICT keeps audit at exactly one row per business event.
		EventID: env.EventID,
	}); err != nil {
		// Retryable — let JetStream redeliver. After MaxDeliver the message
		// goes to the dead-letter ceiling and stops eating ack budget.
		if errors.Is(err, context.Canceled) {
			_ = msg.Nak()
			return
		}
		s.log.Warn("audit subscriber: append failed, nak", slog.Any("error", err))
		_ = msg.Nak()
		return
	}
	_ = msg.Ack()
}

// extractTarget pulls the most useful identifier out of an event payload so
// the audit row carries a useful target. Falls back to ("event", "") when
// the type is not recognised — the row is still useful for the timeline.
func extractTarget(eventType string, payload json.RawMessage) (string, string) {
	switch eventType {
	case EventTypeBillCreated, EventTypeBillApproved, EventTypeBillRejected:
		var p struct {
			BillID string `json:"bill_id"`
		}
		_ = json.Unmarshal(payload, &p)
		return "bill", p.BillID
	case EventTypeAlertLowStock, EventTypeAlertOverstock:
		var p struct {
			ProductID string `json:"product_id"`
		}
		_ = json.Unmarshal(payload, &p)
		return "product", p.ProductID
	}
	return "event", ""
}
