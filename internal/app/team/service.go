package team

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/app/entitlement"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/team"
)

const (
	// Product / default invite lifetime handed to the platform.
	platformProduct  = "tally"
	defaultInviteTTL = 72
	maxInviteTTL     = 168
)

// Service bundles the team use cases. org and accounts may be nil: production
// without the platform internal address/key (team features answer
// ErrUnavailable) and plain unit setups.
type Service struct {
	store    Store
	org      OrgClient
	accounts AccountResolver
	cache    *roleCache
	log      *slog.Logger
	now      func() time.Time
	guard    PlanGuard
}

// PlanGuard checks the plan's member limit before a new member can be added.
// *entitlement.Guard satisfies it; nil means no limit is applied.
type PlanGuard interface {
	Check(ctx context.Context, tenantID uuid.UUID, key string, delta int) error
}

// WithPlanGuard enables the max_users plan check on CreateInvite and Join.
func (s *Service) WithPlanGuard(g PlanGuard) *Service {
	s.guard = g
	return s
}

func (s *Service) checkMemberLimit(ctx context.Context, tenantID uuid.UUID) error {
	if s.guard == nil {
		return nil
	}
	return s.guard.Check(ctx, tenantID, entitlement.KeyMaxUsers, 1)
}

// NewService wires the team use cases. A nil logger falls back to slog.Default().
func NewService(store Store, org OrgClient, accounts AccountResolver, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{store: store, org: org, accounts: accounts, cache: newRoleCache(), log: log, now: time.Now}
}

// Available reports whether the platform organization API is configured.
func (s *Service) Available() bool { return s != nil && s.org != nil }

// Caller identifies the signed-in person acting on a shop.
type Caller struct {
	TenantID uuid.UUID
	Sub      string
	Email    string
	Name     string
}

func (s *Service) accountID(ctx context.Context, sub, email, name string) (int64, error) {
	if s.accounts == nil {
		return 0, ErrUnavailable
	}
	if email == "" {
		email = sub + "@idp.local"
	}
	return s.accounts.EnsureAccount(ctx, sub, email, name)
}

// EnsureTenantOrg makes sure the shop has a platform organization owned by
// ownerAccountID and pins its id on the tenant row. Idempotent.
func (s *Service) EnsureTenantOrg(ctx context.Context, tenantID uuid.UUID, ownerAccountID int64) error {
	_, err := s.ensureOrg(ctx, tenantID, ownerAccountID)
	return err
}

