package alert

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/smtp"
	"strings"
	"time"
)

// Sender dispatches breach notifications through a specific channel.
// Implementations must be safe for concurrent use.
type Sender interface {
	// Send delivers breach information to the configured destination.
	// A non-nil error means the delivery attempt failed; the caller
	// (MultiSender) will continue to other senders regardless.
	Send(ctx context.Context, breaches []Breach) error
}

// ─────────────────────────────────────────────────────────────────────────────
// LogSender — audit fallback, always active
// ─────────────────────────────────────────────────────────────────────────────

// LogSender writes breaches to the structured logger.
// It acts as the permanent audit trail even when other senders succeed.
type LogSender struct {
	Logger *slog.Logger
}

// Send logs each breach at WARN level.
func (s *LogSender) Send(_ context.Context, breaches []Breach) error {
	log := s.Logger
	if log == nil {
		log = slog.Default()
	}
	for _, b := range breaches {
		log.Warn("kill-switch breach detected",
			slog.String("signal", b.SignalName),
			slog.Int("consecutive_days", b.ConsecutiveDays),
			slog.String("first_red_date", b.FirstRedDate.Format(time.DateOnly)),
		)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// FeishuSender — Feishu custom bot webhook
// ─────────────────────────────────────────────────────────────────────────────

// FeishuSender delivers breach cards to a Feishu custom-bot webhook.
// Set WebhookURL from env FEISHU_WEBHOOK_URL. The URL is treated as a secret
// and is never written to logs.
type FeishuSender struct {
	// WebhookURL is the full endpoint for the Feishu custom bot.
	WebhookURL string
	// HTTPClient is optional; nil falls back to a default 15-second client.
	HTTPClient *http.Client
}

// feishuPayload is the message envelope accepted by Feishu custom bots.
type feishuPayload struct {
	MsgType string         `json:"msg_type"`
	Content feishuMarkdown `json:"content"`
}

type feishuMarkdown struct {
	Text string `json:"text"`
}

// Send posts a markdown card listing all breaches to the Feishu webhook.
func (s *FeishuSender) Send(ctx context.Context, breaches []Breach) error {
	if s.WebhookURL == "" {
		return nil // not configured — skip silently
	}

	var sb strings.Builder
	sb.WriteString("🚨 **Tally Kill-Switch 告警**\n\n")
	for _, b := range breaches {
		_, _ = fmt.Fprintf(&sb,
			"- **%s** 已连续 %d 天触发 (首次红: %s)\n",
			b.SignalName, b.ConsecutiveDays,
			b.FirstRedDate.Format(time.DateOnly),
		)
	}
	sb.WriteString("\n> 任一 Kill-Switch 连红 2 周 → 立即安排 Pivot 会议\n")

	payload := feishuPayload{
		MsgType: "text",
		Content: feishuMarkdown{Text: sb.String()},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("feishu: marshal payload: %w", err)
	}

	client := s.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("feishu: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("feishu: POST webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("feishu: webhook returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// EmailSender — SMTP
// ─────────────────────────────────────────────────────────────────────────────

// EmailSender delivers breach notifications via SMTP.
// All six env vars (SMTP_HOST, SMTP_PORT, SMTP_USER, SMTP_PASS, SMTP_FROM,
// SMTP_TO) must be non-empty for this sender to activate.
type EmailSender struct {
	Host string
	Port string
	User string
	Pass string
	From string
	// To is a comma-separated list of recipient addresses.
	To string
}

// IsConfigured reports whether all required fields are present.
func (s *EmailSender) IsConfigured() bool {
	return s.Host != "" && s.Port != "" &&
		s.User != "" && s.Pass != "" &&
		s.From != "" && s.To != ""
}

// Send dials SMTP with STARTTLS and sends a plain-text breach summary.
func (s *EmailSender) Send(_ context.Context, breaches []Breach) error {
	if !s.IsConfigured() {
		return nil // not configured — skip silently
	}

	var sb strings.Builder
	sb.WriteString("Tally Kill-Switch 告警\n\n")
	for _, b := range breaches {
		_, _ = fmt.Fprintf(&sb,
			"  %s: 连续 %d 天 (首次红: %s)\n",
			b.SignalName, b.ConsecutiveDays,
			b.FirstRedDate.Format(time.DateOnly),
		)
	}
	sb.WriteString("\n任一 Kill-Switch 连红 2 周 → 立即安排 Pivot 会议\n")

	addr := net.JoinHostPort(s.Host, s.Port)
	auth := smtp.PlainAuth("", s.User, s.Pass, s.Host)

	to := strings.Split(s.To, ",")
	for i, t := range to {
		to[i] = strings.TrimSpace(t)
	}

	msg := []byte(
		"From: " + s.From + "\r\n" +
			"To: " + s.To + "\r\n" +
			"Subject: [Tally] Kill-Switch 告警 — 需要 Pivot 会议\r\n" +
			"Content-Type: text/plain; charset=utf-8\r\n" +
			"\r\n" +
			sb.String(),
	)

	// Dial with TLS on port 465, fall back to STARTTLS on 587/25.
	if s.Port == "465" {
		tlsCfg := &tls.Config{ServerName: s.Host} //nolint:gosec // standard SMTP TLS
		conn, err := tls.Dial("tcp", addr, tlsCfg)
		if err != nil {
			return fmt.Errorf("email: tls dial %s: %w", s.Host, err)
		}
		c, err := smtp.NewClient(conn, s.Host)
		if err != nil {
			return fmt.Errorf("email: smtp client: %w", err)
		}
		defer func() { _ = c.Close() }()
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("email: auth: %w", err)
		}
		if err := c.Mail(s.From); err != nil {
			return fmt.Errorf("email: MAIL FROM: %w", err)
		}
		for _, t := range to {
			if err := c.Rcpt(t); err != nil {
				return fmt.Errorf("email: RCPT TO %s: %w", t, err)
			}
		}
		w, err := c.Data()
		if err != nil {
			return fmt.Errorf("email: DATA: %w", err)
		}
		if _, err := w.Write(msg); err != nil {
			return fmt.Errorf("email: write body: %w", err)
		}
		return w.Close()
	}

	return smtp.SendMail(addr, auth, s.From, to, msg)
}

// ─────────────────────────────────────────────────────────────────────────────
// MultiSender — fan-out wrapper
// ─────────────────────────────────────────────────────────────────────────────

// MultiSender wraps multiple Sender implementations and calls each in order.
// A failure from one sender is recorded but does not prevent the remaining
// senders from being called.
type MultiSender struct {
	Senders []Sender
}

// Send calls every inner Sender and collects errors. Returns a joined error
// string if any inner call failed, nil otherwise.
func (m *MultiSender) Send(ctx context.Context, breaches []Breach) error {
	var errs []string
	for _, s := range m.Senders {
		if err := s.Send(ctx, breaches); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("multi-sender errors: %s", strings.Join(errs, "; "))
	}
	return nil
}
