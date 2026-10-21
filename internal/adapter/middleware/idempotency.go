package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/loghelper"
)

const (
	// HeaderIdempotencyKey is the request header carrying the client-generated key.
	HeaderIdempotencyKey = "Idempotency-Key"
	// HeaderIdempotencyReplay is set on responses replayed from cache so clients
	// can distinguish a cache hit (true) from a fresh execution (absent).
	HeaderIdempotencyReplay = "Idempotent-Replay"

	idempotencyKeyMaxLen = 200
	idempotencyCacheTTL  = 24 * time.Hour
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
//
// Kind classifies the response for cache retention:
//   - "ok"           — 2xx; always cached.
//   - "client_error" — other 4xx (validation, conflict, etc.); NOT cached so a
//     corrected retry with the same key reaches the handler.
//   - "transient"    — 429 (rate limited); NOT cached so a retry can succeed after
//     the rate-limit budget resets.
type idempotencyEntry struct {
	Status      int    `json:"status"`
	Kind        string `json:"kind"` // "ok" | "client_error" | "transient"
	ContentType string `json:"ct,omitempty"`
	Body        []byte `json:"body,omitempty"`
	// Fingerprint is sha256(method|path|body) of the request that produced this
	// entry. A replay with the same key but a different fingerprint is a client
	// bug (key reused for a different payment) and must not be served the old result.
	Fingerprint string `json:"fp,omitempty"`
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
//   - 5xx and 4xx responses are NOT cached so a failed request can be retried
//     (after fixing the input) with the same key.
//   - Reusing a key with a different request fingerprint (method, path, body
//     sha256) returns 422 idempotency_key_reused instead of replaying.
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
		if !ok {
			idempotencySkipped.WithLabelValues("no_tenant").Inc()
			c.Next()
			return
		}
		// AuthMiddleware injects uuid.UUID; tests historically used string.
		// Both shapes are accepted so tests don't have to know the prod type.
		var tid string
		switch t := tenant.(type) {
		case uuid.UUID:
			if t == uuid.Nil {
				idempotencySkipped.WithLabelValues("no_tenant").Inc()
				c.Next()
				return
			}
			tid = t.String()
		case string:
			if t == "" {
				idempotencySkipped.WithLabelValues("no_tenant").Inc()
				c.Next()
				return
			}
			tid = t
		default:
			idempotencySkipped.WithLabelValues("wrong_type").Inc()
			c.Next()
			return
		}
		ctx := c.Request.Context()
		fp, ferr := requestFingerprint(c.Request)
		if ferr != nil {
			// Body is partially consumed and cannot be restored; do not run the handler on a truncated payload.
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
				"error":   "request_body_unreadable",
				"message": "提交的内容过大或读取失败，请精简后重新提交",
			})
			return
		}
		storeKey := idempotencyKeyPrefix + tid + ":" + rawKey
		lockKey := storeKey + ":lock"

		// Cache lookup: hit → replay; not-found → proceed; any other error → degrade open.
		raw, err := store.Get(ctx, storeKey)
		switch {
		case err == nil:
			var entry idempotencyEntry
			if jerr := json.Unmarshal(raw, &entry); jerr == nil && entry.Status > 0 {
				if entry.Fingerprint != "" && entry.Fingerprint != fp {
					c.AbortWithStatusJSON(http.StatusUnprocessableEntity, gin.H{
						"error":   "idempotency_key_reused",
						"message": "这次提交的编号已经用在另一笔不同的请求上，请刷新页面后重新提交",
					})
					return
				}
				if entry.ContentType != "" {
					c.Header("Content-Type", entry.ContentType)
				}
				c.Header(HeaderIdempotencyReplay, "true")
				c.Status(entry.Status)
				_, _ = c.Writer.Write(entry.Body)
				loghelper.Info(c.Request.Context(), "idempotency_skipped", map[string]any{
					"reason": "cache_hit",
					"key":    rawKey,
					"status": entry.Status,
				})
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
				"message": "上一次提交还在处理，请稍等几秒再看结果",
			})
			return
		}

		rec := &recorder{ResponseWriter: c.Writer, body: &bytes.Buffer{}}
		c.Writer = rec
		c.Next()

		// Best-effort cleanup; ignore lock-delete errors.
		_ = store.Del(ctx, lockKey)

		status := rec.Status()
		kind := classifyStatus(status)
		if kind == idempotencyKindTransient {
			// Do not cache transient errors (e.g. 429) so retries can succeed
			// after the rate-limit budget resets.
			return
		}
		if kind == idempotencyKindOK {
			payload, mErr := json.Marshal(idempotencyEntry{
				Status:      status,
				Kind:        kind,
				ContentType: rec.Header().Get("Content-Type"),
				Body:        rec.body.Bytes(),
				Fingerprint: fp,
			})
			if mErr == nil {
				_ = store.Set(ctx, storeKey, payload, idempotencyCacheTTL)
			}
		}
	}
}

// requestFingerprint hashes method, path and body, then restores the body so
// downstream handlers can still read it.
func requestFingerprint(r *http.Request) (string, error) {
	h := sha256.New()
	h.Write([]byte(r.Method))
	h.Write([]byte{0})
	h.Write([]byte(r.URL.Path))
	h.Write([]byte{0})
	if r.Body != nil {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			return "", err
		}
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(b))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

const (
	idempotencyKindOK          = "ok"
	idempotencyKindClientError = "client_error"
	idempotencyKindTransient   = "transient"
)

// classifyStatus returns the idempotency cache kind for a given HTTP status code.
// Returns "" for status codes that must not be cached (5xx, or unrecognised ranges).
//
// Conservative policy:
//   - 2xx → "ok", always cache.
//   - 429 → "transient", never cache (retry after budget reset must reach the handler).
//   - Other 4xx → "client_error", not cached (a corrected retry must reach the handler).
//   - 5xx → "" (not cached).
func classifyStatus(status int) string {
	switch {
	case status >= http.StatusOK && status < http.StatusMultipleChoices:
		return idempotencyKindOK
	case status == http.StatusTooManyRequests:
		return idempotencyKindTransient
	case status >= http.StatusBadRequest && status < http.StatusInternalServerError:
		return idempotencyKindClientError
	default:
		return ""
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
