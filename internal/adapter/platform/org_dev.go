package platform

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/google/uuid"

	appteam "github.com/hanmahong5-arch/lurus-tally/internal/app/team"
)

// DevOrgClient is a development-only stand-in for the platform organization
// API: same interface (appteam.OrgClient + appteam.AccountResolver), data kept
// in the local dev_* tables of migration 000059. It mirrors the platform's
// membership rules (only owner/admin manage, admin cannot grant or touch
// admin, owner is immutable and protected, single-use hashed invite tokens).
// NewDevOrgClient refuses to construct unless dev mode is explicitly on.
type DevOrgClient struct {
	db  *sql.DB
	now func() time.Time
}

var (
	_ appteam.OrgClient       = (*DevOrgClient)(nil)
	_ appteam.AccountResolver = (*DevOrgClient)(nil)
)

// NewDevOrgClient builds the stand-in; devMode must be true (TALLY_DEV_MODE).
func NewDevOrgClient(db *sql.DB, devMode bool) (*DevOrgClient, error) {
	if !devMode {
		return nil, errors.New("platform: dev organization stand-in requires TALLY_DEV_MODE=true")
	}
	if db == nil {
		return nil, errors.New("platform: dev organization stand-in requires a database")
	}
	return &DevOrgClient{db: db, now: time.Now}, nil
}

// EnsureAccount derives a stable fake account id from the subject.
func (d *DevOrgClient) EnsureAccount(ctx context.Context, idpSubject, email, displayName string) (int64, error) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(idpSubject))
	id := int64(h.Sum32()) + 1_000_000 // keep clear of small real ids
	if _, err := d.db.ExecContext(ctx, `
		INSERT INTO tally.dev_account (account_id, idp_subject, email, display_name)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (idp_subject) DO UPDATE SET email = EXCLUDED.email,
			display_name = CASE WHEN EXCLUDED.display_name <> '' THEN EXCLUDED.display_name ELSE tally.dev_account.display_name END`,
		id, idpSubject, email, displayName); err != nil {
		return 0, fmt.Errorf("dev org: ensure account: %w", err)
	}
	return id, nil
}

func decodePR(raw []byte) map[string]string {
	pr := map[string]string{}
	_ = json.Unmarshal(raw, &pr)
	return pr
}

func encodePR(pr map[string]string) []byte {
	if pr == nil {
		pr = map[string]string{}
	}
	b, _ := json.Marshal(pr)
	return b
}

// EnsureOrg creates the org (and its owner) once per (product, external_ref).
func (d *DevOrgClient) EnsureOrg(ctx context.Context, name string, ownerAccountID int64, product, externalRef string) (int64, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var id int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM tally.dev_org WHERE product = $1 AND external_ref = $2`, product, externalRef).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		if err = tx.QueryRowContext(ctx,
			`INSERT INTO tally.dev_org (product, external_ref, name) VALUES ($1, $2, $3) RETURNING id`,
			product, externalRef, name).Scan(&id); err != nil {
			return 0, fmt.Errorf("dev org: create: %w", err)
		}
		if _, err = tx.ExecContext(ctx,
			`INSERT INTO tally.dev_org_member (org_id, account_id, role) VALUES ($1, $2, 'owner')`, id, ownerAccountID); err != nil {
			return 0, fmt.Errorf("dev org: add owner: %w", err)
		}
	} else if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

type memberQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (d *DevOrgClient) role(ctx context.Context, q memberQuerier, orgID, accountID int64) (string, map[string]string, error) {
	var role string
	var raw []byte
	err := q.QueryRowContext(ctx,
		`SELECT role, product_roles FROM tally.dev_org_member WHERE org_id = $1 AND account_id = $2`, orgID, accountID).Scan(&role, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, appteam.ErrNotMember
	}
	if err != nil {
		return "", nil, err
	}
	return role, decodePR(raw), nil
}

func (d *DevOrgClient) memberRows(ctx context.Context, orgID, only int64) ([]appteam.OrgMember, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT m.account_id, m.role, m.product_roles, COALESCE(a.display_name, ''), COALESCE(a.email, ''), m.joined_at
		FROM tally.dev_org_member m LEFT JOIN tally.dev_account a ON a.account_id = m.account_id
		WHERE m.org_id = $1 AND ($2::bigint = 0 OR m.account_id = $2::bigint)
		ORDER BY m.joined_at, m.account_id`, orgID, only)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []appteam.OrgMember
	for rows.Next() {
		var m appteam.OrgMember
		var raw []byte
		var email string
		if err := rows.Scan(&m.AccountID, &m.Role, &raw, &m.DisplayName, &email, &m.JoinedAt); err != nil {
			return nil, err
		}
		m.ProductRoles = decodePR(raw)
		m.EmailMasked = maskEmail(email)
		out = append(out, m)
	}
	return out, rows.Err()
}

