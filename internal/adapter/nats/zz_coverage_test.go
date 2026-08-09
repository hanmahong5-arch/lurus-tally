package nats

// Internal-package coverage tests for internal/adapter/nats. Kept separate
// from the existing *_test.go files (which live in package nats_test) so this
// file can reach unexported seams: nowFunc/newUUIDFunc, buildEvent, dispatch,
// extractTarget, processRow/drainOnce, and the unexported jsPublisher /
// AuditSubscriber struct fields.
//
// NewPublisher against a live JetStream connection is exercised (via an
// in-process nats-server, same pattern as publisher_test.go) where a real
// round trip is the only way to hit a branch (e.g. PublishWebTelemetry happy
// path, AuditSubscriber.Start/dispatch via a real Consume callback). Nothing
// here talks to an external NATS deployment.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats-server/v2/test"

	appacct "github.com/hanmahong5-arch/lurus-tally/internal/app/account"
)

// ---- shared test helpers ----------------------------------------------------

func zzTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// zzStartJetStreamServer starts an in-process NATS server with JetStream
// enabled. Caller must Shutdown.
func zzStartJetStreamServer(t *testing.T) *natsserver.Server {
	t.Helper()
	opts := test.DefaultTestOptions
	opts.Port = -1
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

// zzFakeAppender records every AppendInput passed to Execute so tests can
// assert the dedup/target-mapping invariants without a real DB.
type zzFakeAppender struct {
	mu    sync.Mutex
	calls []appacct.AppendInput
	err   error
}

func (f *zzFakeAppender) Execute(_ context.Context, in appacct.AppendInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, in)
	return f.err
}

func (f *zzFakeAppender) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *zzFakeAppender) firstCall() appacct.AppendInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[0]
}

// zzFakeMsg implements jetstream.Msg, recording Ack/Nak/Term calls so the
// ack-policy matrix in dispatch can be asserted without a live NATS.
type zzFakeMsg struct {
	mu      sync.Mutex
	data    []byte
	subject string

	ackCalled  int
	nakCalled  int
	termCalled int
}

func (m *zzFakeMsg) Metadata() (*jetstream.MsgMetadata, error) { return nil, nil }
func (m *zzFakeMsg) Data() []byte                              { return m.data }
func (m *zzFakeMsg) Headers() nats.Header                      { return nil }
func (m *zzFakeMsg) Subject() string                           { return m.subject }
func (m *zzFakeMsg) Reply() string                             { return "" }

func (m *zzFakeMsg) Ack() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ackCalled++
	return nil
}
func (m *zzFakeMsg) DoubleAck(_ context.Context) error { return nil }
func (m *zzFakeMsg) Nak() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nakCalled++
	return nil
}
func (m *zzFakeMsg) NakWithDelay(_ time.Duration) error { return nil }
func (m *zzFakeMsg) InProgress() error                  { return nil }
func (m *zzFakeMsg) Term() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.termCalled++
	return nil
}
func (m *zzFakeMsg) TermWithReason(_ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.termCalled++
	return nil
}

func (m *zzFakeMsg) counts() (ack, nak, term int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ackCalled, m.nakCalled, m.termCalled
}

// ---- buildEvent (publisher_typed.go) ---------------------------------------

func TestBuildEvent_EmptyTenantID_ReturnsError(t *testing.T) {
	data, evt, err := buildEvent(EventTypeBillCreated, "", BillCreatedPayload{BillID: "b1"})
	if err == nil {
		t.Fatal("expected error for empty tenant_id, got nil")
	}
	wantMsg := `nats publisher: tenant_id is required for event "bill.created" (caller must pass a non-empty tenant scope)`
	if err.Error() != wantMsg {
		t.Errorf("error = %q, want %q", err.Error(), wantMsg)
	}
	if data != nil {
		t.Errorf("expected nil data on error, got %q", data)
	}
	if evt.EventID != "" || evt.EventType != "" || evt.TenantID != "" || evt.Source != "" || evt.Payload != nil || !evt.OccurredAt.IsZero() {
		t.Errorf("expected zero Event on error, got %+v", evt)
	}
}

func TestBuildEvent_MarshalPayloadError(t *testing.T) {
	// A channel cannot be JSON-marshalled.
	_, _, err := buildEvent(EventTypeBillCreated, "tenant-1", make(chan int))
	if err == nil {
		t.Fatal("expected marshal error, got nil")
	}
}

