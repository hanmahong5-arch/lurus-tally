package llmgateway

import (
	"context"
	"sync/atomic"
)

// UsageSink receives one notification per completed LLM call, carrying the same
// (ctx, model, tokens) tuple that RecordUsage attributes to Prometheus. It lets
// a higher layer (unified-billing Wave 2) fan a usage event out to the platform
// without this pure-observability package importing a platform client or DB.
//
// Implementations MUST be non-blocking: Record runs on the LLM response hot
// path. A reporter should enqueue and return, never do network/DB work inline.
type UsageSink interface {
	Record(ctx context.Context, model string, promptTokens, completionTokens int)
}

// sinkHolder wraps the interface so the atomic pointer always stores ONE
// concrete type — different UsageSink implementations would make a bare
// atomic.Value panic with "inconsistently typed value".
type sinkHolder struct{ s UsageSink }

// sink holds the optional process-wide UsageSink. atomic.Pointer so
// SetUsageSink (wiring time) and RecordUsage (hot path) never race.
var sink atomic.Pointer[sinkHolder]

// SetUsageSink installs the process-wide usage sink. Call once at lifecycle
// wiring; passing nil is a no-op (leaves any prior sink in place). Wiring code
// that wants to disable a sink should simply never call this.
func SetUsageSink(s UsageSink) {
	if s == nil {
		return
	}
	sink.Store(&sinkHolder{s: s})
}

// emitToSink forwards a recorded completion to the sink when one is installed.
func emitToSink(ctx context.Context, model string, promptTokens, completionTokens int) {
	if h := sink.Load(); h != nil && h.s != nil {
		h.s.Record(ctx, model, promptTokens, completionTokens)
	}
}
