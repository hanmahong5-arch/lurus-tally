package ai_test

import (
	"context"
	"testing"
	"time"

	appai "github.com/hanmahong5-arch/lurus-tally/internal/app/ai"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/memorusclient"
)

// captureMemoryClient records the context passed to Add and signals completion.
type captureMemoryClient struct {
	gotCtx chan context.Context
}

func (c *captureMemoryClient) Search(_ context.Context, _, _ string, _ int) ([]memorusclient.Memory, error) {
	return nil, nil
}

func (c *captureMemoryClient) Add(ctx context.Context, _, _ string, _ map[string]any) (*memorusclient.Memory, error) {
	c.gotCtx <- ctx
	return nil, nil
}

// TestAsyncWriteMemory_BoundsContextWithDeadline asserts the fire-and-forget
// write runs under a bounded (deadline-carrying) context, so a hung memorus
// cannot leak the goroutine indefinitely.
func TestAsyncWriteMemory_BoundsContextWithDeadline(t *testing.T) {
	mc := &captureMemoryClient{gotCtx: make(chan context.Context, 1)}

	appai.AsyncWriteMemory(mc, "user-1", "summary", nil)

	select {
	case ctx := <-mc.gotCtx:
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("AsyncWriteMemory must pass a context WITH a deadline")
		}
		if d := time.Until(deadline); d <= 0 || d > time.Minute {
			t.Fatalf("deadline out of expected range: %v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("AsyncWriteMemory did not call Add within 2s")
	}
}

// TestAsyncWriteMemory_NilClientIsNoop ensures a nil client never panics.
func TestAsyncWriteMemory_NilClientIsNoop(t *testing.T) {
	appai.AsyncWriteMemory(nil, "user-1", "summary", nil)
}
