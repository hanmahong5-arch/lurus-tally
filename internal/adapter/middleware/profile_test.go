package middleware_test

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
)

// stubProfileQuerier satisfies middleware.ProfileQuerier for unit tests.
// It returns the configured profile string (or err) and records the tenant
// IDs it was called with so tests can assert the middleware looks up the
// right tenant.
type stubProfileQuerier struct {
	mu        sync.Mutex
	profile   string
	err       error
	calledFor []uuid.UUID
}

func (s *stubProfileQuerier) QueryProfileType(_ context.Context, tenantID uuid.UUID) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calledFor = append(s.calledFor, tenantID)
	return s.profile, s.err
}

// silentLogger returns a slog.Logger that drops every record so test output
// stays clean even when the middleware logs at ERROR.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

// newProfileTestEngine builds a Gin engine that injects tenant_id (via
// preMW) and then runs ProfileMiddleware. The test handler reflects whatever
// profile_type ended up in the context so assertions are simple.
func newProfileTestEngine(preMW gin.HandlerFunc, q middleware.ProfileQuerier) *gin.Engine {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	if preMW != nil {
		e.Use(preMW)
	}
	e.Use(middleware.ProfileMiddleware(q, silentLogger()))
	e.GET("/probe", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"profile_type": middleware.GetProfileType(c),
		})
	})
	return e
}

// TestProfileMiddleware_NoTenantID_Skipped verifies that when AuthMiddleware
// has not yet injected tenant_id (e.g. /me, /tenant/profile pre-onboarding),
// ProfileMiddleware MUST NOT abort — it lets the request proceed so the
// auth-aware handlers can return their own 401 / first-time response.
func TestProfileMiddleware_NoTenantID_Skipped(t *testing.T) {
	q := &stubProfileQuerier{}
	e := newProfileTestEngine(nil, q)

	req, _ := http.NewRequest(http.MethodGet, "/probe", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (request proceeds), got %d", rec.Code)
	}
	if len(q.calledFor) != 0 {
		t.Errorf("querier must not be called when tenant_id is absent, was called %d time(s)", len(q.calledFor))
	}
}

// TestProfileMiddleware_ValidTenant_InjectsProfileType verifies the happy path:
// AuthMiddleware-injected tenant_id is passed to the querier and the result
// becomes available via GetProfileType().
func TestProfileMiddleware_ValidTenant_InjectsProfileType(t *testing.T) {
	tid := uuid.New()
	pre := func(c *gin.Context) {
		c.Set(middleware.CtxKeyTenantID, tid)
		c.Next()
	}
	q := &stubProfileQuerier{profile: "cross_border"}
	e := newProfileTestEngine(pre, q)

	req, _ := http.NewRequest(http.MethodGet, "/probe", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !contains(rec.Body.String(), "cross_border") {
		t.Errorf("expected response to contain profile_type=cross_border, got %s", rec.Body.String())
	}
	if len(q.calledFor) != 1 || q.calledFor[0] != tid {
		t.Errorf("querier should be called once with %s, got %v", tid, q.calledFor)
	}
}

// TestProfileMiddleware_TenantWithoutProfile_EmptyString verifies that when
// the tenant has not yet picked a profile (sql.ErrNoRows), the middleware
// injects "" rather than aborting — handlers can detect "no profile" and
// route the user to /setup.
func TestProfileMiddleware_TenantWithoutProfile_EmptyString(t *testing.T) {
	tid := uuid.New()
	pre := func(c *gin.Context) {
		c.Set(middleware.CtxKeyTenantID, tid)
		c.Next()
	}
	q := &stubProfileQuerier{err: sql.ErrNoRows}
	e := newProfileTestEngine(pre, q)

	req, _ := http.NewRequest(http.MethodGet, "/probe", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (graceful degrade on no-rows), got %d", rec.Code)
	}
	if !contains(rec.Body.String(), `"profile_type":""`) {
		t.Errorf("expected empty profile_type in response, got %s", rec.Body.String())
	}
}

// TestProfileMiddleware_QuerierFailure_DegradesGracefully verifies that a
// transient DB error does NOT crash the request — the middleware logs and
// proceeds with empty profile so business handlers can still serve.
// (CLAUDE.md: defensive coding — degrade non-critical deps gracefully.)
func TestProfileMiddleware_QuerierFailure_DegradesGracefully(t *testing.T) {
	tid := uuid.New()
	pre := func(c *gin.Context) {
		c.Set(middleware.CtxKeyTenantID, tid)
		c.Next()
	}
	q := &stubProfileQuerier{err: errors.New("connection refused")}
	e := newProfileTestEngine(pre, q)

	req, _ := http.NewRequest(http.MethodGet, "/probe", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (graceful degrade on DB failure), got %d", rec.Code)
	}
	if !contains(rec.Body.String(), `"profile_type":""`) {
		t.Errorf("expected empty profile_type fallback, got %s", rec.Body.String())
	}
}

// TestProfileMiddleware_MalformedTenantID_500 verifies that when something
// upstream poisons the context with a non-UUID tenant_id, the middleware
// surfaces 500 rather than swallowing the bug. This is a programmer-error
// path; in practice AuthMiddleware always injects uuid.UUID.
func TestProfileMiddleware_MalformedTenantID_500(t *testing.T) {
	pre := func(c *gin.Context) {
		c.Set(middleware.CtxKeyTenantID, "not-a-uuid")
		c.Next()
	}
	q := &stubProfileQuerier{}
	e := newProfileTestEngine(pre, q)

	req, _ := http.NewRequest(http.MethodGet, "/probe", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for malformed tenant_id, got %d", rec.Code)
	}
}

// TestGetProfileType_AbsentReturnsEmpty verifies the helper degrades when no
// middleware has run.
func TestGetProfileType_AbsentReturnsEmpty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.GET("/x", func(c *gin.Context) {
		if got := middleware.GetProfileType(c); got != "" {
			t.Errorf("expected empty string when not set, got %q", got)
		}
		c.Status(http.StatusOK)
	})
	req, _ := http.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
}

// TestGetTenantID_AbsentReturnsNil verifies the helper falls back to uuid.Nil.
func TestGetTenantID_AbsentReturnsNil(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.GET("/x", func(c *gin.Context) {
		if got := middleware.GetTenantID(c); got != uuid.Nil {
			t.Errorf("expected uuid.Nil when not set, got %s", got)
		}
		c.Status(http.StatusOK)
	})
	req, _ := http.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
}

// contains is a small helper that avoids pulling in strings.Contains across
// every assertion call site.
func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && stringIndex(haystack, needle) >= 0
}

func stringIndex(haystack, needle string) int {
outer:
	for i := 0; i+len(needle) <= len(haystack); i++ {
		for j := 0; j < len(needle); j++ {
			if haystack[i+j] != needle[j] {
				continue outer
			}
		}
		return i
	}
	return -1
}