func TestBuildEvent_HappyPath_PinnedTimeAndUUID(t *testing.T) {
	origNow, origUUID := nowFunc, newUUIDFunc
	t.Cleanup(func() { nowFunc, newUUIDFunc = origNow, origUUID })

	pinnedTime := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	pinnedUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	nowFunc = func() time.Time { return pinnedTime }
	newUUIDFunc = func() string { return pinnedUUID }

	payload := BillCreatedPayload{
		BillID:      "bill-1",
		BillNo:      "PI-1",
		BillType:    "purchase_in",
		TotalAmount: "10.00",
		TenantID:    "tenant-9",
	}

	data, evt, err := buildEvent(EventTypeBillCreated, "tenant-9", payload)
	if err != nil {
		t.Fatalf("buildEvent: %v", err)
	}

	// Hand-computed expectation: the Event envelope built from the pinned
	// seams, independent of buildEvent's own marshaling.
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload for expectation: %v", err)
	}
	wantEvt := Event{
		EventID:    pinnedUUID,
		EventType:  EventTypeBillCreated,
		TenantID:   "tenant-9",
		OccurredAt: pinnedTime,
		Source:     Source,
		Payload:    rawPayload,
	}
	if evt.EventID != wantEvt.EventID ||
		evt.EventType != wantEvt.EventType ||
		evt.TenantID != wantEvt.TenantID ||
		!evt.OccurredAt.Equal(wantEvt.OccurredAt) ||
		evt.Source != wantEvt.Source ||
		string(evt.Payload) != string(wantEvt.Payload) {
		t.Errorf("evt = %+v, want %+v", evt, wantEvt)
	}

	wantData, err := json.Marshal(wantEvt)
	if err != nil {
		t.Fatalf("marshal expected envelope: %v", err)
	}
	if string(data) != string(wantData) {
		t.Errorf("data = %s, want %s", data, wantData)
	}
}

// ---- SubjectFor / SubjectWebTelemetry / AllowedWebTelemetryEvents ----------

func TestSubjectFor_Table(t *testing.T) {
	cases := []struct {
		eventType string
		want      string
	}{
		{EventTypeStockMovementRecorded, "PSI_EVENTS.stock.movement_recorded"},
		{EventTypeBillCreated, "PSI_EVENTS.bill.created"},
		{EventTypeAlertLowStock, "PSI_EVENTS.alert.low_stock"},
		{"totally.unknown.type", "PSI_EVENTS.totally.unknown.type"},
	}
	for _, tc := range cases {
		if got := SubjectFor(tc.eventType); got != tc.want {
			t.Errorf("SubjectFor(%q) = %q, want %q", tc.eventType, got, tc.want)
		}
	}
}

func TestSubjectWebTelemetry(t *testing.T) {
	if got, want := SubjectWebTelemetry("wad_increment"), "PSI_TELEMETRY.web.wad_increment"; got != want {
		t.Errorf("SubjectWebTelemetry = %q, want %q", got, want)
	}
}

func TestAllowedWebTelemetryEvents_ContainsExpected(t *testing.T) {
	want := []string{
		"draft_restore", "undo_used", "palette_invocation", "ai_drawer_open",
		"plan_accept_rate", "onboarding_first_po_exported", "cmd_z_used", "wad_increment",
	}
	for _, name := range want {
		if _, ok := AllowedWebTelemetryEvents[name]; !ok {
			t.Errorf("AllowedWebTelemetryEvents missing %q", name)
		}
	}
	if _, ok := AllowedWebTelemetryEvents["not_a_real_event"]; ok {
		t.Error("AllowedWebTelemetryEvents unexpectedly contains an unlisted event")
	}
}

// ---- extractTarget (audit_subscriber.go) -----------------------------------

func TestExtractTarget_Table(t *testing.T) {
	cases := []struct {
		name       string
		eventType  string
		payload    string
		wantKind   string
		wantTarget string
	}{
		{"bill_created", EventTypeBillCreated, `{"bill_id":"bill-1"}`, "bill", "bill-1"},
		{"bill_approved", EventTypeBillApproved, `{"bill_id":"bill-2"}`, "bill", "bill-2"},
		{"bill_rejected", EventTypeBillRejected, `{"bill_id":"bill-3"}`, "bill", "bill-3"},
		{"alert_low_stock", EventTypeAlertLowStock, `{"product_id":"prod-1"}`, "product", "prod-1"},
		{"alert_overstock", EventTypeAlertOverstock, `{"product_id":"prod-2"}`, "product", "prod-2"},
		{"unknown_type", "stock.movement_recorded", `{"product_id":"prod-3"}`, "event", ""},
		{"unparseable_payload_falls_back_to_zero_value", EventTypeBillCreated, `not json`, "bill", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kind, id := extractTarget(tc.eventType, json.RawMessage(tc.payload))
			if kind != tc.wantKind || id != tc.wantTarget {
				t.Errorf("extractTarget(%q, %s) = (%q, %q), want (%q, %q)",
					tc.eventType, tc.payload, kind, id, tc.wantKind, tc.wantTarget)
			}
		})
	}
}

