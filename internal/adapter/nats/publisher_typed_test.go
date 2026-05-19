package nats_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	adapternats "github.com/hanmahong5-arch/lurus-tally/internal/adapter/nats"
)

// fetchOne consumes a single message from streamName and returns its envelope
// + the raw bytes received on the wire. Fatals the test on timeout.
func fetchOne(t *testing.T, url, streamName, subject string) (adapternats.Event, []byte) {
	t.Helper()
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("test consumer connect: %v", err)
	}
	t.Cleanup(func() { nc.Close() })

	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("test consumer js: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := js.Stream(ctx, streamName)
	if err != nil {
		t.Fatalf("get stream %s: %v", streamName, err)
	}
	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		FilterSubject: subject,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		t.Fatalf("create consumer: %v", err)
	}

	msgs, err := cons.Fetch(1, jetstream.FetchMaxWait(3*time.Second))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	if msg, ok := <-msgs.Messages(); ok {
		_ = msg.Ack()
		var env adapternats.Event
		if err := json.Unmarshal(msg.Data(), &env); err != nil {
			t.Fatalf("decode envelope: %v (raw=%s)", err, string(msg.Data()))
		}
		return env, msg.Data()
	}
	if err := msgs.Error(); err != nil {
		t.Fatalf("fetch iter: %v", err)
	}
	t.Fatalf("no message received on subject %q", subject)
	return adapternats.Event{}, nil
}

// assertEnvelope checks the invariants every typed publish must satisfy.
func assertEnvelope(t *testing.T, env adapternats.Event, wantType, wantTenant string) {
	t.Helper()
	if env.EventID == "" {
		t.Errorf("event_id must be non-empty")
	}
	if env.EventType != wantType {
		t.Errorf("event_type = %q, want %q", env.EventType, wantType)
	}
	if env.TenantID != wantTenant {
		t.Errorf("tenant_id = %q, want %q", env.TenantID, wantTenant)
	}
	if env.Source != "tally" {
		t.Errorf("source = %q, want \"tally\"", env.Source)
	}
	if dt := time.Since(env.OccurredAt); dt < 0 || dt > 10*time.Second {
		t.Errorf("occurred_at = %v not within 10s of now", env.OccurredAt)
	}
}

