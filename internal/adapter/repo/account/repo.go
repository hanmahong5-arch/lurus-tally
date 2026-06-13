// Package account implements the account-center repositories (sessions,
// account_audit_log, user_profile) using PostgreSQL.
//
// All queries enforce tenant_id = $1 in WHERE clauses as defence in depth on
// top of the RLS policies declared in migration 000036.
package account

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
	appacct "github.com/hanmahong5-arch/lurus-tally/internal/app/account"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/account"
)

// ----- Session repository ----------------------------------------------------

// SessionRepo persists tally.user_session.
type SessionRepo struct{ db *sql.DB }

// NewSessionRepo wires the repo to db.
func NewSessionRepo(db *sql.DB) *SessionRepo { return &SessionRepo{db: db} }

// Compile-time interface assertion.
var _ appacct.SessionRepo = (*SessionRepo)(nil)

// Upsert inserts a fresh session row. The composite identity is
// (tenant_id, user_id, user_agent) — multiple sessions per user are
// supported (e.g. desktop + mobile browser). Touch() refreshes last_active
// for the matching row.
func (r *SessionRepo) Upsert(ctx context.Context, s *domain.Session) error {
	// Try to refresh an existing row first to avoid stamping a new id on
	// every navigation.
	const updateQ = `
		UPDATE tally.user_session
		SET last_active = $4
		WHERE tenant_id = $1 AND user_id = $2 AND user_agent = $3 AND revoked_at IS NULL
		RETURNING id`
	dbh := dbscope.From(ctx, r.db)
	var existingID uuid.UUID
	err := dbh.QueryRowContext(ctx, updateQ, s.TenantID, s.UserID, s.UserAgent, s.LastActive).Scan(&existingID)
	if err == nil {
		s.ID = existingID
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("account repo: session upsert refresh: %w", err)
	}

	const insertQ = `
		INSERT INTO tally.user_session
			(id, tenant_id, user_id, user_agent, ip_addr, created_at, last_active)
		VALUES
			($1, $2, $3, $4, $5, $6, $7)`
	if _, err := dbh.ExecContext(ctx, insertQ,
		s.ID, s.TenantID, s.UserID, s.UserAgent, nullableIP(s.IPAddr), s.CreatedAt, s.LastActive,
	); err != nil {
		return fmt.Errorf("account repo: session insert: %w", err)
	}
	return nil
}

