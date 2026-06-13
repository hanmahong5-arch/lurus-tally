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
	// SetPlatformAccountID pins the lurus-platform account id (resolved at
	// onboarding via UpsertAccount) onto the tenant registry row so the LLM
	// usage reporter can attribute spend without a hot-path round-trip.
	SetPlatformAccountID(ctx context.Context, tenantID uuid.UUID, accountID int64) error
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

// SetPlatformAccountID updates tally.tenant.platform_account_id for one tenant.
// tally.tenant is the tenant registry (keyed by id, no tenant_id column, outside
// the RLS-scoped tables), so a plain UPDATE with no app.tenant_id pin is correct
// — and is what lets the post-onboarding heal path and any future reconcile run
// outside a request scope. Idempotent: re-writing the same id is a no-op write.
func (s *SQLBootstrapStore) SetPlatformAccountID(ctx context.Context, tenantID uuid.UUID, accountID int64) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE tally.tenant SET platform_account_id = $1, updated_at = now() WHERE id = $2`,
		accountID, tenantID); err != nil {
		return fmt.Errorf("set platform_account_id: %w", err)
	}
	return nil
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
	// Postgres SET commands don't accept parameter binding ($1), so use set_config()
	// with is_local=true (LOCAL = scoped to the current transaction).
	if _, err := tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", in.TenantID.String()); err != nil {
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

	// 4. Seed demo warehouse + supplier so the dashboard isn't empty on first
	// login. Without this, the user lands on /products and has to manually
	// create a warehouse + supplier before their first PO — measured ~45 min.
	// Names are profile-flavoured so the system "knows" what business they run.
	// User can rename or delete; both rows are marked as defaults (is_default
	// on warehouse, no flag on partner — we just rely on it being the only
	// supplier row at onboarding time).
	if err := seedDemoEntities(ctx, tx, in.TenantID, in.ProfileType, now); err != nil {
		return fmt.Errorf("bootstrap: seed demo entities: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("bootstrap: commit: %w", err)
	}
	return nil
}

// seedDemoEntities inserts one default warehouse + one default supplier so the
// fresh tenant has something to fill in the dashboard. Both rows live inside
// the same tx as the rest of Bootstrap — if either INSERT fails, the entire
// onboarding rolls back. INSERT cost is ~2ms each on a warm PG; the latency
// hit on the onboarding critical path is negligible compared to the 30+
// minutes of manual data entry it removes from the user's first session.
//
// Profile presets:
//
//	retail        → "门店仓"    + "现金采购"
//	cross_border  → "海外仓"    + "默认供应商"
//	horticulture  → "苗圃仓"    + "苗木供应商"
//
// Unknown / future profile types fall through to generic "主仓库" + "默认供应商"
// so adding a new profile_type doesn't break onboarding.
func seedDemoEntities(ctx context.Context, tx *sql.Tx, tenantID uuid.UUID, pt domain.ProfileType, now time.Time) error {
	warehouseName, supplierName := demoEntityNames(pt)

	// is_sample=true so the dashboard can later offer a "clear sample data"
	// button without scanning name patterns. See migration 000032 for the column.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO tally.warehouse (id, tenant_id, name, is_default, enabled, is_sample, created_at)
		VALUES ($1, $2, $3, true, true, true, $4)
	`, uuid.New(), tenantID, warehouseName, now); err != nil {
		return fmt.Errorf("insert warehouse: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO tally.partner (id, tenant_id, partner_type, name, enabled, is_sample, created_at, updated_at)
		VALUES ($1, $2, 'supplier', $3, true, true, $4, $4)
	`, uuid.New(), tenantID, supplierName, now); err != nil {
		return fmt.Errorf("insert supplier: %w", err)
	}

	return nil
}

// demoEntityNames maps a profile type to friendly default names. Pure function,
// no DB access — kept separate so tests can assert preset coverage without a
// real PG.
func demoEntityNames(pt domain.ProfileType) (warehouse, supplier string) {
	switch pt {
	case "retail":
		return "门店仓", "现金采购"
	case "cross_border":
		return "海外仓", "默认供应商"
	case "horticulture":
		return "苗圃仓", "苗木供应商"
	default:
		return "主仓库", "默认供应商"
	}
}
