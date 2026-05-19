package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// memStore is a minimal in-memory IdempotencyStore for tests. It does not
// honour TTL — tests are short enough that expiry is irrelevant. SetNX
// failures are surfaced via storeErr to exercise the degrade-open path.
type memStore struct {
	mu       sync.Mutex
	data     map[string][]byte
	getErr   error
	setNXErr error
}

func newMemStore() *memStore {
	return &memStore{data: make(map[string][]byte)}
}

func (m *memStore) Get(_ context.Context, key string) ([]byte, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[key]
	if !ok {
		return nil, middleware.ErrIdemNotFound
	}
	return v, nil
}

func (m *memStore) SetNX(_ context.Context, key string, value []byte, _ time.Duration) (bool, error) {
	if m.setNXErr != nil {
		return false, m.setNXErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.data[key]; exists {
		return false, nil
	}
	m.data[key] = value
	return true, nil
}

func (m *memStore) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return nil
}

func (m *memStore) Del(_ context.Context, keys ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range keys {
		delete(m.data, k)
	}
	return nil
}

// newRouter builds a Gin engine with the Idempotency middleware in front of
// a counter handler that returns the call count in the body. Each call
// increments calls; tests assert on the count to detect cache hits.
func newRouter(store middleware.IdempotencyStore, tenantID string, calls *int32) *gin.Engine {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if tenantID != "" {
			c.Set(middleware.CtxKeyTenantID, tenantID)
		}
		c.Next()
	})
	r.Use(middleware.Idempotency(store))
	r.POST("/things", func(c *gin.Context) {
		n := atomic.AddInt32(calls, 1)
		c.JSON(http.StatusCreated, gin.H{"call": n})
	})
	r.GET("/things", func(c *gin.Context) {
		atomic.AddInt32(calls, 1)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return r
}

func TestIdempotency_NoStore_PassesThrough(t *testing.T) {
	var calls int32
	r := newRouter(nil, "tenant-a", &calls)

	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/things", nil)
		req.Header.Set(middleware.HeaderIdempotencyKey, "k1")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("call %d: expected 201, got %d", i, w.Code)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("nil store should pass through; expected 3 calls, got %d", got)
	}
}

func TestIdempotency_FirstCallExecutes_SecondReplays(t *testing.T) {
	var calls int32
	store := newMemStore()
	r := newRouter(store, "tenant-a", &calls)

	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest(http.MethodPost, "/things", nil)
	req1.Header.Set(middleware.HeaderIdempotencyKey, "k1")
	r.ServeHTTP(w1, req1)

	if w1.Code != http.StatusCreated {
		t.Fatalf("first call expected 201, got %d", w1.Code)
	}
	if w1.Header().Get(middleware.HeaderIdempotencyReplay) != "" {
		t.Error("first call should not have Idempotent-Replay header")
	}
	body1 := w1.Body.String()

	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodPost, "/things", nil)
	req2.Header.Set(middleware.HeaderIdempotencyKey, "k1")
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusCreated {
		t.Fatalf("replayed call expected 201, got %d", w2.Code)
	}
	if w2.Header().Get(middleware.HeaderIdempotencyReplay) != "true" {
		t.Errorf("replayed call should have Idempotent-Replay=true, got %q", w2.Header().Get(middleware.HeaderIdempotencyReplay))
	}
	if w2.Body.String() != body1 {
		t.Errorf("replayed body mismatch:\nfirst:  %s\nsecond: %s", body1, w2.Body.String())
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("handler should have run once, ran %d times", got)
	}
}

func TestIdempotency_DifferentKeys_BothExecute(t *testing.T) {
	var calls int32
	store := newMemStore()
	r := newRouter(store, "tenant-a", &calls)

	for _, k := range []string{"a", "b"} {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/things", nil)
		req.Header.Set(middleware.HeaderIdempotencyKey, k)
		r.ServeHTTP(w, req)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("distinct keys should both execute; got %d calls", got)
	}
}

