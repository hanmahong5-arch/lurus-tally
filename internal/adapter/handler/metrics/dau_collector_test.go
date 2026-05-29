package metrics_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/redis/go-redis/v9"

	handlermetrics "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/metrics"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dau"
)

func TestDAUCollector_ExposesGaugesFromHLL(t *testing.T) {
	ctx := context.Background()
	day := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()

	store := dau.New(rdb).WithClock(func() time.Time { return day })

	// Two distinct users invoked the palette; one of them also opened the drawer.
	for _, uid := range []string{"user-a", "user-b"} {
		if err := store.Record(ctx, "palette_invocation", uid); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	if err := store.Record(ctx, "ai_drawer_open", "user-a"); err != nil {
		t.Fatalf("Record: %v", err)
	}

	collector := handlermetrics.NewDAUCollector(store, nil)

	want := `
# HELP tally_ai_drawer_open_dau Unique users who opened the AI Drawer today (HLL cardinality, UTC day).
# TYPE tally_ai_drawer_open_dau gauge
tally_ai_drawer_open_dau 1
# HELP tally_palette_invocation_dau Unique users who invoked the ⌘K command palette today (HLL cardinality, UTC day).
# TYPE tally_palette_invocation_dau gauge
tally_palette_invocation_dau 2
# HELP tally_total_dau Unique active users today across all tracked product events (HLL cardinality, UTC day).
# TYPE tally_total_dau gauge
tally_total_dau 2
`
	if err := testutil.CollectAndCompare(collector, strings.NewReader(want),
		"tally_palette_invocation_dau", "tally_ai_drawer_open_dau", "tally_total_dau"); err != nil {
		t.Errorf("collected metrics mismatch:\n%v", err)
	}
}
