package notify

import (
	"context"
	"net/http"
)

// feishuTextMessage is the Feishu (飞书) custom-bot webhook payload for a
// plain-text message. See:
// https://open.feishu.cn/document/client-docs/bot-v3/add-custom-bot
type feishuTextMessage struct {
	MsgType string `json:"msg_type"`
	Content struct {
		Text string `json:"text"`
	} `json:"content"`
}

// FeishuNotifier posts plain-text messages to a Feishu custom-bot webhook.
type FeishuNotifier struct {
	webhookURL string
	client     *http.Client
}

// NewFeishuNotifier returns nil (not an error) when webhookURL is empty —
// see WeComNotifier's constructor for the shared degrade rationale.
func NewFeishuNotifier(webhookURL string) *FeishuNotifier {
	if webhookURL == "" {
		return nil
	}
	return &FeishuNotifier{
		webhookURL: webhookURL,
		client:     &http.Client{Timeout: defaultHTTPTimeout},
	}
}

// Send posts title and body as a single Feishu text message.
func (n *FeishuNotifier) Send(ctx context.Context, title, body string) error {
	msg := feishuTextMessage{MsgType: "text"}
	msg.Content.Text = joinTitleBody(title, body)

	return postJSON(ctx, n.client, n.webhookURL, msg, "feishu")
}
