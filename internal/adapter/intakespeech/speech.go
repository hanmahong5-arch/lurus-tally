// Package intakespeech posts audio to an optional speech endpoint. An empty URL
// leaves the recording for a person to type.
package intakespeech

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// Parser calls SPEECH_URL. It does not invent amounts.
type Parser struct {
	URL  string
	HTTP *http.Client
}

// Transcribe returns the text field, or empty when the endpoint is off.
func (p *Parser) Transcribe(ctx context.Context, contentType string, body []byte) (string, error) {
	if p == nil || p.URL == "" || len(body) == 0 {
		return "", nil
	}
	client := p.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.URL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	req.Header.Set("Content-Type", contentType)
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return "", err
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	return out.Text, nil
}