// ---- AuditSubscriber.dispatch (audit_subscriber.go) ------------------------

const zzValidTenantID = "11111111-1111-1111-1111-111111111111"

func TestDispatch_MalformedJSON_TermsNoAppenderCall(t *testing.T) {
	appender := &zzFakeAppender{}
	sub := &AuditSubscriber{appender: appender, log: zzTestLogger()}
	msg := &zzFakeMsg{data: []byte("{not valid json"), subject: "PSI_EVENTS.bill.created"}

	sub.dispatch(msg)

	ack, nak, term := msg.counts()
	if term != 1 || ack != 0 || nak != 0 {
		t.Errorf("counts (ack=%d nak=%d term=%d), want (ack=0 nak=0 term=1)", ack, nak, term)
	}
	if n := appender.callCount(); n != 0 {
		t.Errorf("appender called %d times, want 0", n)
	}
}

func TestDispatch_InvalidTenantUUID_Terms(t *testing.T) {
	appender := &zzFakeAppender{}
	sub := &AuditSubscriber{appender: appender, log: zzTestLogger()}
	env := Event{
		EventID:   "evt-1",
		EventType: EventTypeBillCreated,
		TenantID:  "not-a-uuid",
		Payload:   json.RawMessage(`{"bill_id":"b1"}`),
	}
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal env: %v", err)
	}
	msg := &zzFakeMsg{data: data, subject: "PSI_EVENTS.bill.created"}

	sub.dispatch(msg)

	ack, nak, term := msg.counts()
	if term != 1 || ack != 0 || nak != 0 {
		t.Errorf("counts (ack=%d nak=%d term=%d), want (ack=0 nak=0 term=1)", ack, nak, term)
	}
	if n := appender.callCount(); n != 0 {
		t.Errorf("appender called %d times, want 0", n)
	}
}

func TestDispatch_AppenderSuccess_Acks_And_PassesEventIDAndTarget(t *testing.T) {
	appender := &zzFakeAppender{}
	sub := &AuditSubscriber{appender: appender, log: zzTestLogger()}
	env := Event{
		EventID:   "evt-dedup-key-1",
		EventType: EventTypeBillCreated,
		TenantID:  zzValidTenantID,
		Source:    Source,
		Payload:   json.RawMessage(`{"bill_id":"bill-42"}`),
	}
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal env: %v", err)
	}
	msg := &zzFakeMsg{data: data, subject: "PSI_EVENTS.bill.created"}

	sub.dispatch(msg)

	ack, nak, term := msg.counts()
	if ack != 1 || nak != 0 || term != 0 {
		t.Errorf("counts (ack=%d nak=%d term=%d), want (ack=1 nak=0 term=0)", ack, nak, term)
	}
	if n := appender.callCount(); n != 1 {
		t.Fatalf("appender called %d times, want 1", n)
	}
	in := appender.firstCall()
	wantTenant, _ := uuid.Parse(zzValidTenantID)
	if in.TenantID != wantTenant {
		t.Errorf("TenantID = %v, want %v", in.TenantID, wantTenant)
	}
	// Business invariant: EventID flows through so redelivery dedups to one row.
	if in.EventID != env.EventID {
		t.Errorf("EventID = %q, want %q (dedup key must match envelope)", in.EventID, env.EventID)
	}
	if in.Action != EventTypeBillCreated {
		t.Errorf("Action = %q, want %q", in.Action, EventTypeBillCreated)
	}
	if in.ActorID != Source {
		t.Errorf("ActorID = %q, want %q", in.ActorID, Source)
	}
	if in.TargetKind != "bill" || in.TargetID != "bill-42" {
		t.Errorf("target = (%q, %q), want (\"bill\", \"bill-42\")", in.TargetKind, in.TargetID)
	}
}