// maskEmail keeps the first rune of the local part: "a***@example.com".
func maskEmail(e string) string {
	at := -1
	for i, r := range e {
		if r == '@' {
			at = i
			break
		}
	}
	if at <= 0 {
		return ""
	}
	first := []rune(e[:at])[0]
	return string(first) + "***" + e[at:]
}

// ListMembers lists the org members.
func (d *DevOrgClient) ListMembers(ctx context.Context, orgID int64) ([]appteam.OrgMember, error) {
	return d.memberRows(ctx, orgID, 0)
}

// GetMember returns one member or ErrNotMember.
func (d *DevOrgClient) GetMember(ctx context.Context, orgID, accountID int64) (*appteam.OrgMember, error) {
	ms, err := d.memberRows(ctx, orgID, accountID)
	if err != nil {
		return nil, err
	}
	if len(ms) == 0 {
		return nil, appteam.ErrNotMember
	}
	return &ms[0], nil
}

func isManager(role string) bool { return role == "owner" || role == "admin" }

// UpdateMember follows the platform rules: owner is immutable, only an owner
// grants or touches admin.
func (d *DevOrgClient) UpdateMember(ctx context.Context, orgID, accountID, actorAccountID int64, role string, productRoles map[string]string) error {
	actorRole, _, err := d.role(ctx, d.db, orgID, actorAccountID)
	if err != nil {
		if errors.Is(err, appteam.ErrNotMember) {
			return appteam.ErrForbidden
		}
		return err
	}
	targetRole, _, err := d.role(ctx, d.db, orgID, accountID)
	if err != nil {
		return err
	}
	if !isManager(actorRole) || targetRole == "owner" || role == "owner" {
		return appteam.ErrForbidden
	}
	if (role == "admin" || targetRole == "admin") && actorRole != "owner" {
		return appteam.ErrForbidden
	}
	if role != "admin" && role != "member" {
		return appteam.ErrInvalidRole
	}
	_, err = d.db.ExecContext(ctx,
		`UPDATE tally.dev_org_member SET role = $3, product_roles = $4 WHERE org_id = $1 AND account_id = $2`,
		orgID, accountID, role, encodePR(productRoles))
	return err
}

// RemoveMember removes a member; a person may leave on their own, the last owner is protected.
func (d *DevOrgClient) RemoveMember(ctx context.Context, orgID, accountID, actorAccountID int64) error {
	actorRole, _, err := d.role(ctx, d.db, orgID, actorAccountID)
	if err != nil {
		if errors.Is(err, appteam.ErrNotMember) {
			return appteam.ErrForbidden
		}
		return err
	}
	targetRole, _, err := d.role(ctx, d.db, orgID, accountID)
	if err != nil {
		return err
	}
	if targetRole == "owner" {
		return appteam.ErrLastOwner
	}
	if accountID != actorAccountID {
		if !isManager(actorRole) || (targetRole == "admin" && actorRole != "owner") {
			return appteam.ErrForbidden
		}
	}
	_, err = d.db.ExecContext(ctx, `DELETE FROM tally.dev_org_member WHERE org_id = $1 AND account_id = $2`, orgID, accountID)
	return err
}

func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

