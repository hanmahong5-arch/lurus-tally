package ai

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/hanmahong5-arch/lurus-tally/internal/domain/permission"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmclient"
)

type costHiddenKey struct{}

// WithCostHidden marks the request as coming from someone without cost.view
// (clerk, cashier): the assistant must not read or reveal cost, purchase price
// or margin for them.
func WithCostHidden(ctx context.Context) context.Context {
	return context.WithValue(ctx, costHiddenKey{}, true)
}

func costHidden(ctx context.Context) bool {
	v, _ := ctx.Value(costHiddenKey{}).(bool)
	return v
}

// costToolNames are the tools whose answer is cost by nature, so they are not
// even offered to the model for a cost-hidden caller.
var costToolNames = map[string]bool{
	"gross_margin_summary":          true,
	"propose_create_purchase_draft": true, // its lines are purchase prices
}

// costDeniedResult is the tool result when a cost-hidden caller reaches a cost tool anyway.
const costDeniedResult = `{"error":"当前账号没有查看成本和毛利的权限，请告诉用户需要老板或店长来查看"}`

// toolDefsFor is toolDefs minus the cost tools for a cost-hidden caller.
func (o *Orchestrator) toolDefsFor(ctx context.Context) []llmclient.Tool {
	defs := o.toolDefs()
	if !costHidden(ctx) {
		return defs
	}
	out := make([]llmclient.Tool, 0, len(defs))
	for _, d := range defs {
		if !costToolNames[d.Function.Name] {
			out = append(out, d)
		}
	}
	return out
}

// trimCostResult removes cost keys from a tool's JSON result (stock value and
// dead-stock value are inventory cost). Unparseable output is returned as is:
// it is an error string, never data.
func trimCostResult(content string) string {
	var v any
	dec := json.NewDecoder(bytes.NewReader([]byte(content)))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return content
	}
	b, err := json.Marshal(permission.StripGeneric(v))
	if err != nil {
		return content
	}
	return string(b)
}
