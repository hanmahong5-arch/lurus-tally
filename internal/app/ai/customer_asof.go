package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmclient"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/memorusclient"
)

// recallAsOfTool answers 「三月时李四怎么付款」: what the owner had told about a
// customer as it stood at a past moment, before later changes replaced it.
// Like remember_customer_fact it exists only when memory is on.
const recallAsOfTool = "recall_customer_facts_as_of"

// AsOfSearcher is implemented by memory clients that can search the facts
// that held at a moment (memorusclient.Client does).
type AsOfSearcher interface {
	SearchAsOf(ctx context.Context, userID, query string, limit int, metaEq map[string]string, asOf time.Time) ([]memorusclient.Memory, error)
}

// shopZone is the clock a shop owner's 「三月」 refers to.
var shopZone = time.FixedZone("CST", 8*3600)

func recallAsOfDef() llmclient.Tool {
	params, _ := json.Marshal(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"customer": map[string]string{"type": "string", "description": "Customer as the user named them, including address forms like 老张/王老板/李总"},
			"as_of":    map[string]string{"type": "string", "description": "The moment the user asks about: YYYY-MM-DD, YYYY-MM, or just the month (\"03\") when the user names no year — the most recent such month is used. A period means its end."},
			"topic":    map[string]string{"type": "string", "description": "What the user asks about, in their words (e.g. 付款方式, 送货地址); optional"},
		},
		"required": []string{"customer", "as_of"},
	})
	return llmclient.Tool{Type: "function", Function: llmclient.FunctionDef{
		Name: recallAsOfTool,
		Description: "What the user had told you about a named customer as it stood at a past time — the values that held then, before later changes. " +
			"Call it for questions about an earlier time: 三月时/以前/上个月/去年 李四怎么付款、原来送到哪. For how things are now, use the notes you already have instead.",
		Parameters: params,
	}}
}

var monthOnly = regexp.MustCompile(`^(\d{1,2})\s*月?$`)

// parseAsOf turns the model's as_of into the end of the named period in the
// shop's clock. now anchors a month given without a year to its most recent
// occurrence that has started (「三月」 in September = this year's March;
// in February = last year's).
func parseAsOf(raw string, now time.Time) (time.Time, error) {
	s := strings.TrimSpace(raw)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if d, err := time.ParseInLocation("2006-01-02", s, shopZone); err == nil {
		return d.AddDate(0, 0, 1).Add(-time.Nanosecond), nil
	}
	if m, err := time.ParseInLocation("2006-01", s, shopZone); err == nil {
		return m.AddDate(0, 1, 0).Add(-time.Nanosecond), nil
	}
	if g := monthOnly.FindStringSubmatch(s); g != nil {
		month, _ := strconv.Atoi(g[1])
		if month >= 1 && month <= 12 {
			local := now.In(shopZone)
			year := local.Year()
			if time.Month(month) > local.Month() {
				year--
			}
			start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, shopZone)
			return start.AddDate(0, 1, 0).Add(-time.Nanosecond), nil
		}
	}
	return time.Time{}, fmt.Errorf("as_of %q: use YYYY-MM-DD, YYYY-MM or a month number", raw)
}

// recallCustomerFactsAsOf resolves the customer and reads the notes that
// held at as_of. Unknown or ambiguous customers read nothing.
func (o *Orchestrator) recallCustomerFactsAsOf(ctx context.Context, tenantID uuid.UUID, argsJSON string) (string, error) {
	var args struct {
		Customer string `json:"customer"`
		AsOf     string `json:"as_of"`
		Topic    string `json:"topic"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("%s: invalid args: %w", recallAsOfTool, err)
	}
	name := strings.TrimSpace(args.Customer)
	if name == "" {
		return "", fmt.Errorf("%s: customer is required", recallAsOfTool)
	}
	at, err := parseAsOf(args.AsOf, time.Now())
	if err != nil {
		return "", fmt.Errorf("%s: %w", recallAsOfTool, err)
	}
	searcher, ok := o.memory.(AsOfSearcher)
	if !ok {
		return "", fmt.Errorf("%s: memory client cannot search by time", recallAsOfTool)
	}
	res, err := o.registry.ResolveCustomerName(ctx, tenantID, name)
	if err != nil {
		return "", fmt.Errorf("%s: %w", recallAsOfTool, err)
	}
	if len(res.Ambiguous) > 0 {
		return ambiguousCustomers(res.Ambiguous, "several customers match — ask the user which one")
	}
	if res.Customer == nil || res.Customer.ID == uuid.Nil {
		return jsonMarshal(map[string]interface{}{"found": false, "customer": name, "note": "no customer record with that name"})
	}
	c := res.Customer
	query := strings.TrimSpace(args.Topic)
	if query == "" {
		query = c.Name
	}
	rctx, cancel := context.WithTimeout(ctx, asyncMemoryWriteTimeout)
	defer cancel()
	hits, err := searcher.SearchAsOf(rctx, tenantID.String(), query, customerMemoryLimit,
		map[string]string{MemorySubjectKey: CustomerSubject(c.ID)}, at)
	if err != nil {
		return jsonMarshal(map[string]interface{}{"customer": c.Name, "note": "memory is unavailable — tell the user"})
	}
	facts := make([]string, 0, len(hits))
	for _, h := range hits {
		facts = append(facts, h.Content)
	}
	out := map[string]interface{}{
		"customer": c.Name,
		"as_of":    at.In(shopZone).Format("2006-01-02 15:04"),
		"facts":    facts,
		"note":     "notes that were current at as_of; later changes are not included",
	}
	if len(facts) == 0 {
		out["note"] = "nothing had been noted about this customer by then"
	}
	return jsonMarshal(out)
}
