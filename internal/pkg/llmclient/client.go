// Package llmclient provides an OpenAI-compatible HTTP client routed through
// lurus newapi (newapi.lurus.cn). It never imports the openai SDK — all
// communication is plain HTTP/JSON per the cost-llm skill guidance.
//
// DeepSeek v4 trap defenses applied:
//  1. thinking disabled by default (prevents silent content="" with small max_tokens)
//  2. min max_tokens=1024 enforced
//  3. error.code classified (not HTTP status) to distinguish retryable vs deterministic
//  4. multi-turn tool calls: full assistant message passed back verbatim
//  5. tools and response_format never sent together
package llmclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultMaxTokens   = 1024
	defaultHTTPTimeout = 120 * time.Second
	sseDataPrefix      = "data: "
	sseDone            = "[DONE]"
)

// Message is a single chat message.
type Message struct {
	Role       string      `json:"role"`
	Content    interface{} `json:"content"` // string or []ContentPart
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
	Name       string      `json:"name,omitempty"`
	// ReasoningContent must be passed back verbatim in multi-turn tool calls (trap 4).
	ReasoningContent *string `json:"reasoning_content,omitempty"`
}

// Tool is an OpenAI-compatible function tool definition.
type Tool struct {
	Type     string      `json:"type"`
	Function FunctionDef `json:"function"`
}

// FunctionDef describes a callable function.
type FunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// ToolCall is a tool invocation returned by the model.
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction carries the name and arguments of a tool call.
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatRequest is the full request body for /v1/chat/completions.
type ChatRequest struct {
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	MaxTokens int       `json:"max_tokens"`
	Stream    bool      `json:"stream"`
	Tools     []Tool    `json:"tools,omitempty"`
	Thinking  *struct {
		Type string `json:"type"`
	} `json:"thinking,omitempty"`
}

