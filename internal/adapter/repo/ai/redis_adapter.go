package ai

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// GoRedisAdapter wraps *redis.Client to satisfy the RedisClient interface
// required by RedisPlanStore. This thin adapter avoids importing go-redis
// throughout the domain/app layers.
type GoRedisAdapter struct {
	client *redis.Client
}

// NewGoRedisAdapter wraps a go-redis v9 client.
func NewGoRedisAdapter(client *redis.Client) *GoRedisAdapter {
	return &GoRedisAdapter{client: client}
}

// Set sets the value for key with the given expiration.
func (a *GoRedisAdapter) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return a.client.Set(ctx, key, value, expiration).Err()
}

// Get returns the value for key. Returns an error containing "redis: nil" when the key does not exist.
func (a *GoRedisAdapter) Get(ctx context.Context, key string) (string, error) {
	return a.client.Get(ctx, key).Result()
}

// Del deletes one or more keys.
func (a *GoRedisAdapter) Del(ctx context.Context, keys ...string) error {
	return a.client.Del(ctx, keys...).Err()
}
