package llmgateway

import (
	"context"
	"testing"
)

type capturingSink struct {
	calls  int
	tenant string
	model  string
	in     int
	out    int
}

func (s *capturingSink) Record(ctx context.Context, model string, promptTokens, completionTokens int) {
	s.calls++
	s.tenant = TenantFrom(ctx)
	s.model = model
	s.in = promptTokens
	s.out = completionTokens
}

// TestRecordUsage_FanOutsToSink verifies RecordUsage forwards the same tuple to
// an installed UsageSink, carrying the tenant from context.
func TestRecordUsage_FanOutsToSink(t *testing.T) {
	prev := sink.Load()
	t.Cleanup(func() {
		// Restore whatever (if anything) was installed before this test.
		if prev != nil {
			sink.Store(prev)
		} else {
			sink.Store(&sinkHolder{s: noopSink{}})
		}
	})

	cs := &capturingSink{}
	SetUsageSink(cs)

	ctx := WithTenant(context.Background(), "tenant-xyz")
	RecordUsage(ctx, "deepseek-v4", 12, 7)

	if cs.calls != 1 {
		t.Fatalf("expected sink called once, got %d", cs.calls)
	}
	if cs.tenant != "tenant-xyz" || cs.model != "deepseek-v4" || cs.in != 12 || cs.out != 7 {
		t.Errorf("sink received wrong tuple: tenant=%s model=%s in=%d out=%d",
			cs.tenant, cs.model, cs.in, cs.out)
	}
}

// TestSetUsageSink_NilIsNoOp verifies passing nil does not panic and leaves a
// prior sink in place.
func TestSetUsageSink_NilIsNoOp(t *testing.T) {
	prev := sink.Load()
	t.Cleanup(func() {
		if prev != nil {
			sink.Store(prev)
		} else {
			sink.Store(&sinkHolder{s: noopSink{}})
		}
	})

	marker := &capturingSink{}
	SetUsageSink(marker)
	SetUsageSink(nil) // must not clear the marker

	RecordUsage(WithTenant(context.Background(), "t"), "deepseek-v4", 1, 1)
	if marker.calls != 1 {
		t.Errorf("nil SetUsageSink should not clear the installed sink; calls=%d", marker.calls)
	}
}

type noopSink struct{}

func (noopSink) Record(context.Context, string, int, int) {}
