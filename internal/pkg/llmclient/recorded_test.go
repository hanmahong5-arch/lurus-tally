package llmclient

// Contract tests against RECORDED gateway traffic. The other tests in this
// package feed hand-written bodies; these replay what newapi (routing to
// deepseek-v4-flash, the production model) actually answered, so a drift
// between our structs and the real wire format — or a request the real
// gateway would reject — shows up here.
//
// Each scenario is run twice: once live to record (record_test.go,
// `-tags=record`, needs NEWAPI_BASE_URL + NEWAPI_API_KEY), and in every normal
// `go test` against the recording. Replay also checks that the client still
// sends the exact request bodies the gateway accepted.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const recordedModel = "deepseek-v4-flash"

// exchange is one recorded HTTP round trip. The Authorization header is never
// stored.
type exchange struct {
	Path        string          `json:"path"`
	Request     json.RawMessage `json:"request"`
	Status      int             `json:"status"`
	ContentType string          `json:"content_type"`
	Body        string          `json:"body"`
}

type recordedScenario struct {
	name string
	// badKey runs the scenario with a key the gateway must refuse.
	badKey bool
	run    func(t *testing.T, c *Client)
}

var shopSystem = Message{Role: "system", Content: "你是店铺助手，回答尽量简短。"}

var stockTool = Tool{Type: "function", Function: FunctionDef{
	Name:        "query_stock",
	Description: "查询某个商品的当前库存",
	Parameters: json.RawMessage(`{"type":"object","properties":{"product":{"type":"string","description":"商品名"}},` +
		`"required":["product"]}`),
}}

func recordedScenarios() []recordedScenario {
	sum := []Message{shopSystem, {Role: "user", Content: "一箱矿泉水 24 瓶，3 箱一共多少瓶？只回答数字。"}}
	return []recordedScenario{
		{name: "plain", run: func(t *testing.T, c *Client) {
			resp, err := c.Chat(context.Background(), "", sum, nil)
			if err != nil {
				t.Fatalf("Chat: %v", err)
			}
			if len(resp.Choices) != 1 {
				t.Fatalf("choices = %d, want 1", len(resp.Choices))
			}
			ch := resp.Choices[0]
			content, _ := ch.Message.Content.(string)
			// Trap 1: with thinking left on, a small budget comes back as "".
			if !strings.Contains(content, "72") {
				t.Errorf("content = %q, want the answer 72", content)
			}
			if ch.FinishReason != "stop" {
				t.Errorf("finish_reason = %q, want stop", ch.FinishReason)
			}
			if resp.Usage.PromptTokens == 0 || resp.Usage.CompletionTokens == 0 {
				t.Errorf("usage not parsed: %+v", resp.Usage)
			}
		}},
		{name: "tool_round_trip", run: func(t *testing.T, c *Client) {
			msgs := []Message{shopSystem, {Role: "user", Content: "可乐还有多少库存？"}}
			resp, err := c.Chat(context.Background(), "", msgs, []Tool{stockTool})
			if err != nil {
				t.Fatalf("Chat (tool call): %v", err)
			}
			ch := resp.Choices[0]
			if ch.FinishReason != "tool_calls" || len(ch.Message.ToolCalls) == 0 {
				t.Fatalf("want a tool call, got finish=%q message=%+v", ch.FinishReason, ch.Message)
			}
			tc := ch.Message.ToolCalls[0]
			var args struct {
				Product string `json:"product"`
			}
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				t.Fatalf("arguments are not JSON: %q", tc.Function.Arguments)
			}
			if tc.ID == "" || tc.Type != "function" || tc.Function.Name != "query_stock" || !strings.Contains(args.Product, "可乐") {
				t.Fatalf("tool call = %+v", tc)
			}
			// Trap 4: the assistant message goes back verbatim, then the result.
			msgs = append(msgs, ch.Message, Message{
				Role: "tool", Content: `{"product":"可乐","stock":37,"unit":"瓶"}`,
				ToolCallID: tc.ID, Name: tc.Function.Name,
			})
			resp, err = c.Chat(context.Background(), "", msgs, []Tool{stockTool})
			if err != nil {
				t.Fatalf("Chat (tool result): %v", err)
			}
			final := resp.Choices[0]
			content, _ := final.Message.Content.(string)
			if len(final.Message.ToolCalls) != 0 || !strings.Contains(content, "37") {
				t.Errorf("final answer = %q (tool calls %d), want it to use the stock 37", content, len(final.Message.ToolCalls))
			}
		}},
		{name: "stream", run: func(t *testing.T, c *Client) {
			var text strings.Builder
			var finish string
			err := c.Stream(context.Background(), "", sum, nil, func(d StreamDelta) {
				text.WriteString(d.Content)
				if d.FinishReason != "" {
					finish = d.FinishReason
				}
			})
			if err != nil {
				t.Fatalf("Stream: %v", err)
			}
			if !strings.Contains(text.String(), "72") || finish != "stop" {
				t.Errorf("streamed %q finish=%q, want 72 / stop", text.String(), finish)
			}
		}},
		{name: "unknown_model", run: func(t *testing.T, c *Client) {
			_, err := c.Chat(context.Background(), "no-such-model-recorded", sum, nil)
			assertNotRetryable(t, err)
		}},
		{name: "bad_key", badKey: true, run: func(t *testing.T, c *Client) {
			_, err := c.Chat(context.Background(), "", sum, nil)
			assertNotRetryable(t, err)
		}},
	}
}

