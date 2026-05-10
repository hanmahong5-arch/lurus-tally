package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	// HeaderIdempotencyKey is the request header carrying the client-generated key.
	HeaderIdempotencyKey = "Idempotency-Key"
	// HeaderIdempotencyReplay is set on responses replayed from cache so clients
	// can distinguish a cache hit (true) from a fresh execution (absent).
	HeaderIdempotencyReplay = "Idempotent-Replay"

	idempotencyKeyMaxLen = 200
	idempotencyCacheTTL  = 10 * time.Minute
	idempotencyLockTTL   = 30 * time.Second
	idempotencyKeyPrefix = "tally:idem:"
)

// IdempotencyStore is the minimal contract the middleware needs from a kv store.
// Any implementation MUST honour go-redis-style errors: ErrIdemNotFound when the
// key is missing on Get; SetNX returning false when the lock already exists.
type IdempotencyStore interface {
	Get(ctx context.Context, key string) ([]byte, error)
	SetNX(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Del(ctx context.Context, keys ...string) error
}

// ErrIdemNotFound is the sentinel error a store MUST return from Get when the
// key does not exist. Any other error is treated as a transient store fault and
// the middleware degrades open (processes the request without dedup).
var ErrIdemNotFound = errors.New("idempotency: key not found")

// idempotencyEntry is the JSON envelope persisted in the store.
type idempotencyEntry struct {
	Status      int    `json:"status"`
	ContentType string `json:"ct,omitempty"`
	Body        []byte `json:"body,omitempty"`
}

// Idempotency returns a Gin middleware that dedupes write requests carrying an
// Idempotency-Key header. The first request to use a given (tenant, key) pair
// runs normally and its response (status + body + content-type) is cached for
// idempotencyCacheTTL. Subsequent requests with the same pair short-circuit
// and replay the cached response without invoking the handler chain.
//
// Behaviour rules:
//   - GET / HEAD / OPTIONS pass through unchanged.
//   - Missing or oversized header passes through unchanged.
//   - Missing tenant context (auth not run / failed) passes through —
//     downstream handlers will reject the request anyway.
//   - 5xx responses are NOT cached so a transient failure can be retried.
//   - Concurrent requests that both miss the cache compete on a short SetNX
//     lock; the loser receives 409 with Retry-After: 1 and is expected to
//     retry once the leader finishes (≤ idempotencyLockTTL).
//   - Any store fault (Get/SetNX/Set returning a non-NotFound error) makes
//     the middleware degrade open: processes the request without caching.
//     This keeps API availability decoupled from cache availability.
//
// When store is nil the middleware is a no-op — useful in dev / no-Redis.
func Idempotency(store IdempotencyStore) gin.HandlerFunc {
	if store == nil {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		method := c.Request.Method
		if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
			c.Next()
			return
		}
		rawKey := c.GetHeader(HeaderIdempotencyKey)
		if rawKey == "" || len(rawKey) > idempotencyKeyMaxLen {
			c.Next()
			return
		}
		tenant, ok := c.Get(CtxKeyTenantID)
		tid, _ := tenant.(string)
		if !ok || tid == "" {
			c.Next()
			return
		}
		ctx := c.Request.Context()
		storeKey := idempotencyKeyPrefix + tid + ":" + rawKey
		lockKey := storeKey + ":lock"

		// Cache lookup: hit → replay; not-found → proceed; any other error → degrade open.
		raw, err := store.Get(ctx, storeKey)
		switch {
		case err == nil:
			var entry idempotencyEntry
			if jerr := json.Unmarshal(raw, &entry); jerr == nil && entry.Status > 0 {
				if entry.ContentType != "" {
					c.Header("Content-Type", entry.ContentType)
				}
				c.Header(HeaderIdempotencyReplay, "true")
				c.Status(entry.Status)
				_, _ = c.Writer.Write(entry.Body)
				c.Abort()
				return
			}
			// Malformed cache entry — fall through to re-execute.
		case errors.Is(err, ErrIdemNotFound):
			// Expected miss — continue.
		default:
			// Store fault — degrade open (no caching this round).
			c.Next()
			return
		}

		// Acquire short-lived lock so concurrent dup requests do not both run.
		acquired, lerr := store.SetNX(ctx, lockKey, []byte("1"), idempotencyLockTTL)
		if lerr != nil {
			c.Next()
			return
		}
		if !acquired {
			c.Header("Retry-After", "1")
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{
				"error":   "idempotency_in_flight",
				"message": "an earlier request with this Idempotency-Key is still being processed",
			})
			return
		}

		rec := &recorder{ResponseWriter: c.Writer, body: &bytes.Buffer{}}
		c.Writer = rec
		c.Next()

		// Best-effort cleanup; ignore lock-delete errors.
		_ = store.Del(ctx, lockKey)

		status := rec.Status()
		if status >= http.StatusOK && status < http.StatusInternalServerError {
			payload, mErr := json.Marshal(idempotencyEntry{
				Status:      status,
				ContentType: rec.Header().Get("Content-Type"),
				Body:        rec.body.Bytes(),
			})
			if mErr == nil {
				_ = store.Set(ctx, storeKey, payload, idempotencyCacheTTL)
			}
		}
	}
}

// recorder is a gin.ResponseWriter wrapper that mirrors writes into a buffer
// so the middleware can persist the response body alongside the status.
type recorder struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (r *recorder) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

func (r *recorder) WriteString(s string) (int, error) {
	r.body.WriteString(s)
	return r.ResponseWriter.WriteString(s)
}