func TestDispatch_AppenderContextCanceled_Naks(t *testing.T) {
	appender := &zzFakeAppender{err: context.Canceled}
	sub := &AuditSubscriber{appender: appender, log: zzTestLogger()}
	env := Event{
		EventID:   "evt-2",
		EventType: EventTypeAlertLowStock,
		TenantID:  zzValidTenantID,
		Payload:   json.RawMessage(`{"product_id":"p1"}`),
	}
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal env: %v", err)
	}
	msg := &zzFakeMsg{data: data, subject: "PSI_EVENTS.alert.low_stock"}

	sub.dispatch(msg)

	ack, nak, term := msg.counts()
	if nak != 1 || ack != 0 || term != 0 {
		t.Errorf("counts (ack=%d nak=%d term=%d), want (ack=0 nak=1 term=0)", ack, nak, term)
	}
}

func TestDispatch_AppenderOtherError_Naks(t *testing.T) {
	appender := &zzFakeAppender{err: errors.New("db: connection reset")}
	sub := &AuditSubscriber{appender: appender, log: zzTestLogger()}
	env := Event{
		EventID:   "evt-3",
		EventType: EventTypeAlertOverstock,
		TenantID:  zzValidTenantID,
		Payload:   json.RawMessage(`{"product_id":"p2"}`),
	}
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal env: %v", err)
	}
	msg := &zzFakeMsg{data: data, subject: "PSI_EVENTS.alert.overstock"}

	sub.dispatch(msg)

	ack, nak, term := msg.counts()
	if nak != 1 || ack != 0 || term != 0 {
		t.Errorf("counts (ack=%d nak=%d term=%d), want (ack=0 nak=1 term=0)", ack, nak, term)
	}
}

// ---- NewAuditSubscriber / Start / Stop (audit_subscriber.go) ---------------

func TestNewAuditSubscriber_NilConn_ReturnsNilNil(t *testing.T) {
	sub, err := NewAuditSubscriber(nil, &zzFakeAppender{}, zzTestLogger())
	if sub != nil || err != nil {
		t.Errorf("got (%v, %v), want (nil, nil)", sub, err)
	}
}

func TestNewAuditSubscriber_NilAppender_ReturnsNilNil(t *testing.T) {
	// nc must be non-nil to reach the appender-nil branch; the early nil
	// checks short-circuit before nc is ever dereferenced, so a zero-value
	// *nats.Conn is safe here (never connected, never used).
	nc := &nats.Conn{}
	sub, err := NewAuditSubscriber(nc, nil, zzTestLogger())
	if sub != nil || err != nil {
		t.Errorf("got (%v, %v), want (nil, nil)", sub, err)
	}
}

func TestAuditSubscriber_NilReceiver_StartStopSafe(t *testing.T) {
	var sub *AuditSubscriber
	if err := sub.Start(context.Background()); err != nil {
		t.Errorf("Start on nil receiver: %v, want nil", err)
	}
	// Must not panic.
	sub.Stop()
}

func TestAuditSubscriber_Stop_IdempotentWithoutConsumer(t *testing.T) {
	sub := &AuditSubscriber{log: zzTestLogger()}
	sub.Stop()
	if !sub.closeOnce {
		t.Error("closeOnce should be true after Stop")
	}
	// Second call must be a no-op, not panic.
	sub.Stop()
}

// TestAuditSubscriber_LiveIntegration_StartDispatchStop drives NewAuditSubscriber,
// Start and the real Consume-driven dispatch path against an in-process
// JetStream server — the only way to exercise CreateOrUpdateConsumer/Consume
// without a live NATS deployment.
func TestAuditSubscriber_LiveIntegration_StartDispatchStop(t *testing.T) {
	srv := zzStartJetStreamServer(t)
	defer srv.Shutdown()

	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream.New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     defaultStreamName,
		Subjects: []string{defaultStreamName + ".>"},
	}); err != nil {
		t.Fatalf("create stream: %v", err)
	}

	appender := &zzFakeAppender{}
	sub, err := NewAuditSubscriber(nc, appender, zzTestLogger())
	if err != nil {
		t.Fatalf("NewAuditSubscriber: %v", err)
	}
	if sub == nil {
		t.Fatal("NewAuditSubscriber returned nil subscriber for valid inputs")
	}
	if err := sub.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sub.Stop()

	env := Event{
		EventID:   "live-evt-1",
		EventType: EventTypeBillCreated,
		TenantID:  zzValidTenantID,
		Source:    Source,
		Payload:   json.RawMessage(`{"bill_id":"bill-live-1"}`),
	}
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal env: %v", err)
	}
	if _, err := js.Publish(ctx, SubjectBillCreated, data); err != nil {
		t.Fatalf("publish: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if appender.callCount() > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n := appender.callCount(); n != 1 {
		t.Fatalf("appender called %d times within deadline, want 1", n)
	}
	in := appender.firstCall()
	if in.EventID != env.EventID {
		t.Errorf("EventID = %q, want %q", in.EventID, env.EventID)
	}
	if in.TargetKind != "bill" || in.TargetID != "bill-live-1" {
		t.Errorf("target = (%q, %q), want (\"bill\", \"bill-live-1\")", in.TargetKind, in.TargetID)
	}

	sub.Stop()
	sub.Stop() // idempotent
}

