package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmclient"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/memorusclient"
)

// FactWriter is implemented by memory clients that take the contract-v2
// write extras (memorusclient.Client does): when the fact started to hold,
// and which old value of a multi-valued slot it replaces.
type FactWriter interface {
	AddWithOptions(ctx context.Context, userID, content string, meta map[string]any, opts memorusclient.AddOptions) (*memorusclient.AddResult, error)
}

// parseSince turns the model's `since` into the start of the named period in
// the shop's clock: a day, a month, or a month number (the most recent one
// that has started, as parseAsOf does). A moment after now is refused —
// memorus does not take future facts, and a typo'd year must not backdate
// nothing silently.
func parseSince(raw string, now time.Time) (time.Time, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}, fmt.Errorf("since is empty")
	}
	var start time.Time
	switch {
	case isRFC3339(s):
		start, _ = time.Parse(time.RFC3339, s)
	default:
		if d, err := time.ParseInLocation("2006-01-02", s, shopZone); err == nil {
			start = d
		} else if m, err := time.ParseInLocation("2006-01", s, shopZone); err == nil {
			start = m
		} else if end, err := parseAsOf(s, now); err == nil {
			// A bare month: parseAsOf gives its end; its start is the first.
			e := end.In(shopZone)
			start = time.Date(e.Year(), e.Month(), 1, 0, 0, 0, 0, shopZone)
		} else {
			return time.Time{}, fmt.Errorf("since %q: use YYYY-MM-DD or YYYY-MM", raw)
		}
	}
	if start.After(now) {
		return time.Time{}, fmt.Errorf("since %s is in the future", start.In(shopZone).Format("2006-01-02"))
	}
	return start, nil
}

func isRFC3339(s string) bool {
	_, err := time.Parse(time.RFC3339, s)
	return err == nil
}

// rememberCustomerFactTool saves one attribute of one customer to memorus.
// It is not a Registry tool: it exists only when memory is on, and it writes
// through the orchestrator's MemoryClient.
const rememberCustomerFactTool = "remember_customer_fact"

func rememberCustomerFactDef() llmclient.Tool {
	return llmclient.Tool{Type: "function", Function: llmclient.FunctionDef{
		Name: rememberCustomerFactTool,
		Description: "Save a fact the user states about a named customer to long-term memory. Call it whenever the user tells you how a customer pays, settles, where and when to deliver, how to pack, who to contact, invoice needs, agreed prices, allergies, tastes or regular items — including a change (改用/换成/搬到/以后). One call per attribute: a sentence that mentions two attributes needs two calls. A new value of the same attribute replaces the old one. If the result says the customer is ambiguous or unknown, nothing was saved — ask the user. Attributes:" +
			customerSlotGuide(),
		Parameters: toolParams(rememberCustomerFactArgs{}),
	}}
}

// toolDefs is the tool list sent to the model: the Registry's tools, plus
// remember_customer_fact (and recall_customer_facts_as_of when the client can
// search by time) when memory is on.
func (o *Orchestrator) toolDefs() []llmclient.Tool {
	defs := ToolDefs()
	if o.memory != nil {
		defs = append(defs, rememberCustomerFactDef())
		if _, ok := o.memory.(AsOfSearcher); ok {
			defs = append(defs, recallAsOfDef())
		}
	}
	return defs
}

// dispatch runs one tool call. remember_customer_fact is handled here and
// sets *remembered; every other tool goes to the Registry.
func (o *Orchestrator) dispatch(ctx context.Context, tenantID uuid.UUID, tc llmclient.ToolCall, remembered *bool) DispatchResult {
	if tc.Function.Name == recallAsOfTool && o.memory != nil {
		content, err := o.recallCustomerFactsAsOf(ctx, tenantID, tc.Function.Arguments)
		if err != nil {
			content = jsonError(err.Error())
		}
		return DispatchResult{ToolCallID: tc.ID, Name: tc.Function.Name, Content: content}
	}
	if tc.Function.Name != rememberCustomerFactTool || o.memory == nil {
		return o.registry.Dispatch(ctx, tenantID, tc)
	}
	*remembered = true
	content, err := o.rememberCustomerFact(ctx, tenantID, tc.Function.Arguments)
	if err != nil {
		content = jsonError(err.Error())
	}
	return DispatchResult{ToolCallID: tc.ID, Name: tc.Function.Name, Content: content}
}