// ChatResponse is the non-streaming response body.
type ChatResponse struct {
	ID      string   `json:"id"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
	Error   *APIErr  `json:"error,omitempty"`
}

// Choice is one completion choice.
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// Usage reports token consumption.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// APIErr is the error object returned inside a JSON 4xx/5xx body.
type APIErr struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Type    string `json:"type"`
}

func (e *APIErr) Error() string {
	return fmt.Sprintf("llmclient: API error code=%s message=%s", e.Code, e.Message)
}

// StreamDelta is one chunk from an SSE stream.
type StreamDelta struct {
	Content      string
	ToolCalls    []ToolCall
	FinishReason string
}

// Config holds the client configuration.
type Config struct {
	// BaseURL is the newapi base, e.g. "https://newapi.lurus.cn/v1". No trailing slash.
	BaseURL string
	// APIKey is the Bearer token for newapi.
	APIKey string
	// DefaultModel is the model name to use when none is specified in the request.
	// Defaults to "deepseek-v4-flash" when empty.
	DefaultModel string
	// HTTPTimeout overrides the per-request deadline. Defaults to 120s.
	HTTPTimeout time.Duration
}

// Client is the LLM API client.
type Client struct {
	baseURL      string
	apiKey       string
	defaultModel string
	http         *http.Client
}

// New creates a new Client. Returns an error if required config fields are missing.
func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("llmclient: BaseURL is required")
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("llmclient: APIKey is required")
	}
	model := cfg.DefaultModel
	if model == "" {
		model = "deepseek-v4-flash"
	}
	timeout := cfg.HTTPTimeout
	if timeout == 0 {
		timeout = defaultHTTPTimeout
	}
	return &Client{
		baseURL:      strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:       cfg.APIKey,
		defaultModel: model,
		http:         &http.Client{Timeout: timeout},
	}, nil
}

// Chat sends a non-streaming chat completion request.
// Traps applied: thinking disabled, max_tokens enforced ≥1024.
func (c *Client) Chat(ctx context.Context, model string, messages []Message, tools []Tool) (*ChatResponse, error) {
	if model == "" {
		model = c.defaultModel
	}
	req := c.buildRequest(model, messages, tools, false)
	return c.doChat(ctx, req)
}

// Stream sends a streaming chat completion and calls onDelta for each chunk.
// The caller should collect deltas to reconstruct the full content.
func (c *Client) Stream(ctx context.Context, model string, messages []Message, tools []Tool, onDelta func(StreamDelta)) error {
	if model == "" {
		model = c.defaultModel
	}
	req := c.buildRequest(model, messages, tools, true)
	return c.doStream(ctx, req, onDelta)
}

// buildRequest assembles a ChatRequest with safe defaults.
func (c *Client) buildRequest(model string, messages []Message, tools []Tool, stream bool) ChatRequest {
	maxTok := defaultMaxTokens
	req := ChatRequest{
		Model:     model,
		Messages:  messages,
		MaxTokens: maxTok,
		Stream:    stream,
		Thinking: &struct {
			Type string `json:"type"`
		}{Type: "disabled"}, // trap 1: always disable thinking for flash
	}
	if len(tools) > 0 {
		req.Tools = tools
		// trap 3: never send tools + response_format together (not applicable here since we
		// never send response_format in this path, but document the invariant).
	}
	return req
}

// doChat performs a non-streaming request and returns the parsed response.
func (c *Client) doChat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("llmclient: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("llmclient: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llmclient: http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("llmclient: read response body: %w", err)
	}

	var chat ChatResponse
	if err := json.Unmarshal(respBody, &chat); err != nil {
		return nil, fmt.Errorf("llmclient: unmarshal response (status=%d): %w", resp.StatusCode, err)
	}

	// trap 2: classify by error.code, not HTTP status
	if chat.Error != nil {
		return nil, classifyAPIError(resp.StatusCode, chat.Error)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("llmclient: unexpected HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return &chat, nil
}

// doStream performs an SSE streaming request and calls onDelta per chunk.
func (c *Client) doStream(ctx context.Context, req ChatRequest, onDelta func(StreamDelta)) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("llmclient: marshal stream request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("llmclient: create stream request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("llmclient: http stream request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		errBody, _ := io.ReadAll(resp.Body)
		var apiResp ChatResponse
		if jsonErr := json.Unmarshal(errBody, &apiResp); jsonErr == nil && apiResp.Error != nil {
			return classifyAPIError(resp.StatusCode, apiResp.Error)
		}
		return fmt.Errorf("llmclient: stream HTTP %d: %s", resp.StatusCode, string(errBody))
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, sseDataPrefix) {
			continue
		}
		data := strings.TrimPrefix(line, sseDataPrefix)
		if data == sseDone {
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string     `json:"content"`
					ToolCalls []ToolCall `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // skip malformed chunks
		}
		if len(chunk.Choices) > 0 {
			onDelta(StreamDelta{
				Content:      chunk.Choices[0].Delta.Content,
				ToolCalls:    chunk.Choices[0].Delta.ToolCalls,
				FinishReason: chunk.Choices[0].FinishReason,
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("llmclient: reading SSE stream: %w", err)
	}
	return nil
}

// LLMError is returned when the API returns a recognisable error code.
type LLMError struct {
	Code       string
	Message    string
	HTTPStatus int
	Retryable  bool
}

func (e *LLMError) Error() string {
	return fmt.Sprintf("llmclient: error code=%s http=%d retryable=%v: %s",
		e.Code, e.HTTPStatus, e.Retryable, e.Message)
}

// classifyAPIError maps newapi error codes to LLMError with correct Retryable flag.
// trap 2: newapi returns model_not_found as 503, invalid_request as 500 — branch on code.
func classifyAPIError(httpStatus int, apiErr *APIErr) *LLMError {
	e := &LLMError{
		Code:       apiErr.Code,
		Message:    apiErr.Message,
		HTTPStatus: httpStatus,
	}
	switch apiErr.Code {
	case "model_not_found":
		e.Retryable = false
	case "invalid_request", "invalid_request_error":
		e.Retryable = false
	default:
		if httpStatus == http.StatusUnauthorized {
			e.Retryable = false
		} else if httpStatus == http.StatusTooManyRequests {
			e.Retryable = true
		} else if httpStatus >= 500 {
			e.Retryable = true
		}
	}
	return e
}