// assertNotRetryable: a deterministic refusal must surface as a classified,
// non-retryable LLMError — otherwise doChat replays it three times.
func assertNotRetryable(t *testing.T, err error) {
	t.Helper()
	var le *LLMError
	if !errors.As(err, &le) {
		t.Fatalf("err = %v (%T), want *LLMError", err, err)
	}
	if le.Retryable {
		t.Errorf("gateway refusal classified retryable: %+v", le)
	}
}

func recordingPath(name string) string {
	return filepath.Join("testdata", "recorded", name+".json")
}

func TestRecordedGatewayContract(t *testing.T) {
	for _, sc := range recordedScenarios() {
		t.Run(sc.name, func(t *testing.T) {
			raw, err := os.ReadFile(recordingPath(sc.name))
			if err != nil {
				t.Fatalf("no recording (run go test -tags=record -run TestRecordGateway): %v", err)
			}
			var ex []exchange
			if err := json.Unmarshal(raw, &ex); err != nil {
				t.Fatal(err)
			}
			rp := &replayer{t: t, ex: ex}
			c := newTestClient(t, rp)
			c.defaultModel = recordedModel
			sc.run(t, c)
			if rp.i != len(ex) {
				t.Errorf("client made %d requests, the recording has %d", rp.i, len(ex))
			}
		})
	}
}

// replayer serves recorded responses in order and fails the test when the
// client sends a request body the gateway did not see at record time.
type replayer struct {
	t  *testing.T
	ex []exchange
	i  int
}

func (p *replayer) RoundTrip(r *http.Request) (*http.Response, error) {
	if p.i >= len(p.ex) {
		return nil, fmt.Errorf("replayer: unexpected request #%d", p.i+1)
	}
	e := p.ex[p.i]
	p.i++
	body, _ := io.ReadAll(r.Body)
	if !strings.HasSuffix(r.URL.Path, e.Path) {
		p.t.Errorf("request #%d path %s, recorded %s", p.i, r.URL.Path, e.Path)
	}
	if !jsonEqual(body, e.Request) {
		p.t.Errorf("request #%d body drifted from the recorded one:\n sent     %s\n recorded %s", p.i, body, e.Request)
	}
	return &http.Response{
		StatusCode: e.Status,
		Header:     http.Header{"Content-Type": []string{e.ContentType}},
		Body:       io.NopCloser(bytes.NewReader([]byte(e.Body))),
		Request:    r,
	}, nil
}

func jsonEqual(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}
