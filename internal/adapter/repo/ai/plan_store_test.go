package ai_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	repoai "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/ai"
	domainai "github.com/hanmahong5-arch/lurus-tally/internal/domain/ai"
)

// inMemRedis is a minimal in-memory Redis mock for tests.
type inMemRedis struct {
	data   map[string]string
	expiry map[string]time.Time
	getErr error
	setErr error
}

func newInMemRedis() *inMemRedis {
	return &inMemRedis{
		data:   make(map[string]string),
		expiry: make(map[string]time.Time),
	}
}

func (r *inMemRedis) Set(_ context.Context, key string, value interface{}, expiration time.Duration) error {
	if r.setErr != nil {
		return r.setErr
	}
	r.data[key] = fmt.Sprintf("%v", value)
	if expiration > 0 {
		r.expiry[key] = time.Now().Add(expiration)
	}
	return nil
}

func (r *inMemRedis) Get(_ context.Context, key string) (string, error) {
	if r.getErr != nil {
		return "", r.getErr
	}
	v, ok := r.data[key]
	if !ok {
		return "", fmt.Errorf("redis: nil") // matches isNotFound
	}
	exp, hasExp := r.expiry[key]
	if hasExp && time.Now().After(exp) {
		delete(r.data, key)
		return "", fmt.Errorf("redis: nil")
	}
	return v, nil
}

func (r *inMemRedis) Del(_ context.Context, keys ...string) error {
	for _, k := range keys {
		delete(r.data, k)
		delete(r.expiry, k)
	}
	return nil
}

func makePlan(tenantID uuid.UUID) *domainai.Plan {
	return &domainai.Plan{
		ID:       uuid.New(),
		TenantID: tenantID,
		Type:     domainai.PlanTypePriceChange,
		Status:   domainai.PlanStatusPending,
		Payload:  map[string]interface{}{"filter": "all", "action": "+5%"},
		Preview: domainai.PlanPreview{
			Description:   "Test",
			AffectedCount: 3,
		},
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(30 * time.Minute),
	}
}

// TestRedisPlanStore_SaveAndGet_RoundTrip verifies a plan persists and deserialises correctly.
func TestRedisPlanStore_SaveAndGet_RoundTrip(t *testing.T) {
	rdb := newInMemRedis()
	store := repoai.New(rdb, 30*time.Minute)
	tenantID := uuid.New()
	plan := makePlan(tenantID)

	if err := store.SavePlan(context.Background(), plan); err != nil {
		t.Fatalf("SavePlan failed: %v", err)
	}

	got, err := store.GetPlan(context.Background(), tenantID, plan.ID)
	if err != nil {
		t.Fatalf("GetPlan failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected plan, got nil")
	}
	if got.ID != plan.ID {
		t.Errorf("plan ID mismatch: got %s, want %s", got.ID, plan.ID)
	}
	if got.Status != domainai.PlanStatusPending {
		t.Errorf("expected pending, got %s", got.Status)
	}
}

// TestRedisPlanStore_GetPlan_NotFound_ReturnsNil verifies missing keys return nil.
func TestRedisPlanStore_GetPlan_NotFound_ReturnsNil(t *testing.T) {
	rdb := newInMemRedis()
	store := repoai.New(rdb, 30*time.Minute)

	got, err := store.GetPlan(context.Background(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got != nil {
		t.Error("expected nil plan for missing key")
	}
}

// TestRedisPlanStore_UpdatePlan_ChangesStatus verifies status updates persist.
func TestRedisPlanStore_UpdatePlan_ChangesStatus(t *testing.T) {
	rdb := newInMemRedis()
	store := repoai.New(rdb, 30*time.Minute)
	tenantID := uuid.New()
	plan := makePlan(tenantID)

	_ = store.SavePlan(context.Background(), plan)

	plan.Status = domainai.PlanStatusConfirmed
	if err := store.UpdatePlan(context.Background(), plan); err != nil {
		t.Fatalf("UpdatePlan failed: %v", err)
	}

	got, _ := store.GetPlan(context.Background(), tenantID, plan.ID)
	if got.Status != domainai.PlanStatusConfirmed {
		t.Errorf("expected confirmed, got %s", got.Status)
	}
}

// TestRedisPlanStore_TTL_ExpiresAfterDeadline verifies plans with past ExpiresAt are rejected on update.
func TestRedisPlanStore_TTL_ExpiresAfterDeadline(t *testing.T) {
	rdb := newInMemRedis()
	store := repoai.New(rdb, 1*time.Millisecond) // tiny TTL
	tenantID := uuid.New()
	plan := makePlan(tenantID)
	plan.ExpiresAt = time.Now().Add(-1 * time.Second) // already expired

	// UpdatePlan should fail since ExpiresAt is in the past.
	plan.Status = domainai.PlanStatusConfirmed
	err := store.UpdatePlan(context.Background(), plan)
	if err == nil {
		t.Error("expected error for expired plan, got nil")
	}
}

// TestRedisPlanStore_TenantIsolation_DifferentTenantsIsolated verifies tenants cannot see each other's plans.
func TestRedisPlanStore_TenantIsolation_DifferentTenantsIsolated(t *testing.T) {
	rdb := newInMemRedis()
	store := repoai.New(rdb, 30*time.Minute)

	tenantA := uuid.New()
	tenantB := uuid.New()
	plan := makePlan(tenantA)

	_ = store.SavePlan(context.Background(), plan)

	// Tenant B should not see tenant A's plan.
	got, err := store.GetPlan(context.Background(), tenantB, plan.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Error("tenant isolation failed: tenant B can see tenant A's plan")
	}
}
