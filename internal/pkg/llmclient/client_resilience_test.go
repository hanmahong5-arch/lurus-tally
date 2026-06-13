package llmclient

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmgateway"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func newTestClient(t *testing.T, rt http.RoundTripper) *Client {
	t.Helper()
	c, err := New(Config{BaseURL: "http://test.local/v1", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.http = &http.Client{Transport: rt}
	return c
}

func httpResp(code int, body string) *http.Response {
	return &http.Response{
		StatusCode: code,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

// TestDoChat_RetriesOnRetryableThenSucceeds: a non-streaming call that gets two
// retryable 5xx API errors then a 200 succeeds, after exactly 3 attempts —
// consuming the LLMError.Retryable classification.
func TestDoChat_RetriesOnRetryableThenSucceeds(t *testing.T) {
	var calls int32
	stub := roundTripFunc(func(*http.Request) (*http.Response, error) {
		if atomic.AddInt32(&calls, 1) < 3 {
			return httpResp(http.StatusServiceUnavailable,
				`{"error":{"code":"server_overloaded","message":"try later"}}`), nil
		}
		return httpResp(http.StatusOK,
			`{"id":"c1","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`), nil
	})
	c := newTestClient(t, stub)

	resp, err := c.Chat(context.Background(), "", []Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content != "ok" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("transport attempts: got %d, want 3 (1 + 2 retries)", got)
	}
}

// TestDoChat_DoesNotRetryNonRetryable: a 4xx invalid_request is classified
// non-retryable and must NOT be replayed.
func TestDoChat_DoesNotRetryNonRetryable(t *testing.T) {
	var calls int32
	stub := roundTripFunc(func(*http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return httpResp(http.StatusBadRequest,
			`{"error":{"code":"invalid_request","message":"bad input"}}`), nil
	})
	c := newTestClient(t, stub)

	if _, err := c.Chat(context.Background(), "", []Message{{Role: "user", Content: "hi"}}, nil); err == nil {
		t.Fatal("expected an error for invalid_request")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("non-retryable error replayed: got %d attempts, want 1", got)
	}
}

// TestDoStream_RecordsUsage: a streaming response that carries a terminal usage
// chunk drives RecordUsage just like the non-streaming path (handler.go :247
// parity). Observed via the public Prometheus gatherer on a unique tenant.
func TestDoStream_RecordsUsage(t *testing.T) {
	const tenant = "test-stream-usage-tenant"
	ctx := llmgateway.WithTenant(context.Background(), tenant)

	sse := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n" +
		"data: [DONE]\n\n"
	stub := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return httpResp(http.StatusOK, sse), nil
	})
	c := newTestClient(t, stub)

	inBefore := sumTokens(t, tenant, "in")
	outBefore := sumTokens(t, tenant, "out")

	var got string
	err := c.Stream(ctx, "deepseek-v4-flash", []Message{{Role: "user", Content: "hi"}}, nil,
		func(d StreamDelta) { got += d.Content })
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if got != "hi" {
		t.Errorf("streamed content: got %q, want %q", got, "hi")
	}
	if d := sumTokens(t, tenant, "in") - inBefore; d != 10 {
		t.Errorf("prompt tokens recorded: delta %v, want 10", d)
	}
	if d := sumTokens(t, tenant, "out") - outBefore; d != 5 {
		t.Errorf("completion tokens recorded: delta %v, want 5", d)
	}
}

// sumTokens reads the current tally_llm_tokens_total counter for a tenant +
// direction across all models, via the default gatherer (public surface).
func sumTokens(t *testing.T, tenant, direction string) float64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	var sum float64
	for _, mf := range mfs {
		if mf.GetName() != "tally_llm_tokens_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			var tn, dir string
			for _, lp := range m.GetLabel() {
				switch lp.GetName() {
				case "tenant":
					tn = lp.GetValue()
				case "direction":
					dir = lp.GetValue()
				}
			}
			if tn == tenant && dir == direction {
				sum += m.GetCounter().GetValue()
			}
		}
	}
	return sum
}