// CreateInvite stores only the sha256 of a random token and returns the plaintext once.
func (d *DevOrgClient) CreateInvite(ctx context.Context, orgID, actorAccountID int64, role string, productRoles map[string]string, ttlHours int) (*appteam.InviteCreated, error) {
	actorRole, _, err := d.role(ctx, d.db, orgID, actorAccountID)
	if err != nil {
		if errors.Is(err, appteam.ErrNotMember) {
			return nil, appteam.ErrForbidden
		}
		return nil, err
	}
	if !isManager(actorRole) || (role == "admin" && actorRole != "owner") {
		return nil, appteam.ErrForbidden
	}
	if role != "admin" && role != "member" {
		return nil, appteam.ErrInvalidRole
	}
	if ttlHours < 1 || ttlHours > 168 {
		ttlHours = 72
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(buf)
	id := uuid.New()
	expires := d.now().Add(time.Duration(ttlHours) * time.Hour).UTC()
	if _, err := d.db.ExecContext(ctx, `
		INSERT INTO tally.dev_org_invite (id, org_id, token_hash, role, product_roles, invited_by_account_id, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		id, orgID, hashToken(token), role, encodePR(productRoles), actorAccountID, expires); err != nil {
		return nil, err
	}
	return &appteam.InviteCreated{InviteID: id, Token: token, ExpiresAt: expires}, nil
}

// ListInvites lists unexpired, unaccepted, unrevoked invites.
func (d *DevOrgClient) ListInvites(ctx context.Context, orgID int64) ([]appteam.OrgInvite, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, role, product_roles, invited_by_account_id, expires_at, created_at
		FROM tally.dev_org_invite
		WHERE org_id = $1 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > $2
		ORDER BY created_at DESC`, orgID, d.now().UTC())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []appteam.OrgInvite
	for rows.Next() {
		var iv appteam.OrgInvite
		var raw []byte
		if err := rows.Scan(&iv.ID, &iv.Role, &raw, &iv.InvitedBy, &iv.ExpiresAt, &iv.CreatedAt); err != nil {
			return nil, err
		}
		iv.ProductRoles = decodePR(raw)
		out = append(out, iv)
	}
	return out, rows.Err()
}

// RevokeInvite marks a pending invite revoked.
func (d *DevOrgClient) RevokeInvite(ctx context.Context, orgID int64, inviteID uuid.UUID, actorAccountID int64) error {
	actorRole, _, err := d.role(ctx, d.db, orgID, actorAccountID)
	if err != nil {
		if errors.Is(err, appteam.ErrNotMember) {
			return appteam.ErrForbidden
		}
		return err
	}
	if !isManager(actorRole) {
		return appteam.ErrForbidden
	}
	res, err := d.db.ExecContext(ctx,
		`UPDATE tally.dev_org_invite SET revoked_at = now()
		 WHERE id = $1 AND org_id = $2 AND accepted_at IS NULL AND revoked_at IS NULL`, inviteID, orgID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return appteam.ErrInviteInvalid
	}
	return nil
}

// AcceptInvite consumes an invite; an existing member gets their current role back.
func (d *DevOrgClient) AcceptInvite(ctx context.Context, token string, accountID int64) (*appteam.AcceptResult, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var (
		id                   uuid.UUID
		orgID                int64
		role                 string
		raw                  []byte
		expires              time.Time
		accepted, revokedRaw sql.NullTime
	)
	err = tx.QueryRowContext(ctx, `
		SELECT id, org_id, role, product_roles, expires_at, accepted_at, revoked_at
		FROM tally.dev_org_invite WHERE token_hash = $1 FOR UPDATE`, hashToken(token)).
		Scan(&id, &orgID, &role, &raw, &expires, &accepted, &revokedRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, appteam.ErrInviteInvalid
	}
	if err != nil {
		return nil, err
	}
	// Already a member: idempotent, hand back the current role even for a used invite.
	if curRole, curPR, rerr := d.role(ctx, tx, orgID, accountID); rerr == nil {
		return &appteam.AcceptResult{OrgID: orgID, Role: curRole, ProductRoles: curPR}, nil
	} else if !errors.Is(rerr, appteam.ErrNotMember) {
		return nil, rerr
	}
	if accepted.Valid || revokedRaw.Valid || !d.now().Before(expires) {
		return nil, appteam.ErrInviteInvalid
	}
	pr := decodePR(raw)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO tally.dev_org_member (org_id, account_id, role, product_roles) VALUES ($1, $2, $3, $4)`,
		orgID, accountID, role, encodePR(pr)); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE tally.dev_org_invite SET accepted_by_account_id = $2, accepted_at = now() WHERE id = $1`, id, accountID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &appteam.AcceptResult{OrgID: orgID, Role: role, ProductRoles: pr}, nil
}
