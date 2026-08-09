package notify

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// LowStockLine is one product at or below its reorder point, shaped for an IM
// alert. Quantity fields are decimal strings to preserve precision on the way
// out (mirrors app/replenish.LowStockRow field-for-field so the mapping in
// replenish.NotifyLines is a straight copy — see that function for the seam
// that connects the real low-stock use case to this dispatcher).
type LowStockLine struct {
	ProductName  string
	ProductCode  string
	AvailableQty string
	ReorderPoint string
	DaysOfSupply string
}

// maxAlertLines caps how many SKUs a single IM message enumerates. Group-bot
// webhooks reject oversized bodies and a wall of 200 lines is unreadable on a
// phone anyway; overflow is summarised as "…and N more".
const maxAlertLines = 15

// FormatLowStockAlert renders a low-stock alert as a title + body pair.
// scope names the tenant/warehouse the alert is for (e.g. a company name) so a
// shared bot group can tell whose stock is low. Returns ("", "") for no lines
// — the dispatcher treats that as "nothing to alert" and stays silent.
func FormatLowStockAlert(scope string, lines []LowStockLine) (title, body string) {
	if len(lines) == 0 {
		return "", ""
	}

	title = fmt.Sprintf("库存预警:%d 个商品已低于补货点", len(lines))
	if scope != "" {
		title = scope + " · " + title
	}

	var b strings.Builder
	shown := lines
	if len(shown) > maxAlertLines {
		shown = shown[:maxAlertLines]
	}
	for _, l := range shown {
		name := l.ProductName
		if l.ProductCode != "" {
			name = fmt.Sprintf("%s(%s)", l.ProductName, l.ProductCode)
		}
		// One SKU per line: name — 可用 X / 补货点 Y / 约可撑 Z 天.
		fmt.Fprintf(&b, "• %s — 可用 %s / 补货点 %s / 约可撑 %s 天\n",
			name, l.AvailableQty, l.ReorderPoint, l.DaysOfSupply)
	}
	if len(lines) > maxAlertLines {
		fmt.Fprintf(&b, "…另有 %d 个商品待补货", len(lines)-maxAlertLines)
	}

	return title, strings.TrimRight(b.String(), "\n")
}

// Dispatcher fans a formatted low-stock alert out to every configured IM
// notifier. It is fire-and-log by contract: a webhook failure is logged and
// swallowed, never propagated, so an unreachable IM provider can never fail or
// block the business transaction that produced the alert (same precedent as
// the NATS publisher — see package doc.go).
type Dispatcher struct {
	notifiers []Notifier
	logger    *slog.Logger
}

// NewDispatcher builds a Dispatcher from the given notifiers, dropping any nil
// entries so callers can pass the constructors' nil-on-empty-URL results
// directly (NewWeComNotifier / NewDingTalkNotifier / NewFeishuNotifier all
// return nil when unconfigured). A nil logger falls back to slog.Default so the
// dispatcher never panics on an unconfigured logger.
func NewDispatcher(logger *slog.Logger, notifiers ...Notifier) *Dispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	live := make([]Notifier, 0, len(notifiers))
	for _, n := range notifiers {
		if n == nil || isNilNotifier(n) {
			continue
		}
		live = append(live, n)
	}
	return &Dispatcher{notifiers: live, logger: logger}
}

// Configured reports whether at least one live notifier is wired. Callers can
// skip the (cheap) formatting work when no IM destination is configured.
func (d *Dispatcher) Configured() bool {
	return len(d.notifiers) > 0
}

// DispatchLowStock formats the lines and sends them to every configured
// notifier. It never returns an error: each provider's outcome is logged
// individually (info on success, error on failure) so a partial failure is
// visible without aborting the fan-out. A no-op when there are no lines or no
// notifiers configured.
func (d *Dispatcher) DispatchLowStock(ctx context.Context, scope string, lines []LowStockLine) {
	if len(lines) == 0 || len(d.notifiers) == 0 {
		return
	}
	title, body := FormatLowStockAlert(scope, lines)

	for _, n := range d.notifiers {
		if err := n.Send(ctx, title, body); err != nil {
			d.logger.ErrorContext(ctx, "notify: low-stock alert send failed",
				slog.String("scope", scope),
				slog.Int("sku_count", len(lines)),
				slog.String("error", err.Error()))
			continue
		}
		d.logger.InfoContext(ctx, "notify: low-stock alert sent",
			slog.String("scope", scope),
			slog.Int("sku_count", len(lines)))
	}
}

// LogNotifier is a Notifier that records the message to a structured log
// instead of POSTing it to a webhook. It is the dry-run destination: wire it in
// place of (or alongside) the real notifiers to prove the alert seam end-to-end
// without hitting an external IM provider. Send never fails.
type LogNotifier struct {
	logger *slog.Logger
}

// NewLogNotifier returns a dry-run notifier. A nil logger falls back to
// slog.Default.
func NewLogNotifier(logger *slog.Logger) *LogNotifier {
	if logger == nil {
		logger = slog.Default()
	}
	return &LogNotifier{logger: logger}
}

// Send logs the alert at info level (dry-run: no network call) and returns nil.
func (n *LogNotifier) Send(ctx context.Context, title, body string) error {
	n.logger.InfoContext(ctx, "notify/dry-run: would send IM alert",
		slog.String("title", title),
		slog.String("body", body))
	return nil
}

// isNilNotifier guards against a typed-nil interface value slipping through the
// nil check in NewDispatcher — e.g. a *FeishuNotifier that is nil boxed into a
// non-nil Notifier interface. Sending on such a value would panic.
func isNilNotifier(n Notifier) bool {
	switch v := n.(type) {
	case *WeComNotifier:
		return v == nil
	case *DingTalkNotifier:
		return v == nil
	case *FeishuNotifier:
		return v == nil
	case *LogNotifier:
		return v == nil
	default:
		return false
	}
}
