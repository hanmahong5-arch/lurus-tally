package entitlement_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/app/entitlement"
)

type countingEnts struct {
	ents  map[string]string
	err   error
	calls atomic.Int32
}

func (c *countingEnts) GetEntitlements(_ context.Context, _ int64, _ string) (map[string]string, error) {
	c.calls.Add(1)
	return c.ents, c.err
}

func TestLimit_Parsing(t *testing.T) {
	cases := []struct {
		name string
		ents map[string]string
		key  string
		want int64
	}{
		{"present", map[string]string{"max_users": "5"}, entitlement.KeyMaxUsers, 5},
		{"missing", map[string]string{}, entitlement.KeyMaxSKUs, -1},
		{"minus one", map[string]string{"max_skus": "-1"}, entitlement.KeyMaxSKUs, -1},
		{"not a number", map[string]string{"max_warehouses": "unlimited"}, entitlement.KeyMaxWarehouses, -1},
		{"legacy users", map[string]string{"users": "3"}, entitlement.KeyMaxUsers, 3},
		{"legacy skus", map[string]string{"skus": " 200 "}, entitlement.KeyMaxSKUs, 200},
		{"legacy warehouses", map[string]string{"warehouses": "2"}, entitlement.KeyMaxWarehouses, 2},
		{"new key wins", map[string]string{"max_users": "9", "users": "3"}, entitlement.KeyMaxUsers, 9},
		{"zero is a real limit", map[string]string{"max_users": "0"}, entitlement.KeyMaxUsers, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := entitlement.NewService(stubResolver{id: 1, ok: true}, &countingEnts{ents: tc.ents}, nil)
			got, err := s.Limit(context.Background(), uuid.New(), tc.key)
			if err != nil || got != tc.want {
				t.Fatalf("got %d err=%v, want %d", got, err, tc.want)
			}
		})
	}
}

func TestLimit_FailOpen(t *testing.T) {
	boom := errors.New("boom")
	if got, err := entitlement.NewService(stubResolver{err: boom}, &countingEnts{}, nil).Limit(context.Background(), uuid.New(), entitlement.KeyMaxUsers); err != nil || got != -1 {
		t.Fatalf("resolver error: got %d err=%v", got, err)
	}
	if got, err := entitlement.NewService(stubResolver{id: 1, ok: true}, &countingEnts{err: boom}, nil).Limit(context.Background(), uuid.New(), entitlement.KeyMaxUsers); err != nil || got != -1 {
		t.Fatalf("platform error: got %d err=%v", got, err)
	}
	if got, _ := entitlement.NewService(stubResolver{ok: false}, &countingEnts{}, nil).Limit(context.Background(), uuid.New(), entitlement.KeyMaxUsers); got != -1 {
		t.Fatalf("unpinned tenant should be unlimited, got %d", got)
	}
}

func TestLimit_Cache(t *testing.T) {
	now := time.Unix(1000, 0)
	ents := &countingEnts{ents: map[string]string{"max_users": "5", "max_skus": "50"}}
	s := entitlement.NewService(stubResolver{id: 1, ok: true}, ents, nil).WithClock(func() time.Time { return now })
	tid := uuid.New()
	ctx := context.Background()
	for _, k := range []string{entitlement.KeyMaxUsers, entitlement.KeyMaxSKUs, entitlement.KeyMaxUsers} {
		if _, err := s.Limit(ctx, tid, k); err != nil {
			t.Fatal(err)
		}
	}
	if n := ents.calls.Load(); n != 1 {
		t.Fatalf("cache hit expected, platform calls = %d", n)
	}
	now = now.Add(61 * time.Second)
	_, _ = s.Limit(ctx, tid, entitlement.KeyMaxUsers)
	if n := ents.calls.Load(); n != 2 {
		t.Fatalf("expired entry must refetch, platform calls = %d", n)
	}
	// A failed fetch is not cached.
	bad := &countingEnts{err: errors.New("x")}
	s2 := entitlement.NewService(stubResolver{id: 1, ok: true}, bad, nil)
	_, _ = s2.Limit(ctx, tid, entitlement.KeyMaxUsers)
	_, _ = s2.Limit(ctx, tid, entitlement.KeyMaxUsers)
	if n := bad.calls.Load(); n != 2 {
		t.Fatalf("errors must not be cached, calls = %d", n)
	}
}
