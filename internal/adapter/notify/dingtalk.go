package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// dingTalkTextMessage is the DingTalk (钉钉) group-bot webhook payload for a
// plain-text message. See:
// https://open.dingtalk.com/document/robots/custom-robot-access
type dingTalkTextMessage struct {
	MsgType string `json:"msgtype"`
	Text    struct {
		Content string `json:"content"`
	} `json:"text"`
}

// DingTalkNotifier posts plain-text messages to a DingTalk group-bot
// webhook. When a signing secret is configured (recommended; DingTalk's
// "加签" security option), every request is signed per DingTalk's HMAC
// scheme: sign = base64(hmac_sha256(secret, "{timestamp}\n{secret}")),
// appended to the webhook URL as &timestamp=...&sign=....
type DingTalkNotifier struct {
	webhookURL string
	secret     string // optional; empty disables request signing
	client     *http.Client
	now        func() time.Time // overridable in tests
}

// NewDingTalkNotifier returns nil (not an error) when webhookURL is empty —
// see WeComNotifier's constructor for the shared degrade rationale. secret
// may be empty if the DingTalk bot was created without "加签" enabled.
func NewDingTalkNotifier(webhookURL, secret string) *DingTalkNotifier {
	if webhookURL == "" {
		return nil
	}
	return &DingTalkNotifier{
		webhookURL: webhookURL,
		secret:     secret,
		client:     &http.Client{Timeout: defaultHTTPTimeout},
		now:        time.Now,
	}
}

// Send posts title and body as a single DingTalk text message, signing the
// request URL when a secret is configured.
func (n *DingTalkNotifier) Send(ctx context.Context, title, body string) error {
	msg := dingTalkTextMessage{MsgType: "text"}
	msg.Text.Content = joinTitleBody(title, body)

	targetURL := n.webhookURL
	if n.secret != "" {
		signedURL, err := n.signedURL()
		if err != nil {
			return fmt.Errorf("notify/dingtalk: sign request: %w", err)
		}
		targetURL = signedURL
	}

	return postJSON(ctx, n.client, targetURL, msg, "dingtalk")
}

// signedURL appends DingTalk's required &timestamp=&sign= query parameters
// to the configured webhook URL, per DingTalk's custom-robot signing spec.
func (n *DingTalkNotifier) signedURL() (string, error) {
	timestamp := strconv.FormatInt(n.now().UnixMilli(), 10)

	stringToSign := timestamp + "\n" + n.secret
	mac := hmac.New(sha256.New, []byte(n.secret))
	if _, err := mac.Write([]byte(stringToSign)); err != nil {
		return "", fmt.Errorf("hmac write: %w", err)
	}
	sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	parsed, err := url.Parse(n.webhookURL)
	if err != nil {
		return "", fmt.Errorf("parse webhook url: %w", err)
	}
	q := parsed.Query()
	q.Set("timestamp", timestamp)
	q.Set("sign", sign)
	parsed.RawQuery = q.Encode()

	return parsed.String(), nil
}
