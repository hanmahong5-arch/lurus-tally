// Wiring guide for internal/adapter/notify.
//
// This package is intentionally NOT wired into internal/lifecycle/app.go yet:
// at the time it was added, every candidate call site (billing webhook
// handlers, low-stock alert path, etc.) had uncommitted work-in-progress
// changes from a concurrent session, and the safe-editing rule for this
// repo is "never touch a hot file with someone else's uncommitted diff in
// it." Wiring it up is a follow-up task once those files land. Until then,
// the notifiers are ready to import and use directly wherever a future PR
// needs a Chinese-IM alert (e.g. platform billing failures, replenishment
// stockout alerts, NATS outbox_worker dead-letter events).
//
// # Configuration
//
// Read the three webhook env vars in cmd/server or internal/lifecycle/app.go
// (see .env.example for the full variable docs) and construct whichever
// notifiers are configured. All three constructors return nil when their
// webhook URL is empty, so unconfigured providers are safe no-ops — always
// nil-check before calling Send, or wrap in a small helper that skips nil
// notifiers:
//
//	notifiers := []notify.Notifier{}
//	if n := notify.NewWeComNotifier(os.Getenv("TALLY_NOTIFY_WECOM_WEBHOOK")); n != nil {
//		notifiers = append(notifiers, n)
//	}
//	if n := notify.NewDingTalkNotifier(
//		os.Getenv("TALLY_NOTIFY_DINGTALK_WEBHOOK"),
//		os.Getenv("TALLY_NOTIFY_DINGTALK_SECRET"),
//	); n != nil {
//		notifiers = append(notifiers, n)
//	}
//	if n := notify.NewFeishuNotifier(os.Getenv("TALLY_NOTIFY_FEISHU_WEBHOOK")); n != nil {
//		notifiers = append(notifiers, n)
//	}
//
//	for _, n := range notifiers {
//		if err := n.Send(ctx, "Tally alert", "some low-stock message"); err != nil {
//			slog.Error("notify send failed", slog.String("error", err.Error()))
//		}
//	}
//
// # Suggested first call site
//
// internal/app/replenish (low-stock / reorder-point alerts) is the most
// natural first consumer: it already computes a human-readable alert
// message for PublishLowStockAlert (see internal/adapter/nats/payloads.go),
// so the same string can fan out to a configured IM notifier. Fire-and-log:
// never let a notify failure block or fail the business transaction that
// triggered it (same pattern as internal/adapter/nats.Publisher — see its
// package doc for the precedent this package follows).
package notify
