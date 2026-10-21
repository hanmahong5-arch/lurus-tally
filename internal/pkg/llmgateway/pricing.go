package llmgateway

import (
	"strconv"
	"strings"
	"sync/atomic"
)

// pricePerKToken is the per-1k-token price in CNY for one priced model.
type pricePerKToken struct {
	prompt     float64
	completion float64
}

// modelPrices holds the priced-model table. It has no built-in entries: model
// names are deployment config, so the table is injected at wiring time from
// config.Config.LLMModelPricesCNY via SetModelPrices. Unknown models fall back
// to zero price + the "unknown" model label. atomic.Pointer so SetModelPrices
// (wiring) and RecordUsage (hot path) never race.
var modelPrices atomic.Pointer[map[string]pricePerKToken]

// SetModelPrices replaces the priced-model table from a spec of the form
// "name:prompt:completion,name:prompt:completion" (CNY per 1k tokens). An empty
// spec clears the table; malformed entries are skipped.
func SetModelPrices(spec string) {
	t := parseModelPrices(spec)
	modelPrices.Store(&t)
}

// priceFor looks up the price of a model in the current table.
func priceFor(model string) (pricePerKToken, bool) {
	t := modelPrices.Load()
	if t == nil {
		return pricePerKToken{}, false
	}
	p, ok := (*t)[model]
	return p, ok
}

// parseModelPrices reads "name:prompt:completion" entries separated by commas;
// malformed or negative entries are skipped.
func parseModelPrices(spec string) map[string]pricePerKToken {
	out := map[string]pricePerKToken{}
	for _, entry := range strings.Split(spec, ",") {
		parts := strings.Split(strings.TrimSpace(entry), ":")
		if len(parts) != 3 || parts[0] == "" {
			continue
		}
		prompt, err1 := strconv.ParseFloat(parts[1], 64)
		completion, err2 := strconv.ParseFloat(parts[2], 64)
		if err1 != nil || err2 != nil || prompt < 0 || completion < 0 {
			continue
		}
		out[parts[0]] = pricePerKToken{prompt: prompt, completion: completion}
	}
	return out
}

// costCNYFor returns the CNY cost for the given model + token counts.
// Unknown model returns 0 and the caller will label the metric with model="unknown".
func costCNYFor(model string, promptTokens, completionTokens int) float64 {
	p, ok := priceFor(model)
	if !ok {
		return 0
	}
	return float64(promptTokens)/1000.0*p.prompt + float64(completionTokens)/1000.0*p.completion
}

// modelLabel returns the model name normalized for Prometheus label use.
// Unknown models collapse to "unknown" to bound label cardinality.
func modelLabel(model string) string {
	if _, ok := priceFor(model); !ok {
		return "unknown"
	}
	return model
}
