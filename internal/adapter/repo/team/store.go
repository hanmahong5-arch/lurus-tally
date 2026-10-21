// Package team is the PostgreSQL store behind the team use cases: the
// tenant -> platform org link and the user_identity_mapping projection.
// user_identity_mapping RLS is relaxed when no tenant is pinned (migration
// 000025), so these calls work on the plain pool exactly like the tenant
// bootstrap store.
package team

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/onboarding"
	appteam "github.com/hanmahong5-arch/lurus-tally/internal/app/team"
	domainteam "github.com/hanmahong5-arch/lurus-tally/internal/domain/team"
)

// Store implements appteam.Store on a *sql.DB.
type Store struct {
	db *sql.DB
}

// New creates a Store.
func New(db *sql.DB) *Store { return &Store{db: db} }

var (
	_ appteam.Store          = (*Store)(nil)
	_ appteam.ShopTypeReader = (*Store)(nil)
)

// TenantOrg returns the shop name and platform org id (uuid.Nil when unset).
func (s *Store) TenantOrg(ctx context.Context, tenantID uuid.UUID) (string, int64, error) {
	var name string
	var org sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT name, platform_org_id FROM tally.tenant WHERE id = $1`, tenantID).Scan(&name, &org)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, appteam.ErrTenantNotFound
	}
	if err != nil {
		return "", 0, fmt.Errorf("team store: tenant org: %w", err)
	}
	if !org.Valid {
		return name, 0, nil
	}
	return name, org.Int64, nil
}

// SetTenantOrg pins the platform org id on the tenant. First writer wins: an
// already linked shop keeps its org.
func (s *Store) SetTenantOrg(ctx context.Context, tenantID uuid.UUID, orgID int64) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE tally.tenant SET platform_org_id = $1, updated_at = now()
		 WHERE id = $2 AND platform_org_id IS NULL`, orgID, tenantID); err != nil {
		return fmt.Errorf("team store: set tenant org: %w", err)
	}
	return nil
}

// TenantByOrg returns the shop linked to a platform org, uuid.Nil when none.
func (s *Store) TenantByOrg(ctx context.Context, orgID int64) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.db.QueryRowContext(ctx, `SELECT id FROM tally.tenant WHERE platform_org_id = $1 AND status <> 0`, orgID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, nil
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("team store: tenant by org: %w", err)
	}
	return id, nil
}

const mappingCols = `tenant_id, zitadel_sub, email, COALESCE(display_name, ''), role, is_owner, COALESCE(platform_account_id, 0)`

func scanMapping(row *sql.Row) (*appteam.Mapping, error) {
	m := &appteam.Mapping{}
	err := row.Scan(&m.TenantID, &m.IDPSubject, &m.Email, &m.DisplayName, &m.Role, &m.IsOwner, &m.AccountID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("team store: scan mapping: %w", err)
	}
	return m, nil
}

// MappingBySub looks a person up by IdP subject. The physical column is still `zitadel_sub`.
func (s *Store) MappingBySub(ctx context.Context, sub string) (*appteam.Mapping, error) {
	return scanMapping(s.db.QueryRowContext(ctx,
		`SELECT `+mappingCols+` FROM tally.user_identity_mapping WHERE zitadel_sub = $1`, sub))
}

// MappingByAccount looks a person up by platform account within one shop.
func (s *Store) MappingByAccount(ctx context.Context, tenantID uuid.UUID, accountID int64) (*appteam.Mapping, error) {
	return scanMapping(s.db.QueryRowContext(ctx,
		`SELECT `+mappingCols+` FROM tally.user_identity_mapping
		 WHERE tenant_id = $1 AND platform_account_id = $2`, tenantID, accountID))
}

// InsertMapping adds a person to a shop; ErrMappingExists when the subject is already mapped.
func (s *Store) InsertMapping(ctx context.Context, m appteam.Mapping) error {
	var acct any
	if m.AccountID > 0 {
		acct = m.AccountID
	}
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tally.user_identity_mapping
			(id, tenant_id, zitadel_sub, email, display_name, role, is_owner, platform_account_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)`,
		uuid.New(), m.TenantID, m.IDPSubject, m.Email, m.DisplayName, m.Role, m.IsOwner, acct, now)
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return appteam.ErrMappingExists
	}
	if err != nil {
		return fmt.Errorf("team store: insert mapping: %w", err)
	}
	return nil
}

// SyncMapping refreshes the projected role (and account id when known) of a person.
// The owner row keeps role 'owner' regardless of what the platform reports.
func (s *Store) SyncMapping(ctx context.Context, tenantID uuid.UUID, sub, role string, accountID int64) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE tally.user_identity_mapping
		   SET role = CASE WHEN is_owner THEN 'owner' ELSE $3::varchar END,
		       platform_account_id = CASE WHEN $4::bigint > 0 THEN $4::bigint ELSE platform_account_id END,
		       updated_at = now()
		 WHERE tenant_id = $1 AND zitadel_sub = $2`, tenantID, sub, role, accountID); err != nil {
		return fmt.Errorf("team store: sync mapping: %w", err)
	}
	return nil
}

