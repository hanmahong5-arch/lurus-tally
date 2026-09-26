package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmclient"
)

// rememberCustomerFactTool saves one attribute of one customer to memorus.
// It is not a Registry tool: it exists only when memory is on, and it writes
// through the orchestrator's MemoryClient.
const rememberCustomerFactTool = "remember_customer_fact"

func rememberCustomerFactDef() llmclient.Tool {
	params, _ := json.Marshal(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"customer":  map[string]string{"type": "string", "description": "Customer as the user named them, including address forms like 老张/王老板/李总"},
			"attribute": map[string]interface{}{"type": "string", "enum": customerSlotIDs(), "description": "Which attribute of the customer the statement is about"},
			"value":     map[string]string{"type": "string", "description": "The attribute's value now, short (e.g. 微信, 城南路18号, 每周四, 他儿子)"},
			"quote":     map[string]string{"type": "string", "description": "The part of the user's message that states it, verbatim"},
		},
		"required": []string{"customer", "attribute", "value", "quote"},
	})
	return llmclient.Tool{Type: "function", Function: llmclient.FunctionDef{
		Name: rememberCustomerFactTool,
		Description: "Save a fact the user states about a named customer to long-term memory. Call it whenever the user tells you how a customer pays, settles, where and when to deliver, how to pack, who to contact, invoice needs, agreed prices, allergies, tastes or regular items — including a change (改用/换成/搬到/以后). One call per attribute: a sentence that mentions two attributes needs two calls. A new value of the same attribute replaces the old one. If the result says the customer is ambiguous or unknown, nothing was saved — ask the user. Attributes:" +
			customerSlotGuide(),
		Parameters: params,
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
	var args struct {
		Customer  string `json:"customer"`
		Attribute string `json:"attribute"`
		Value     string `json:"value"`
		Quote     string `json:"quote"`
	}
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
	wctx, cancel := context.WithTimeout(ctx, asyncMemoryWriteTimeout)
	defer cancel()
	_, err = o.memory.Add(wctx, tenantID.String(), text, meta)
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
	if res.ResolvedFrom != "" {
		out["note"] = fmt.Sprintf("%s was taken to mean customer %s (the only customer with that surname); say so", res.ResolvedFrom, c.Name)
	}
	return jsonMarshal(out)
}
