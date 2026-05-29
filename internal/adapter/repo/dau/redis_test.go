package dau_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dau"
)

// newStore spins up an in-memory Redis (miniredis supports PFADD/PFCOUNT) and
// returns a Store pinned to a fixed clock so day boundaries are deterministic.
func newStore(t *testing.T, now time.Time) (*dau.Store, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	store := dau.New(rdb).WithClock(func() time.Time { return now })
	return store, mr
}

func TestStore_Record_CountsDistinctUsers(t *testing.T) {
	ctx := context.Background()
	day := time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC)
	store, _ := newStore(t, day)

	for _, uid := range []string{"user-a", "user-b"} {
		if err := store.Record(ctx, "palette_invocation", uid); err != nil {
			t.Fatalf("Record(%s): %v", uid, err)
		}
	}

	got, err := store.CountToday(ctx, "palette_invocation")
	if err != nil {
		t.Fatalf("CountToday: %v", err)
	}
	if got != 2 {
		t.Errorf("distinct users = %d, want 2", got)
	}
}

func TestStore_Record_DedupesSameUser(t *testing.T) {
	ctx := context.Background()
	day := time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC)
	store, _ := newStore(t, day)

	for i := 0; i < 5; i++ {
		if err := store.Record(ctx, "ai_drawer_open", "user-a"); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	got, err := store.CountToday(ctx, "ai_drawer_open")
	if err != nil {
		t.Fatalf("CountToday: %v", err)
	}
	if got != 1 {
		t.Errorf("repeat hits from one user = %d, want 1", got)
	}
}

func TestStore_Record_PopulatesTotalBucket(t *testing.T) {
	ctx := context.Background()
	day := time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC)
	store, _ := newStore(t, day)

	// Two users active via different events should both count toward the total.
	if err := store.Record(ctx, "palette_invocation", "user-a"); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := store.Record(ctx, "ai_drawer_open", "user-b"); err != nil {
		t.Fatalf("Record: %v", err)
	}

	total, err := store.CountTotalToday(ctx)
	if err != nil {
		t.Fatalf("CountTotalToday: %v", err)
	}
	if total != 2 {
		t.Errorf("total dau = %d, want 2", total)
	}
}

func TestStore_Record_IsolatesDays(t *testing.T) {
	ctx := context.Background()
	d1 := time.Date(2026, 5, 29, 23, 59, 0, 0, time.UTC)
	store, mr := newStore(t, d1)

	if err := store.Record(ctx, "palette_invocation", "user-a"); err != nil {
		t.Fatalf("Record d1: %v", err)
	}

	// Advance the clock to the next UTC day; the new day's key starts empty.
	d2 := time.Date(2026, 5, 30, 0, 1, 0, 0, time.UTC)
	store.WithClock(func() time.Time { return d2 })

	got, err := store.CountToday(ctx, "palette_invocation")
	if err != nil {
		t.Fatalf("CountToday d2: %v", err)
	}
	if got != 0 {
		t.Errorf("next-day count = %d, want 0 (keys are per-UTC-day)", got)
	}

	// And the prior day's key still exists with TTL set (not yet reclaimed).
	if ttl := mr.TTL("dau:palette_invocation:2026-05-29"); ttl <= 0 {
		t.Errorf("expected positive TTL on prior-day key, got %v", ttl)
	}
}

func TestStore_Record_BlankUserIsNoOp(t *testing.T) {
	ctx := context.Background()
	day := time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC)
	store, _ := newStore(t, day)

	if err := store.Record(ctx, "palette_invocation", ""); err != nil {
		t.Fatalf("Record blank user: %v", err)
	}

	got, err := store.CountToday(ctx, "palette_invocation")
	if err != nil {
		t.Fatalf("CountToday: %v", err)
	}
	if got != 0 {
		t.Errorf("blank user counted = %d, want 0 (anonymous must not inflate DAU)", got)
	}
}

func TestStore_Nil_IsSafe(t *testing.T) {
	ctx := context.Background()
	var store *dau.Store
	if err := store.Record(ctx, "palette_invocation", "user-a"); err != nil {
		t.Errorf("nil Record err = %v, want nil", err)
	}
	n, err := store.CountToday(ctx, "palette_invocation")
	if err != nil || n != 0 {
		t.Errorf("nil CountToday = (%d, %v), want (0, nil)", n, err)
	}
}
