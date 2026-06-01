// kill-switch-monitor is a one-shot CLI that evaluates kill-switch signals
// from the assumptions.md daily snapshot table and fires breach notifications.
//
// Designed to run as a Kubernetes CronJob (not a daemon). Exit codes:
//
//	0 — no signals in breach
//	1 — breaches detected and all configured senders delivered successfully
//	2 — breaches detected but at least one sender failed
//	3 — input unavailable: the assumptions file is missing/unparseable/empty,
//	    so the monitor is DEGRADED and refuses to emit a breach verdict
//
// Required env vars: none. Missing senders are skipped. A missing, unparseable,
// or empty assumptions file does NOT synthesize a breach: the monitor exits 3
// (DATA-UNAVAILABLE) so an infra failure can never masquerade as a real
// kill-switch signal. For local dry-runs, set KILLSWITCH_ALLOW_MOCK=true to opt
// into synthetic all-breach data; it is off by default and must never be set in
// production.
package main

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/hanmahong5-arch/lurus-tally/internal/alert"
)

const (
	// exitDataUnavailable signals that the monitor could not load real input and
	// is running degraded — distinct from a genuine breach (exit 1/2) so absent
	// data never fabricates a pivot alert.
	exitDataUnavailable = 3
	// envAllowMock opts into synthetic all-breach data for local dry-runs only.
	// Off by default; must never be set in production.
	envAllowMock = "KILLSWITCH_ALLOW_MOCK"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})).
		With(slog.String("service", "kill-switch-monitor"))
	slog.SetDefault(log)

	assumptionsFile := envOr("ASSUMPTIONS_FILE", "_bmad-output/planning-artifacts/assumptions.md")

	snapshots, err := parseAssumptions(assumptionsFile)
	snapshots, degraded := resolveSnapshots(snapshots, err, mockAllowed())
	if degraded {
		// Input is missing/unparseable/empty and mock is not explicitly enabled.
		// Refuse to emit a breach verdict from absent data: exit DATA-UNAVAILABLE
		// so operators see a degraded monitor instead of a fabricated pivot alert.
		log.Error("assumptions input unavailable — monitor degraded, refusing to fabricate a breach verdict",
			slog.String("file", assumptionsFile),
			slog.String("reason", safeErr(err)),
			slog.Int("snapshots_parsed", len(snapshots)),
		)
		os.Exit(exitDataUnavailable)
	}

	breaches := alert.Evaluate(snapshots, alert.DefaultRequiredConsecutiveDays)
	log.Info("evaluation complete",
		slog.Int("snapshots_parsed", len(snapshots)),
		slog.Int("breaches_found", len(breaches)),
	)

	if len(breaches) == 0 {
		log.Info("no kill-switch breaches detected")
		os.Exit(0)
	}

	for _, b := range breaches {
		log.Warn("kill-switch breach",
			slog.String("signal", b.SignalName),
			slog.Int("consecutive_days", b.ConsecutiveDays),
			slog.String("first_red_date", b.FirstRedDate.Format(time.DateOnly)),
		)
	}

	sender := buildSender(log)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := sender.Send(ctx, breaches); err != nil {
		log.Error("breach notification delivery failed", slog.String("error", err.Error()))
		os.Exit(2)
	}
	log.Info("breach notifications sent")
	os.Exit(1)
}

// buildSender assembles a MultiSender from all configured channels.
// LogSender is always included as the audit fallback.
func buildSender(log *slog.Logger) alert.Sender {
	senders := []alert.Sender{
		&alert.LogSender{Logger: log},
	}

	if url := os.Getenv("FEISHU_WEBHOOK_URL"); url != "" {
		senders = append(senders, &alert.FeishuSender{WebhookURL: url})
		log.Info("feishu sender enabled")
	}

	email := &alert.EmailSender{
		Host: os.Getenv("SMTP_HOST"),
		Port: envOr("SMTP_PORT", "587"),
		User: os.Getenv("SMTP_USER"),
		Pass: os.Getenv("SMTP_PASS"),
		From: os.Getenv("SMTP_FROM"),
		To:   os.Getenv("SMTP_TO"),
	}
	if email.IsConfigured() {
		senders = append(senders, email)
		log.Info("email sender enabled", slog.String("smtp_host", email.Host))
	}

	return &alert.MultiSender{Senders: senders}
}

// resolveSnapshots decides what the monitor evaluates given the parse result.
// It is a pure function (no I/O, no os.Exit) so the degraded-vs-evaluate
// decision can be unit-tested directly.
//
//   - Real snapshots present (parse ok, non-empty): evaluate them; degraded=false.
//   - Input missing/unparseable/empty AND allowMock: substitute synthetic
//     all-breach data for a local dry-run; degraded=false.
//   - Input missing/unparseable/empty AND NOT allowMock: return the (empty)
//     snapshots unchanged with degraded=true so the caller exits DATA-UNAVAILABLE
//     instead of fabricating a breach verdict.
func resolveSnapshots(snapshots []alert.Snapshot, parseErr error, allowMock bool) ([]alert.Snapshot, bool) {
	if parseErr == nil && len(snapshots) > 0 {
		return snapshots, false
	}
	if allowMock {
		return mockSnapshots(), false
	}
	return snapshots, true
}

// mockAllowed reports whether synthetic mock data is explicitly opted into via
// KILLSWITCH_ALLOW_MOCK=true. Off by default; must never be set in production.
func mockAllowed() bool {
	return strings.EqualFold(os.Getenv(envAllowMock), "true")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func safeErr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
