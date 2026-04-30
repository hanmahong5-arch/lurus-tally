// Package ai implements the Redis-backed plan store for the AI assistant.
// Plans are stored with a TTL of 1800 seconds (30 minutes) and keyed by tenant_id + plan_id.
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	domainai "github.com/hanmahong5-arch/lurus-tally/internal/domain/ai"
	"github.com/google/uuid"
)

// RedisClient is the minimal Redis interface required by the plan store.
// Using a small interface rather than the full go-redis client keeps tests simple.
type RedisClient interface {
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	Get(ctx context.Context, key string) (string, error)
	Del(ctx context.Context, keys ...string) error
}

const (
	// defaultTTL is the plan TTL: 30 minutes.
	defaultTTL = 30 * time.Minute
	// keyPrefix namespaces all plan keys in Redis.
	keyPrefix = "tally:ai:plan:"
)

// ErrNotFound is returned when a key does not exist in Redis.
var ErrNotFound = errors.New("plan store: key not found")

// RedisPlanStore implements app/ai.PlanStore backed by Redis.
type RedisPlanStore struct {
	client RedisClient
	ttl    time.Duration
}

// New constructs a RedisPlanStore. Pass ttl=0 to use the default (1800s).
func New(client RedisClient, ttl time.Duration) *RedisPlanStore {
	if ttl == 0 {
		ttl = defaultTTL
	}
	return &RedisPlanStore{client: client, ttl: ttl}
}

// planKey returns the Redis key for a given tenant + plan pair.
func planKey(tenantID, planID uuid.UUID) string {
	return keyPrefix + tenantID.String() + ":" + planID.String()
}

// SavePlan serialises and stores a Plan in Redis with the configured TTL.
func (s *RedisPlanStore) SavePlan(ctx context.Context, plan *domainai.Plan) error {
	b, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("plan store: marshal plan: %w", err)
	}
	key := planKey(plan.TenantID, plan.ID)
	if err := s.client.Set(ctx, key, string(b), s.ttl); err != nil {
		return fmt.Errorf("plan store: redis SET: %w", err)
	}
	return nil
}

// GetPlan retrieves a Plan by tenant + plan ID. Returns nil, nil when not found.
func (s *RedisPlanStore) GetPlan(ctx context.Context, tenantID, planID uuid.UUID) (*domainai.Plan, error) {
	key := planKey(tenantID, planID)
	raw, err := s.client.Get(ctx, key)
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("plan store: redis GET: %w", err)
	}
	var plan domainai.Plan
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		return nil, fmt.Errorf("plan store: unmarshal plan: %w", err)
	}
	return &plan, nil
}

// UpdatePlan re-serialises and overwrites the plan in Redis, preserving the original TTL
// as a best-effort (resets the TTL to remaining time is not possible with simple SET).
// For plan status updates this is acceptable — the plan expires regardless.
func (s *RedisPlanStore) UpdatePlan(ctx context.Context, plan *domainai.Plan) error {
	remaining := time.Until(plan.ExpiresAt)
	if remaining <= 0 {
		// Plan already expired; delete it.
		_ = s.client.Del(ctx, planKey(plan.TenantID, plan.ID))
		return fmt.Errorf("plan store: plan %s has expired", plan.ID)
	}
	b, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("plan store: marshal plan for update: %w", err)
	}
	key := planKey(plan.TenantID, plan.ID)
	if err := s.client.Set(ctx, key, string(b), remaining); err != nil {
		return fmt.Errorf("plan store: redis SET (update): %w", err)
	}
	return nil
}

// isNotFound checks for Redis nil/not-found errors without importing go-redis.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "nil") || strings.Contains(msg, "not found") ||
		strings.Contains(msg, "redis: nil")
}
