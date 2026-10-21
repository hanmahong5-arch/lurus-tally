// Package team implements the shop-team use cases: membership lives in the
// platform organization behind each shop, the shop-level role is derived from
// it, and a local projection (user_identity_mapping) is kept in sync.
package team

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Errors surfaced to the handler layer.
var (
	// ErrUnavailable — team features are not configured (no platform internal
	// address/key in production) or the platform is unreachable.
	ErrUnavailable = errors.New("team: platform organization api unavailable")
	// ErrForbidden — the caller's role does not allow this action.
	ErrForbidden = errors.New("team: forbidden")
	// ErrNotMember — the person is not a member of the shop.
	ErrNotMember = errors.New("team: not a member")
	// ErrAlreadyInShop — phase 1 is one person, one shop.
	ErrAlreadyInShop = errors.New("team: account already belongs to a shop")
	// ErrAlreadyMemberHere — the invite is one of the caller's own shop's pending invites.
	ErrAlreadyMemberHere = errors.New("team: already a member of the inviting shop")
	// ErrInviteInvalid — invite expired, revoked, used or unknown.
	ErrInviteInvalid = errors.New("team: invite invalid")
	// ErrLastOwner — the last owner can neither be removed nor changed.
	ErrLastOwner = errors.New("team: last owner protected")
	// ErrInvalidRole — the requested role cannot be assigned.
	ErrInvalidRole = errors.New("team: invalid role")
	// ErrOrgNotReady — the shop has no platform organization yet and the caller cannot create it.
	ErrOrgNotReady = errors.New("team: shop organization not provisioned")
	// ErrTenantNotFound — no such shop.
	ErrTenantNotFound = errors.New("team: tenant not found")
	// ErrMappingExists — the identity subject is already mapped to a shop.
	ErrMappingExists = errors.New("team: identity mapping exists")
	// ErrShopNotEmpty — the caller's own shop holds real data or other members, so it cannot be abandoned.
	ErrShopNotEmpty = errors.New("team: shop has real data or other members")
	// ErrRoleNotForShop — the role does not fit this kind of shop (cashier outside retail).
	ErrRoleNotForShop = errors.New("team: role not for this shop type")
	// ErrOrgNotDissolvable — the platform refused to dissolve an organization that still
	// has other members (or whose owner is not the caller).
	ErrOrgNotDissolvable = errors.New("team: organization has other members")
)

// AlreadyInShopError is ErrAlreadyInShop plus whether the caller's current shop
// is an empty one they may abandon in order to join (errors.Is matches ErrAlreadyInShop).
type AlreadyInShopError struct {
	CanAbandon bool
	IsOwner    bool // the caller's current mapping is the owner of its shop
}

func (e *AlreadyInShopError) Error() string { return ErrAlreadyInShop.Error() }

// Is lets errors.Is(err, ErrAlreadyInShop) match.
func (e *AlreadyInShopError) Is(target error) bool { return target == ErrAlreadyInShop }

// OrgMember is a platform organization member as the platform reports it.
type OrgMember struct {
	AccountID    int64
	Role         string
	ProductRoles map[string]string
	DisplayName  string
	EmailMasked  string
	PhoneMasked  string
	JoinedAt     time.Time
}

// OrgInvite is a pending invite (never carries the token).
type OrgInvite struct {
	ID           uuid.UUID
	Role         string
	ProductRoles map[string]string
	InvitedBy    int64
	ExpiresAt    time.Time
	CreatedAt    time.Time
}

// InviteCreated is the one-time creation result; Token is shown exactly once.
type InviteCreated struct {
	InviteID  uuid.UUID
	Token     string
	ExpiresAt time.Time
	ShopName  string // filled by the service, for the display-only link parameters
}

// AcceptResult is what the platform reports after an invite is accepted.
type AcceptResult struct {
	OrgID        int64
	Role         string
	ProductRoles map[string]string
}

