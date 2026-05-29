// Package dau records and reads per-user Daily Active User counts using Redis
// HyperLogLog. Each tracked product event gets its own HLL key per UTC day,
// plus a shared "all" bucket; PFCOUNT yields the unique-user cardinality with
// ~0.8% standard error at O(1) memory regardless of how many users are seen.
// Keys self-expire after the retention window so no sweeper job is required.
//
// Counting is keyed on a real user id injected server-side by the Next
// /api/otel-events route from the verified session — never a client-supplied
// value. A blank user id is dropped rather than bucketed under "anonymous",
// because an anonymous DAU is not a measurement.
package dau

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// keyTTL bounds every daily HLL key. 14 days comfortably covers the
	// assumption-snapshot query window with slack for late scrapes; after that
	// Redis reclaims the key on its own.
	keyTTL = 14 * 24 * time.Hour
	// allBucket is the synthetic event name for the cross-event "any active
	// user today" cardinality that backs tally_total_dau.
	allBucket = "all"
	// dateLayout formats the UTC day component of a key (dau:<event>:<day>).
	dateLayout = "2006-01-02"
)

// Store records DAU hits into Redis HLL keys and reads their cardinality back.
type Store struct {
	rdb *redis.Client
	now func() time.Time
}

// New builds a Store over an already-connected go-redis client. The clock is
// time.Now; tests inject a fixed clock via WithClock for deterministic dates.
func New(rdb *redis.Client) *Store {
	return &Store{rdb: rdb, now: time.Now}
}

// WithClock overrides the time source (test-only) and returns the receiver so
// it can be chained onto New.
func (s *Store) WithClock(now func() time.Time) *Store {
	s.now = now
	return s
}

// Record adds userID to today's HLL for the event and to the cross-event "all"
// bucket, refreshing the TTL on both. A nil store, blank userID, or blank event
// is a no-op so callers need not guard each call site.
func (s *Store) Record(ctx context.Context, event, userID string) error {
	if s == nil || s.rdb == nil || userID == "" || event == "" {
		return nil
	}
	day := s.today()
	eventKey := key(event, day)
	allKey := key(allBucket, day)

	// One round-trip: add to both buckets and (re)set their TTL.
	pipe := s.rdb.Pipeline()
	pipe.PFAdd(ctx, eventKey, userID)
	pipe.Expire(ctx, eventKey, keyTTL)
	pipe.PFAdd(ctx, allKey, userID)
	pipe.Expire(ctx, allKey, keyTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("dau: record event %q: %w", event, err)
	}
	return nil
}

// CountToday returns the unique-user cardinality for the event on the current
// UTC day. A nil store returns 0 with no error so degraded configs read as
// "no data" rather than failing the scrape.
func (s *Store) CountToday(ctx context.Context, event string) (int64, error) {
	if s == nil || s.rdb == nil {
		return 0, nil
	}
	n, err := s.rdb.PFCount(ctx, key(event, s.today())).Result()
	if err != nil {
		return 0, fmt.Errorf("dau: count event %q: %w", event, err)
	}
	return n, nil
}

// CountTotalToday returns the unique-user cardinality across all tracked events
// for the current UTC day.
func (s *Store) CountTotalToday(ctx context.Context) (int64, error) {
	return s.CountToday(ctx, allBucket)
}

func (s *Store) today() string {
	return s.now().UTC().Format(dateLayout)
}

// key builds the Redis key for a bucket on a given day, e.g.
// "dau:palette_invocation:2026-05-29".
func key(bucket, day string) string {
	return fmt.Sprintf("dau:%s:%s", bucket, day)
}