func TestIdempotency_DifferentTenants_DoNotCollide(t *testing.T) {
	store := newMemStore()
	var callsA, callsB int32
	rA := newRouter(store, "tenant-a", &callsA)
	rB := newRouter(store, "tenant-b", &callsB)

	for _, r := range []*gin.Engine{rA, rB} {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/things", nil)
		req.Header.Set(middleware.HeaderIdempotencyKey, "shared-key")
		r.ServeHTTP(w, req)
	}
	if atomic.LoadInt32(&callsA) != 1 || atomic.LoadInt32(&callsB) != 1 {
		t.Errorf("each tenant should run handler once; got A=%d B=%d", callsA, callsB)
	}
}

func TestIdempotency_GET_PassesThrough(t *testing.T) {
	var calls int32
	store := newMemStore()
	r := newRouter(store, "tenant-a", &calls)

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/things", nil)
		req.Header.Set(middleware.HeaderIdempotencyKey, "k1")
		r.ServeHTTP(w, req)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("GET requests should not be deduped; got %d", got)
	}
}

func TestIdempotency_NoHeader_PassesThrough(t *testing.T) {
	var calls int32
	store := newMemStore()
	r := newRouter(store, "tenant-a", &calls)

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/things", nil)
		r.ServeHTTP(w, req)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("missing header should not dedupe; got %d", got)
	}
}

func TestIdempotency_NoTenant_PassesThrough(t *testing.T) {
	var calls int32
	store := newMemStore()
	r := newRouter(store, "", &calls)

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/things", nil)
		req.Header.Set(middleware.HeaderIdempotencyKey, "k1")
		r.ServeHTTP(w, req)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("missing tenant context should not dedupe; got %d", got)
	}
}

func TestIdempotency_OversizedKey_PassesThrough(t *testing.T) {
	var calls int32
	store := newMemStore()
	r := newRouter(store, "tenant-a", &calls)

	huge := strings.Repeat("x", 300)
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/things", nil)
		req.Header.Set(middleware.HeaderIdempotencyKey, huge)
		r.ServeHTTP(w, req)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("oversized key should be ignored; got %d", got)
	}
}

func TestIdempotency_5xxNotCached(t *testing.T) {
	store := newMemStore()
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.CtxKeyTenantID, "tenant-a")
		c.Next()
	})
	r.Use(middleware.Idempotency(store))
	var calls int32
	r.POST("/boom", func(c *gin.Context) {
		atomic.AddInt32(&calls, 1)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "boom"})
	})

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/boom", nil)
		req.Header.Set(middleware.HeaderIdempotencyKey, "k-fail")
		r.ServeHTTP(w, req)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("5xx must not be cached so retry can succeed; ran %d times", got)
	}
}

func TestIdempotency_4xxIsCached(t *testing.T) {
	store := newMemStore()
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.CtxKeyTenantID, "tenant-a")
		c.Next()
	})
	r.Use(middleware.Idempotency(store))
	var calls int32
	r.POST("/bad", func(c *gin.Context) {
		atomic.AddInt32(&calls, 1)
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad"})
	})

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/bad", nil)
		req.Header.Set(middleware.HeaderIdempotencyKey, "k-bad")
		r.ServeHTTP(w, req)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("deterministic 4xx should be cached; ran %d times (want 1)", got)
	}
}

// TestIdempotency_422IsCached verifies that a 422 (validation error) response is cached
// — replaying the same bad payload should return the same error without re-running.
func TestIdempotency_422IsCached(t *testing.T) {
	store := newMemStore()
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.CtxKeyTenantID, "tenant-a")
		c.Next()
	})
	r.Use(middleware.Idempotency(store))
	var calls int32
	r.POST("/validate", func(c *gin.Context) {
		atomic.AddInt32(&calls, 1)
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "validation_error", "message": "qty must be > 0"})
	})

	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/validate", nil)
		req.Header.Set(middleware.HeaderIdempotencyKey, "k-422")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("call %d: expected 422, got %d", i, w.Code)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("422 should be cached after first call; handler ran %d times (want 1)", got)
	}
}

