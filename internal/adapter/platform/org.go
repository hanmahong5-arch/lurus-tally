package platform

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	appteam "github.com/hanmahong5-arch/lurus-tally/internal/app/team"
)

// OrgClient talks to the platform organization internal API
// (/internal/v1/organizations*, /internal/v1/invites/accept). It satisfies
// appteam.OrgClient and maps platform failures to the team sentinels.
type OrgClient struct {
	c *Client
}

var (
	_ appteam.OrgClient    = (*OrgClient)(nil)
	_ appteam.OrgDissolver = (*OrgClient)(nil)
)

// NewOrgClient wraps a configured platform Client.
func NewOrgClient(c *Client) *OrgClient { return &OrgClient{c: c} }

func orgPath(orgID int64, rest string) string {
	return "/internal/v1/organizations/" + strconv.FormatInt(orgID, 10) + rest
}

// routeMissing reports the statuses a platform answers with when it does not
// serve the endpoint at all.
func routeMissing(status int) bool {
	return status == http.StatusNotFound || status == http.StatusMethodNotAllowed || status == http.StatusNotImplemented
}

// mapOrgErr translates a platform error into a team sentinel.
func mapOrgErr(err error) error {
	if err == nil {
		return nil
	}
	var pe *Error
	if !errors.As(err, &pe) {
		return err
	}
	code := bodyErrorCode([]byte(pe.Body))
	switch {
	case pe.Code == ErrCodeUnavailable:
		return appteam.ErrUnavailable
	case code == "" && routeMissing(pe.HTTPStatus):
		// No business code means the route itself is absent (platform not upgraded
		// yet, or a proxy answered): the service is unavailable, the caller is not
		// "not a member" / "invite invalid".
		return appteam.ErrUnavailable
	case pe.HTTPStatus == http.StatusUnauthorized:
		// our own service key was rejected: a deployment problem, not the caller's.
		return appteam.ErrUnavailable
	case pe.HTTPStatus == http.StatusForbidden:
		return appteam.ErrForbidden
	case pe.HTTPStatus == http.StatusNotFound && code == "not_member":
		return appteam.ErrNotMember
	case pe.HTTPStatus == http.StatusGone || code == "invite_invalid" || (pe.HTTPStatus == http.StatusConflict && code == "org_dissolved"):
		// a dissolved org can no longer take members: its invites are dead
		return appteam.ErrInviteInvalid
	case pe.HTTPStatus == http.StatusConflict && code == "last_owner":
		return appteam.ErrLastOwner
	case pe.HTTPStatus == http.StatusConflict && code == "org_not_dissolvable":
		return appteam.ErrOrgNotDissolvable
	}
	return err
}

type memberWire struct {
	AccountID    int64             `json:"account_id"`
	Role         string            `json:"role"`
	ProductRoles map[string]string `json:"product_roles"`
	DisplayName  string            `json:"display_name"`
	EmailMasked  string            `json:"email_masked"`
	PhoneMasked  string            `json:"phone_masked"`
	JoinedAt     time.Time         `json:"joined_at"`
}

func (m memberWire) toMember() appteam.OrgMember {
	pr := m.ProductRoles
	if pr == nil {
		pr = map[string]string{}
	}
	return appteam.OrgMember{
		AccountID: m.AccountID, Role: m.Role, ProductRoles: pr,
		DisplayName: m.DisplayName, EmailMasked: m.EmailMasked, PhoneMasked: m.PhoneMasked, JoinedAt: m.JoinedAt,
	}
}

// EnsureOrg provisions (idempotently, keyed by product + external_ref) the org of a shop.
func (o *OrgClient) EnsureOrg(ctx context.Context, name string, ownerAccountID int64, product, externalRef string) (int64, error) {
	var out struct {
		OrgID int64 `json:"org_id"`
	}
	err := o.c.do(ctx, http.MethodPost, "/internal/v1/organizations", map[string]any{
		"name": name, "owner_account_id": ownerAccountID, "product": product, "external_ref": externalRef,
	}, &out)
	if err != nil {
		return 0, mapOrgErr(err)
	}
	if out.OrgID <= 0 {
		return 0, &Error{Code: ErrCodeUnknown, Message: "organization id missing in response"}
	}
	return out.OrgID, nil
}

