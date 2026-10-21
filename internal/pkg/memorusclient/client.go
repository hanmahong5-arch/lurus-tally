// Package memorusclient provides an HTTP client wrapper for the memorus memory
// engine (http://memorus.lurus-system.svc:8880).
//
// Degraded-mode design: when BaseURL or APIKey is empty, New returns (nil, nil).
// All methods panic if called on a nil receiver — callers must guard with
// "if memClient != nil { ... }".
//
// Error contract:
//   - Network/timeout errors → ErrUnavailable (caller should skip gracefully)
//   - HTTP 401 → ErrUnauthorized
//   - HTTP 404 → ErrNotFound
//   - Other non-2xx → ErrUnavailable
package memorusclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/httpx"
)

const defaultTimeout = 3 * time.Second

// Sentinel errors returned by all methods.
var (
	// ErrUnavailable signals a transient or persistent memorus failure.
	// Callers should degrade gracefully (skip recall/write, AI still works).
	ErrUnavailable = errors.New("memorus unavailable")

	// ErrNotFound is returned when a specific resource does not exist.
	ErrNotFound = errors.New("memory not found")

	// ErrUnauthorized is returned on HTTP 401 (bad API key).
	ErrUnauthorized = errors.New("memorus auth failed")
)

// IsUnavailable returns true if err wraps ErrUnavailable.
func IsUnavailable(err error) bool {
	return errors.Is(err, ErrUnavailable)
}

// Config holds the construction parameters for Client.
type Config struct {
	// BaseURL is the base URL of memorus and MUST include the "/api/v1" prefix,
	// e.g. "http://memorus.lurus-system.svc:8880/api/v1" — every call below does
	// baseURL+"/memories", while the server nests those routes under /api/v1
	// (memorus-server/src/rest/mod.rs: Router::new().nest("/api/v1", api)).
	// Omitting the prefix yields 404 on every call, and the failure is quiet:
	// recall just returns nothing. The example here used to omit it (fixed
	// 2026-09-07); repo .env.example has always had it right.
	// Empty → New returns (nil, nil).
	BaseURL string

	// APIKey is the X-API-Key header value (env MEMORUS_API_KEY).
	// Empty → New returns (nil, nil).
	APIKey string

	// Timeout is the per-request deadline. Defaults to 3s when zero.
	Timeout time.Duration
}

// Client wraps memorus REST calls. All fields are unexported and immutable after
// construction.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New creates a Client from cfg. Returns (nil, nil) when either BaseURL or APIKey
// is empty — this signals degraded mode and is NOT an error.
func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" || cfg.APIKey == "" {
		return nil, nil
	}
	warnIfBaseURLLooksPrefixless(cfg.BaseURL)
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	return &Client{
		baseURL: cfg.BaseURL,
		apiKey:  cfg.APIKey,
		http: &http.Client{
			Timeout: timeout,
			// Resilient transport: idempotent GET searches retry on 429/5xx with
			// backoff; the POST that adds a memory is single-shot (never replayed,
			// so a retry cannot duplicate a memory). A circuit breaker fast-fails
			// when memorus is down so AI requests degrade instead of stalling.
			Transport: httpx.New(http.DefaultTransport, httpx.DefaultConfig()),
		},
	}, nil
}

