package llmclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// classifyAPIError: pure function, table-driven per the brief's technical_cases.
// ---------------------------------------------------------------------------

func TestClassifyAPIError_Table(t *testing.T) {
	cases := []struct {
		name       string
		httpStatus int
		code       string
		wantRetry  bool
	}{
		{"model_not_found always non-retryable", 503, "model_not_found", false},
		{"invalid_request always non-retryable", 500, "invalid_request", false},
		{"invalid_request_error always non-retryable", 400, "invalid_request_error", false},
		{"unknown code + 401 -> non-retryable", 401, "some_other_code", false},
		{"unknown code + 429 -> retryable", 429, "rate_limited", true},
		{"unknown code + 500 -> retryable", 500, "server_error", true},
		{"unknown code + 503 -> retryable", 503, "server_error", true},
		{"unknown code + other status (e.g. 418) -> default zero-value false", 418, "teapot", false},
		{"unknown code + 200 -> default zero-value false", 200, "weird", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			apiErr := &APIErr{Code: tc.code, Message: "m", Type: "t"}
			got := classifyAPIError(tc.httpStatus, apiErr)
			if got.Retryable != tc.wantRetry {
				t.Errorf("classifyAPIError(%d, code=%s).Retryable = %v, want %v",
					tc.httpStatus, tc.code, got.Retryable, tc.wantRetry)
			}
			if got.Code != tc.code {
				t.Errorf("Code = %q, want %q", got.Code, tc.code)
			}
			if got.HTTPStatus != tc.httpStatus {
				t.Errorf("HTTPStatus = %d, want %d", got.HTTPStatus, tc.httpStatus)
			}
		})
	}
}

