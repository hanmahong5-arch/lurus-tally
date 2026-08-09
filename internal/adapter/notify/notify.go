// Package notify sends operational alerts to Chinese IM group-bot webhooks
// (WeCom / DingTalk / Feishu). Each provider has its own wire format and
// (for DingTalk) an HMAC-SHA256 request signature, but callers only see the
// shared Notifier interface — see doc.go for wiring instructions.
package notify

import "context"

// Notifier sends a short operational message to a configured destination.
// Implementations must be safe for concurrent use.
type Notifier interface {
	// Send delivers title and body to the underlying webhook. body may
	// contain newlines; providers that only support a single text field
	// join title and body with a blank line.
	Send(ctx context.Context, title, body string) error
}