// ListMembers returns every member of the org.
func (o *OrgClient) ListMembers(ctx context.Context, orgID int64) ([]appteam.OrgMember, error) {
	var out struct {
		Items []memberWire `json:"items"`
	}
	if err := o.c.do(ctx, http.MethodGet, orgPath(orgID, "/members"), nil, &out); err != nil {
		return nil, mapOrgErr(err)
	}
	ms := make([]appteam.OrgMember, 0, len(out.Items))
	for _, m := range out.Items {
		ms = append(ms, m.toMember())
	}
	return ms, nil
}

// GetMember returns one member, appteam.ErrNotMember when absent.
func (o *OrgClient) GetMember(ctx context.Context, orgID, accountID int64) (*appteam.OrgMember, error) {
	var out memberWire
	if err := o.c.do(ctx, http.MethodGet, orgPath(orgID, "/members/"+strconv.FormatInt(accountID, 10)), nil, &out); err != nil {
		return nil, mapOrgErr(err)
	}
	m := out.toMember()
	return &m, nil
}

// UpdateMember sets the platform role and product_roles of a member.
func (o *OrgClient) UpdateMember(ctx context.Context, orgID, accountID, actorAccountID int64, role string, productRoles map[string]string) error {
	body := map[string]any{"actor_account_id": actorAccountID, "role": role, "product_roles": productRoles}
	return mapOrgErr(o.c.do(ctx, http.MethodPatch, orgPath(orgID, "/members/"+strconv.FormatInt(accountID, 10)), body, nil))
}

// RemoveMember removes a member (or lets the member leave).
func (o *OrgClient) RemoveMember(ctx context.Context, orgID, accountID, actorAccountID int64) error {
	q := "?actor_account_id=" + strconv.FormatInt(actorAccountID, 10)
	return mapOrgErr(o.c.do(ctx, http.MethodDelete, orgPath(orgID, "/members/"+strconv.FormatInt(accountID, 10)+q), nil, nil))
}

// DissolveOrg gives up the org of an abandoned shop; the platform only agrees
// while actorAccountID is its sole member and owner (appteam.ErrOrgNotDissolvable
// otherwise) and repeats are no-ops.
func (o *OrgClient) DissolveOrg(ctx context.Context, orgID, actorAccountID int64) error {
	body := map[string]any{"actor_account_id": actorAccountID}
	return mapOrgErr(o.c.do(ctx, http.MethodPost, orgPath(orgID, "/dissolve"), body, nil))
}