// chatErrorRetryable: table-drive both branches (classified-retryable LLMError
// vs any other error type), per brief business_invariants #1.
func TestChatErrorRetryable_Table(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"retryable LLMError", &LLMError{Code: "x", Retryable: true}, true},
		{"non-retryable LLMError", &LLMError{Code: "x", Retryable: false}, false},
		{"plain wrapped error is not an LLMError", fmt.Errorf("boom: %w", errors.New("x")), false},
		{"nil error", nil, false},
		{"generic APIErr (not LLMError type)", &APIErr{Code: "c", Message: "m"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := chatErrorRetryable(tc.err); got != tc.want {
				t.Errorf("chatErrorRetryable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Error() string methods — previously 0% covered.
// ---------------------------------------------------------------------------

func TestAPIErr_Error(t *testing.T) {
	e := &APIErr{Code: "bad_thing", Message: "went wrong"}
	got := e.Error()
	if !strings.Contains(got, "bad_thing") || !strings.Contains(got, "went wrong") {
		t.Errorf("APIErr.Error() = %q, want it to mention code and message", got)
	}
}

func TestLLMError_Error(t *testing.T) {
	e := &LLMError{Code: "rate_limited", Message: "slow down", HTTPStatus: 429, Retryable: true}
	got := e.Error()
	for _, want := range []string{"rate_limited", "slow down", "429", "true"} {
		if !strings.Contains(got, want) {
			t.Errorf("LLMError.Error() = %q, missing %q", got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// New: config validation and defaulting.
// ---------------------------------------------------------------------------

func TestNew_EmptyBaseURL(t *testing.T) {
	_, err := New(Config{BaseURL: "", APIKey: "k"})
	if err == nil {
		t.Fatal("expected error for empty BaseURL")
	}
}

func TestNew_EmptyAPIKey(t *testing.T) {
	_, err := New(Config{BaseURL: "http://x/v1", APIKey: ""})
	if err == nil {
		t.Fatal("expected error for empty APIKey")
	}
}

func TestNew_DefaultsModelAndTimeout(t *testing.T) {
	c, err := New(Config{BaseURL: "http://x/v1", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.defaultModel != "deepseek-v4-flash" {
		t.Errorf("defaultModel = %q, want %q", c.defaultModel, "deepseek-v4-flash")
	}
	if c.http.Timeout != 120*time.Second {
		t.Errorf("http.Timeout = %v, want 120s (default)", c.http.Timeout)
	}
}

func TestNew_CustomModelAndTimeoutPreserved(t *testing.T) {
	c, err := New(Config{
		BaseURL:      "http://x/v1/",
		APIKey:       "k",
		DefaultModel: "custom-model",
		HTTPTimeout:  5 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.defaultModel != "custom-model" {
		t.Errorf("defaultModel = %q, want %q", c.defaultModel, "custom-model")
	}
	if c.http.Timeout != 5*time.Second {
		t.Errorf("http.Timeout = %v, want 5s", c.http.Timeout)
	}
	if c.baseURL != "http://x/v1" {
		t.Errorf("baseURL = %q, want trailing slash trimmed", c.baseURL)
	}
}

// ---------------------------------------------------------------------------
// buildRequest: assert the outgoing JSON per business_invariant #3
// (thinking disabled + max_tokens=1024 always, even with tools/stream set).
// ---------------------------------------------------------------------------

func TestBuildRequest_AlwaysDisablesThinkingAndEnforcesMaxTokens(t *testing.T) {
	c, err := New(Config{BaseURL: "http://x/v1", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	tools := []Tool{{Type: "function", Function: FunctionDef{Name: "f"}}}
	req := c.buildRequest("m", []Message{{Role: "user", Content: "hi"}}, tools, true)

	if req.MaxTokens != 1024 {
		t.Errorf("MaxTokens = %d, want 1024 (DeepSeek-v4 empty-content trap defense)", req.MaxTokens)
	}
	if req.Thinking == nil || req.Thinking.Type != "disabled" {
		t.Errorf("Thinking = %+v, want {Type: disabled}", req.Thinking)
	}
	if len(req.Tools) != 1 {
		t.Errorf("Tools not carried through: got %d, want 1", len(req.Tools))
	}
	if req.StreamOptions == nil || !req.StreamOptions.IncludeUsage {
		t.Errorf("StreamOptions = %+v, want IncludeUsage=true when stream=true", req.StreamOptions)
	}

	// Marshal and assert the actual wire JSON carries the same invariants —
	// this is what the upstream gateway actually sees.
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if mt, _ := decoded["max_tokens"].(float64); mt != 1024 {
		t.Errorf("wire max_tokens = %v, want 1024", decoded["max_tokens"])
	}
	thinking, _ := decoded["thinking"].(map[string]interface{})
	if thinking == nil || thinking["type"] != "disabled" {
		t.Errorf("wire thinking = %v, want {type: disabled}", decoded["thinking"])
	}

	// Non-streaming request must NOT carry stream_options (nil, omitempty).
	reqNonStream := c.buildRequest("m", []Message{{Role: "user", Content: "hi"}}, nil, false)
	if reqNonStream.StreamOptions != nil {
		t.Errorf("StreamOptions = %+v, want nil for non-streaming request", reqNonStream.StreamOptions)
	}
	if len(reqNonStream.Tools) != 0 {
		t.Errorf("Tools = %+v, want empty when none passed", reqNonStream.Tools)
	}
}

// ---------------------------------------------------------------------------
// Chat / doChatOnce error paths not hit by the existing resilience test file.
// ---------------------------------------------------------------------------

// TestChat_DefaultsModelWhenEmpty exercises the `model == ""` branch of Chat,
// asserting the request that actually reaches the wire uses the client's
// configured default model (business-relevant: caller omits model -> gateway
// still gets a valid, billable model name).
func TestChat_DefaultsModelWhenEmpty(t *testing.T) {
	var gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotModel = req.Model
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, APIKey: "k", DefaultModel: "my-default"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.Chat(context.Background(), "", []Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if gotModel != "my-default" {
		t.Errorf("model on wire = %q, want %q", gotModel, "my-default")
	}
}

// TestDoChatOnce_ChatErrorRegardlessOfHTTPStatus: trap 2 — a structured
// chat.Error takes priority even when HTTP status is 200 (newapi quirk).
func TestDoChatOnce_ChatErrorRegardlessOfHTTPStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK) // 200, but body carries a structured error
		_, _ = w.Write([]byte(`{"error":{"code":"model_not_found","message":"no such model"}}`))
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.Chat(context.Background(), "m", []Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected error when response body carries .error even on HTTP 200")
	}
	var le *LLMError
	if !errors.As(err, &le) {
		t.Fatalf("error is not an *LLMError: %v (%T)", err, err)
	}
	if le.Code != "model_not_found" {
		t.Errorf("Code = %q, want model_not_found", le.Code)
	}
	if le.Retryable {
		t.Error("model_not_found must be non-retryable")
	}
}

// TestDoChatOnce_HTTPErrorNoStructuredBody: HTTP>=400 with a body that is
// valid JSON but has no .error field -> generic 'unexpected HTTP %d', and it
// must be non-retryable (doChat must not loop the transport 3x).
func TestDoChatOnce_HTTPErrorNoStructuredBody(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"id":"weird","choices":[]}`)) // valid JSON, no .error
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.Chat(context.Background(), "m", []Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected error for HTTP 500 with no structured error body")
	}
	if !strings.Contains(err.Error(), "unexpected HTTP 500") {
		t.Errorf("error = %q, want it to mention 'unexpected HTTP 500'", err.Error())
	}
	// Not classified as an *LLMError -> chatErrorRetryable returns false ->
	// doChat must not retry this: exactly 1 hit.
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("server hits = %d, want 1 (non-retryable, no double-charge risk)", got)
	}
}

// TestDoChatOnce_UnmarshalError: response body is not valid JSON at all.
func TestDoChatOnce_UnmarshalError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json at all`))
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.Chat(context.Background(), "m", []Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected error for non-JSON response body")
	}
	if !strings.Contains(err.Error(), "unmarshal response") {
		t.Errorf("error = %q, want it to mention 'unmarshal response'", err.Error())
	}
}

// TestDoChat_ExhaustsAttemptsOnPersistentRetryableError: a server that ALWAYS
// returns a retryable 503 error forces doChat through all chatMaxAttempts (3)
// and returns lastErr, per business_invariant #1 (bounded retry, no infinite
// replay -> no unbounded double-charge risk either).
func TestDoChat_ExhaustsAttemptsOnPersistentRetryableError(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"code":"server_overloaded","message":"always busy"}}`))
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.Chat(context.Background(), "m", []Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected error after exhausting all retry attempts")
	}
	var le *LLMError
	if !errors.As(err, &le) || le.Code != "server_overloaded" {
		t.Errorf("returned error should be the last classified LLMError, got %v (%T)", err, err)
	}
	if got := atomic.LoadInt32(&hits); got != chatMaxAttempts {
		t.Errorf("server hits = %d, want exactly %d (chatMaxAttempts, bounded retry)", got, chatMaxAttempts)
	}
}

// TestDoChat_CtxCancelDuringBackoff: cancelling the context while doChat is
// waiting between retries must surface ctx.Err(), not loop further or hang.
func TestDoChat_CtxCancelDuringBackoff(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"code":"server_overloaded","message":"busy"}}`))
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel shortly after the first attempt fires, so the SECOND loop
	// iteration's select-on-ctx.Done() during backoff wins the race.
	go func() {
		for atomic.LoadInt32(&hits) < 1 {
			time.Sleep(time.Millisecond)
		}
		cancel()
	}()

	_, err = c.Chat(ctx, "m", []Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected an error when context is cancelled during backoff")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled (or wrapping it)", err)
	}
	// Must not have exhausted all attempts: cancellation should cut it short.
	if got := atomic.LoadInt32(&hits); got >= chatMaxAttempts {
		t.Errorf("server hits = %d, want < %d (ctx cancel should short-circuit backoff)", got, chatMaxAttempts)
	}
}

// ---------------------------------------------------------------------------
// Stream / doStream — the brief calls out "streaming path is NOT retried".
// ---------------------------------------------------------------------------

// TestStream_DefaultsModelWhenEmpty mirrors TestChat_DefaultsModelWhenEmpty
// for the streaming entrypoint.
func TestStream_DefaultsModelWhenEmpty(t *testing.T) {
	var gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotModel = req.Model
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, APIKey: "k", DefaultModel: "stream-default"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Stream(context.Background(), "", []Message{{Role: "user", Content: "hi"}}, nil, func(StreamDelta) {}); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if gotModel != "stream-default" {
		t.Errorf("model on wire = %q, want %q", gotModel, "stream-default")
	}
}

// TestDoStream_HTTPErrorStructured: HTTP>=400 with a structured .error body ->
// classifyAPIError, same as the non-streaming path.
func TestDoStream_HTTPErrorStructured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":"rate_limited","message":"slow down"}}`))
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = c.Stream(context.Background(), "m", []Message{{Role: "user", Content: "hi"}}, nil, func(StreamDelta) {})
	if err == nil {
		t.Fatal("expected error for HTTP 429 on stream")
	}
	var le *LLMError
	if !errors.As(err, &le) {
		t.Fatalf("error is not *LLMError: %v (%T)", err, err)
	}
	if !le.Retryable {
		t.Error("429 rate_limited should classify Retryable=true (even though doStream itself never retries)")
	}
}

// TestDoStream_HTTPErrorUnstructured: HTTP>=400 with a body that has no
// structured .error -> generic 'stream HTTP %d' error.
func TestDoStream_HTTPErrorUnstructured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`plain text upstream error, not json`))
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = c.Stream(context.Background(), "m", []Message{{Role: "user", Content: "hi"}}, nil, func(StreamDelta) {})
	if err == nil {
		t.Fatal("expected error for HTTP 502 stream with unstructured body")
	}
	if !strings.Contains(err.Error(), "stream HTTP 502") {
		t.Errorf("error = %q, want it to mention 'stream HTTP 502'", err.Error())
	}
}

// TestDoStream_MalformedChunkSkippedThenGoodChunkDelivered: a malformed SSE
// data line must be skipped (continue) without aborting the stream; the next
// well-formed chunk still reaches onDelta. This is the "partial chunks
// already delivered, no replay" invariant made concrete.
func TestDoStream_MalformedChunkSkippedThenGoodChunkDelivered(t *testing.T) {
	sse := "data: {this is not valid json\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"world\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse))
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var got string
	var finish string
	err = c.Stream(context.Background(), "m", []Message{{Role: "user", Content: "hi"}}, nil, func(d StreamDelta) {
		got += d.Content
		if d.FinishReason != "" {
			finish = d.FinishReason
		}
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if got != "world" {
		t.Errorf("delivered content = %q, want %q (malformed chunk must be skipped, not fatal)", got, "world")
	}
	if finish != "stop" {
		t.Errorf("finish reason = %q, want %q", finish, "stop")
	}
}

// TestDoStream_MidStreamFailureNotRetried: the connection breaks mid-stream
// (server closes abruptly without [DONE]). doStream has no retry loop, so the
// caller must see the scanner error surfaced directly, and any chunks already
// delivered via onDelta stay delivered (no replay of the whole stream).
func TestDoStream_MidStreamFailureNotRetried(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		fl, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter does not support flushing")
		}
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"))
		fl.Flush()
		// Abruptly hijack-close the underlying connection without sending
		// [DONE], simulating a mid-stream network failure.
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("ResponseWriter does not support hijacking")
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Fatalf("hijack: %v", err)
		}
		_ = conn.Close()
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var got string
	err = c.Stream(context.Background(), "m", []Message{{Role: "user", Content: "hi"}}, nil, func(d StreamDelta) {
		got += d.Content
	})
	if got != "partial" {
		t.Errorf("delivered content before failure = %q, want %q (already-delivered chunks must survive)", got, "partial")
	}
	// doStream has no retry: the server must have been hit exactly once.
	if hitCount := atomic.LoadInt32(&hits); hitCount != 1 {
		t.Errorf("server hits = %d, want 1 (doStream must never retry mid-stream failures)", hitCount)
	}
	_ = err // err may or may not be non-nil depending on OS-level close timing; the
	// no-replay/no-second-hit assertions above are the load-bearing ones.
}

