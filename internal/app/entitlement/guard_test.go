package entitlement_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/app/entitlement"
)

type fakeUsage struct {
	users, skus, whs int64
	err              error
	calls            int
}

func (f *fakeUsage) CountUsers(context.Context, uuid.UUID) (int64, error) {
	f.calls++
	return f.users, f.err
}
func (f *fakeUsage) CountSKUs(context.Context, uuid.UUID) (int64, error) {
	f.calls++
	return f.skus, f.err
}
func (f *fakeUsage) CountWarehouses(context.Context, uuid.UUID) (int64, error) {
	f.calls++
	return f.whs, f.err
}

func guardFor(mode entitlement.Mode, ents map[string]string, u *fakeUsage) *entitlement.Guard {
	svc := entitlement.NewService(stubResolver{id: 1, ok: true}, &countingEnts{ents: ents}, nil)
	return entitlement.NewGuard(svc, mode, u, nil)
}

func TestGuard_Modes(t *testing.T) {
	ents := map[string]string{"max_skus": "10"}
	ctx, tid := context.Background(), uuid.New()

	t.Run("off never counts", func(t *testing.T) {
		u := &fakeUsage{skus: 99}
		if err := guardFor(entitlement.ModeOff, ents, u).Check(ctx, tid, entitlement.KeyMaxSKUs, 1); err != nil || u.calls != 0 {
			t.Fatalf("err=%v calls=%d", err, u.calls)
		}
	})
	t.Run("shadow over limit passes", func(t *testing.T) {
		u := &fakeUsage{skus: 10}
		if err := guardFor(entitlement.ModeShadow, ents, u).Check(ctx, tid, entitlement.KeyMaxSKUs, 1); err != nil {
			t.Fatalf("shadow must not block: %v", err)
		}
	})
	t.Run("enforce over limit rejects", func(t *testing.T) {
		u := &fakeUsage{skus: 10}
		err := guardFor(entitlement.ModeEnforce, ents, u).Check(ctx, tid, entitlement.KeyMaxSKUs, 1)
		var pl *entitlement.ErrPlanLimitReached
		if !errors.As(err, &pl) || pl.Used != 10 || pl.Limit != 10 || pl.Key != entitlement.KeyMaxSKUs {
			t.Fatalf("want ErrPlanLimitReached, got %v", err)
		}
		if pl.Message() != "当前套餐最多 10 个商品，已用 10，请升级套餐" {
			t.Fatalf("message: %s", pl.Message())
		}
	})
	t.Run("enforce within limit passes", func(t *testing.T) {
		u := &fakeUsage{skus: 9}
		if err := guardFor(entitlement.ModeEnforce, ents, u).Check(ctx, tid, entitlement.KeyMaxSKUs, 1); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("batch delta pushes over", func(t *testing.T) {
		u := &fakeUsage{skus: 8}
		g := guardFor(entitlement.ModeEnforce, ents, u)
		if err := g.Check(ctx, tid, entitlement.KeyMaxSKUs, 2); err != nil {
			t.Fatalf("exactly at limit is allowed: %v", err)
		}
		if err := g.Check(ctx, tid, entitlement.KeyMaxSKUs, 3); err == nil {
			t.Fatal("8+3 > 10 must be rejected")
		}
	})
	t.Run("unlimited passes", func(t *testing.T) {
		u := &fakeUsage{skus: 1_000_000}
		if err := guardFor(entitlement.ModeEnforce, map[string]string{}, u).Check(ctx, tid, entitlement.KeyMaxSKUs, 1); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("usage error fails open", func(t *testing.T) {
		u := &fakeUsage{err: errors.New("db down")}
		if err := guardFor(entitlement.ModeEnforce, ents, u).Check(ctx, tid, entitlement.KeyMaxSKUs, 1); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("nil guard", func(t *testing.T) {
		var g *entitlement.Guard
		if err := g.Check(ctx, tid, entitlement.KeyMaxUsers, 1); err != nil {
			t.Fatal(err)
		}
	})
}

func TestGuard_Status(t *testing.T) {
	now := time.Unix(0, 0)
	u := &fakeUsage{users: 2, skus: 7, whs: 1}
	g := guardFor(entitlement.ModeEnforce, map[string]string{"max_users": "3", "warehouses": "1"}, u).
		WithClock(func() time.Time { return now })
	tid := uuid.New()
	st := g.Status(context.Background(), tid)
	if st[entitlement.KeyMaxUsers] != (entitlement.LimitStatus{Limit: 3, Used: 2, Enforced: true}) ||
		st[entitlement.KeyMaxSKUs] != (entitlement.LimitStatus{Limit: -1, Used: 7, Enforced: true}) ||
		st[entitlement.KeyMaxWarehouses] != (entitlement.LimitStatus{Limit: 1, Used: 1, Enforced: true}) {
		t.Fatalf("status: %+v", st)
	}
	calls := u.calls
	g.Status(context.Background(), tid)
	if u.calls != calls {
		t.Fatal("second status within TTL must be cached")
	}
	if entitlement.ParseMode("") != entitlement.ModeShadow || entitlement.ParseMode("enforce") != entitlement.ModeEnforce || entitlement.ParseMode("off") != entitlement.ModeOff {
		t.Fatal("ParseMode")
	}
}