// ---- OutboxWorker.processRow / drainOnce (outbox_worker.go) ----------------

type zzFakeOutboxStore struct {
	mu   sync.Mutex
	rows []OutboxRow

	drainErr        error
	markErr         error
	recordErr       error
	pendingStatsErr error

	markCalls   []uuid.UUID
	recordCalls []uuid.UUID
}

func (s *zzFakeOutboxStore) Drain(_ context.Context, limit int) ([]OutboxRow, error) {
	if s.drainErr != nil {
		return nil, s.drainErr
	}
	if limit < len(s.rows) {
		return s.rows[:limit], nil
	}
	return s.rows, nil
}

func (s *zzFakeOutboxStore) MarkPublished(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.markCalls = append(s.markCalls, id)
	return s.markErr
}

func (s *zzFakeOutboxStore) RecordAttemptError(_ context.Context, id uuid.UUID, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordCalls = append(s.recordCalls, id)
	return s.recordErr
}

func (s *zzFakeOutboxStore) PendingStats(_ context.Context) (OutboxPendingStats, error) {
	if s.pendingStatsErr != nil {
		return OutboxPendingStats{}, s.pendingStatsErr
	}
	return OutboxPendingStats{PendingCount: int64(len(s.rows))}, nil
}

// zzFakePublisher is a controllable Publisher stub satisfying the full
// interface (only Publish is exercised by OutboxWorker).
type zzFakePublisher struct {
	mu         sync.Mutex
	publishErr error
	calls      []string
}

func (f *zzFakePublisher) Publish(_ context.Context, subject string, _ any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, subject)
	return f.publishErr
}
func (f *zzFakePublisher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}
func (f *zzFakePublisher) PublishStockMovementRecorded(context.Context, string, StockMovementRecordedPayload) error {
	return nil
}
func (f *zzFakePublisher) PublishStockSnapshotUpdated(context.Context, string, StockSnapshotUpdatedPayload) error {
	return nil
}
func (f *zzFakePublisher) PublishBillCreated(context.Context, string, BillCreatedPayload) error {
	return nil
}
func (f *zzFakePublisher) PublishBillApproved(context.Context, string, BillApprovedPayload) error {
	return nil
}
func (f *zzFakePublisher) PublishBillRejected(context.Context, string, BillRejectedPayload) error {
	return nil
}
func (f *zzFakePublisher) PublishLowStockAlert(context.Context, string, LowStockAlertPayload) error {
	return nil
}
func (f *zzFakePublisher) PublishWebTelemetry(context.Context, string, string, any) error {
	return nil
}
func (f *zzFakePublisher) Close() error { return nil }

func zzMakeRow() OutboxRow {
	return OutboxRow{ID: uuid.New(), Subject: "PSI_EVENTS.bill.created", Payload: json.RawMessage(`{}`)}
}

func TestOutboxWorker_ProcessRow_PublishError_RecordsAttempt_NoMark(t *testing.T) {
	row := zzMakeRow()
	store := &zzFakeOutboxStore{rows: []OutboxRow{row}}
	pub := &zzFakePublisher{publishErr: errors.New("nats: no responders")}
	w := NewOutboxWorker(store, pub, zzTestLogger())

	w.processRow(context.Background(), row)

	if len(store.recordCalls) != 1 || store.recordCalls[0] != row.ID {
		t.Errorf("RecordAttemptError calls = %v, want exactly [%v]", store.recordCalls, row.ID)
	}
	if len(store.markCalls) != 0 {
		t.Errorf("MarkPublished must NOT be called on publish error, got calls = %v", store.markCalls)
	}
}

