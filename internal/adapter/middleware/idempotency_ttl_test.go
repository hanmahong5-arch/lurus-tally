package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
)

// ttlStore records the TTL the middleware hands to the store when caching a response.
type ttlStore struct {
	*memStore
	setTTL atomic.Int64
}

func (s *ttlStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	s.setTTL.Store(int64(ttl))
	return s.memStore.Set(ctx, key, value, ttl)
}

// 离线收银的回放可能隔数小时才到，幂等结果必须缓存满 24 小时，否则晚到的重放会重复记账。
func TestIdempotency_CachesSuccessFor24Hours(t *testing.T) {
	var calls int32
	store := &ttlStore{memStore: newMemStore()}
	r := newRouter(store, "tenant-a", &calls)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/things", nil)
	req.Header.Set(middleware.HeaderIdempotencyKey, "k-ttl")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
	if got := time.Duration(store.setTTL.Load()); got != 24*time.Hour {
		t.Errorf("cache TTL = %v, want 24h", got)
	}
}
