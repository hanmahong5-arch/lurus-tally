// kill-switch-monitor is a one-shot CLI that evaluates kill-switch signals
// from the assumptions.md daily snapshot table and fires breach notifications.
//
// Designed to run as a Kubernetes CronJob (not a daemon). Exit codes:
//   0 — no signals in breach
//   1 — breaches detected and all configured senders delivered successfully
//   2 — breaches detected but at least one sender failed
//
// Required env vars: none. All env vars degrade gracefully (missing senders
// are skipped; a missing or unparseable assumptions file falls back to mock
// data with a WARN log so the job never silently disappears).
package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/hanmahong5-arch/lurus-tally/internal/alert"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})).
		With(slog.String("service", "kill-switch-monitor"))
	slog.SetDefault(log)

	assumptionsFile := envOr("ASSUMPTIONS_FILE", "_bmad-output/planning-artifacts/assumptions.md")

	snapshots, err := parseAssumptions(assumptionsFile)
	if err != nil || len(snapshots) == 0 {
		log.Warn("assumptions.md parse failed or empty — using mock data for dry-run",
			slog.String("file", assumptionsFile),
			slog.String("reason", safeErr(err)),
		)
		snapshots = mockSnapshots()
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
