package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// defaultTimeout caps every outbound platform request. Platform itself enforces
// internal SLOs in the order of hundreds of ms; 5s gives generous headroom for
// payment provider hops while still failing fast under outage.
const defaultTimeout = 5 * time.Second

// maxBodyBytes bounds error-body capture so a misbehaving platform response
// cannot blow up Tally logs.
const maxBodyBytes = 4 * 1024

// Client calls lurus-platform's /internal/v1/* endpoints.
// Construct it via New and reuse across requests — Client is safe for concurrent use.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// Config bundles the values read from env at lifecycle wiring time.
type Config struct {
	// BaseURL of the platform internal API, e.g. http://platform-core.lurus-platform.svc:18104
	BaseURL string
	// APIKey is the bearer token (INTERNAL_API_KEY).
	APIKey string
	// Timeout overrides defaultTimeout when > 0.
	Timeout time.Duration
}

// New constructs a Client. Returns an error when BaseURL or APIKey is empty.
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("platform: BaseURL is required")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("platform: APIKey is required")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Client{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
		http: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

// do executes an HTTP request against the platform and decodes JSON into out.
// payload may be nil; out may be nil when the caller does not care about the body.
func (c *Client) do(ctx context.Context, method, path string, payload, out any) error {
	var body io.Reader
	if payload != nil {
		buf, err := json.Marshal(payload)
		if err != nil {
			return &Error{Code: ErrCodeUnknown, Message: fmt.Sprintf("encode payload: %v", err)}
		}
		body = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return &Error{Code: ErrCodeUnknown, Message: fmt.Sprintf("build request: %v", err)}
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	res, err := c.http.Do(req)
	if err != nil {
		return &Error{Code: ErrCodeUnavailable, Message: fmt.Sprintf("transport: %v", err)}
	}
	defer func() { _ = res.Body.Close() }()

	rawBody, _ := io.ReadAll(io.LimitReader(res.Body, maxBodyBytes))

	if res.StatusCode >= 200 && res.StatusCode < 300 {
		if out == nil || len(rawBody) == 0 {
			return nil
		}
		if err := json.Unmarshal(rawBody, out); err != nil {
			return &Error{
				Code:       ErrCodeUnknown,
				HTTPStatus: res.StatusCode,
				Message:    fmt.Sprintf("decode response: %v", err),
				Body:       string(rawBody),
			}
		}
		return nil
	}

	return &Error{
		Code:       mapStatus(res.StatusCode, rawBody),
		HTTPStatus: res.StatusCode,
		Message:    extractMessage(rawBody),
		Body:       string(rawBody),
	}
}

// mapStatus translates an HTTP status (and best-effort body inspection) into ErrCode.
func mapStatus(status int, body []byte) ErrCode {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrCodeUnauthorized
	case http.StatusNotFound:
		return ErrCodeNotFound
	case http.StatusPaymentRequired:
		return ErrCodeInsufficientBalance
	case http.StatusBadRequest:
		return ErrCodeInvalidParameter
	}
	if status >= 500 {
		return ErrCodeUnavailable
	}
	if bytes.Contains(body, []byte("insufficient_balance")) {
		return ErrCodeInsufficientBalance
	}
	return ErrCodeUnknown
}

// extractMessage pulls the platform's `message` or `error` field if present.
func extractMessage(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var generic struct {
		Message string `json:"message"`
		Error   string `json:"error"`
		Detail  string `json:"detail"`
	}
	if err := json.Unmarshal(body, &generic); err == nil {
		switch {
		case generic.Message != "":
			return generic.Message
		case generic.Detail != "":
			return generic.Detail
		case generic.Error != "":
			return generic.Error
		}
	}
	return ""
}