func (s *Service) ensureOrg(ctx context.Context, tenantID uuid.UUID, ownerAccountID int64) (int64, error) {
	name, orgID, err := s.store.TenantOrg(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	if orgID != 0 {
		return orgID, nil
	}
	if s.org == nil {
		return 0, ErrUnavailable
	}
	if ownerAccountID <= 0 {
		return 0, ErrOrgNotReady
	}
	orgID, err = s.org.EnsureOrg(ctx, name, ownerAccountID, platformProduct, tenantID.String())
	if err != nil {
		return 0, err
	}
	if err := s.store.SetTenantOrg(ctx, tenantID, orgID); err != nil {
		return 0, err
	}
	return orgID, nil
}

// actor is the verified caller context for a team operation.
type actor struct {
	role      domain.Role
	accountID int64
	orgID     int64
}

// authorize resolves the caller's role, requires owner/manager, and returns the
// shop's org (lazily creating it when the caller is the owner).
func (s *Service) authorize(ctx context.Context, c Caller) (*actor, error) {
	if s.org == nil {
		return nil, ErrUnavailable
	}
	role, err := s.ResolveRole(ctx, c.TenantID, c.Sub)
	if err != nil {
		if errors.Is(err, ErrNotMember) {
			return nil, ErrForbidden
		}
		return nil, err
	}
	if !role.ManagesTeam() {
		return nil, ErrForbidden
	}
	m, err := s.store.MappingBySub(ctx, c.Sub)
	if err != nil {
		return nil, err
	}
	accountID := int64(0)
	if m != nil {
		accountID = m.AccountID
	}
	if accountID == 0 {
		email, name := c.Email, c.Name
		if m != nil {
			email, name = m.Email, m.DisplayName
		}
		if accountID, err = s.accountID(ctx, c.Sub, email, name); err != nil {
			return nil, err
		}
		if err := s.store.SyncMapping(ctx, c.TenantID, c.Sub, string(role), accountID); err != nil {
			s.log.Warn("team: persist account id failed", slog.String("error", err.Error()))
		}
	}
	_, orgID, err := s.store.TenantOrg(ctx, c.TenantID)
	if err != nil {
		return nil, err
	}
	if orgID == 0 {
		if role != domain.RoleOwner {
			return nil, ErrOrgNotReady
		}
		if orgID, err = s.ensureOrg(ctx, c.TenantID, accountID); err != nil {
			return nil, err
		}
	}
	return &actor{role: role, accountID: accountID, orgID: orgID}, nil
}

// MemberView is a member as shown to the shop owner/manager.
type MemberView struct {
	AccountID   int64       `json:"account_id"`
	Role        domain.Role `json:"role"`
	RoleLabel   string      `json:"role_label"`
	DisplayName string      `json:"display_name"`
	EmailMasked string      `json:"email_masked"`
	PhoneMasked string      `json:"phone_masked"`
	JoinedAt    time.Time   `json:"joined_at"`
	IsSelf      bool        `json:"is_self"`
	// RoleHint is a gentle suggestion shown next to the role, e.g. a cashier in a shop without a cashier desk.
	RoleHint string `json:"role_hint,omitempty"`
}

// cashierHint is shown for cashiers of shops that have no cashier desk.
const cashierHint = "建议改为店员"

// CashierAllowed reports whether the shop may have cashier members: only retail
// shops have a cashier desk (hybrid and cross-border do not, matching POS and the
// cashier PIN). When the shop type cannot be read, nothing is blocked.
func (s *Service) CashierAllowed(ctx context.Context, tenantID uuid.UUID) bool {
	r, ok := s.store.(ShopTypeReader)
	if !ok {
		return true
	}
	pt, err := r.ProfileType(ctx, tenantID)
	if err != nil || pt == "" {
		return true
	}
	return pt == "retail"
}

// ListMembers lists the shop's members. Owner/manager only.
func (s *Service) ListMembers(ctx context.Context, c Caller) ([]MemberView, error) {
	a, err := s.authorize(ctx, c)
	if err != nil {
		return nil, err
	}
	ms, err := s.org.ListMembers(ctx, a.orgID)
	if err != nil {
		return nil, err
	}
	out := make([]MemberView, 0, len(ms))
	cashierOK := s.CashierAllowed(ctx, c.TenantID)
	for _, m := range ms {
		r := domain.FromPlatform(m.Role, m.ProductRoles)
		hint := ""
		if r == domain.RoleCashier && !cashierOK {
			hint = cashierHint
		}
		out = append(out, MemberView{RoleHint: hint,
			AccountID: m.AccountID, Role: r, RoleLabel: r.Label(),
			DisplayName: m.DisplayName, EmailMasked: m.EmailMasked, PhoneMasked: m.PhoneMasked,
			JoinedAt: m.JoinedAt, IsSelf: m.AccountID == a.accountID,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].JoinedAt.Before(out[j].JoinedAt) })
	return out, nil
}

// UpdateMemberRole changes a member's shop role. The owner row cannot be changed.
func (s *Service) UpdateMemberRole(ctx context.Context, c Caller, targetAccountID int64, newRole domain.Role) error {
	platformRole, platformPR, err := domain.ToPlatform(newRole)
	if err != nil {
		return ErrInvalidRole
	}
	a, err := s.authorize(ctx, c)
	if err != nil {
		return err
	}
	if newRole == domain.RoleCashier && !s.CashierAllowed(ctx, c.TenantID) {
		return ErrRoleNotForShop
	}
	target, err := s.org.GetMember(ctx, a.orgID, targetAccountID)
	if err != nil {
		return err
	}
	targetRole := domain.FromPlatform(target.Role, target.ProductRoles)
	if !domain.CanManage(a.role, targetRole) || !domain.CanAssign(a.role, newRole) {
		return ErrForbidden
	}
	// The platform merges product_roles per key, so send only Tally's own key:
	// other products' roles on the member are untouched, and an empty value
	// (a manager has no product role) clears Tally's key.
	pr := map[string]string{domain.ProductKey: platformPR[domain.ProductKey]}
	if err := s.org.UpdateMember(ctx, a.orgID, targetAccountID, a.accountID, platformRole, pr); err != nil {
		return err
	}
	s.cache.invalidate(a.orgID, targetAccountID)
	if tm, _ := s.store.MappingByAccount(ctx, c.TenantID, targetAccountID); tm != nil {
		if err := s.store.SyncMapping(ctx, c.TenantID, tm.IDPSubject, string(newRole), targetAccountID); err != nil {
			s.log.Warn("team: sync mapping after role change failed", slog.String("error", err.Error()))
		}
	}
	return nil
}

// RemoveMember removes a member from the shop. A person may always leave on
// their own, except the owner (the last owner is protected).
func (s *Service) RemoveMember(ctx context.Context, c Caller, targetAccountID int64) error {
	a, err := s.authorize(ctx, c)
	if err != nil {
		return err
	}
	if targetAccountID == a.accountID {
		if a.role == domain.RoleOwner {
			return ErrLastOwner
		}
	} else {
		target, err := s.org.GetMember(ctx, a.orgID, targetAccountID)
		if err != nil {
			return err
		}
		if !domain.CanManage(a.role, domain.FromPlatform(target.Role, target.ProductRoles)) {
			return ErrForbidden
		}
	}
	if err := s.org.RemoveMember(ctx, a.orgID, targetAccountID, a.accountID); err != nil {
		return err
	}
	s.cache.invalidate(a.orgID, targetAccountID)
	if err := s.store.DeleteMappingByAccount(ctx, c.TenantID, targetAccountID); err != nil {
		s.log.Warn("team: drop mapping after removal failed", slog.String("error", err.Error()))
	}
	return nil
}

// CreateInvite issues a one-time invite for the given shop role. The token is
// only available in the returned value.
func (s *Service) CreateInvite(ctx context.Context, c Caller, role domain.Role, ttlHours int) (*InviteCreated, error) {
	platformRole, pr, err := domain.ToPlatform(role)
	if err != nil {
		return nil, ErrInvalidRole
	}
	a, err := s.authorize(ctx, c)
	if err != nil {
		return nil, err
	}
	if !domain.CanAssign(a.role, role) {
		return nil, ErrForbidden
	}
	if role == domain.RoleCashier && !s.CashierAllowed(ctx, c.TenantID) {
		return nil, ErrRoleNotForShop
	}
	if err := s.checkMemberLimit(ctx, c.TenantID); err != nil {
		return nil, err
	}
	if ttlHours <= 0 {
		ttlHours = defaultInviteTTL
	}
	if ttlHours > maxInviteTTL {
		ttlHours = maxInviteTTL
	}
	res, err := s.org.CreateInvite(ctx, a.orgID, a.accountID, platformRole, pr, ttlHours)
	if err != nil {
		return nil, err
	}
	res.ShopName = s.ShopDisplayName(ctx, c.TenantID)
	return res, nil
}

// ShopDisplayName is the name people know the shop by: the one the owner printed
// on receipts, else the tenant's own name. Invite and join must agree on it.
func (s *Service) ShopDisplayName(ctx context.Context, tenantID uuid.UUID) string {
	name, _, _ := s.store.TenantOrg(ctx, tenantID)
	if ps, ok := s.store.(printShopNamer); ok {
		if pn, perr := ps.PrintShopName(ctx, tenantID); perr == nil && strings.TrimSpace(pn) != "" {
			return strings.TrimSpace(pn)
		}
	}
	return name
}

// printShopNamer is an optional Store capability: the shop name saved in the print settings.
type printShopNamer interface {
	PrintShopName(ctx context.Context, tenantID uuid.UUID) (string, error)
}

// InviteView is a pending invite as shown to the shop owner/manager.
type InviteView struct {
	ID        uuid.UUID   `json:"id"`
	Role      domain.Role `json:"role"`
	RoleLabel string      `json:"role_label"`
	ExpiresAt time.Time   `json:"expires_at"`
	CreatedAt time.Time   `json:"created_at"`
}

// ListInvites lists pending invites. Owner/manager only.
func (s *Service) ListInvites(ctx context.Context, c Caller) ([]InviteView, error) {
	a, err := s.authorize(ctx, c)
	if err != nil {
		return nil, err
	}
	ivs, err := s.org.ListInvites(ctx, a.orgID)
	if err != nil {
		return nil, err
	}
	out := make([]InviteView, 0, len(ivs))
	for _, iv := range ivs {
		r := domain.FromPlatform(iv.Role, iv.ProductRoles)
		out = append(out, InviteView{ID: iv.ID, Role: r, RoleLabel: r.Label(), ExpiresAt: iv.ExpiresAt, CreatedAt: iv.CreatedAt})
	}
	return out, nil
}

// RevokeInvite cancels a pending invite. Owner/manager only.
func (s *Service) RevokeInvite(ctx context.Context, c Caller, inviteID uuid.UUID) error {
	a, err := s.authorize(ctx, c)
	if err != nil {
		return err
	}
	return s.org.RevokeInvite(ctx, a.orgID, inviteID, a.accountID)
}

// JoinInput is a signed-in person redeeming an invite token.
type JoinInput struct {
	Sub   string
	Email string
	Name  string
	Token string
	// InviteID is the invite's id as carried on the link (optional). It only lets Join
	// recognise an invite of the caller's own shop without consuming it.
	InviteID uuid.UUID
	// AbandonEmptyShop confirms giving up the caller's own shop first; allowed
	// only when it holds nothing but sample data and the caller is its sole member and owner.
	AbandonEmptyShop bool
}

// JoinResult is the shop the person has joined and the role they got.
type JoinResult struct {
	TenantID uuid.UUID
	ShopName string
	Role     domain.Role
}

// abandonable reports whether m's shop is an empty one its sole owner may give up.
func (s *Service) abandonable(ctx context.Context, m *Mapping) (bool, error) {
	if !m.IsOwner {
		return false, nil
	}
	n, err := s.store.MemberCount(ctx, m.TenantID)
	if err != nil || n != 1 {
		return false, err
	}
	empty, err := s.store.ShopIsEmpty(ctx, m.TenantID)
	if err != nil || !empty {
		return false, err
	}
	_, orgID, err := s.store.TenantOrg(ctx, m.TenantID)
	if err != nil {
		return false, err
	}
	if orgID != 0 && s.org != nil {
		ms, err := s.org.ListMembers(ctx, orgID)
		if err != nil {
			return false, err
		}
		if len(ms) > 1 || (len(ms) == 1 && m.AccountID > 0 && ms[0].AccountID != m.AccountID) {
			return false, nil
		}
	}
	return true, nil
}

// isOwnShopInvite reports whether inviteID is among the pending invites of m's own
// shop. It only reads the list, so the invite is never consumed.
func (s *Service) isOwnShopInvite(ctx context.Context, m *Mapping, inviteID uuid.UUID) (bool, error) {
	if inviteID == uuid.Nil || s.org == nil {
		return false, nil
	}
	_, orgID, err := s.store.TenantOrg(ctx, m.TenantID)
	if err != nil || orgID == 0 {
		return false, err
	}
	ivs, err := s.org.ListInvites(ctx, orgID)
	if err != nil {
		return false, err
	}
	for _, iv := range ivs {
		if iv.ID == inviteID {
			return true, nil
		}
	}
	return false, nil
}

// Join redeems an invite. Phase 1 is one person, one shop, so a person who is
// already mapped to a shop is refused before the invite is consumed, unless
// they confirm abandoning an empty shop of their own (see abandonable).
func (s *Service) Join(ctx context.Context, in JoinInput) (*JoinResult, error) {
	if s.org == nil {
		return nil, ErrUnavailable
	}
	if in.Sub == "" || in.Token == "" {
		return nil, ErrInviteInvalid
	}
	existing, err := s.store.MappingBySub(ctx, in.Sub)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		own, oerr := s.isOwnShopInvite(ctx, existing, in.InviteID)
		if oerr != nil {
			s.log.Warn("team: own-shop invite check failed", slog.String("error", oerr.Error()))
			return nil, ErrUnavailable
		}
		if own {
			return nil, ErrAlreadyMemberHere
		}
		can, aerr := s.abandonable(ctx, existing)
		if aerr != nil {
			// A platform or database failure is not "the shop is not empty".
			s.log.Warn("team: abandon check failed", slog.String("error", aerr.Error()))
			return nil, ErrUnavailable
		}
		if !in.AbandonEmptyShop {
			return nil, &AlreadyInShopError{CanAbandon: can, IsOwner: existing.IsOwner}
		}
		if !existing.IsOwner {
			// A staff member has no shop of their own to give up.
			return nil, &AlreadyInShopError{IsOwner: false}
		}
		if !can {
			return nil, ErrShopNotEmpty
		}
	}
	accountID, err := s.accountID(ctx, in.Sub, in.Email, in.Name)
	if err != nil {
		return nil, err
	}
	res, err := s.org.AcceptInvite(ctx, in.Token, accountID)
	if err != nil {
		return nil, err
	}
	tenantID, err := s.store.TenantByOrg(ctx, res.OrgID)
	if err != nil {
		return nil, err
	}
	if tenantID == uuid.Nil {
		// The invite belonged to an org no live shop is linked to (e.g. an abandoned
		// one): do not stay a member of it.
		s.leaveOrg(ctx, res.OrgID, accountID)
		return nil, ErrInviteInvalid
	}
	if existing != nil && existing.TenantID == tenantID {
		// An old link without an invite id led back to the person's own shop. The
		// accept was idempotent for a member, so nothing needs undoing.
		s.log.Warn("team: invite accepted into own shop", slog.Int64("org_id", res.OrgID))
		return nil, ErrAlreadyMemberHere
	}
	// The platform join is already done; a full shop must not keep the new member.
	if err := s.checkMemberLimit(ctx, tenantID); err != nil {
		s.leaveOrg(ctx, res.OrgID, accountID)
		return nil, err
	}
	if existing != nil {
		if err := s.store.AbandonShop(ctx, existing.TenantID, in.Sub); err != nil {
			if !s.abandonRaced(ctx, existing.TenantID, in.Sub, err) {
				// The shop gained data (or the database failed): undo the platform join.
				s.leaveOrg(ctx, res.OrgID, accountID)
				return nil, err
			}
			// A concurrent request already retired the old shop. The platform join is
			// shared with that request (accepting is idempotent), so it stays; the
			// mapping insert below decides which shop the person ends up in.
		} else {
			s.cache.delete(roleKey{existing.TenantID, in.Sub})
			s.retireOrg(ctx, existing.TenantID, accountID)
		}
	}
	role := domain.FromPlatform(res.Role, res.ProductRoles)
	email := in.Email
	if email == "" {
		email = in.Sub + "@idp.local"
	}
	err = s.store.InsertMapping(ctx, Mapping{
		TenantID: tenantID, IDPSubject: in.Sub, Email: email, DisplayName: in.Name,
		Role: string(role), IsOwner: role == domain.RoleOwner, AccountID: accountID,
	})
	if errors.Is(err, ErrMappingExists) {
		cur, lerr := s.store.MappingBySub(ctx, in.Sub)
		if lerr != nil {
			return nil, lerr
		}
		if cur != nil && cur.TenantID != tenantID {
			// Raced into another shop: do not stay a member of the invited one.
			s.leaveOrg(ctx, res.OrgID, accountID)
			return nil, &AlreadyInShopError{}
		}
		err = nil
	}
	if err != nil {
		s.leaveOrg(ctx, res.OrgID, accountID)
		return nil, fmt.Errorf("team join: persist mapping: %w", err)
	}
	s.cache.invalidate(res.OrgID, accountID)
	return &JoinResult{TenantID: tenantID, ShopName: s.ShopDisplayName(ctx, tenantID), Role: role}, nil
}

