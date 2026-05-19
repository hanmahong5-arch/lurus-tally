package llmgateway

// modelPriceCNY is the per-1k-token price in CNY for each known model.
// Edit here when newapi adds new models or vendor adjusts pricing.
// Unknown models fall back to zero price + the "unknown" model label.
type pricePerKToken struct {
	prompt     float64
	completion float64
}

var modelPriceCNY = map[string]pricePerKToken{
	// DeepSeek v4 (Lurus default via newapi). Source: newapi pricing page 2026-05.
	"deepseek-v4":       {prompt: 0.002, completion: 0.008},
	"deepseek-chat":     {prompt: 0.002, completion: 0.008},
	"deepseek-reasoner": {prompt: 0.004, completion: 0.016},

	// OpenAI compatibility (kept for fallback / experiments).
	"gpt-4o-mini": {prompt: 0.001, completion: 0.004},
	"gpt-4o":      {prompt: 0.02, completion: 0.08},

	// Claude families (newapi router 兼容).
	"claude-sonnet-4-6": {prompt: 0.018, completion: 0.09},
	"claude-haiku-4-5":  {prompt: 0.005, completion: 0.025},
}

// costCNYFor returns the CNY cost for the given model + token counts.
// Unknown model returns 0 and the caller will label the metric with model="unknown".
func costCNYFor(model string, promptTokens, completionTokens int) float64 {
	p, ok := modelPriceCNY[model]
	if !ok {
		return 0
	}
	return float64(promptTokens)/1000.0*p.prompt + float64(completionTokens)/1000.0*p.completion
}

// modelLabel returns the model name normalized for Prometheus label use.
// Unknown models collapse to "unknown" to bound label cardinality.
func modelLabel(model string) string {
	if _, ok := modelPriceCNY[model]; !ok {
		return "unknown"
	}
	return model
}
