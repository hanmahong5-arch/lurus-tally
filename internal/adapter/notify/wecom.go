package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// defaultHTTPTimeout bounds every webhook POST so a slow/unreachable IM
// provider never stalls the caller's request path.
const defaultHTTPTimeout = 5 * time.Second

// wecomTextMessage is the WeCom (企业微信) group-bot webhook payload for a
// plain-text message. See:
// https://developer.work.weixin.qq.com/document/path/91770
type wecomTextMessage struct {
	MsgType string `json:"msgtype"`
	Text    struct {
		Content string `json:"content"`
	} `json:"text"`
}

// WeComNotifier posts plain-text messages to a WeCom group-bot webhook.
type WeComNotifier struct {
	webhookURL string
	client     *http.Client
}

// NewWeComNotifier returns nil (not an error) when webhookURL is empty, so
// callers can wire it unconditionally and nil-check before use — the same
// degrade pattern used by other optional Tally integrations.
func NewWeComNotifier(webhookURL string) *WeComNotifier {
	if webhookURL == "" {
		return nil
	}
	return &WeComNotifier{
		webhookURL: webhookURL,
		client:     &http.Client{Timeout: defaultHTTPTimeout},
	}
}

// Send posts title and body as a single WeCom text message.
func (n *WeComNotifier) Send(ctx context.Context, title, body string) error {
	msg := wecomTextMessage{MsgType: "text"}
	msg.Text.Content = joinTitleBody(title, body)

	return postJSON(ctx, n.client, n.webhookURL, msg, "wecom")
}

// joinTitleBody formats a title+body pair for providers whose payload only
// carries a single text field.
func joinTitleBody(title, body string) string {
	if body == "" {
		return title
	}
	return title + "\n" + body
}

// postJSON marshals payload, POSTs it as application/json, and treats any
// non-2xx status as an error. provider is used only to prefix error messages.
func postJSON(ctx context.Context, client *http.Client, url string, payload any, provider string) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("notify/%s: marshal payload: %w", provider, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("notify/%s: build request: %w", provider, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("notify/%s: send request: %w", provider, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("notify/%s: webhook returned HTTP %d: %s", provider, resp.StatusCode, string(respBody))
	}
	return nil
}