func TestOutboxWorker_ProcessRow_PublishOK_MarkPublishedError_NoPanic(t *testing.T) {
	row := zzMakeRow()
	store := &zzFakeOutboxStore{rows: []OutboxRow{row}, markErr: errors.New("db: write conflict")}
	pub := &zzFakePublisher{}
	w := NewOutboxWorker(store, pub, zzTestLogger())

	// Must not panic even though MarkPublished fails after a successful publish.
	w.processRow(context.Background(), row)

	if pub.callCount() != 1 {
		t.Errorf("Publish called %d times, want 1", pub.callCount())
	}
	if len(store.markCalls) != 1 || store.markCalls[0] != row.ID {
		t.Errorf("MarkPublished calls = %v, want exactly [%v]", store.markCalls, row.ID)
	}
	if len(store.recordCalls) != 0 {
		t.Errorf("RecordAttemptError must not be called when Publish succeeded, got %v", store.recordCalls)
	}
}

func TestOutboxWorker_DrainOnce_ProcessesAllRowsFromDrain(t *testing.T) {
	if MaxOutboxAttempts != 10 {
		t.Fatalf("MaxOutboxAttempts = %d, want 10 (the documented drain gate)", MaxOutboxAttempts)
	}
	rows := make([]OutboxRow, 0, 12)
	for i := 0; i < 12; i++ {
		rows = append(rows, zzMakeRow())
	}
	store := &zzFakeOutboxStore{rows: rows}
	pub := &zzFakePublisher{}
	w := NewOutboxWorker(store, pub, zzTestLogger())

	w.drainOnce(context.Background())

	if got, want := pub.callCount(), len(rows); got != want {
		t.Errorf("drainOnce published %d rows, want %d (every row Drain returned)", got, want)
	}
	if got, want := len(store.markCalls), len(rows); got != want {
		t.Errorf("drainOnce marked %d rows published, want %d", got, want)
	}
}

func TestOutboxWorker_DrainOnce_DrainError_NoPublishes(t *testing.T) {
	store := &zzFakeOutboxStore{drainErr: errors.New("db: connection refused")}
	pub := &zzFakePublisher{}
	w := NewOutboxWorker(store, pub, zzTestLogger())

	// Must not panic; must not publish anything when Drain itself fails.
	w.drainOnce(context.Background())

	if pub.callCount() != 0 {
		t.Errorf("Publish called %d times after Drain error, want 0", pub.callCount())
	}
}

func TestOutboxWorker_DrainOnce_PendingStatsError_StillDrains(t *testing.T) {
	row := zzMakeRow()
	store := &zzFakeOutboxStore{rows: []OutboxRow{row}, pendingStatsErr: errors.New("db: timeout")}
	pub := &zzFakePublisher{}
	w := NewOutboxWorker(store, pub, zzTestLogger())

	// PendingStats failing must not block the drain itself.
	w.drainOnce(context.Background())

	if pub.callCount() != 1 {
		t.Errorf("Publish called %d times, want 1 even when PendingStats errors", pub.callCount())
	}
}

func TestNewOutboxWorker_NilLog_DefaultsToSlogDefault(t *testing.T) {
	w := NewOutboxWorker(&zzFakeOutboxStore{}, &zzFakePublisher{}, nil)
	if w.log == nil {
		t.Error("NewOutboxWorker with nil log should default to a non-nil logger")
	}
}

// ---- jsPublisher.PublishWebTelemetry (publisher_typed.go) ------------------

func TestJsPublisher_PublishWebTelemetry_DisallowedEvent_ErrorBeforePublish(t *testing.T) {
	// Zero-value jsPublisher: js is nil, so any attempt to actually publish
	// would panic. Reaching the allow-list check first (and returning before
	// ever touching p.js) is exactly the invariant under test.
	p := &jsPublisher{log: zzTestLogger()}
	err := p.PublishWebTelemetry(context.Background(), "tenant-1", "not_in_allow_list", map[string]any{"k": "v"})
	if err == nil {
		t.Fatal("expected error for disallowed telemetry event name, got nil")
	}
}

func TestJsPublisher_PublishWebTelemetry_EmptyTenantID_ErrorBeforePublish(t *testing.T) {
	p := &jsPublisher{log: zzTestLogger()}
	err := p.PublishWebTelemetry(context.Background(), "", "wad_increment", map[string]any{"k": "v"})
	if err == nil {
		t.Fatal("expected error for empty tenant_id, got nil")
	}
}

func TestJsPublisher_PublishWebTelemetry_MarshalError_ErrorBeforePublish(t *testing.T) {
	p := &jsPublisher{log: zzTestLogger()}
	// channel cannot be JSON-marshalled; eventName is allow-listed so this
	// exercises buildEvent's marshal-error branch specifically.
	err := p.PublishWebTelemetry(context.Background(), "tenant-1", "wad_increment", make(chan int))
	if err == nil {
		t.Fatal("expected marshal error, got nil")
	}
}

