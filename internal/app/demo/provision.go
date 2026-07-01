// Package demo is the application/orchestration layer for the no-OIDC public
// sandbox. It provisions a fresh, write-isolated demo tenant so a prospect (e.g.
// a nursery owner in a sales interview) can walk onboarding → record a purchase →
// see stock and AI replenishment WITHOUT a real account or OIDC login.
//
// Provisioning reuses shipped primitives only — the normal onboarding bootstrap
// (tenant + mapping + profile + default 苗圃仓 warehouse/supplier), the demo seed
// (nursery products + opening stock + backdated sales), and Personal Access
// Tokens (the existing headless auth path) — so a sandbox visitor is just a
// short-lived, RLS-isolated tenant carrying a throwaway PAT. No new tables.
//
// The layer depends on interfaces and an injected clock only, so the orchestration
// is unit-testable with fakes. The concrete adapters (ChooseProfile, dbscope
// pinning, PAT repo, SeedDemo) are wired in lifecycle behind the TALLY_DEMO_MODE
// gate; the public HTTP entry point is added in the adapter layer.
package demo

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Naming/lifetime defaults for a sandbox tenant. The PAT is deliberately
// short-lived: a demo session is throwaway, and an expired token plus a reaper
// keeps abandoned sandboxes from accumulating credentials.
const (
	demoTenantName = "苗木演示"
	demoPATName    = "demo-session"
	// DefaultPATTTL bounds how long a sandbox PAT stays valid.
	DefaultPATTTL = 24 * time.Hour
	// demoSubPrefix namespaces the synthetic identity so a demo tenant can never
	// collide with — or be mistaken for — a real OIDC subject.
	demoSubPrefix = "demo:"
)

// TenantBootstrapper provisions a brand-new, isolated demo tenant (horticulture
// profile, synthetic identity) and returns its id. The adapter wraps the normal
// onboarding bootstrap, which also seeds the default 苗圃仓 warehouse + supplier.
type TenantBootstrapper interface {
	CreateDemoTenant(ctx context.Context, sub, name string) (uuid.UUID, error)
}

// ScopedRunner executes fn with the RLS context pinned to tenantID (SET
// app.tenant_id), so every write inside fn satisfies row-level security for that
// tenant. The adapter wraps dbscope.WithPinnedConn — the same primitive the
// public Shopify webhook uses to write under a tenant resolved outside auth.
type ScopedRunner interface {
	Run(ctx context.Context, tenantID uuid.UUID, fn func(context.Context) error) error
}

// PATMinter issues a Personal Access Token scoped to tenantID and returns the
// plaintext token (shown exactly once). MUST be called inside the tenant scope so
// the PAT row insert passes RLS.
type PATMinter interface {
	Mint(ctx context.Context, tenantID uuid.UUID, name string, expiresAt time.Time) (token string, err error)
}

// Seeder fills the tenant with demo nursery products, opening stock and backdated
// sales so stock views and AI replenishment have real signal. MUST be called
// inside the tenant scope.
type Seeder interface {
	SeedHorticulture(ctx context.Context, tenantID uuid.UUID) error
}

// Result is what a sandbox visitor needs to enter: the isolated tenant, a
// throwaway bearer token, and when it stops working.
type Result struct {
	TenantID  uuid.UUID `json:"tenant_id"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Provisioner orchestrates demo-tenant creation over the four ports.
type Provisioner struct {
	bootstrap TenantBootstrapper
	scope     ScopedRunner
	pat       PATMinter
	seeder    Seeder
	clock     func() time.Time
	patTTL    time.Duration
}

// NewProvisioner wires the use case. A nil clock falls back to time.Now and a
// zero ttl to DefaultPATTTL, so callers can pass the common case tersely.
func NewProvisioner(bootstrap TenantBootstrapper, scope ScopedRunner, pat PATMinter, seeder Seeder, clock func() time.Time, ttl time.Duration) *Provisioner {
	if clock == nil {
		clock = time.Now
	}
	if ttl <= 0 {
		ttl = DefaultPATTTL
	}
	return &Provisioner{bootstrap: bootstrap, scope: scope, pat: pat, seeder: seeder, clock: clock, patTTL: ttl}
}

// Provision creates one isolated demo tenant and returns its entry credentials.
//
// Two phases, deliberately not one transaction: the tenant must be committed
// before a connection can be pinned to its RLS context. Phase 1 bootstraps the
// tenant (its own tx); phase 2 mints the PAT and seeds nursery data inside the
// tenant scope. A failure in phase 2 surfaces to the caller — the reaper later
// collects any tenant left without a usable session.
func (p *Provisioner) Provision(ctx context.Context) (Result, error) {
	// Synthetic, namespaced identity — never collides with a real OIDC sub.
	sub := demoSubPrefix + uuid.NewString()

	tenantID, err := p.bootstrap.CreateDemoTenant(ctx, sub, demoTenantName)
	if err != nil {
		return Result{}, fmt.Errorf("demo: create tenant: %w", err)
	}

	expiresAt := p.clock().Add(p.patTTL)
	var token string
	if err := p.scope.Run(ctx, tenantID, func(ctx context.Context) error {
		t, err := p.pat.Mint(ctx, tenantID, demoPATName, expiresAt)
		if err != nil {
			return fmt.Errorf("mint pat: %w", err)
		}
		token = t
		if err := p.seeder.SeedHorticulture(ctx, tenantID); err != nil {
			return fmt.Errorf("seed nursery data: %w", err)
		}
		return nil
	}); err != nil {
		return Result{}, fmt.Errorf("demo: provision tenant %s: %w", tenantID, err)
	}

	return Result{TenantID: tenantID, Token: token, ExpiresAt: expiresAt}, nil
}
