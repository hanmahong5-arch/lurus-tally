package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmclient"
)

type extractLLM struct {
	replies []string // one per call; "ERR" = transport error
	models  []string
}

func (s *extractLLM) Chat(_ context.Context, model string, _ []llmclient.Message, _ []llmclient.Tool) (*llmclient.ChatResponse, error) {
	s.models = append(s.models, model)
	i := len(s.models) - 1
	if i >= len(s.replies) || s.replies[i] == "ERR" {
		return nil, errors.New("boom")
	}
	return &llmclient.ChatResponse{Choices: []llmclient.Choice{{Message: llmclient.Message{Role: "assistant", Content: s.replies[i]}}}}, nil
}

const extractGoodReply = `{"lines":[{"product_name":"樱花","qty":"10"},{"product_name":"桃树","qty":20,"unit_price":"3.50"}]}`

func extractCount(outcome string) float64 {
	return testutil.ToFloat64(extractOps.WithLabelValues(outcome))
}

func TestExtract_NoRouter_UsesDefaultModel(t *testing.T) {
	llm := &extractLLM{replies: []string{extractGoodReply}}
	lines, err := NewExtractor(llm, nil, "default").Extract(context.Background(), uuid.New(), "樱花十棵，桃树二十棵")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || !lines[1].UnitPrice.Equal(dec("3.5")) || !lines[1].Qty.Equal(dec("20")) {
		t.Fatalf("lines=%+v", lines)
	}
	if len(llm.models) != 1 || llm.models[0] != "default" {
		t.Fatalf("models=%v", llm.models)
	}
}

func TestExtract_RoutedBadSchemaRetriesOnceThenSucceeds(t *testing.T) {
	llm := &extractLLM{replies: []string{"这是文字", extractGoodReply}}
	r := llmclient.NewRouter(map[llmclient.Task]string{llmclient.TaskExtract: "small"})
	beforeRetry, beforeFB := extractCount(extractRetry), extractCount(extractFallback)
	if _, err := NewExtractor(llm, r, "default").Extract(context.Background(), uuid.New(), "樱花十棵"); err != nil {
		t.Fatal(err)
	}
	if got := llm.models; len(got) != 2 || got[0] != "small" || got[1] != "small" {
		t.Fatalf("models=%v", got)
	}
	if extractCount(extractRetry)-beforeRetry != 1 || extractCount(extractFallback) != beforeFB {
		t.Fatal("retry counted once, no fallback")
	}
}

func TestExtract_RoutedFailsTwice_FallsBackToDefaultAndCounts(t *testing.T) {
	llm := &extractLLM{replies: []string{`{"lines":[]}`, `{"lines":[{"product_name":"樱花","qty":"0"}]}`, extractGoodReply}}
	r := llmclient.NewRouter(map[llmclient.Task]string{llmclient.TaskExtract: "small"})
	beforeFB := extractCount(extractFallback)
	lines, err := NewExtractor(llm, r, "default").Extract(context.Background(), uuid.New(), "樱花十棵")
	if err != nil || len(lines) != 2 {
		t.Fatalf("lines=%v err=%v", lines, err)
	}
	if got := llm.models; len(got) != 3 || got[2] != "default" {
		t.Fatalf("models=%v", got)
	}
	if extractCount(extractFallback)-beforeFB != 1 {
		t.Fatal("fallback must be counted once")
	}
}

func TestExtract_AllFail_ErrExtractFailed(t *testing.T) {
	llm := &extractLLM{replies: []string{"x", "x", "x"}}
	r := llmclient.NewRouter(map[llmclient.Task]string{llmclient.TaskExtract: "small"})
	before := extractCount(extractFailed)
	_, err := NewExtractor(llm, r, "default").Extract(context.Background(), uuid.New(), "樱花十棵")
	if !errors.Is(err, ErrExtractFailed) {
		t.Fatalf("err=%v", err)
	}
	if extractCount(extractFailed)-before != 1 {
		t.Fatal("failed must be counted")
	}
}

func TestExtract_TransportErrorSkipsRetryButFallsBack(t *testing.T) {
	llm := &extractLLM{replies: []string{"ERR", extractGoodReply}}
	r := llmclient.NewRouter(map[llmclient.Task]string{llmclient.TaskExtract: "small"})
	if _, err := NewExtractor(llm, r, "default").Extract(context.Background(), uuid.New(), "樱花十棵"); err != nil {
		t.Fatal(err)
	}
	if got := llm.models; len(got) != 2 || got[0] != "small" || got[1] != "default" {
		t.Fatalf("models=%v", got)
	}
}

func TestParseExtractReply_Schema(t *testing.T) {
	bad := []string{
		``, `[]`, `{"lines":"x"}`, `{"lines":[{"product_name":"","qty":"1"}]}`,
		`{"lines":[{"product_name":"a","qty":"-1"}]}`, `{"lines":[{"product_name":"a"}]}`,
		`{"lines":[{"product_name":"a","qty":"1","unit_price":"x"}]}`,
		`{"lines":[{"product_name":"a","qty":"1","unit_price":"-2"}]}`,
	}
	for _, b := range bad {
		if _, err := parseExtractReply(b); err == nil {
			t.Errorf("accepted %q", b)
		}
	}
	if _, err := parseExtractReply("```json\n" + extractGoodReply + "\n```"); err != nil {
		t.Errorf("fenced reply rejected: %v", err)
	}
}

func TestExtractChecked_FlagsWrongQuantityAndTotal(t *testing.T) {
	// The model read 一千二 as 12 and the lines do not sum to the spoken total.
	reply := `{"lines":[{"product_name":"樱花","qty":"12","unit_price":"3"}]}`
	llm := &extractLLM{replies: []string{reply}}
	_, flags, err := NewExtractor(llm, nil, "default").ExtractChecked(context.Background(), uuid.New(), "樱花一千二棵三块，一共三千六百元")
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]bool{}
	for _, f := range flags {
		codes[f.Code] = true
	}
	if !codes[FlagQtyMismatch] || !codes[FlagTotalMismatch] {
		t.Fatalf("flags=%+v", flags)
	}
}

func TestExtract_RetryCountedOnlyWhenRetrying(t *testing.T) {
	before := extractCount(extractRetry)
	llm := &extractLLM{replies: []string{"bad", "bad", extractGoodReply}}
	ex := NewExtractor(llm, llmclient.NewRouter(map[llmclient.Task]string{llmclient.TaskExtract: "small"}), "big")
	if _, err := ex.Extract(context.Background(), uuid.New(), "樱花十棵"); err != nil {
		t.Fatal(err)
	}
	if got := extractCount(extractRetry) - before; got != 1 {
		t.Fatalf("retry counted %v times for one real retry", got)
	}
}