// TestJsPublisher_Publish_NoMatchingStream_ReturnsError exercises the js.Publish
// error branch in jsPublisher.Publish: the stream only registers PSI_EVENTS.>,
// so publishing to an unrelated subject has no stream to accept it.
func TestJsPublisher_Publish_NoMatchingStream_ReturnsError(t *testing.T) {
	srv := zzStartJetStreamServer(t)
	defer srv.Shutdown()

	pub, err := NewPublisher(Config{
		URL:        srv.ClientURL(),
		StreamName: "PSI_EVENTS",
		Timeout:    3 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewPublisher: %v", err)
	}
	defer func() { _ = pub.Close() }()

	if err := pub.Publish(context.Background(), "NO_SUCH_STREAM.subject", map[string]any{"k": "v"}); err == nil {
		t.Error("expected error publishing to a subject with no matching stream, got nil")
	}
}

// TestJsPublisher_PublishBillCreated_NoMatchingStream_ReturnsError exercises
// publishEnvelope's js.Publish error branch: the configured stream only
// accepts OTHER_STREAM.>, so the typed PSI_EVENTS.bill.created subject has
// nowhere to land.
func TestJsPublisher_PublishBillCreated_NoMatchingStream_ReturnsError(t *testing.T) {
	srv := zzStartJetStreamServer(t)
	defer srv.Shutdown()

	pub, err := NewPublisher(Config{
		URL:        srv.ClientURL(),
		StreamName: "OTHER_STREAM",
		Timeout:    3 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewPublisher: %v", err)
	}
	defer func() { _ = pub.Close() }()

	err = pub.PublishBillCreated(context.Background(), "tenant-1", BillCreatedPayload{BillID: "b1"})
	if err == nil {
		t.Error("expected error publishing typed event to a mismatched stream, got nil")
	}
}

// TestJsPublisher_PublishWebTelemetry_NoMatchingStream_ReturnsError exercises
// PublishWebTelemetry's js.Publish error branch the same way.
func TestJsPublisher_PublishWebTelemetry_NoMatchingStream_ReturnsError(t *testing.T) {
	srv := zzStartJetStreamServer(t)
	defer srv.Shutdown()

	pub, err := NewPublisher(Config{
		URL:        srv.ClientURL(),
		StreamName: "PSI_EVENTS", // does not cover PSI_TELEMETRY.>
		Timeout:    3 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewPublisher: %v", err)
	}
	defer func() { _ = pub.Close() }()

	err = pub.PublishWebTelemetry(context.Background(), "tenant-1", "wad_increment", map[string]any{"k": "v"})
	if err == nil {
		t.Error("expected error publishing telemetry to a mismatched stream, got nil")
	}
}

// TestNoopPublisher_PublishWebTelemetry_ReturnsNil covers the NoOpFallback
// implementation of PublishWebTelemetry, which is otherwise never exercised.
func TestNoopPublisher_PublishWebTelemetry_ReturnsNil(t *testing.T) {
	pub, err := NewPublisher(Config{NoOpFallback: true})
	if err != nil {
		t.Fatalf("NewPublisher: %v", err)
	}
	defer func() { _ = pub.Close() }()

	if err := pub.PublishWebTelemetry(context.Background(), "tenant-x", "wad_increment", map[string]any{"k": "v"}); err != nil {
		t.Errorf("noop PublishWebTelemetry: %v, want nil", err)
	}
}

// TestNewPublisher_TimeoutDefault_AndStreamNameDefault_Applied hits the
// timeout<=0 and streamName=="" default-assignment branches. URL is set
// explicitly (to a fast-failing address) so the test doesn't pay the ~14s
// DNS timeout of the hardcoded default NATS URL.
func TestNewPublisher_TimeoutDefault_AndStreamNameDefault_Applied(t *testing.T) {
	_, err := NewPublisher(Config{
		URL: "nats://127.0.0.1:1", // nothing listening; connect fails fast
		// StreamName and Timeout intentionally left zero-valued.
	})
	if err == nil {
		t.Error("expected connect error against a closed port, got nil")
	}
}

// TestAuditSubscriber_Start_StreamMissing_ReturnsError exercises the
// CreateOrUpdateConsumer error branch in Start: PSI_EVENTS was never created
// on this server, so binding a durable consumer to it must fail.
// TestNewPublisher_EnsureStreamConflict_ReturnsError exercises the
// CreateOrUpdateStream error branch: a different stream already owns the
// exact subject NewPublisher is about to claim, so JetStream rejects it.
func TestNewPublisher_EnsureStreamConflict_ReturnsError(t *testing.T) {
	srv := zzStartJetStreamServer(t)
	defer srv.Shutdown()

	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream.New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     "MANUAL_OWNER",
		Subjects: []string{"DUPTEST.>"},
	}); err != nil {
		t.Fatalf("create manual stream: %v", err)
	}

	_, err = NewPublisher(Config{
		URL:        srv.ClientURL(),
		StreamName: "DUPTEST", // subjects "DUPTEST.>" already owned by MANUAL_OWNER
		Timeout:    3 * time.Second,
	})
	if err == nil {
		t.Error("expected ensure-stream conflict error, got nil")
	}
}

func TestAuditSubscriber_Start_StreamMissing_ReturnsError(t *testing.T) {
	srv := zzStartJetStreamServer(t)
	defer srv.Shutdown()

	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer nc.Close()

	appender := &zzFakeAppender{}
	sub, err := NewAuditSubscriber(nc, appender, zzTestLogger())
	if err != nil {
		t.Fatalf("NewAuditSubscriber: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sub.Start(ctx); err == nil {
		t.Error("expected Start to fail when the PSI_EVENTS stream does not exist, got nil")
	}
}

func TestOutboxWorker_ProcessRow_PublishError_RecordAttemptErrorAlsoFails_NoPanic(t *testing.T) {
	row := zzMakeRow()
	store := &zzFakeOutboxStore{
		rows:      []OutboxRow{row},
		recordErr: errors.New("db: write conflict"),
	}
	pub := &zzFakePublisher{publishErr: errors.New("nats: no responders")}
	w := NewOutboxWorker(store, pub, zzTestLogger())

	// Must not panic even when the fallback error-recording write itself fails.
	w.processRow(context.Background(), row)

	if len(store.recordCalls) != 1 || store.recordCalls[0] != row.ID {
		t.Errorf("RecordAttemptError calls = %v, want exactly [%v]", store.recordCalls, row.ID)
	}
	if len(store.markCalls) != 0 {
		t.Errorf("MarkPublished must NOT be called on publish error, got calls = %v", store.markCalls)
	}
}

func TestJsPublisher_PublishWebTelemetry_HappyPath(t *testing.T) {
	srv := zzStartJetStreamServer(t)
	defer srv.Shutdown()

	pub, err := NewPublisher(Config{
		URL:        srv.ClientURL(),
		StreamName: "PSI_TELEMETRY",
		Timeout:    3 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewPublisher: %v", err)
	}
	defer func() { _ = pub.Close() }()

	type telemetryPayload struct {
		Count int `json:"count"`
	}
	if err := pub.PublishWebTelemetry(context.Background(), "tenant-7", "wad_increment", telemetryPayload{Count: 3}); err != nil {
		t.Fatalf("PublishWebTelemetry: %v", err)
	}

	// Fetch the published message back and assert the envelope invariants.
	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("consumer connect: %v", err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("consumer jetstream: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := js.Stream(ctx, "PSI_TELEMETRY")
	if err != nil {
		t.Fatalf("get stream: %v", err)
	}
	wantSubject := SubjectWebTelemetry("wad_increment")
	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		FilterSubject: wantSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		t.Fatalf("create consumer: %v", err)
	}
	msgs, err := cons.Fetch(1, jetstream.FetchMaxWait(3*time.Second))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	msg, ok := <-msgs.Messages()
	if !ok {
		t.Fatalf("no message received on subject %q", wantSubject)
	}
	_ = msg.Ack()
	if msg.Subject() != wantSubject {
		t.Errorf("subject = %q, want %q", msg.Subject(), wantSubject)
	}
	var env Event
	if err := json.Unmarshal(msg.Data(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.EventType != "web.wad_increment" {
		t.Errorf("event_type = %q, want %q", env.EventType, "web.wad_increment")
	}
	if env.TenantID != "tenant-7" {
		t.Errorf("tenant_id = %q, want %q", env.TenantID, "tenant-7")
	}
	if env.Source != Source {
		t.Errorf("source = %q, want %q", env.Source, Source)
	}
	var gotPayload telemetryPayload
	if err := json.Unmarshal(env.Payload, &gotPayload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if gotPayload.Count != 3 {
		t.Errorf("payload.count = %d, want 3", gotPayload.Count)
	}
}