// DeleteMappingByAccount removes a person from a shop; the owner row is never deleted.
func (s *Store) DeleteMappingByAccount(ctx context.Context, tenantID uuid.UUID, accountID int64) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM tally.user_identity_mapping
		 WHERE tenant_id = $1 AND platform_account_id = $2 AND is_owner = false`, tenantID, accountID); err != nil {
		return fmt.Errorf("team store: delete mapping: %w", err)
	}
	return nil
}

// MemberCount counts the local member rows of a shop.
func (s *Store) MemberCount(ctx context.Context, tenantID uuid.UUID) (int, error) {
	tx, err := s.pinned(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	return memberCount(ctx, tx, tenantID)
}

func memberCount(ctx context.Context, tx *sql.Tx, tenantID uuid.UUID) (int, error) {
	var n int
	if err := tx.QueryRowContext(ctx,
		`SELECT count(*) FROM tally.user_identity_mapping WHERE tenant_id = $1`, tenantID).Scan(&n); err != nil {
		return 0, fmt.Errorf("team store: member count: %w", err)
	}
	return n, nil
}

// pinned opens a transaction with app.tenant_id set: the business tables are
// strict-RLS, so an unpinned count would see zero rows and call every shop empty.
func (s *Store) pinned(ctx context.Context, tenantID uuid.UUID) (*sql.Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("team store: begin: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID.String()); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("team store: set rls context: %w", err)
	}
	return tx, nil
}

// realDataProbe is true when the shop holds anything beyond the onboarding sample
// data: the default warehouse + supplier (is_sample) and the demo products/sales
// the setup wizard can fill in (remark='DEMO', see repo/onboarding) together with
// the stock rows those products own. Soft-deleted rows count: the shop was used.
// A real bill on a demo product is still caught through bill_head: sample bills
// are decided by the same rule ClearDemo uses (onboarding.DemoBillsSQL), so the
// draft purchase the wizard generates from a demo shortage does not count.
var realDataProbe = `SELECT
	EXISTS (SELECT 1 FROM tally.product        WHERE tenant_id = $1 AND remark IS DISTINCT FROM 'DEMO') OR
	EXISTS (SELECT 1 FROM tally.bill_head      WHERE tenant_id = $1 AND id NOT IN ` + onboarding.DemoBillsSQL() + `) OR
	EXISTS (SELECT 1 FROM tally.payment_head   WHERE tenant_id = $1) OR
	EXISTS (SELECT 1 FROM tally.stock_movement m WHERE m.tenant_id = $1 AND NOT EXISTS (
		SELECT 1 FROM tally.product p WHERE p.id = m.product_id AND p.tenant_id = $1 AND p.remark = 'DEMO')) OR
	EXISTS (SELECT 1 FROM tally.stock_initial i WHERE i.tenant_id = $1 AND NOT EXISTS (
		SELECT 1 FROM tally.product p WHERE p.id = i.product_id AND p.tenant_id = $1 AND p.remark = 'DEMO')) OR
	EXISTS (SELECT 1 FROM tally.project        WHERE tenant_id = $1) OR
	EXISTS (SELECT 1 FROM tally.invoice        WHERE tenant_id = $1) OR
	EXISTS (SELECT 1 FROM tally.warehouse      WHERE tenant_id = $1 AND NOT is_sample) OR
	EXISTS (SELECT 1 FROM tally.partner        WHERE tenant_id = $1 AND NOT is_sample)`

func shopHasRealData(ctx context.Context, tx *sql.Tx, tenantID uuid.UUID) (bool, error) {
	var has bool
	if err := tx.QueryRowContext(ctx, realDataProbe, tenantID).Scan(&has); err != nil {
		return false, fmt.Errorf("team store: probe shop data: %w", err)
	}
	return has, nil
}

// ShopIsEmpty reports a shop with only the onboarding sample data.
func (s *Store) ShopIsEmpty(ctx context.Context, tenantID uuid.UUID) (bool, error) {
	tx, err := s.pinned(ctx, tenantID)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	has, err := shopHasRealData(ctx, tx, tenantID)
	return !has, err
}

// AbandonShop soft-deletes an empty shop and its sole owner mapping. Nothing is
// physically deleted: the tenant gets status 0 plus an audit note in settings,
// and the mapping row keeps its id/tenant/account but its subject is replaced by
// 'abandoned:<mapping id>' so the person can be mapped to another shop (the
// original subject is kept in the tenant note).
func (s *Store) AbandonShop(ctx context.Context, tenantID uuid.UUID, sub string) error {
	tx, err := s.pinned(ctx, tenantID)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Serialize with other writers that lock the tenant row, then re-check under
	// the transaction: the shop must still be empty with this owner as its only member.
	if _, err := tx.ExecContext(ctx, `SELECT 1 FROM tally.tenant WHERE id = $1 FOR UPDATE`, tenantID); err != nil {
		return fmt.Errorf("team store: lock tenant: %w", err)
	}
	n, err := memberCount(ctx, tx, tenantID)
	if err != nil {
		return err
	}
	var isOwner bool
	err = tx.QueryRowContext(ctx,
		`SELECT is_owner FROM tally.user_identity_mapping WHERE tenant_id = $1 AND zitadel_sub = $2 FOR UPDATE`,
		tenantID, sub).Scan(&isOwner)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (!isOwner || n != 1)) {
		return appteam.ErrShopNotEmpty
	}
	if err != nil {
		return fmt.Errorf("team store: lock owner mapping: %w", err)
	}
	has, err := shopHasRealData(ctx, tx, tenantID)
	if err != nil {
		return err
	}
	if has {
		return appteam.ErrShopNotEmpty
	}
	note, err := json.Marshal(map[string]any{"abandoned": map[string]string{
		"at": time.Now().UTC().Format(time.RFC3339), "by_sub": sub,
	}})
	if err != nil {
		return fmt.Errorf("team store: encode abandon note: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE tally.user_identity_mapping SET zitadel_sub = 'abandoned:' || id::text, updated_at = now()
		 WHERE tenant_id = $1 AND zitadel_sub = $2`, tenantID, sub); err != nil {
		return fmt.Errorf("team store: retire mapping: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE tally.tenant SET status = 0, settings = settings || $2::jsonb, updated_at = now() WHERE id = $1`,
		tenantID, string(note)); err != nil {
		return fmt.Errorf("team store: retire tenant: %w", err)
	}
	// API keys the dead shop issued must stop working with it.
	if _, err := tx.ExecContext(ctx,
		`UPDATE tally.personal_access_token SET revoked_at = now() WHERE tenant_id = $1 AND revoked_at IS NULL`,
		tenantID); err != nil {
		return fmt.Errorf("team store: revoke shop api keys: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("team store: commit abandon: %w", err)
	}
	return nil
}

// MemberNames maps every person of one shop to a display label, keyed by the creator id
// their bills carry (ActorIDFromSubject of the IdP subject). The label is the display name,
// else the masked email, else a placeholder; a full email address is never returned.
func (s *Store) MemberNames(ctx context.Context, tenantID uuid.UUID) (map[uuid.UUID]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT zitadel_sub, COALESCE(email, ''), COALESCE(display_name, '')
		 FROM tally.user_identity_mapping WHERE tenant_id = $1`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("team store: member names: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[uuid.UUID]string{}
	for rows.Next() {
		var sub, email, name string
		if err := rows.Scan(&sub, &email, &name); err != nil {
			return nil, fmt.Errorf("team store: member names scan: %w", err)
		}
		switch {
		case strings.TrimSpace(name) != "":
			name = strings.TrimSpace(name)
		case maskEmail(email) != "":
			name = maskEmail(email)
		default:
			name = "未命名成员"
		}
		out[domainteam.ActorIDFromSubject(sub)] = name
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("team store: member names rows: %w", err)
	}
	return out, nil
}

// maskEmail keeps the first rune of the local part: "a***@example.com"; "" when there is none.
func maskEmail(e string) string {
	at := strings.IndexByte(e, '@')
	if at <= 0 {
		return ""
	}
	first := []rune(e[:at])[0]
	return string(first) + "***" + e[at:]
}
