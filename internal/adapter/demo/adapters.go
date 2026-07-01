// Package demo wires the demo-sandbox app-layer Provisioner (internal/app/demo)
// to concrete infrastructure, reusing shipped primitives only:
//
//   - tenant creation  → the normal onboarding bootstrap (ChooseProfile), which
//     also seeds the default 苗圃仓 warehouse + supplier;
//   - RLS scoping       → dbscope.WithPinnedConn (the same primitive the public
//     Shopify webhook uses to write under a tenant resolved outside auth);
//   - the throwaway PAT → the existing Personal Access Token path (its table has
//     a relaxed RLS policy, migration 000031, so the insert needs no pinned conn);
//   - demo data         → SeedDemoUseCase(horticulture): real nursery products,
//     opening stock and backdated sales (is_sample-marked demo content).
//
// No new tables: a sandbox visitor is just a short-lived, RLS-isolated tenant.
package demo

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
	demoapp "github.com/hanmahong5-arch/lurus-tally/internal/app/demo"
	appob "github.com/hanmahong5-arch/lurus-tally/internal/app/onboarding"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	apptenant "github.com/hanmahong5-arch/lurus-tally/internal/app/tenant"
	domainauth "github.com/hanmahong5-arch/lurus-tally/internal/domain/auth"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// PATStore is the slice of the PAT repository the minter needs.
type PATStore interface {
	Create(ctx context.Context, p *domainauth.PAT) error
}

// Build assembles a *demoapp.Provisioner from concrete dependencies. The caller
// (lifecycle) passes a ChooseProfile use case built with a NIL platform upserter
// so demo tenants are never registered as billable accounts on lurus-platform.
func Build(
	choose *apptenant.ChooseProfileUseCase,
	db *sql.DB,
	patStore PATStore,
	productCreator appob.ProductCreator,
	stockUC *appstock.RecordMovementUseCase,
	ttl time.Duration,
) *demoapp.Provisioner {
	sa := &stockAdapter{uc: stockUC}
	seedUC := appob.NewSeedDemoUseCase(productCreator, sa, sa)
	return demoapp.NewProvisioner(
		&bootstrapAdapter{choose: choose},
		&scopeAdapter{db: db},
		&patMinter{store: patStore},
		&seeder{db: db, seed: seedUC},
		nil, // clock → time.Now
		ttl,
	)
}

// bootstrapAdapter creates the isolated demo tenant via the normal onboarding
// path, forcing the horticulture profile (so the dashboard, seed and dictionary
// all present the nursery vertical).
type bootstrapAdapter struct {
	choose *apptenant.ChooseProfileUseCase
}

func (a *bootstrapAdapter) CreateDemoTenant(ctx context.Context, sub, name string) (uuid.UUID, error) {
	profile, err := a.choose.Execute(ctx, apptenant.ChooseProfileInput{
		ZitadelSub:  sub,
		DisplayName: name,
		ProfileType: "horticulture",
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("demo bootstrap: %w", err)
	}
	return profile.TenantID, nil
}

// scopeAdapter pins the request to the demo tenant's RLS context so writes inside
// fn (PAT insert is exempt, but the seed's product/stock writes are not) pass
// row-level security.
type scopeAdapter struct {
	db *sql.DB
}

func (a *scopeAdapter) Run(ctx context.Context, tenantID uuid.UUID, fn func(context.Context) error) error {
	return dbscope.WithPinnedConn(ctx, a.db, tenantID.String(), fn)
}

// patMinter issues a short-lived PAT scoped to the demo tenant. The PAT table's
// relaxed RLS policy lets the insert run on the raw pool (the auth middleware
// must read PATs before any tenant context exists), so no pinned conn is needed.
type patMinter struct {
	store PATStore
}

func (a *patMinter) Mint(ctx context.Context, tenantID uuid.UUID, name string, expiresAt time.Time) (string, error) {
	plaintext, prefix, hash, err := domainauth.GenerateToken()
	if err != nil {
		return "", fmt.Errorf("demo pat: generate: %w", err)
	}
	exp := expiresAt
	pat := &domainauth.PAT{
		ID:        uuid.New(),
		TenantID:  tenantID,
		Name:      name,
		Prefix:    prefix,
		Hash:      hash,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: &exp,
	}
	if err := a.store.Create(ctx, pat); err != nil {
		return "", fmt.Errorf("demo pat: persist: %w", err)
	}
	return plaintext, nil
}

// seeder resolves the tenant's default warehouse (created by the bootstrap) and
// seeds the horticulture demo catalogue against it. It runs inside the tenant
// scope (see scopeAdapter), so dbscope.From returns the pinned conn and every
// product/stock write is RLS-bound to the demo tenant.
type seeder struct {
	db   *sql.DB
	seed *appob.SeedDemoUseCase
}

func (a *seeder) SeedHorticulture(ctx context.Context, tenantID uuid.UUID) error {
	var warehouseID uuid.UUID
	const q = `SELECT id FROM tally.warehouse
	           WHERE tenant_id = $1 AND is_default = true AND enabled = true
	           ORDER BY created_at LIMIT 1`
	if err := dbscope.From(ctx, a.db).QueryRowContext(ctx, q, tenantID).Scan(&warehouseID); err != nil {
		return fmt.Errorf("demo seed: resolve default warehouse: %w", err)
	}
	if _, err := a.seed.Execute(ctx, appob.SeedInput{
		TenantID:    tenantID,
		WarehouseID: warehouseID,
		Persona:     appob.PersonaHorticulture,
	}); err != nil {
		return fmt.Errorf("demo seed: %w", err)
	}
	return nil
}

// stockAdapter bridges the onboarding StockInitializer/SalesRecorder ports to the
// real RecordMovementUseCase. It mirrors the unexported adapter in the onboarding
// HTTP handler (kept here so the demo package stays self-contained rather than
// reaching into a handler package).
type stockAdapter struct {
	uc *appstock.RecordMovementUseCase
}

func (a *stockAdapter) Execute(ctx context.Context, req appob.StockInitRequest) (*domainstock.Snapshot, error) {
	snap, err := a.uc.Execute(ctx, appstock.RecordMovementRequest{
		TenantID:      req.TenantID,
		ProductID:     req.ProductID,
		WarehouseID:   req.WarehouseID,
		Direction:     domainstock.DirectionIn,
		Qty:           req.Qty,
		ConvFactor:    "1",
		UnitCost:      req.UnitCost,
		CostStrategy:  domainstock.CostStrategyWAC,
		ReferenceType: domainstock.RefInit,
		OccurredAt:    req.OccurredAt,
	})
	if err != nil {
		return nil, fmt.Errorf("demo stock init: %w", err)
	}
	return snap, nil
}

func (a *stockAdapter) RecordSale(ctx context.Context, req appob.DemoSaleRequest) error {
	ref := uuid.New()
	if _, err := a.uc.Execute(ctx, appstock.RecordMovementRequest{
		TenantID:      req.TenantID,
		ProductID:     req.ProductID,
		WarehouseID:   req.WarehouseID,
		Direction:     domainstock.DirectionOut,
		Qty:           req.Qty,
		ConvFactor:    "1",
		CostStrategy:  domainstock.CostStrategyWAC,
		ReferenceType: domainstock.RefSale,
		ReferenceID:   &ref,
		OccurredAt:    req.OccurredAt,
	}); err != nil {
		return fmt.Errorf("demo stock sale: %w", err)
	}
	return nil
}
