package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmclient"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmgateway"
)

// extract.go is the "dictation -> document lines" step as its own JSON call,
// separate from the tool-calling chat loop, so it can run on a smaller model
// (AI_MODEL_EXTRACT). The model only proposes lines; CheckSpokenQuantities and
// friends decide whether to trust them. A reply that fails the schema is
// retried once, then the call falls back to the default model; each fallback is
// counted so a too-small model shows up on /internal/v1/metrics.

var extractOps = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "tally_ai_extract_total",
		Help: "Dictation line extractions by outcome (ok|retry|fallback|failed).",
	},
	[]string{"outcome"},
)

func init() {
	prometheus.MustRegister(extractOps)
}

const (
	extractOK       = "ok"
	extractRetry    = "retry"
	extractFallback = "fallback"
	extractFailed   = "failed"
)

// extractPrompt pins the output to one JSON object. Static, so it stays cacheable.
const extractPrompt = `You turn a shop owner's dictated order into document lines.
Reply with ONE JSON object and nothing else (no prose, no code fence):
{"lines":[{"product_name":"<name as said>","qty":"<positive decimal string>","unit_price":"<decimal string or omit>"}]}
Rules: one line per product; qty is the number of units said, written as Arabic digits (一千二 = 1200, 两箱半 = 2.5, 各十棵 = 10 for every product named); never invent products, quantities or prices that were not said; omit unit_price when no price was said.`

// ExtractedLine is one validated line of an extraction.
type ExtractedLine struct {
	ProductName string
	Qty         decimal.Decimal
	UnitPrice   *decimal.Decimal
}

// ErrExtractFailed is returned when neither the routed nor the default model
// produced a schema-valid reply.
var ErrExtractFailed = errors.New("extract: no schema-valid reply")

var errBadSchema = errors.New("reply does not match the line schema")

// chatCaller is the slice of *llmclient.Client the extractor needs.
type chatCaller interface {
	Chat(ctx context.Context, model string, messages []llmclient.Message, tools []llmclient.Tool) (*llmclient.ChatResponse, error)
}

// Extractor runs the extraction call.
type Extractor struct {
	llm          chatCaller
	router       *llmclient.Router
	defaultModel string
}

// NewExtractor builds an Extractor. A nil router (or one without an extract
// route) runs everything on defaultModel.
func NewExtractor(llm chatCaller, router *llmclient.Router, defaultModel string) *Extractor {
	return &Extractor{llm: llm, router: router, defaultModel: defaultModel}
}

// Extract returns the lines the model read out of the dictation.
func (e *Extractor) Extract(ctx context.Context, tenantID uuid.UUID, spoken string) ([]ExtractedLine, error) {
	ctx = llmgateway.WithTenant(ctx, tenantID.String())
	routed := e.router.Model(llmclient.TaskExtract, e.defaultModel)

	// Routed model: first try + one retry on a schema failure.
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		lines, err := e.callOnce(ctx, routed, spoken)
		if err == nil {
			extractOps.WithLabelValues(extractOK).Inc()
			return lines, nil
		}
		lastErr = err
		if !errors.Is(err, errBadSchema) {
			break // transport / API failure: replaying the same call will not help
		}
		if attempt == 0 {
			extractOps.WithLabelValues(extractRetry).Inc() // only a retry that really happens
		}
	}
	if routed != e.defaultModel {
		extractOps.WithLabelValues(extractFallback).Inc()
		lines, err := e.callOnce(ctx, e.defaultModel, spoken)
		if err == nil {
			extractOps.WithLabelValues(extractOK).Inc()
			return lines, nil
		}
		lastErr = err
	}
	extractOps.WithLabelValues(extractFailed).Inc()
	return nil, fmt.Errorf("%w: %v", ErrExtractFailed, lastErr)
}

func (e *Extractor) callOnce(ctx context.Context, model, spoken string) ([]ExtractedLine, error) {
	resp, err := e.llm.Chat(ctx, model, []llmclient.Message{
		{Role: "system", Content: extractPrompt},
		{Role: "user", Content: spoken},
	}, nil)
	if err != nil {
		return nil, err
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("%w: no choices", errBadSchema)
	}
	content, _ := extractContent(resp.Choices[0].Message.Content)
	return parseExtractReply(content)
}

// parseExtractReply validates the reply against the line schema: a JSON object
// with a non-empty "lines" array whose items have a non-empty product_name and
// a positive qty (and, when present, a non-negative unit_price).
func parseExtractReply(content string) ([]ExtractedLine, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	dec := json.NewDecoder(bytes.NewReader([]byte(strings.TrimSpace(content))))
	dec.UseNumber()
	var raw struct {
		Lines []struct {
			ProductName string      `json:"product_name"`
			Qty         interface{} `json:"qty"`
			UnitPrice   interface{} `json:"unit_price"`
		} `json:"lines"`
	}
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("%w: %v", errBadSchema, err)
	}
	if len(raw.Lines) == 0 {
		return nil, fmt.Errorf("%w: empty lines", errBadSchema)
	}
	out := make([]ExtractedLine, 0, len(raw.Lines))
	for i, l := range raw.Lines {
		name := strings.TrimSpace(l.ProductName)
		qty, ok := toDecimal(l.Qty)
		if name == "" || !ok || !qty.IsPositive() {
			return nil, fmt.Errorf("%w: line %d", errBadSchema, i)
		}
		line := ExtractedLine{ProductName: name, Qty: qty}
		if l.UnitPrice != nil {
			p, ok := toDecimal(l.UnitPrice)
			if !ok || p.IsNegative() {
				return nil, fmt.Errorf("%w: line %d price", errBadSchema, i)
			}
			line.UnitPrice = &p
		}
		out = append(out, line)
	}
	return out, nil
}

// toDecimal reads a JSON number or numeric string (money and quantities are
// never float64 here).
func toDecimal(v interface{}) (decimal.Decimal, bool) {
	switch t := v.(type) {
	case json.Number:
		d, err := decimal.NewFromString(t.String())
		return d, err == nil
	case string:
		d, err := decimal.NewFromString(strings.TrimSpace(t))
		return d, err == nil
	}
	return decimal.Zero, false
}

// ExtractChecked extracts lines and runs the deterministic rules on them
// (spoken quantities, plus the spoken total when lines carry prices). The
// returned flags are what the owner must confirm line by line.
func (e *Extractor) ExtractChecked(ctx context.Context, tenantID uuid.UUID, spoken string) ([]ExtractedLine, []DraftFlag, error) {
	lines, err := e.Extract(ctx, tenantID, spoken)
	if err != nil {
		return nil, nil, err
	}
	cl := make([]CheckLine, 0, len(lines))
	for _, l := range lines {
		cl = append(cl, CheckLine(l))
	}
	flags := CheckSpokenQuantities(spoken, cl)
	if total, ok := SpokenTotal(spoken); ok {
		flags = append(flags, CheckTotal(cl, total)...)
	}
	return lines, flags, nil
}