// OrgClient is the platform organization internal API. Implementations map
// platform errors to the sentinels above (ErrNotMember, ErrForbidden,
// ErrLastOwner, ErrInviteInvalid, ErrUnavailable).
type OrgClient interface {
	EnsureOrg(ctx context.Context, name string, ownerAccountID int64, product, externalRef string) (int64, error)
	ListMembers(ctx context.Context, orgID int64) ([]OrgMember, error)
	GetMember(ctx context.Context, orgID int64, accountID int64) (*OrgMember, error)
	UpdateMember(ctx context.Context, orgID int64, accountID, actorAccountID int64, role string, productRoles map[string]string) error
	RemoveMember(ctx context.Context, orgID int64, accountID, actorAccountID int64) error
	CreateInvite(ctx context.Context, orgID int64, actorAccountID int64, role string, productRoles map[string]string, ttlHours int) (*InviteCreated, error)
	ListInvites(ctx context.Context, orgID int64) ([]OrgInvite, error)
	RevokeInvite(ctx context.Context, orgID int64, inviteID uuid.UUID, actorAccountID int64) error
	AcceptInvite(ctx context.Context, token string, accountID int64) (*AcceptResult, error)
}

// OrgDissolver is optionally implemented by an OrgClient that can dissolve a
// platform organization its only member (the owner) no longer needs. Without it
// an abandoned shop leaves a harmless single-member organization behind.
type OrgDissolver interface {
	DissolveOrg(ctx context.Context, orgID int64, actorAccountID int64) error
}

// AccountResolver maps an IdP subject to its platform account id (idempotent upsert).
type AccountResolver interface {
	EnsureAccount(ctx context.Context, idpSubject, email, displayName string) (int64, error)
}

// Mapping is the local projection row of one person in one shop.
type Mapping struct {
	TenantID    uuid.UUID
	IDPSubject  string
	Email       string
	DisplayName string
	Role        string
	IsOwner     bool
	AccountID   int64 // 0 = not yet resolved
}

// Store is the local persistence the team use cases need.
type Store interface {
	// TenantOrg returns the shop name and its platform org id (0 when unset).
	TenantOrg(ctx context.Context, tenantID uuid.UUID) (name string, orgID int64, err error)
	SetTenantOrg(ctx context.Context, tenantID uuid.UUID, orgID int64) error
	// TenantByOrg returns uuid.Nil, nil when no shop is linked to the org.
	TenantByOrg(ctx context.Context, orgID int64) (uuid.UUID, error)
	MappingBySub(ctx context.Context, sub string) (*Mapping, error)
	MappingByAccount(ctx context.Context, tenantID uuid.UUID, accountID int64) (*Mapping, error)
	// InsertMapping returns ErrMappingExists when the subject is already mapped.
	InsertMapping(ctx context.Context, m Mapping) error
	// SyncMapping refreshes role (+account id when > 0) of an existing row.
	SyncMapping(ctx context.Context, tenantID uuid.UUID, sub, role string, accountID int64) error
	DeleteMappingByAccount(ctx context.Context, tenantID uuid.UUID, accountID int64) error
	// MemberCount is the number of local member rows of a shop.
	MemberCount(ctx context.Context, tenantID uuid.UUID) (int, error)
	// ShopIsEmpty reports a shop that holds only the onboarding sample data: no
	// real product, bill, payment, stock record, project or non-sample warehouse/partner.
	ShopIsEmpty(ctx context.Context, tenantID uuid.UUID) (bool, error)
	// AbandonShop soft-deletes a shop and its owner mapping (data kept for tracing),
	// freeing the subject to be mapped elsewhere. It re-checks emptiness and sole
	// ownership atomically and returns ErrShopNotEmpty when they no longer hold.
	AbandonShop(ctx context.Context, tenantID uuid.UUID, sub string) error
}

// ShopTypeReader is an optional Store capability: the shop's chosen business
// type ("retail", "horticulture", ...), empty when none is set.
type ShopTypeReader interface {
	ProfileType(ctx context.Context, tenantID uuid.UUID) (string, error)
}