// abandonRaced reports whether a refused AbandonShop only lost a race: the store
// answers "not empty" both when the shop gained data and when a concurrent request
// already retired the person's mapping, and only the second case has no mapping left.
func (s *Service) abandonRaced(ctx context.Context, oldTenant uuid.UUID, sub string, err error) bool {
	if !errors.Is(err, ErrShopNotEmpty) {
		return false
	}
	cur, lerr := s.store.MappingBySub(ctx, sub)
	if lerr != nil {
		return false
	}
	return cur == nil || cur.TenantID != oldTenant
}

// leaveOrg takes the person out of an organization they just joined by invite
// and drops the cached role; a failure is logged (the platform member list then
// shows the person until the owner removes them).
func (s *Service) leaveOrg(ctx context.Context, orgID, accountID int64) {
	if err := s.org.RemoveMember(ctx, orgID, accountID, accountID); err != nil {
		s.log.Warn("team: leave invited org failed", slog.Int64("org_id", orgID), slog.String("error", err.Error()))
	}
	s.cache.invalidate(orgID, accountID)
}

// retireOrg cleans up the platform organization behind an abandoned shop: every
// pending invite is revoked so nobody can still be added to a dead shop, then the
// organization is dissolved when the client supports it. Failures only leave a
// harmless single-member organization behind.
func (s *Service) retireOrg(ctx context.Context, tenantID uuid.UUID, accountID int64) {
	_, orgID, err := s.store.TenantOrg(ctx, tenantID)
	if err != nil || orgID == 0 {
		return
	}
	invites, err := s.org.ListInvites(ctx, orgID)
	if err != nil {
		s.log.Warn("team: list abandoned shop invites failed", slog.Int64("org_id", orgID), slog.String("error", err.Error()))
	}
	for _, inv := range invites {
		if err := s.org.RevokeInvite(ctx, orgID, inv.ID, accountID); err != nil {
			s.log.Warn("team: revoke abandoned shop invite failed", slog.Int64("org_id", orgID), slog.String("error", err.Error()))
		}
	}
	if d, ok := s.org.(OrgDissolver); ok {
		if err := d.DissolveOrg(ctx, orgID, accountID); err != nil {
			s.log.Warn("team: dissolve abandoned shop org failed", slog.Int64("org_id", orgID), slog.String("error", err.Error()))
		}
	}
}
