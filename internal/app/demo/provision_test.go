package demo_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	demoapp "github.com/hanmahong5-arch/lurus-tally/internal/app/demo"
)

// recorder captures the call order across the fakes so a test can assert that
// minting and seeding happen INSIDE the tenant scope, and in that order.
type recorder struct{ events []string }

type fakeBootstrap struct {
	rec     *recorder
	id      uuid.UUID
	gotSub  string
	gotName string
	err     error
}

func (f *fakeBootstrap) CreateDemoTenant(_ context.Context, sub, name string) (uuid.UUID, error) {
	f.rec.events = append(f.rec.events, "bootstrap")
	f.gotSub, f.gotName = sub, name
	if f.err != nil {
		return uuid.Nil, f.err
	}
	return f.id, nil
}

type fakeScope struct {
	rec       *recorder
	ranTenant uuid.UUID
	skip      bool // if true, do NOT run fn (simulates a scope that never entered)
}

func (f *fakeScope) Run(ctx context.Context, tenantID uuid.UUID, fn func(context.Context) error) error {
	f.rec.events = append(f.rec.events, "scope:enter")
	f.ranTenant = tenantID
	if f.skip {
		return nil
	}
	return fn(ctx)
}

type fakePAT struct {
	rec       *recorder
	gotTenant uuid.UUID
	gotName   string
	gotExpiry time.Time
	token     string
	err       error
}

func (f *fakePAT) Mint(_ context.Context, tenantID uuid.UUID, name string, expiresAt time.Time) (string, error) {
	f.rec.events = append(f.rec.events, "mint")
	f.gotTenant, f.gotName, f.gotExpiry = tenantID, name, expiresAt
	if f.err != nil {
		return "", f.err
	}
	return f.token, nil
}

type fakeSeeder struct {
	rec       *recorder
	gotTenant uuid.UUID
	err       error
}

func (f *fakeSeeder) SeedHorticulture(_ context.Context, tenantID uuid.UUID) error {
	f.rec.events = append(f.rec.events, "seed")
	f.gotTenant = tenantID
	return f.err
}

// fixedClock returns a deterministic instant so the PAT expiry is assertable.
var baseTime = time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)

func fixedClock() time.Time { return baseTime }

func newProvisioner(t *testing.T, rec *recorder, b *fakeBootstrap, s *fakeScope, p *fakePAT, sd *fakeSeeder, ttl time.Duration) *demoapp.Provisioner {
	t.Helper()
	return demoapp.NewProvisioner(b, s, p, sd, fixedClock, ttl)
}

// TestProvision_HappyPath: one isolated tenant, a PAT minted inside its scope,
// nursery data seeded, and entry credentials returned with a bounded expiry.
func TestProvision_HappyPath(t *testing.T) {
	rec := &recorder{}
	tenantID := uuid.New()
	b := &fakeBootstrap{rec: rec, id: tenantID}
	s := &fakeScope{rec: rec}
	p := &fakePAT{rec: rec, token: "tally_pat_abc123"}
	sd := &fakeSeeder{rec: rec}
	prov := newProvisioner(t, rec, b, s, p, sd, time.Hour)

	res, err := prov.Provision(context.Background())
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	if res.TenantID != tenantID {
		t.Errorf("tenant=%v, want %v", res.TenantID, tenantID)
	}
	if res.Token != "tally_pat_abc123" {
		t.Errorf("token=%q, want minted token", res.Token)
	}
	if !res.ExpiresAt.Equal(baseTime.Add(time.Hour)) {
		t.Errorf("expiresAt=%v, want base+1h", res.ExpiresAt)
	}

	// Synthetic identity is namespaced so it can never be a real OIDC sub.
	if !strings.HasPrefix(b.gotSub, "demo:") {
		t.Errorf("sub=%q, want demo: prefix", b.gotSub)
	}

	// PAT + seed both ran against the provisioned tenant...
	if p.gotTenant != tenantID || sd.gotTenant != tenantID || s.ranTenant != tenantID {
		t.Errorf("scope/pat/seed tenant mismatch: scope=%v pat=%v seed=%v want %v", s.ranTenant, p.gotTenant, sd.gotTenant, tenantID)
	}
	if !p.gotExpiry.Equal(baseTime.Add(time.Hour)) {
		t.Errorf("pat expiry=%v, want base+1h", p.gotExpiry)
	}

	// ...and crucially, mint + seed happened INSIDE the scope, in order.
	want := []string{"bootstrap", "scope:enter", "mint", "seed"}
	if strings.Join(rec.events, ",") != strings.Join(want, ",") {
		t.Errorf("call order=%v, want %v", rec.events, want)
	}
}

// TestProvision_BootstrapFailsShortCircuits: if the tenant can't be created we
// never enter the scope, mint, or seed.
func TestProvision_BootstrapFailsShortCircuits(t *testing.T) {
	rec := &recorder{}
	b := &fakeBootstrap{rec: rec, err: errors.New("db down")}
	s := &fakeScope{rec: rec}
	p := &fakePAT{rec: rec, token: "x"}
	sd := &fakeSeeder{rec: rec}
	prov := newProvisioner(t, rec, b, s, p, sd, 0)

	if _, err := prov.Provision(context.Background()); err == nil {
		t.Fatal("expected bootstrap error to propagate")
	}
	if len(rec.events) != 1 || rec.events[0] != "bootstrap" {
		t.Errorf("events=%v, want only [bootstrap]", rec.events)
	}
}

// TestProvision_SeedFailsPropagates: a seed failure inside the scope surfaces to
// the caller (so the handler returns an error rather than a half-built sandbox).
func TestProvision_SeedFailsPropagates(t *testing.T) {
	rec := &recorder{}
	b := &fakeBootstrap{rec: rec, id: uuid.New()}
	s := &fakeScope{rec: rec}
	p := &fakePAT{rec: rec, token: "x"}
	sd := &fakeSeeder{rec: rec, err: errors.New("seed boom")}
	prov := newProvisioner(t, rec, b, s, p, sd, 0)

	if _, err := prov.Provision(context.Background()); err == nil {
		t.Fatal("expected seed error to propagate")
	}
	// mint ran before the failing seed, both inside the scope.
	want := []string{"bootstrap", "scope:enter", "mint", "seed"}
	if strings.Join(rec.events, ",") != strings.Join(want, ",") {
		t.Errorf("call order=%v, want %v", rec.events, want)
	}
}

// TestProvision_DefaultTTL: a zero ttl falls back to DefaultPATTTL.
func TestProvision_DefaultTTL(t *testing.T) {
	rec := &recorder{}
	b := &fakeBootstrap{rec: rec, id: uuid.New()}
	s := &fakeScope{rec: rec}
	p := &fakePAT{rec: rec, token: "x"}
	sd := &fakeSeeder{rec: rec}
	prov := newProvisioner(t, rec, b, s, p, sd, 0)

	res, err := prov.Provision(context.Background())
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if !res.ExpiresAt.Equal(baseTime.Add(demoapp.DefaultPATTTL)) {
		t.Errorf("expiresAt=%v, want base+DefaultPATTTL", res.ExpiresAt)
	}
}