// rememberCustomerFact resolves the customer and writes the fact, in the
// request: the model confirms "记住了" from this result, so it must be real.
// An unknown or ambiguous customer is not written.
func (o *Orchestrator) rememberCustomerFact(ctx context.Context, tenantID uuid.UUID, argsJSON string) (string, error) {
	var args rememberCustomerFactArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("%s: invalid args: %w", rememberCustomerFactTool, err)
	}
	name := strings.TrimSpace(args.Customer)
	value := strings.TrimSpace(args.Value)
	if name == "" || value == "" {
		return "", fmt.Errorf("%s: customer and value are required", rememberCustomerFactTool)
	}
	slot, ok := customerSlot(args.Attribute)
	if !ok {
		return "", fmt.Errorf("%s: unknown attribute %q", rememberCustomerFactTool, args.Attribute)
	}
	text := strings.TrimSpace(args.Quote)
	if text == "" {
		text = name + " " + slot.Label + ":" + value
	}
	if r := []rune(text); len(r) > memoryTextMaxRunes {
		text = string(r[:memoryTextMaxRunes])
	}

	res, err := o.registry.ResolveCustomerName(ctx, tenantID, name)
	if err != nil {
		return "", fmt.Errorf("%s: %w", rememberCustomerFactTool, err)
	}
	if len(res.Ambiguous) > 0 {
		return ambiguousCustomers(res.Ambiguous, "several customers match; nothing was saved — ask the user which one")
	}
	// A walk-in name (quick checkout, no partner record) has no id to file
	// the fact under.
	if res.Customer == nil || res.Customer.ID == uuid.Nil {
		return jsonMarshal(map[string]interface{}{
			"saved": false, "found": false, "customer": name,
			"note": "no customer record with that name; nothing was saved",
		})
	}
	c := res.Customer

	meta := markAliasAttribution(MemoryWriteMeta(tenantID, c), res.ResolvedFrom)
	meta[MemorySlotKey] = slot.ID
	meta[MemorySlotSingleKey] = slot.Single
	meta[MemorySlotValueKey] = value

	// Contract-v2 extras, only through a client that speaks them.
	var opts memorusclient.AddOptions
	var notes []string
	if s := strings.TrimSpace(args.Since); s != "" {
		if since, err := parseSince(s, time.Now()); err == nil {
			opts.ValidFrom = &since
		} else {
			notes = append(notes, "the start date was not usable ("+err.Error()+"); the fact was saved as holding from now")
		}
	}
	if r := strings.TrimSpace(args.Replaces); r != "" {
		if slot.Single {
			notes = append(notes, "a single-valued attribute replaces its old value by itself; `replaces` was ignored")
		} else {
			opts.ReplacesValue = r
		}
	}
	wctx, cancel := context.WithTimeout(ctx, asyncMemoryWriteTimeout)
	defer cancel()
	var unmatched bool
	if fw, ok := o.memory.(FactWriter); ok && (opts.ValidFrom != nil || opts.ReplacesValue != "") {
		var r *memorusclient.AddResult
		r, err = fw.AddWithOptions(wctx, tenantID.String(), text, meta, opts)
		if err == nil && r != nil {
			unmatched = r.UnmatchedReplacesValue
		}
	} else {
		_, err = o.memory.Add(wctx, tenantID.String(), text, meta)
	}
	countMemoryOp(memOpFactWrite, err)
	if err != nil {
		return jsonMarshal(map[string]interface{}{
			"saved": false, "customer": c.Name,
			"note": "memory is unavailable; nothing was saved — tell the user",
		})
	}
	out := map[string]interface{}{
		"saved":     true,
		"customer":  c.Name,
		"attribute": slot.Label,
		"value":     value,
	}
	if opts.ValidFrom != nil {
		out["since"] = opts.ValidFrom.In(shopZone).Format("2006-01-02")
	}
	if opts.ReplacesValue != "" {
		out["replaced"] = !unmatched
		if unmatched {
			notes = append(notes, fmt.Sprintf("no note of %q was on record, so nothing was replaced; the new value was saved alongside — say so", opts.ReplacesValue))
		}
	}
	if res.ResolvedFrom != "" {
		notes = append(notes, fmt.Sprintf("%s was taken to mean customer %s (the only customer with that surname); say so", res.ResolvedFrom, c.Name))
	}
	if len(notes) > 0 {
		out["note"] = strings.Join(notes, "; ")
	}
	return jsonMarshal(out)
}