// TestIdempotency_429NotCached verifies that 429 (rate limited) is NOT cached.
// The client must be able to retry after the rate-limit budget resets and succeed.
func TestIdempotency_429NotCached(t *testing.T) {
	store := newMemStore()
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.CtxKeyTenantID, "tenant-a")
		c.Next()
	})
	r.Use(middleware.Idempotency(store))

	var calls int32
	// Handler returns 429 for the first call, then 200 for subsequent calls —
	// this simulates a rate-limit budget resetting between retries.
	r.POST("/limited", func(c *gin.Context) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "rate_limited"})
			return
		}
		c.JSON(http.StatusCreated, gin.H{"ok": true})
	})

	// First call: should hit the handler and get 429.
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest(http.MethodPost, "/limited", nil)
	req1.Header.Set(middleware.HeaderIdempotencyKey, "k-rate")
	r.ServeHTTP(w1, req1)
	if w1.Code != http.StatusTooManyRequests {
		t.Fatalf("first call: expected 429, got %d", w1.Code)
	}

	// Second call with same key: must NOT replay the 429 from cache — handler must run again.
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodPost, "/limited", nil)
	req2.Header.Set(middleware.HeaderIdempotencyKey, "k-rate")
	r.ServeHTTP(w2, req2)
	if w2.Code == http.StatusTooManyRequests && w2.Header().Get(middleware.HeaderIdempotencyReplay) == "true" {
		t.Fatal("429 must not be served from idempotency cache — transient errors must not be replayed")
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("handler should have run twice (429 not cached); ran %d times", got)
	}
	// The second call should have reached the handler and received 201.
	if w2.Code != http.StatusCreated {
		t.Errorf("second call expected 201 after 429 was not cached, got %d", w2.Code)
	}
}

func TestIdempotency_StoreFault_DegradesOpen(t *testing.T) {
	store := newMemStore()
	store.getErr = errSentinel
	var calls int32
	r := newRouter(store, "tenant-a", &calls)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/things", nil)
	req.Header.Set(middleware.HeaderIdempotencyKey, "k1")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected handler to still run on store fault; got %d", w.Code)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("expected 1 call on degrade-open path; got %d", got)
	}
}

var errSentinel = &storeErr{}

type storeErr struct{}

func (e *storeErr) Error() string { return "store fault" }

// TestIdempotency_UUIDTenant_DedupesLikeStringTenant confirms the production
// code path: AuthMiddleware injects a uuid.UUID (not a string) into the Gin
// context. Pre-fix, the middleware silently no-op'd on every uuid tenant
// because tenant.(string) failed, so dedup never engaged in prod. This test
// must run handler exactly once for two identical requests.
func TestIdempotency_UUIDTenant_DedupesLikeStringTenant(t *testing.T) {
	store := newMemStore()
	var calls int32

	tenant, _ := uuid.Parse("11111111-1111-1111-1111-111111111111")
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.CtxKeyTenantID, tenant) // prod injects uuid.UUID
		c.Next()
	})
	r.Use(middleware.Idempotency(store))
	r.POST("/things", func(c *gin.Context) {
		atomic.AddInt32(&calls, 1)
		c.JSON(http.StatusCreated, gin.H{"ok": true})
	})

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/things", nil)
		req.Header.Set(middleware.HeaderIdempotencyKey, "uuid-key")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("call %d: expected 201, got %d", i, w.Code)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("uuid.UUID tenant should dedupe; expected 1 call, got %d", got)
	}
}

// TestIdempotency_NilUUIDTenant_PassesThrough confirms that a uuid.Nil tenant
// is treated as "no tenant" — would happen if AuthMiddleware ran but did not
// resolve a tenant. The handler must still execute (downstream returns 401)
// and must not be deduped (the cache would conflate every unauthenticated
// request).
func TestIdempotency_NilUUIDTenant_PassesThrough(t *testing.T) {
	store := newMemStore()
	var calls int32
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.CtxKeyTenantID, uuid.Nil)
		c.Next()
	})
	r.Use(middleware.Idempotency(store))
	r.POST("/things", func(c *gin.Context) {
		atomic.AddInt32(&calls, 1)
		c.JSON(http.StatusCreated, gin.H{"ok": true})
	})

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/things", nil)
		req.Header.Set(middleware.HeaderIdempotencyKey, "k-nil")
		r.ServeHTTP(w, req)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("uuid.Nil tenant should not dedupe; got %d calls", got)
	}
}
