package middleware

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// IdempotencyRedisStore adapts *redis.Client to IdempotencyStore. It maps
// redis.Nil → ErrIdemNotFound so the middleware can distinguish a clean miss
// from a transport fault.
type IdempotencyRedisStore struct {
	c *redis.Client
}

// NewIdempotencyRedisStore wraps a go-redis v9 client. Pass the same client
// already used by the AI plan store — Idempotency keys live in the shared
// Tally Redis db (DB 5) under the tally:idem: prefix.
func NewIdempotencyRedisStore(c *redis.Client) *IdempotencyRedisStore {
	return &IdempotencyRedisStore{c: c}
}

func (s *IdempotencyRedisStore) Get(ctx context.Context, key string) ([]byte, error) {
	v, err := s.c.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrIdemNotFound
	}
	return v, err
}

func (s *IdempotencyRedisStore) SetNX(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
	return s.c.SetNX(ctx, key, value, ttl).Result()
}

func (s *IdempotencyRedisStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return s.c.Set(ctx, key, value, ttl).Err()
}

func (s *IdempotencyRedisStore) Del(ctx context.Context, keys ...string) error {
	return s.c.Del(ctx, keys...).Err()
}
