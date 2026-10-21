package team

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/team"
)

const (
	// roleCacheTTL bounds how stale a role read from the platform may be.
	roleCacheTTL = 60 * time.Second
	// roleCacheMax caps the cache; when full, expired entries are dropped first.
	roleCacheMax = 20000
)

type roleKey struct {
	tenant uuid.UUID
	sub    string
}

type roleEntry struct {
	role      domain.Role
	orgID     int64
	accountID int64
	expires   time.Time
}

// roleCache holds the last known role per (shop, person). Expired entries are
// kept as the "last known" value used when the platform is unreachable.
type roleCache struct {
	mu sync.Mutex
	m  map[roleKey]roleEntry
}

func newRoleCache() *roleCache { return &roleCache{m: map[roleKey]roleEntry{}} }

func (c *roleCache) get(k roleKey) (roleEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[k]
	return e, ok
}

func (c *roleCache) put(k roleKey, e roleEntry, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) >= roleCacheMax {
		for key, v := range c.m {
			if now.After(v.expires) {
				delete(c.m, key)
			}
		}
		if len(c.m) >= roleCacheMax {
			c.m = map[roleKey]roleEntry{}
		}
	}
	c.m[k] = e
}

func (c *roleCache) delete(k roleKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, k)
}

// invalidate drops every entry of an org (accountID 0) or of one member.
func (c *roleCache) invalidate(orgID int64, accountID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, v := range c.m {
		if v.orgID == orgID && (accountID == 0 || v.accountID == accountID) {
			delete(c.m, k)
		}
	}
}

// ResolveRole returns the person's role in the shop.
//
// Source of truth is the platform org membership, cached for 60s and dropped
// the moment a member event arrives. When the platform cannot be reached the
// last known role is used (in-memory first, then the persisted projection);
// the answer is never widened to owner. A person who is not a member of the
// shop gets ErrNotMember.
func (s *Service) ResolveRole(ctx context.Context, tenantID uuid.UUID, sub string) (domain.Role, error) {
	if sub == "" || tenantID == uuid.Nil {
		return "", ErrNotMember
	}
	key := roleKey{tenantID, sub}
	now := s.now()
	cached, hasCached := s.cache.get(key)
	if hasCached && now.Before(cached.expires) {
		return cached.role, nil
	}

	m, err := s.store.MappingBySub(ctx, sub)
	if err != nil {
		if hasCached {
			return cached.role, nil
		}
		return "", err
	}
	if m == nil || m.TenantID != tenantID {
		s.cache.delete(key)
		return "", ErrNotMember
	}
	projected := domain.NormalizeLegacy(m.Role, m.IsOwner)

	_, orgID, err := s.store.TenantOrg(ctx, tenantID)
	if err != nil || orgID == 0 || s.org == nil {
		// No platform organization behind this shop (yet): the local row is the truth.
		if err == nil {
			s.cache.put(key, roleEntry{role: projected, expires: now.Add(roleCacheTTL)}, now)
			return projected, nil
		}
		if hasCached {
			return cached.role, nil
		}
		return projected, nil
	}

	accountID := m.AccountID
	if accountID == 0 {
		accountID, err = s.accountID(ctx, m.IDPSubject, m.Email, m.DisplayName)
		if err != nil {
			return s.lastKnown(cached, hasCached, projected), nil
		}
	}

	member, err := s.org.GetMember(ctx, orgID, accountID)
	switch {
	case errors.Is(err, ErrNotMember) && m.IsOwner:
		// The local row says this person owns the shop. The platform failing to
		// list them is a platform-side data problem (unresolved account, missing
		// row), not a removal: log it and keep the owner in their shop instead of
		// sending them to open a second one.
		s.log.Warn("team: platform does not list the shop owner as a member, keeping local owner role",
			slog.String("tenant_id", tenantID.String()), slog.Int64("org_id", orgID), slog.Int64("account_id", accountID))
		s.cache.put(key, roleEntry{role: domain.RoleOwner, orgID: orgID, accountID: accountID, expires: now.Add(roleCacheTTL)}, now)
		return domain.RoleOwner, nil
	case errors.Is(err, ErrNotMember):
		s.cache.delete(key)
		return "", ErrNotMember
	case err != nil:
		s.log.Warn("team: platform role lookup failed, using last known role",
			slog.String("tenant_id", tenantID.String()), slog.String("error", err.Error()))
		return s.lastKnown(cached, hasCached, projected), nil
	}

	role := domain.FromPlatform(member.Role, member.ProductRoles)
	s.cache.put(key, roleEntry{role: role, orgID: orgID, accountID: accountID, expires: now.Add(roleCacheTTL)}, now)
	if string(role) != m.Role || m.AccountID != accountID {
		if err := s.store.SyncMapping(ctx, tenantID, sub, string(role), accountID); err != nil {
			s.log.Warn("team: sync mapping projection failed", slog.String("error", err.Error()))
		}
	}
	return role, nil
}

// lastKnown picks the best role when the platform is out of reach: the stale
// in-memory value, else the persisted projection.
func (s *Service) lastKnown(cached roleEntry, hasCached bool, projected domain.Role) domain.Role {
	if hasCached {
		return cached.role
	}
	return projected
}

// InvalidateMember drops cached roles for one member of an org.
func (s *Service) InvalidateMember(orgID int64, accountID int64) {
	s.cache.invalidate(orgID, accountID)
}

// Member event types published by the platform (same stream as member_joined).
const (
	EventJoined      = "identity.org.member_joined"
	EventRemoved     = "identity.org.member_removed"
	EventRoleChanged = "identity.org.member_role_changed"
)

// MemberEvent is the payload of the platform member events.
type MemberEvent struct {
	Type      string
	OrgID     int64
	AccountID int64
}

// HandleMemberEvent reacts to a platform member event: cached roles are dropped
// at once, and a removed member loses the local row so they can join elsewhere.
func (s *Service) HandleMemberEvent(ctx context.Context, ev MemberEvent) {
	s.cache.invalidate(ev.OrgID, ev.AccountID)
	if ev.Type != EventRemoved || ev.OrgID == 0 || ev.AccountID == 0 {
		return
	}
	tenantID, err := s.store.TenantByOrg(ctx, ev.OrgID)
	if err != nil || tenantID == uuid.Nil {
		return
	}
	if err := s.store.DeleteMappingByAccount(ctx, tenantID, ev.AccountID); err != nil {
		s.log.Warn("team: drop mapping after member_removed failed", slog.String("error", err.Error()))
	}
}
