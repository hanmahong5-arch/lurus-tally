// Package tenant — bootstrap.go: atomic first-time onboarding for a Zitadel
// user. Creates tenant + user_identity_mapping + tenant_profile in a single tx.
package tenant

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/tenant"
)

// BootstrapInput is the data needed to atomically create a brand-new tenant +
// owner mapping + initial profile on first login.
type BootstrapInput struct {
	TenantID    uuid.UUID
	TenantName  string
	ZitadelSub  string
	Email       string
	DisplayName string
	ProfileType domain.ProfileType
}

// BootstrapStore exposes the lookup + transactional create operations the
// ChooseProfileUseCase needs. ProfileRepo + MappingRepo + TenantRepo each work
// against a single DB handle, but bootstrap requires a *sql.Tx so we wrap it
// here. Splitting this out keeps the use-case ignorant of database/sql.
type BootstrapStore interface {
	GetMappingBySub(ctx context.Context, sub string) (*domain.UserIdentityMapping, error)
	GetProfileByTenantID(ctx context.Context, tenantID uuid.UUID) (*domain.TenantProfile, error)
	Bootstrap(ctx context.Context, in BootstrapInput) error
}

// SQLBootstrapStore is the *sql.DB-backed implementation.
type SQLBootstrapStore struct {
	db *sql.DB
}

// NewSQLBootstrapStore wraps a *sql.DB so it can satisfy BootstrapStore.
func NewSQLBootstrapStore(db *sql.DB) *SQLBootstrapStore {
	return &SQLBootstrapStore{db: db}
}

// GetMappingBySub delegates to MappingRepo using the underlying *sql.DB.
func (s *SQLBootstrapStore) GetMappingBySub(ctx context.Context, sub string) (*domain.UserIdentityMapping, error) {
	return NewMappingRepo(s.db).GetByZitadelSub(ctx, sub)
}

// GetProfileByTenantID delegates to ProfileRepo using the underlying *sql.DB.
func (s *SQLBootstrapStore) GetProfileByTenantID(ctx context.Context, tenantID uuid.UUID) (*domain.TenantProfile, error) {
	return NewProfileRepo(s.db).GetByTenantID(ctx, tenantID)
}

// Bootstrap atomically creates tenant + mapping + profile rows.
//
// The transaction sets `app.tenant_id` so RLS policies on tenant_profile and
// user_identity_mapping accept the inserts (those tables are RLS-protected by
// tenant_id). Without this SET, the inserts would fail silently with 0 rows.
//
// Failure at any step rolls back the entire transaction; partial state is
// impossible.
func (s *SQLBootstrapStore) Bootstrap(ctx context.Context, in BootstrapInput) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("bootstrap: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Set RLS context so policy checks on tenant_profile / user_identity_mapping pass.
	if _, err := tx.ExecContext(ctx, "SET LOCAL app.tenant_id = $1", in.TenantID.String()); err != nil {
		return fmt.Errorf("bootstrap: set rls context: %w", err)
	}

	now := time.Now().UTC()

	// 1. Create tenant row.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO tally.tenant (id, name, status, settings, created_at, updated_at)
		VALUES ($1, $2, 1, '{}'::jsonb, $3, $3)
	`, in.TenantID, in.TenantName, now); err != nil {
		return fmt.Errorf("bootstrap: insert tenant: %w", err)
	}

	// 2. Create user_identity_mapping (sub → tenant, owner role).
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO tally.user_identity_mapping
			(id, tenant_id, zitadel_sub, email, display_name, role, is_owner, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'admin', true, $6, $6)
	`, uuid.New(), in.TenantID, in.ZitadelSub, in.Email, in.DisplayName, now); err != nil {
		return fmt.Errorf("bootstrap: insert mapping: %w", err)
	}

	// 3. Create tenant_profile.
	profile, err := domain.NewTenantProfile(in.TenantID, in.ProfileType)
	if err != nil {
		return fmt.Errorf("bootstrap: build profile entity: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO tally.tenant_profile
			(id, tenant_id, profile_type, inventory_method, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $5)
	`, profile.ID, profile.TenantID, string(profile.ProfileType), string(profile.InventoryMethod), now); err != nil {
		return fmt.Errorf("bootstrap: insert profile: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("bootstrap: commit: %w", err)
	}
	return nil
}
