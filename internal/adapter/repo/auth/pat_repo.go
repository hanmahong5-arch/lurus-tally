// Package auth implements the Personal Access Token repository using PostgreSQL.
// All queries target the tally.personal_access_token table. Reads are designed
// to work BEFORE app.tenant_id is set (RLS relax policy from migration 000031).
package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	appauth "github.com/hanmahong5-arch/lurus-tally/internal/app/auth"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/auth"
)

// DB abstracts the minimal database/sql surface this repo needs. Both *sql.DB
// and *sql.Tx satisfy it, so callers can opt into a transaction when needed.
type DB interface {
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
}

// Repo is the Postgres-backed implementation of appauth.Repository.
type Repo struct {
	db DB
}

// New returns a Repo backed by db.
func New(db DB) *Repo { return &Repo{db: db} }

// Compile-time assertion that Repo satisfies the port.
var _ appauth.Repository = (*Repo)(nil)

func (r *Repo) Create(ctx context.Context, p *domain.PAT) error {
	const q = `
		INSERT INTO tally.personal_access_token
			(id, tenant_id, name, prefix, hash, created_at, expires_at)
		VALUES
			($1, $2, $3, $4, $5, $6, $7)`
	_, err := r.db.ExecContext(ctx, q,
		p.ID, p.TenantID, p.Name, p.Prefix, p.Hash,
		p.CreatedAt, p.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("auth repo: create pat: %w", err)
	}
	return nil
}

func (r *Repo) GetByPrefix(ctx context.Context, prefix string) (*domain.PAT, error) {
	const q = `
		SELECT id, tenant_id, name, prefix, hash,
		       created_at, expires_at, last_used_at, revoked_at
		FROM tally.personal_access_token
		WHERE prefix = $1`
	row := r.db.QueryRowContext(ctx, q, prefix)
	return scanPAT(row.Scan)
}

func (r *Repo) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*domain.PAT, error) {
	const q = `
		SELECT id, tenant_id, name, prefix, hash,
		       created_at, expires_at, last_used_at, revoked_at
		FROM tally.personal_access_token
		WHERE tenant_id = $1 AND revoked_at IS NULL
		ORDER BY created_at DESC`
	rows, err := r.db.QueryContext(ctx, q, tenantID)
	if err != nil {
		return nil, fmt.Errorf("auth repo: list pats: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []*domain.PAT
	for rows.Next() {
		p, err := scanPAT(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("auth repo: iterate pats: %w", err)
	}
	return out, nil
}

func (r *Repo) Revoke(ctx context.Context, tenantID, id uuid.UUID) error {
	const q = `
		UPDATE tally.personal_access_token
		SET revoked_at = COALESCE(revoked_at, now())
		WHERE tenant_id = $1 AND id = $2`
	res, err := r.db.ExecContext(ctx, q, tenantID, id)
	if err != nil {
		return fmt.Errorf("auth repo: revoke pat: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("auth repo: revoke pat: rows affected: %w", err)
	}
	if n == 0 {
		return appauth.ErrNotFound
	}
	return nil
}

func (r *Repo) TouchLastUsed(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE tally.personal_access_token SET last_used_at = now() WHERE id = $1`
	if _, err := r.db.ExecContext(ctx, q, id); err != nil {
		return fmt.Errorf("auth repo: touch last_used_at: %w", err)
	}
	return nil
}

// scanRow is the lowest-common-denominator of *sql.Row.Scan and *sql.Rows.Scan.
type scanRow func(dest ...any) error

func scanPAT(scan scanRow) (*domain.PAT, error) {
	var p domain.PAT
	err := scan(
		&p.ID, &p.TenantID, &p.Name, &p.Prefix, &p.Hash,
		&p.CreatedAt, &p.ExpiresAt, &p.LastUsedAt, &p.RevokedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, appauth.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("auth repo: scan pat: %w", err)
	}
	return &p, nil
}
