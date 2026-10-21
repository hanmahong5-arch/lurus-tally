// Package subscription decides whether a shop is read-only because its plan
// has lapsed. The rule is deliberately forgiving: only a definitive
// expired/cancelled/suspended answer from the platform locks a shop; every
// doubt (no account, no subscription, platform down) leaves it open.
package subscription

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// CacheTTL is how long a per-shop answer is reused before asking the platform again.
const CacheTTL = 5 * time.Minute

// MaxStale caps how long an old answer may be reused when the platform cannot
// be reached; beyond it the shop reads as not locked.
const MaxStale = 30 * time.Minute

// ReasonExpired is the stable code reported when a shop is locked.
const ReasonExpired = "subscription_expired"

// LockedMessage is the shop-facing sentence shown whenever writes are refused.
const LockedMessage = "套餐已到期，现在只能查看和导出，续费后恢复开单。"

// State is the subscription verdict for one shop.
type State struct {
	Locked    bool
	Reason    string // ReasonExpired when Locked, else ""
	PlanCode  string
	Status    string
	ExpiresAt string
}

// Snapshot is the platform subscription record for the shop plan holder.
type Snapshot struct {
	PlanCode  string
	Status    string
	ExpiresAt string
}

// Source reads the subscription of a platform account. A nil snapshot with a
// nil error means the account has no subscription.
type Source interface {
	Subscription(ctx context.Context, accountID int64) (*Snapshot, error)
}

// AccountResolver maps a shop to the platform account that holds its plan
// (tenant.platform_account_id, else the owner mapped account). found=false
// means no account could be determined.
type AccountResolver interface {
	PlanAccountID(ctx context.Context, tenantID uuid.UUID) (id int64, found bool, err error)
}

// Checker answers State(tenant).
type Checker interface {
	State(ctx context.Context, tenantID uuid.UUID) State
}

// Invalidator drops any cached answer for a shop so the next State asks the
// platform again (called right after a payment).
type Invalidator interface {
	Invalidate(tenantID uuid.UUID)
}

// Never is the Checker that never locks (no platform wired).
type Never struct{}

// State implements Checker.
func (Never) State(context.Context, uuid.UUID) State { return State{} }

// Invalidate implements Invalidator (nothing cached).
func (Never) Invalidate(uuid.UUID) {}

// Fixed always returns the same state; used by the dev stub.
type Fixed State

// State implements Checker.
func (f Fixed) State(context.Context, uuid.UUID) State { return State(f) }

// Invalidate implements Invalidator (nothing cached).
func (Fixed) Invalidate(uuid.UUID) {}

type entry struct {
	state State
	at    time.Time // when the platform answered
	until time.Time
}

// inflight is one platform lookup shared by every concurrent caller of a shop.
type inflight struct {
	done  chan struct{}
	state State
	ok    bool
}

// Service is the caching Checker backed by the platform.
type Service struct {
	accounts AccountResolver
	source   Source
	now      func() time.Time

	mu    sync.Mutex
	cache map[uuid.UUID]entry
	calls map[uuid.UUID]*inflight
}

// NewService builds the platform-backed Checker.
func NewService(accounts AccountResolver, source Source) *Service {
	return &Service{accounts: accounts, source: source, now: time.Now, cache: map[uuid.UUID]entry{}, calls: map[uuid.UUID]*inflight{}}
}

// Evaluate turns a snapshot into a State.
func Evaluate(s *Snapshot) State {
	if s == nil {
		return State{}
	}
	st := State{PlanCode: s.PlanCode, Status: s.Status, ExpiresAt: s.ExpiresAt}
	switch s.Status {
	case "expired", "cancelled", "suspended":
		st.Locked = true
		st.Reason = ReasonExpired
	}
	return st
}

// Invalidate implements Invalidator.
func (s *Service) Invalidate(tenantID uuid.UUID) {
	s.mu.Lock()
	delete(s.cache, tenantID)
	s.mu.Unlock()
}

// State implements Checker. It never fails: on any error the last known answer
// is reused if it is younger than MaxStale, otherwise the shop reads as not
// locked. Concurrent lookups for one shop share a single platform call.
func (s *Service) State(ctx context.Context, tenantID uuid.UUID) State {
	if tenantID == uuid.Nil {
		return State{}
	}
	now := s.now()
	s.mu.Lock()
	e, have := s.cache[tenantID]
	if have && now.Before(e.until) {
		s.mu.Unlock()
		return e.state
	}
	if call, running := s.calls[tenantID]; running {
		s.mu.Unlock()
		select {
		case <-call.done:
			if call.ok {
				return call.state
			}
			return s.stale(e, have, now)
		case <-ctx.Done():
			return s.stale(e, have, now)
		}
	}
	call := &inflight{done: make(chan struct{})}
	s.calls[tenantID] = call
	s.mu.Unlock()

	call.state, call.ok = s.fetch(ctx, tenantID)
	s.mu.Lock()
	if call.ok {
		s.cache[tenantID] = entry{state: call.state, at: now, until: now.Add(CacheTTL)}
	}
	delete(s.calls, tenantID)
	s.mu.Unlock()
	close(call.done)

	if call.ok {
		return call.state
	}
	return s.stale(e, have, now)
}

// stale is the error fallback: the previous answer while it is recent enough.
func (s *Service) stale(e entry, have bool, now time.Time) State {
	if have && now.Sub(e.at) <= MaxStale {
		return e.state
	}
	return State{}
}

func (s *Service) fetch(ctx context.Context, tenantID uuid.UUID) (State, bool) {
	id, found, err := s.accounts.PlanAccountID(ctx, tenantID)
	if err != nil {
		return State{}, false
	}
	if !found {
		return State{}, true // no plan holder: nothing to lock on
	}
	snap, err := s.source.Subscription(ctx, id)
	if err != nil {
		return State{}, false
	}
	return Evaluate(snap), true
}
