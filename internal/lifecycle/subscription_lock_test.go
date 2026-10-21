package lifecycle

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestBuildSubscriptionLock(t *testing.T) {
	ctx := context.Background()
	t.Setenv(devSubscriptionStatusEnv, "expired")
	if !buildSubscriptionLock(true, nil, nil).State(ctx, uuid.New()).Locked {
		t.Error("dev override expired must lock")
	}
	if buildSubscriptionLock(false, nil, nil).State(ctx, uuid.New()).Locked {
		t.Error("override must be ignored outside dev mode")
	}
	t.Setenv(devSubscriptionStatusEnv, "active")
	if buildSubscriptionLock(true, nil, nil).State(ctx, uuid.New()).Locked {
		t.Error("dev override active must not lock")
	}
	t.Setenv(devSubscriptionStatusEnv, "")
	if buildSubscriptionLock(true, nil, nil).State(ctx, uuid.New()).Locked {
		t.Error("no platform must never lock")
	}
}