func newTestPublisher(t *testing.T, url string) adapternats.Publisher {
	t.Helper()
	pub, err := adapternats.NewPublisher(adapternats.Config{
		URL:        url,
		StreamName: "PSI_EVENTS",
		Timeout:    3 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewPublisher: %v", err)
	}
	t.Cleanup(func() { _ = pub.Close() })
	return pub
}

func TestPublisher_PublishStockMovementRecorded(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Shutdown()
	pub := newTestPublisher(t, srv.ClientURL())

	in := adapternats.StockMovementRecordedPayload{
		ProductID:     "prod-1",
		WarehouseID:   "wh-A",
		Direction:     "in",
		QtyDelta:      "10.5",
		OnHandAfter:   "100.5",
		UnitCost:      "12.34",
		ReferenceType: "purchase_in",
		ReferenceID:   "bill-1",
	}
	if err := pub.PublishStockMovementRecorded(context.Background(), "tenant-1", in); err != nil {
		t.Fatalf("publish: %v", err)
	}

	env, _ := fetchOne(t, srv.ClientURL(), "PSI_EVENTS", adapternats.SubjectStockMovementRecorded)
	assertEnvelope(t, env, adapternats.EventTypeStockMovementRecorded, "tenant-1")

	var got adapternats.StockMovementRecordedPayload
	if err := json.Unmarshal(env.Payload, &got); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if got != in {
		t.Errorf("payload mismatch: got %+v, want %+v", got, in)
	}
}

func TestPublisher_PublishStockSnapshotUpdated(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Shutdown()
	pub := newTestPublisher(t, srv.ClientURL())

	in := adapternats.StockSnapshotUpdatedPayload{
		ProductID:    "prod-2",
		WarehouseID:  "wh-B",
		OnHandQty:    "50",
		AvailableQty: "45",
	}
	if err := pub.PublishStockSnapshotUpdated(context.Background(), "tenant-2", in); err != nil {
		t.Fatalf("publish: %v", err)
	}

	env, _ := fetchOne(t, srv.ClientURL(), "PSI_EVENTS", adapternats.SubjectStockSnapshotUpdated)
	assertEnvelope(t, env, adapternats.EventTypeStockSnapshotUpdated, "tenant-2")

	var got adapternats.StockSnapshotUpdatedPayload
	if err := json.Unmarshal(env.Payload, &got); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if got != in {
		t.Errorf("payload mismatch: got %+v, want %+v", got, in)
	}
}

func TestPublisher_PublishBillCreated(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Shutdown()
	pub := newTestPublisher(t, srv.ClientURL())

	in := adapternats.BillCreatedPayload{
		BillID:      "bill-3",
		BillNo:      "PI20260505-001",
		BillType:    "purchase_in",
		TotalAmount: "1234.56",
		TenantID:    "tenant-3",
	}
	if err := pub.PublishBillCreated(context.Background(), "tenant-3", in); err != nil {
		t.Fatalf("publish: %v", err)
	}

	env, _ := fetchOne(t, srv.ClientURL(), "PSI_EVENTS", adapternats.SubjectBillCreated)
	assertEnvelope(t, env, adapternats.EventTypeBillCreated, "tenant-3")

	var got adapternats.BillCreatedPayload
	if err := json.Unmarshal(env.Payload, &got); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if got != in {
		t.Errorf("payload mismatch: got %+v, want %+v", got, in)
	}
}

func TestPublisher_PublishBillApproved(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Shutdown()
	pub := newTestPublisher(t, srv.ClientURL())

	in := adapternats.BillApprovedPayload{
		BillID:      "bill-4",
		BillNo:      "SO20260505-002",
		BillType:    "sale_out",
		TotalAmount: "999.00",
	}
	if err := pub.PublishBillApproved(context.Background(), "tenant-4", in); err != nil {
		t.Fatalf("publish: %v", err)
	}

	env, _ := fetchOne(t, srv.ClientURL(), "PSI_EVENTS", adapternats.SubjectBillApproved)
	assertEnvelope(t, env, adapternats.EventTypeBillApproved, "tenant-4")

	var got adapternats.BillApprovedPayload
	if err := json.Unmarshal(env.Payload, &got); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if got != in {
		t.Errorf("payload mismatch: got %+v, want %+v", got, in)
	}
}

func TestPublisher_PublishBillRejected(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Shutdown()
	pub := newTestPublisher(t, srv.ClientURL())

	in := adapternats.BillRejectedPayload{
		BillID:          "bill-5",
		BillNo:          "PI20260505-003",
		RejectionReason: "missing supplier signature",
	}
	if err := pub.PublishBillRejected(context.Background(), "tenant-5", in); err != nil {
		t.Fatalf("publish: %v", err)
	}

	env, _ := fetchOne(t, srv.ClientURL(), "PSI_EVENTS", adapternats.SubjectBillRejected)
	assertEnvelope(t, env, adapternats.EventTypeBillRejected, "tenant-5")

	var got adapternats.BillRejectedPayload
	if err := json.Unmarshal(env.Payload, &got); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if got != in {
		t.Errorf("payload mismatch: got %+v, want %+v", got, in)
	}
}

func TestPublisher_PublishLowStockAlert(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Shutdown()
	pub := newTestPublisher(t, srv.ClientURL())

	in := adapternats.LowStockAlertPayload{
		ProductID:   "prod-6",
		WarehouseID: "wh-C",
		CurrentQty:  "3",
		Threshold:   "10",
	}
	if err := pub.PublishLowStockAlert(context.Background(), "tenant-6", in); err != nil {
		t.Fatalf("publish: %v", err)
	}

	env, _ := fetchOne(t, srv.ClientURL(), "PSI_EVENTS", adapternats.SubjectAlertLowStock)
	assertEnvelope(t, env, adapternats.EventTypeAlertLowStock, "tenant-6")

	var got adapternats.LowStockAlertPayload
	if err := json.Unmarshal(env.Payload, &got); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if got != in {
		t.Errorf("payload mismatch: got %+v, want %+v", got, in)
	}
}

func TestPublisher_Typed_RejectsEmptyTenantID(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Shutdown()
	pub := newTestPublisher(t, srv.ClientURL())

	err := pub.PublishBillCreated(context.Background(), "", adapternats.BillCreatedPayload{
		BillID: "x", BillNo: "x", BillType: "sale_out", TotalAmount: "0",
	})
	if err == nil {
		t.Fatal("expected error for empty tenant_id, got nil")
	}
}

func TestPublisher_Typed_NoOpFallback_ReturnsNil(t *testing.T) {
	pub, err := adapternats.NewPublisher(adapternats.Config{NoOpFallback: true})
	if err != nil {
		t.Fatalf("NewPublisher: %v", err)
	}
	defer func() { _ = pub.Close() }()

	if err := pub.PublishStockMovementRecorded(context.Background(), "tenant-x", adapternats.StockMovementRecordedPayload{}); err != nil {
		t.Errorf("noop stock_movement_recorded: %v", err)
	}
	if err := pub.PublishStockSnapshotUpdated(context.Background(), "tenant-x", adapternats.StockSnapshotUpdatedPayload{}); err != nil {
		t.Errorf("noop stock_snapshot_updated: %v", err)
	}
	if err := pub.PublishBillCreated(context.Background(), "tenant-x", adapternats.BillCreatedPayload{}); err != nil {
		t.Errorf("noop bill_created: %v", err)
	}
	if err := pub.PublishBillApproved(context.Background(), "tenant-x", adapternats.BillApprovedPayload{}); err != nil {
		t.Errorf("noop bill_approved: %v", err)
	}
	if err := pub.PublishBillRejected(context.Background(), "tenant-x", adapternats.BillRejectedPayload{}); err != nil {
		t.Errorf("noop bill_rejected: %v", err)
	}
	if err := pub.PublishLowStockAlert(context.Background(), "tenant-x", adapternats.LowStockAlertPayload{}); err != nil {
		t.Errorf("noop low_stock_alert: %v", err)
	}
}