// Memory is the memorus MemoryEntry shape.
type Memory struct {
	ID        string         `json:"id"`
	UserID    string         `json:"user_id"`
	Content   string         `json:"content"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Score     float64        `json:"score,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

// Add stores a new memory for userID with the given content and optional metadata.
// On success it returns a Memory echoing the request parameters (memorus does not
// return the full entry on POST /memories; we synthesise one from the inputs).
func (c *Client) Add(ctx context.Context, userID string, content string, meta map[string]any) (*Memory, error) {
	body := map[string]any{
		"content": content,
		"user_id": userID,
	}
	if len(meta) > 0 {
		body["metadata"] = meta
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("memorusclient: marshal add request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/memories", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: create request: %s", ErrUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := classifyStatus(resp.StatusCode); err != nil {
		return nil, err
	}

	// Drain body (memorus returns {"results": {...}}) but we synthesise the Memory
	// from input since the API does not echo the full entry back.
	_, _ = io.Copy(io.Discard, resp.Body)

	return &Memory{
		UserID:    userID,
		Content:   content,
		Metadata:  meta,
		CreatedAt: time.Now().UTC(),
	}, nil
}

// Search runs a semantic query against memorus and returns up to limit results
// scoped to userID.
func (c *Client) Search(ctx context.Context, userID string, query string, limit int) ([]Memory, error) {
	params := url.Values{}
	params.Set("query", query)
	if userID != "" {
		params.Set("user_id", userID)
	}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/memories/search?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: create search request: %s", ErrUnavailable, err)
	}
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := classifyStatus(resp.StatusCode); err != nil {
		return nil, err
	}

	// Response shape: {"results": [...MemoryEntry]}
	var envelope struct {
		Results []struct {
			ID        string         `json:"id"`
			Memory    string         `json:"memory"`
			UserID    string         `json:"user_id"`
			Score     float64        `json:"score"`
			Metadata  map[string]any `json:"metadata"`
			CreatedAt string         `json:"created_at"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("%w: decode search response: %s", ErrUnavailable, err)
	}

	out := make([]Memory, 0, len(envelope.Results))
	for _, r := range envelope.Results {
		var createdAt time.Time
		if r.CreatedAt != "" {
			createdAt, _ = time.Parse(time.RFC3339, r.CreatedAt)
		}
		out = append(out, Memory{
			ID:        r.ID,
			UserID:    r.UserID,
			Content:   r.Memory,
			Metadata:  r.Metadata,
			Score:     r.Score,
			CreatedAt: createdAt,
		})
	}
	return out, nil
}

// Delete removes a single memory by its ID.
func (c *Client) Delete(ctx context.Context, memoryID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		c.baseURL+"/memories/"+memoryID, nil)
	if err != nil {
		return fmt.Errorf("%w: create delete request: %s", ErrUnavailable, err)
	}
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)

	return classifyStatus(resp.StatusCode)
}

// classifyStatus maps HTTP status codes to sentinel errors.
// 2xx → nil; 401 → ErrUnauthorized; 404 → ErrNotFound; other non-2xx → ErrUnavailable.
func classifyStatus(code int) error {
	if code >= 200 && code < 300 {
		return nil
	}
	switch code {
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusNotFound:
		return ErrNotFound
	default:
		return fmt.Errorf("%w: HTTP %d", ErrUnavailable, code)
	}
}

// warnIfBaseURLLooksPrefixless shouts at startup when BaseURL carries no path
// at all, e.g. "http://memorus:8880".
//
// Why a log and not an error: every call here does baseURL+"/memories" while
// the server nests those routes under /api/v1, so a prefixless base means
// every request 404s — and the failure is *silent*: recall simply returns
// nothing and the assistant looks merely unhelpful. Failing startup instead
// would be worse: memorus is an optional, degrade-gracefully dependency, and
// an operator legitimately may front it at a custom path that rewrites the
// prefix. So the rule is narrow — only "no path at all" is flagged, since that
// case cannot be a working deployment.
//
// Deliberately does not guess a fix (no auto-appending "/api/v1"): silently
// rewriting an operator's URL is how you get a second, harder bug.
func warnIfBaseURLLooksPrefixless(raw string) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		slog.Error("memorus: MEMORUS_BASE_URL is not a valid URL; every memory call will fail",
			"value", raw, "err", err)
		return
	}
	if p := strings.Trim(u.Path, "/"); p == "" {
		slog.Error("memorus: MEMORUS_BASE_URL has no path — it must include the API prefix, "+
			"otherwise every call 404s and recall silently returns nothing",
			"value", raw, "expected_suffix", "/api/v1",
			"example", "http://host.docker.internal:8880/api/v1")
	}
}

// SearchWithFilter is Search restricted to memories whose caller metadata
// matches every entry of metaEq exactly, e.g. {"subject_id": "partner:<uuid>"}
// (sent as metadata.<key>=<value>, which memorus turns into equality filters).
func (c *Client) SearchWithFilter(ctx context.Context, userID, query string, limit int, metaEq map[string]string) ([]Memory, error) {
	params := url.Values{}
	params.Set("query", query)
	if userID != "" {
		params.Set("user_id", userID)
	}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}
	for k, v := range metaEq {
		params.Set("metadata."+k, v)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/memories/search?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: create search request: %s", ErrUnavailable, err)
	}
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := classifyStatus(resp.StatusCode); err != nil {
		return nil, err
	}

	var envelope struct {
		Results []struct {
			ID        string         `json:"id"`
			Memory    string         `json:"memory"`
			UserID    string         `json:"user_id"`
			Score     float64        `json:"score"`
			Metadata  map[string]any `json:"metadata"`
			CreatedAt string         `json:"created_at"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("%w: decode search response: %s", ErrUnavailable, err)
	}

	out := make([]Memory, 0, len(envelope.Results))
	for _, r := range envelope.Results {
		var createdAt time.Time
		if r.CreatedAt != "" {
			createdAt, _ = time.Parse(time.RFC3339, r.CreatedAt)
		}
		out = append(out, Memory{
			ID:        r.ID,
			UserID:    r.UserID,
			Content:   r.Memory,
			Metadata:  r.Metadata,
			Score:     r.Score,
			CreatedAt: createdAt,
		})
	}
	return out, nil
}
