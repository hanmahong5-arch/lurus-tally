// Package tenant implements repository operations for tenant_profile and
// user_identity_mapping tables using database/sql (pgx driver).
// All queries target the tally schema and require RLS to be active.
package tenant

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/tenant"
)

// TenantProfileRepository defines the persistence contract for TenantProfile.
// Implementations must be safe for concurrent use.
type TenantProfileRepository interface {
	GetByTenantID(ctx context.Context, tenantID uuid.UUID) (*domain.TenantProfile, error)
	Create(ctx context.Context, p *domain.TenantProfile) error
	QueryProfileType(ctx context.Context, tenantID uuid.UUID) (string, error)
}

// UserMappingRepository defines the persistence contract for UserIdentityMapping.
type UserMappingRepository interface {
	GetByZitadelSub(ctx context.Context, sub string) (*domain.UserIdentityMapping, error)
	Create(ctx context.Context, m *domain.UserIdentityMapping) error
}

// DB abstracts the database/sql surface required by the tenant repo.
// Both *sql.DB and *sql.Tx satisfy this interface.
type DB interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// ProfileRepo implements TenantProfileRepository against PostgreSQL.
type ProfileRepo struct {
	db DB
}

// NewProfileRepo creates a ProfileRepo backed by db.
func NewProfileRepo(db DB) *ProfileRepo {
	return &ProfileRepo{db: db}
}

// GetByTenantID fetches the TenantProfile for the given tenant.
// Returns nil, nil when no profile exists (not an error condition).
func (r *ProfileRepo) GetByTenantID(ctx context.Context, tenantID uuid.UUID) (*domain.TenantProfile, error) {
	const q = `
		SELECT id, tenant_id, profile_type, inventory_method, created_at, updated_at
		FROM tally.tenant_profile
		WHERE tenant_id = $1`

	row := r.db.QueryRowContext(ctx, q, tenantID)
	p := &domain.TenantProfile{}
	var ptStr, imStr string
	err := row.Scan(&p.ID, &p.TenantID, &ptStr, &imStr, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("tenant profile repo get by tenant id: %w", err)
	}
	p.ProfileType = domain.ProfileType(ptStr)
	p.InventoryMethod = domain.InventoryMethod(imStr)
	return p, nil
}

// Create inserts a new TenantProfile row.
// Returns an error wrapping the DB constraint violation if a profile already exists.
func (r *ProfileRepo) Create(ctx context.Context, p *domain.TenantProfile) error {
	const q = `
		INSERT INTO tally.tenant_profile
			(id, tenant_id, profile_type, inventory_method, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)`

	_, err := r.db.ExecContext(ctx, q,
		p.ID, p.TenantID, string(p.ProfileType), string(p.InventoryMethod), p.CreatedAt, p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("tenant profile repo create: %w", err)
	}
	return nil
}

// QueryProfileType returns only the profile_type string for a tenant.
// Returns "", sql.ErrNoRows when no record exists.
// This satisfies the ProfileQuerier interface used by the profile middleware.
func (r *ProfileRepo) QueryProfileType(ctx context.Context, tenantID uuid.UUID) (string, error) {
	const q = `SELECT profile_type FROM tally.tenant_profile WHERE tenant_id = $1`
	var pt string
	err := r.db.QueryRowContext(ctx, q, tenantID).Scan(&pt)
	if err != nil {
		return "", err
	}
	return pt, nil
}

// TenantRepository defines the persistence contract for tally.tenant rows.
// Tally creates these locally on first-time onboarding; future revisions may
// sync them from 2l-svc-platform via NATS IDENTITY_EVENTS.
type TenantRepository interface {
	Create(ctx context.Context, id uuid.UUID, name string) error
}

// TenantRepo implements TenantRepository against PostgreSQL.
type TenantRepo struct {
	db DB
}

// NewTenantRepo creates a TenantRepo backed by db.
func NewTenantRepo(db DB) *TenantRepo {
	return &TenantRepo{db: db}
}

// Create inserts a tenant row with the given id and name.
// status defaults to 1 (active) at the schema level.
func (r *TenantRepo) Create(ctx context.Context, id uuid.UUID, name string) error {
	const q = `
		INSERT INTO tally.tenant (id, name, status, settings, created_at, updated_at)
		VALUES ($1, $2, 1, '{}'::jsonb, $3, $3)`
	_, err := r.db.ExecContext(ctx, q, id, name, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("tenant repo create: %w", err)
	}
	return nil
}

// MappingRepo implements UserMappingRepository against PostgreSQL.
type MappingRepo struct {
	db DB
}

// NewMappingRepo creates a MappingRepo backed by db.
func NewMappingRepo(db DB) *MappingRepo {
	return &MappingRepo{db: db}
}

// GetByZitadelSub fetches the mapping for the given Zitadel sub claim.
// Returns nil, nil when no mapping exists.
func (r *MappingRepo) GetByZitadelSub(ctx context.Context, sub string) (*domain.UserIdentityMapping, error) {
	const q = `
		SELECT id, tenant_id, zitadel_sub, email, display_name, role, is_owner, created_at, updated_at
		FROM tally.user_identity_mapping
		WHERE zitadel_sub = $1`

	row := r.db.QueryRowContext(ctx, q, sub)
	m := &domain.UserIdentityMapping{}
	err := row.Scan(
		&m.ID, &m.TenantID, &m.ZitadelSub, &m.Email, &m.DisplayName,
		&m.Role, &m.IsOwner, &m.CreatedAt, &m.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("user mapping repo get by sub: %w", err)
	}
	return m, nil
}

// Create inserts a new UserIdentityMapping row.
func (r *MappingRepo) Create(ctx context.Context, m *domain.UserIdentityMapping) error {
	const q = `
		INSERT INTO tally.user_identity_mapping
			(id, tenant_id, zitadel_sub, email, display_name, role, is_owner, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

	now := time.Now().UTC()
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
		m.UpdatedAt = now
	}
	_, err := r.db.ExecContext(ctx, q,
		m.ID, m.TenantID, m.ZitadelSub, m.Email, m.DisplayName,
		m.Role, m.IsOwner, m.CreatedAt, m.UpdatedAt)
	if err != nil {
		return fmt.Errorf("user mapping repo create: %w", err)
	}
	return nil
}