// CreateInvite creates a single-use invite; the plaintext token is in the result only once.
func (o *OrgClient) CreateInvite(ctx context.Context, orgID, actorAccountID int64, role string, productRoles map[string]string, ttlHours int) (*appteam.InviteCreated, error) {
	var out struct {
		InviteID  uuid.UUID `json:"invite_id"`
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	body := map[string]any{"actor_account_id": actorAccountID, "role": role, "product_roles": productRoles, "ttl_hours": ttlHours}
	if err := o.c.do(ctx, http.MethodPost, orgPath(orgID, "/invites"), body, &out); err != nil {
		return nil, mapOrgErr(err)
	}
	return &appteam.InviteCreated{InviteID: out.InviteID, Token: out.Token, ExpiresAt: out.ExpiresAt}, nil
}

// ListInvites returns the pending invites (no tokens).
func (o *OrgClient) ListInvites(ctx context.Context, orgID int64) ([]appteam.OrgInvite, error) {
	var out struct {
		Items []struct {
			InviteID     uuid.UUID         `json:"invite_id"`
			Role         string            `json:"role"`
			ProductRoles map[string]string `json:"product_roles"`
			InvitedBy    int64             `json:"invited_by_account_id"`
			ExpiresAt    time.Time         `json:"expires_at"`
			CreatedAt    time.Time         `json:"created_at"`
		} `json:"items"`
	}
	if err := o.c.do(ctx, http.MethodGet, orgPath(orgID, "/invites"), nil, &out); err != nil {
		return nil, mapOrgErr(err)
	}
	ivs := make([]appteam.OrgInvite, 0, len(out.Items))
	for _, i := range out.Items {
		ivs = append(ivs, appteam.OrgInvite{
			ID: i.InviteID, Role: i.Role, ProductRoles: i.ProductRoles,
			InvitedBy: i.InvitedBy, ExpiresAt: i.ExpiresAt, CreatedAt: i.CreatedAt,
		})
	}
	return ivs, nil
}

// RevokeInvite cancels a pending invite.
func (o *OrgClient) RevokeInvite(ctx context.Context, orgID int64, inviteID uuid.UUID, actorAccountID int64) error {
	q := "?actor_account_id=" + strconv.FormatInt(actorAccountID, 10)
	err := o.c.do(ctx, http.MethodDelete, orgPath(orgID, "/invites/"+url.PathEscape(inviteID.String())+q), nil, nil)
	var pe *Error
	if errors.As(err, &pe) && pe.HTTPStatus == http.StatusNotFound && bodyErrorCode([]byte(pe.Body)) != "" {
		return appteam.ErrInviteInvalid
	}
	return mapOrgErr(err)
}

// AcceptInvite redeems an invite token for an account.
func (o *OrgClient) AcceptInvite(ctx context.Context, token string, accountID int64) (*appteam.AcceptResult, error) {
	var out struct {
		OrgID        int64             `json:"org_id"`
		Role         string            `json:"role"`
		ProductRoles map[string]string `json:"product_roles"`
	}
	err := o.c.do(ctx, http.MethodPost, "/internal/v1/invites/accept", map[string]any{"token": token, "account_id": accountID}, &out)
	if err != nil {
		var pe *Error
		if errors.As(err, &pe) && pe.HTTPStatus == http.StatusNotFound && bodyErrorCode([]byte(pe.Body)) != "" {
			return nil, appteam.ErrInviteInvalid
		}
		return nil, mapOrgErr(err)
	}
	return &appteam.AcceptResult{OrgID: out.OrgID, Role: out.Role, ProductRoles: out.ProductRoles}, nil
}

// AccountResolver maps IdP subjects to platform account ids via the idempotent
// upsert endpoint. Account ids never change, so results are cached for the
// life of the process.
type AccountResolver struct {
	c     *Client
	mu    sync.Mutex
	cache map[string]int64
}

var _ appteam.AccountResolver = (*AccountResolver)(nil)

// NewAccountResolver wraps a configured platform Client.
func NewAccountResolver(c *Client) *AccountResolver {
	return &AccountResolver{c: c, cache: map[string]int64{}}
}

// EnsureAccount returns the platform account id of an IdP subject.
func (r *AccountResolver) EnsureAccount(ctx context.Context, idpSubject, email, displayName string) (int64, error) {
	r.mu.Lock()
	id, ok := r.cache[idpSubject]
	r.mu.Unlock()
	if ok {
		return id, nil
	}
	if strings.TrimSpace(email) == "" {
		email = idpSubject + "@idp.local"
	}
	acc, err := r.c.UpsertAccount(ctx, UpsertAccountRequest{IDPSubject: idpSubject, Email: email, DisplayName: displayName})
	if err != nil {
		return 0, mapOrgErr(err)
	}
	if acc.ID <= 0 {
		return 0, appteam.ErrUnavailable
	}
	r.mu.Lock()
	if len(r.cache) > 50000 {
		r.cache = map[string]int64{}
	}
	r.cache[idpSubject] = acc.ID
	r.mu.Unlock()
	return acc.ID, nil
}