// List returns all non-revoked sessions for (tenant, user), most recent first.
func (r *SessionRepo) List(ctx context.Context, tenantID uuid.UUID, userID string) ([]*domain.Session, error) {
	const q = `
		SELECT id, tenant_id, user_id, COALESCE(user_agent, ''), ip_addr,
		       created_at, last_active, revoked_at
		FROM tally.user_session
		WHERE tenant_id = $1 AND user_id = $2 AND revoked_at IS NULL
		ORDER BY last_active DESC`
	rows, err := dbscope.From(ctx, r.db).QueryContext(ctx, q, tenantID, userID)
	if err != nil {
		return nil, fmt.Errorf("account repo: session list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []*domain.Session
	for rows.Next() {
		s := &domain.Session{}
		var ip sql.NullString
		var revoked sql.NullTime
		if err := rows.Scan(&s.ID, &s.TenantID, &s.UserID, &s.UserAgent, &ip,
			&s.CreatedAt, &s.LastActive, &revoked); err != nil {
			return nil, fmt.Errorf("account repo: session scan: %w", err)
		}
		if ip.Valid {
			s.IPAddr = net.ParseIP(ip.String)
		}
		if revoked.Valid {
			t := revoked.Time
			s.RevokedAt = &t
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("account repo: session iterate: %w", err)
	}
	return out, nil
}

// Revoke marks the matching session row revoked. Idempotent: an unknown id
// returns nil.
func (r *SessionRepo) Revoke(ctx context.Context, tenantID, id uuid.UUID) error {
	const q = `
		UPDATE tally.user_session
		SET revoked_at = COALESCE(revoked_at, now())
		WHERE tenant_id = $1 AND id = $2`
	if _, err := dbscope.From(ctx, r.db).ExecContext(ctx, q, tenantID, id); err != nil {
		return fmt.Errorf("account repo: session revoke: %w", err)
	}
	return nil
}

// Touch is a lightweight last_active refresh for the matching session.
func (r *SessionRepo) Touch(ctx context.Context, tenantID uuid.UUID, userID, userAgent string) error {
	const q = `
		UPDATE tally.user_session
		SET last_active = now()
		WHERE tenant_id = $1 AND user_id = $2 AND user_agent = $3 AND revoked_at IS NULL`
	if _, err := dbscope.From(ctx, r.db).ExecContext(ctx, q, tenantID, userID, userAgent); err != nil {
		return fmt.Errorf("account repo: session touch: %w", err)
	}
	return nil
}

func nullableIP(ip net.IP) any {
	if len(ip) == 0 {
		return nil
	}
	return ip.String()
}

// ----- Audit repository -----------------------------------------------------

// AuditRepo persists tally.account_audit_log.
type AuditRepo struct{ db *sql.DB }

// NewAuditRepo wires the repo to db.
func NewAuditRepo(db *sql.DB) *AuditRepo { return &AuditRepo{db: db} }

// Compile-time interface assertion.
var _ appacct.AuditRepo = (*AuditRepo)(nil)

// Append inserts one audit row.
func (r *AuditRepo) Append(ctx context.Context, e *domain.AuditEntry) error {
	const q = `
		INSERT INTO tally.account_audit_log
			(id, tenant_id, actor_id, action, target_kind, target_id, payload, created_at)
		VALUES
			($1, $2, $3, $4, $5, $6, $7::jsonb, $8)`
	payload := e.Payload
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	if _, err := dbscope.From(ctx, r.db).ExecContext(ctx, q,
		e.ID, e.TenantID, e.ActorID, e.Action,
		nullable(e.TargetKind), nullable(e.TargetID), payload, e.CreatedAt,
	); err != nil {
		return fmt.Errorf("account repo: audit append: %w", err)
	}
	return nil
}

// List returns audit rows + the total count for pagination.
func (r *AuditRepo) List(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]*domain.AuditEntry, int, error) {
	dbh := dbscope.From(ctx, r.db)
	var total int
	if err := dbh.QueryRowContext(ctx,
		`SELECT count(*) FROM tally.account_audit_log WHERE tenant_id = $1`, tenantID,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("account repo: audit count: %w", err)
	}
	if total == 0 {
		return nil, 0, nil
	}

	const q = `
		SELECT id, tenant_id, actor_id, action,
		       COALESCE(target_kind, ''), COALESCE(target_id, ''),
		       payload::text, created_at
		FROM tally.account_audit_log
		WHERE tenant_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`
	rows, err := dbh.QueryContext(ctx, q, tenantID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("account repo: audit list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []*domain.AuditEntry
	for rows.Next() {
		e := &domain.AuditEntry{}
		var payloadText string
		if err := rows.Scan(&e.ID, &e.TenantID, &e.ActorID, &e.Action,
			&e.TargetKind, &e.TargetID, &payloadText, &e.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("account repo: audit scan: %w", err)
		}
		e.Payload = []byte(payloadText)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("account repo: audit iterate: %w", err)
	}
	return out, total, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ----- Profile repository ---------------------------------------------------

// ProfileRepo persists tally.user_profile.
type ProfileRepo struct{ db *sql.DB }

// NewProfileRepo wires the repo to db.
func NewProfileRepo(db *sql.DB) *ProfileRepo { return &ProfileRepo{db: db} }

// Compile-time interface assertion.
var _ appacct.ProfileRepo = (*ProfileRepo)(nil)

// Get returns the editable profile, without the avatar bytes.
func (r *ProfileRepo) Get(ctx context.Context, tenantID uuid.UUID, userID string) (*domain.Profile, error) {
	const q = `
		SELECT tenant_id, user_id,
		       COALESCE(display_name, ''),
		       COALESCE(phone, ''),
		       COALESCE(avatar_content_type, ''),
		       (avatar_bytes IS NOT NULL),
		       updated_at
		FROM tally.user_profile
		WHERE tenant_id = $1 AND user_id = $2`
	row := dbscope.From(ctx, r.db).QueryRowContext(ctx, q, tenantID, userID)
	p := &domain.Profile{}
	if err := row.Scan(&p.TenantID, &p.UserID, &p.DisplayName, &p.Phone,
		&p.AvatarContentType, &p.HasAvatar, &p.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, appacct.ErrNotFound
		}
		return nil, fmt.Errorf("account repo: profile get: %w", err)
	}
	return p, nil
}

// Upsert writes the editable fields. Avatar bytes are left untouched.
func (r *ProfileRepo) Upsert(ctx context.Context, tenantID uuid.UUID, userID, displayName, phone string) error {
	const q = `
		INSERT INTO tally.user_profile
			(tenant_id, user_id, display_name, phone, updated_at)
		VALUES
			($1, $2, $3, $4, $5)
		ON CONFLICT (tenant_id, user_id) DO UPDATE
		SET display_name = EXCLUDED.display_name,
		    phone        = EXCLUDED.phone,
		    updated_at   = EXCLUDED.updated_at`
	if _, err := dbscope.From(ctx, r.db).ExecContext(ctx, q,
		tenantID, userID, nullable(displayName), nullable(phone), time.Now().UTC(),
	); err != nil {
		return fmt.Errorf("account repo: profile upsert: %w", err)
	}
	return nil
}

// SetAvatar updates only the avatar columns. Row is created if missing.
func (r *ProfileRepo) SetAvatar(ctx context.Context, tenantID uuid.UUID, userID, contentType string, data []byte) error {
	const q = `
		INSERT INTO tally.user_profile
			(tenant_id, user_id, avatar_content_type, avatar_bytes, updated_at)
		VALUES
			($1, $2, $3, $4, $5)
		ON CONFLICT (tenant_id, user_id) DO UPDATE
		SET avatar_content_type = EXCLUDED.avatar_content_type,
		    avatar_bytes        = EXCLUDED.avatar_bytes,
		    updated_at          = EXCLUDED.updated_at`
	if _, err := dbscope.From(ctx, r.db).ExecContext(ctx, q, tenantID, userID, contentType, data, time.Now().UTC()); err != nil {
		return fmt.Errorf("account repo: avatar upsert: %w", err)
	}
	return nil
}

// GetAvatar returns the content-type and bytes, or ErrNotFound when missing.
func (r *ProfileRepo) GetAvatar(ctx context.Context, tenantID uuid.UUID, userID string) (string, []byte, error) {
	const q = `
		SELECT COALESCE(avatar_content_type, ''), avatar_bytes
		FROM tally.user_profile
		WHERE tenant_id = $1 AND user_id = $2`
	var ct string
	var data []byte
	if err := dbscope.From(ctx, r.db).QueryRowContext(ctx, q, tenantID, userID).Scan(&ct, &data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil, appacct.ErrNotFound
		}
		return "", nil, fmt.Errorf("account repo: avatar get: %w", err)
	}
	if len(data) == 0 {
		return "", nil, appacct.ErrNotFound
	}
	return ct, data, nil
}