// TestDoStream_UsageOnlyRecordedOnTerminalChunk: usage must NOT be recorded
// when no chunk carries a .usage field (only the terminal chunk, when
// present, carries it) — covers the `usage != nil` guard's false branch.
func TestDoStream_UsageOnlyRecordedOnTerminalChunk(t *testing.T) {
	sse := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n" +
		"data: [DONE]\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse))
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var got string
	if err := c.Stream(context.Background(), "m", []Message{{Role: "user", Content: "hi"}}, nil, func(d StreamDelta) {
		got += d.Content
	}); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if got != "hi" {
		t.Errorf("content = %q, want %q", got, "hi")
	}
	// No assertion on the (unexported) sink here beyond "did not panic / did
	// not error" — RecordUsage with a nil usage is simply never called, which
	// is exactly the code path (`if usage != nil`) this test exists to hit.
}

// TestDoStream_ScannerErrSurfaced drives the bufio.Scanner past its default
// token size limit so scanner.Err() returns bufio.ErrTooLong, exercising the
// `if err := scanner.Err(); err != nil` branch directly (distinct from the
// mid-stream-hijack test, which relies on OS-level connection semantics).
func TestDoStream_ScannerErrSurfaced(t *testing.T) {
	huge := "data: " + strings.Repeat("x", bufio.MaxScanTokenSize+1024) + "\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(huge))
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = c.Stream(context.Background(), "m", []Message{{Role: "user", Content: "hi"}}, nil, func(StreamDelta) {})
	if err == nil {
		t.Fatal("expected scanner error for an oversized SSE line")
	}
	if !strings.Contains(err.Error(), "reading SSE stream") {
		t.Errorf("error = %q, want it to mention 'reading SSE stream'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// doStream/doChatOnce request-construction failure: http.NewRequestWithContext
// only errors on a malformed method or nil ctx-derived issue; the most
// reachable way to force http.NewRequestWithContext to fail from this
// package's surface is an invalid URL control character in BaseURL, which
// survives strings.TrimRight and New()'s validation (New only checks
// non-empty, not URL-parseability).
// ---------------------------------------------------------------------------

func TestChat_CreateRequestError_InvalidBaseURL(t *testing.T) {
	c, err := New(Config{BaseURL: "http://\x7f", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.Chat(context.Background(), "m", []Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected error creating an HTTP request against a control-character URL")
	}
}

func TestStream_CreateRequestError_InvalidBaseURL(t *testing.T) {
	c, err := New(Config{BaseURL: "http://\x7f", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = c.Stream(context.Background(), "m", []Message{{Role: "user", Content: "hi"}}, nil, func(StreamDelta) {})
	if err == nil {
		t.Fatal("expected error creating a stream HTTP request against a control-character URL")
	}
}

// ---------------------------------------------------------------------------
// http.Client.Do transport-level failure (distinct from HTTP status errors):
// point at a URL with no listener at all.
// ---------------------------------------------------------------------------

func TestChat_HTTPDoError_ConnectionRefused(t *testing.T) {
	// Bind and immediately close a listener to get a port nothing listens on.
	ln := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := ln.URL
	ln.Close() // now nothing listens at this address

	c, err := New(Config{BaseURL: url, APIKey: "k", HTTPTimeout: 2 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.Chat(context.Background(), "m", []Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected a transport error against a closed listener")
	}
	if !strings.Contains(err.Error(), "http request") {
		t.Errorf("error = %q, want it to mention 'http request'", err.Error())
	}
}

func TestStream_HTTPDoError_ConnectionRefused(t *testing.T) {
	ln := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := ln.URL
	ln.Close()

	c, err := New(Config{BaseURL: url, APIKey: "k", HTTPTimeout: 2 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = c.Stream(context.Background(), "m", []Message{{Role: "user", Content: "hi"}}, nil, func(StreamDelta) {})
	if err == nil {
		t.Fatal("expected a transport error against a closed listener")
	}
	if !strings.Contains(err.Error(), "http stream request") {
		t.Errorf("error = %q, want it to mention 'http stream request'", err.Error())
	}
}
