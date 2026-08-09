package alert_test

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-tally/internal/alert"
)

// ─────────────────────────────────────────────────────────────────────────────
// Signal.IsBreached — pure predicate, boundary + unknown direction
// ─────────────────────────────────────────────────────────────────────────────

func TestSignal_IsBreached_Table(t *testing.T) {
	cases := []struct {
		name      string
		value     float64
		threshold float64
		direction alert.Direction
		want      bool
	}{
		{"lt below threshold is red", 0.10, 0.40, alert.DirectionLT, true},
		{"lt above threshold is green", 0.50, 0.40, alert.DirectionLT, false},
		{"lt equal threshold is NOT breached", 0.40, 0.40, alert.DirectionLT, false},
		{"gt above threshold is red", 150, 100, alert.DirectionGT, true},
		{"gt below threshold is green", 50, 100, alert.DirectionGT, false},
		{"gt equal threshold is NOT breached", 100, 100, alert.DirectionGT, false},
		{"unknown direction never red", 0.10, 0.40, alert.Direction("unknown"), false},
		{"empty direction never red", 999, 1, alert.Direction(""), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := alert.Signal{Name: "x", Value: tc.value, Threshold: tc.threshold, Direction: tc.direction}
			if got := s.IsBreached(); got != tc.want {
				t.Errorf("IsBreached() = %v, want %v", got, tc.want)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Evaluate — technical edge cases
// ─────────────────────────────────────────────────────────────────────────────

func TestEvaluate_ZeroOrNegativeRequiredDays(t *testing.T) {
	snaps := []alert.Snapshot{
		{Date: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Signals: []alert.Signal{
			{Name: "s", Value: 0.1, Threshold: 0.4, Direction: alert.DirectionLT},
		}},
	}
	if got := alert.Evaluate(snaps, 0); got != nil {
		t.Errorf("requiredConsecutiveDays=0: want nil, got %v", got)
	}
	if got := alert.Evaluate(snaps, -3); got != nil {
		t.Errorf("requiredConsecutiveDays=-3: want nil, got %v", got)
	}
}

func TestEvaluate_EmptySliceReturnsNil(t *testing.T) {
	if got := alert.Evaluate([]alert.Snapshot{}, 14); got != nil {
		t.Errorf("empty slice: want nil, got %v", got)
	}
}

// TestEvaluate_13RedsGreenRed hand-computes: 13 consecutive reds, then 1 green
// (resets streak to 0), then 1 red (streak becomes 1) → max streak is 13,
// never reaches 14 → no breach. This differs from the existing "interrupted
// at day 7" fixture by placing the green day right before the window boundary.
func TestEvaluate_13RedsGreenRed(t *testing.T) {
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	snaps := make([]alert.Snapshot, 0, 15)
	// 13 red days (index 0-12)
	for i := 0; i < 13; i++ {
		snaps = append(snaps, alert.Snapshot{
			Date: base.AddDate(0, 0, i),
			Signals: []alert.Signal{
				{Name: "s", Value: 0.05, Threshold: 0.40, Direction: alert.DirectionLT},
			},
		})
	}
	// 1 green day (index 13) — explicit non-breached reading resets streak
	snaps = append(snaps, alert.Snapshot{
		Date: base.AddDate(0, 0, 13),
		Signals: []alert.Signal{
			{Name: "s", Value: 0.50, Threshold: 0.40, Direction: alert.DirectionLT},
		},
	})
	// 1 red day (index 14) — streak restarts at 1
	snaps = append(snaps, alert.Snapshot{
		Date: base.AddDate(0, 0, 14),
		Signals: []alert.Signal{
			{Name: "s", Value: 0.05, Threshold: 0.40, Direction: alert.DirectionLT},
		},
	})

	got := alert.Evaluate(snaps, alert.DefaultRequiredConsecutiveDays)
	if len(got) != 0 {
		t.Fatalf("13 reds + green + red: expected no breach (max streak 13), got %v", got)
	}
}

// TestEvaluate_GapDayDoesNotResetStreak hand-computes: 7 reds, then a day
// where the signal is entirely ABSENT from the snapshot (not an explicit
// green reading), then 7 more reds → streak continues uninterrupted to 14,
// crossing the requiredConsecutiveDays=14 threshold → breach fires, and
// FirstRedDate is the very first red day (since the streak was never reset).
func TestEvaluate_GapDayDoesNotResetStreak(t *testing.T) {
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	var snaps []alert.Snapshot
	for i := 0; i < 7; i++ {
		snaps = append(snaps, alert.Snapshot{
			Date: base.AddDate(0, 0, i),
			Signals: []alert.Signal{
				{Name: "s", Value: 0.05, Threshold: 0.40, Direction: alert.DirectionLT},
			},
		})
	}
	// Gap day: signal "s" absent entirely from this snapshot.
	snaps = append(snaps, alert.Snapshot{
		Date:    base.AddDate(0, 0, 7),
		Signals: []alert.Signal{},
	})
	for i := 8; i < 15; i++ {
		snaps = append(snaps, alert.Snapshot{
			Date: base.AddDate(0, 0, i),
			Signals: []alert.Signal{
				{Name: "s", Value: 0.05, Threshold: 0.40, Direction: alert.DirectionLT},
			},
		})
	}
	// Hand-computed: red count = 7 (days 0-6) + 7 (days 8-14) = 14 red
	// readings; the gap day contributes nothing but does not reset.
	got := alert.Evaluate(snaps, alert.DefaultRequiredConsecutiveDays)
	if len(got) != 1 {
		t.Fatalf("expected 1 breach after gap-day-preserved streak, got %v", got)
	}
	if got[0].ConsecutiveDays != 14 {
		t.Errorf("consecutive_days: want 14, got %d", got[0].ConsecutiveDays)
	}
	if !got[0].FirstRedDate.Equal(base) {
		t.Errorf("first_red_date: want %v (earliest red, streak never reset), got %v", base, got[0].FirstRedDate)
	}
}

// TestEvaluate_FirstRedDateIsCurrentStreakStart hand-computes: a green day
// resets FirstRedDate; the field must equal the first red day AFTER that
// reset, not the earliest red day in history.
func TestEvaluate_FirstRedDateIsCurrentStreakStart(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var snaps []alert.Snapshot
	// Days 0-2: red (early streak, will be reset).
	for i := 0; i < 3; i++ {
		snaps = append(snaps, alert.Snapshot{
			Date: base.AddDate(0, 0, i),
			Signals: []alert.Signal{
				{Name: "s", Value: 0.05, Threshold: 0.40, Direction: alert.DirectionLT},
			},
		})
	}
	// Day 3: green — explicit reset.
	snaps = append(snaps, alert.Snapshot{
		Date: base.AddDate(0, 0, 3),
		Signals: []alert.Signal{
			{Name: "s", Value: 0.50, Threshold: 0.40, Direction: alert.DirectionLT},
		},
	})
	// Days 4-17: 14 consecutive red days — new streak starts at day 4.
	wantFirstRed := base.AddDate(0, 0, 4)
	for i := 4; i < 18; i++ {
		snaps = append(snaps, alert.Snapshot{
			Date: base.AddDate(0, 0, i),
			Signals: []alert.Signal{
				{Name: "s", Value: 0.05, Threshold: 0.40, Direction: alert.DirectionLT},
			},
		})
	}
	got := alert.Evaluate(snaps, alert.DefaultRequiredConsecutiveDays)
	if len(got) != 1 {
		t.Fatalf("expected 1 breach, got %v", got)
	}
	if !got[0].FirstRedDate.Equal(wantFirstRed) {
		t.Errorf("first_red_date: want %v (post-reset streak start, not day 0), got %v", wantFirstRed, got[0].FirstRedDate)
	}
	if got[0].ConsecutiveDays != 14 {
		t.Errorf("consecutive_days: want 14, got %d", got[0].ConsecutiveDays)
	}
}

// TestEvaluate_UnsortedInputSameResultAsSorted feeds snapshots in reverse
// chronological order and asserts the result matches the hand-computed
// expectation for the equivalent sorted-ascending sequence (14 straight red
// days → 1 breach, FirstRedDate = earliest date).
func TestEvaluate_UnsortedInputSameResultAsSorted(t *testing.T) {
	base := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	var snaps []alert.Snapshot
	for i := 13; i >= 0; i-- { // reverse-chronological insertion order
		snaps = append(snaps, alert.Snapshot{
			Date: base.AddDate(0, 0, i),
			Signals: []alert.Signal{
				{Name: "s", Value: 0.05, Threshold: 0.40, Direction: alert.DirectionLT},
			},
		})
	}
	got := alert.Evaluate(snaps, alert.DefaultRequiredConsecutiveDays)
	if len(got) != 1 {
		t.Fatalf("expected 1 breach for reverse-chronological input, got %v", got)
	}
	if got[0].ConsecutiveDays != 14 {
		t.Errorf("consecutive_days: want 14, got %d", got[0].ConsecutiveDays)
	}
	if !got[0].FirstRedDate.Equal(base) {
		t.Errorf("first_red_date: want %v, got %v", base, got[0].FirstRedDate)
	}
}

// TestEvaluate_DeterministicOrderMultipleBreaches hand-computes that with
// three simultaneously-breaching signals named out of alphabetical order,
// the output is sorted by SignalName ascending.
func TestEvaluate_DeterministicOrderMultipleBreaches(t *testing.T) {
	base := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	names := []string{"zulu", "alpha", "mike"}
	var snaps []alert.Snapshot
	for i := 0; i < 14; i++ {
		sigs := make([]alert.Signal, len(names))
		for j, n := range names {
			sigs[j] = alert.Signal{Name: n, Value: 0.05, Threshold: 0.40, Direction: alert.DirectionLT}
		}
		snaps = append(snaps, alert.Snapshot{Date: base.AddDate(0, 0, i), Signals: sigs})
	}
	got := alert.Evaluate(snaps, alert.DefaultRequiredConsecutiveDays)
	if len(got) != 3 {
		t.Fatalf("expected 3 breaches, got %v", got)
	}
	wantOrder := []string{"alpha", "mike", "zulu"}
	for i, w := range wantOrder {
		if got[i].SignalName != w {
			t.Errorf("position %d: want %s, got %s", i, w, got[i].SignalName)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// FeishuSender — transport + status handling
// ─────────────────────────────────────────────────────────────────────────────

func TestFeishuSender_NonSuccessStatusReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := &alert.FeishuSender{WebhookURL: srv.URL, HTTPClient: srv.Client()}
	err := s.Send(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for HTTP 500 response, got nil")
	}
}

func TestFeishuSender_TransportErrorPropagates(t *testing.T) {
	// Bind and immediately close a TCP listener to get a port nothing listens
	// on, guaranteeing a fast "connection refused" from client.Do.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	s := &alert.FeishuSender{
		WebhookURL: "http://" + addr,
		HTTPClient: &http.Client{Timeout: 3 * time.Second},
	}
	err = s.Send(context.Background(), nil)
	if err == nil {
		t.Fatal("expected transport error for unreachable webhook, got nil")
	}
}

func TestFeishuSender_BuildRequestErrorForMalformedURL(t *testing.T) {
	// A control character in the URL makes http.NewRequestWithContext fail
	// during url.Parse, hitting the "build request" error branch.
	s := &alert.FeishuSender{WebhookURL: "http://\x7f"}
	err := s.Send(context.Background(), nil)
	if err == nil {
		t.Fatal("expected build-request error for malformed URL, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// EmailSender — IsConfigured predicate + Send skip/dial paths
// ─────────────────────────────────────────────────────────────────────────────

func fullEmailSender() alert.EmailSender {
	return alert.EmailSender{
		Host: "smtp.example.com",
		Port: "587",
		User: "user",
		Pass: "pass",
		From: "from@example.com",
		To:   "to@example.com",
	}
}

func TestEmailSender_IsConfigured_Table(t *testing.T) {
	base := fullEmailSender()
	cases := []struct {
		name   string
		mutate func(*alert.EmailSender)
		want   bool
	}{
		{"all fields present", func(s *alert.EmailSender) {}, true},
		{"missing host", func(s *alert.EmailSender) { s.Host = "" }, false},
		{"missing port", func(s *alert.EmailSender) { s.Port = "" }, false},
		{"missing user", func(s *alert.EmailSender) { s.User = "" }, false},
		{"missing pass", func(s *alert.EmailSender) { s.Pass = "" }, false},
		{"missing from", func(s *alert.EmailSender) { s.From = "" }, false},
		{"missing to", func(s *alert.EmailSender) { s.To = "" }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := base
			tc.mutate(&s)
			if got := s.IsConfigured(); got != tc.want {
				t.Errorf("IsConfigured() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEmailSender_Send_NotConfiguredSkipsWithoutDialing(t *testing.T) {
	s := &alert.EmailSender{Host: "smtp.example.com"} // missing 5 of 6 fields
	if err := s.Send(context.Background(), nil); err != nil {
		t.Fatalf("expected nil for unconfigured sender (no dial attempted), got %v", err)
	}
}

// unusedLocalAddr returns a host:port pair guaranteed to refuse connections
// (bind-then-close), so dial attempts fail fast instead of timing out.
func unusedLocalAddr(t *testing.T) (host, port string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	h, p, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	ln.Close()
	return h, p
}

// TestEmailSender_Send_TLSDialErrorOnPort465 exercises the port-465
// (implicit TLS) branch's dial-error path.
func TestEmailSender_Send_TLSDialErrorOnPort465(t *testing.T) {
	host, _ := unusedLocalAddr(t)
	s := &alert.EmailSender{
		Host: host, Port: "465", User: "u", Pass: "p", From: "f@example.com", To: "t@example.com",
	}
	err := s.Send(context.Background(), exampleBreaches)
	if err == nil {
		t.Fatal("expected tls dial error on unreachable port 465, got nil")
	}
}

// TestEmailSender_Send_SendMailErrorOnStartTLSPort exercises the
// non-465 (STARTTLS/plain) branch's dial-error path via smtp.SendMail.
func TestEmailSender_Send_SendMailErrorOnStartTLSPort(t *testing.T) {
	host, port := unusedLocalAddr(t)
	s := &alert.EmailSender{
		Host: host, Port: port, User: "u", Pass: "p", From: "f@example.com", To: "t@example.com",
	}
	err := s.Send(context.Background(), exampleBreaches)
	if err == nil {
		t.Fatal("expected smtp.SendMail dial error on unreachable port, got nil")
	}
}

// TestEmailSender_Send_MultipleRecipientsTrimmed exercises the comma-split
// "To" parsing path with surrounding whitespace, still hitting a dial error
// against an unreachable host so the test stays fast and network-free.
func TestEmailSender_Send_MultipleRecipientsTrimmed(t *testing.T) {
	host, port := unusedLocalAddr(t)
	s := &alert.EmailSender{
		Host: host, Port: port, User: "u", Pass: "p", From: "f@example.com",
		To: "a@example.com, b@example.com , c@example.com",
	}
	err := s.Send(context.Background(), exampleBreaches)
	if err == nil {
		t.Fatal("expected dial error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// LogSender — nil Logger fallback to slog.Default
// ─────────────────────────────────────────────────────────────────────────────

func TestLogSender_NilLoggerFallsBackToDefaultWithoutPanic(t *testing.T) {
	var buf bytes.Buffer
	prevDefault := slog.Default()
	defer slog.SetDefault(prevDefault)
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))

	s := &alert.LogSender{Logger: nil}
	if err := s.Send(context.Background(), exampleBreaches); err != nil {
		t.Fatalf("LogSender.Send with nil Logger: %v", err)
	}
	out := buf.String()
	if out == "" {
		t.Fatal("expected slog.Default() to capture the record, got empty output")
	}
	if !bytes.Contains(buf.Bytes(), []byte("ks1_onboarding_rate")) {
		t.Errorf("expected signal name in captured log output, got: %s", out)
	}
}

func TestLogSender_EmptyBreachesNoOp(t *testing.T) {
	s := &alert.LogSender{Logger: slog.Default()}
	if err := s.Send(context.Background(), nil); err != nil {
		t.Fatalf("expected nil for empty breaches, got %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// MultiSender — all-succeed nil path (error-join already covered elsewhere)
// ─────────────────────────────────────────────────────────────────────────────

func TestMultiSender_AllSucceedReturnsNil(t *testing.T) {
	ms := &alert.MultiSender{Senders: []alert.Sender{
		&countingSender{onSend: func() {}},
		&countingSender{onSend: func() {}},
	}}
	if err := ms.Send(context.Background(), exampleBreaches); err != nil {
		t.Fatalf("expected nil when all inner senders succeed, got %v", err)
	}
}

func TestMultiSender_EmptySendersReturnsNil(t *testing.T) {
	ms := &alert.MultiSender{}
	if err := ms.Send(context.Background(), exampleBreaches); err != nil {
		t.Fatalf("expected nil for empty Senders slice, got %v", err)
	}
}
