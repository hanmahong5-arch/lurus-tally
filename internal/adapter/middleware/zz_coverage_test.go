package middleware

// zz_coverage_test.go closes the coverage gap on internal/adapter/middleware
// (baseline 69.5%) without touching any existing *_test.go file. It lives in
// package middleware (not middleware_test) so it can reach unexported helpers
// (extractBearerToken, classifyStatus, parseClientIP) directly.
//
// 2026-08-07: removed the ProfileMiddleware/GetProfileType/CtxKeyProfileType
// coverage block below (was ~150 lines) — those symbols were deleted from
// production in commit 6a1b76d5 (2026-06-20, "drop dead reorder views +
// never-wired profile middleware": the type was never wired into any router,
// the real profile flow runs through ChooseProfileUseCase instead). The test
// file predated that removal and had gone stale/uncompiled since.

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/redis/go-redis/v9"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
)

func init() { gin.SetMode(gin.TestMode) }

// discardLogger returns an *slog.Logger that writes nowhere, for exercising
// error/log branches without spamming test output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ---------------------------------------------------------------------------
// extractBearerToken — technical_cases: SplitN / EqualFold edge cases.
// ---------------------------------------------------------------------------

func TestZZExtractBearerToken_TableDriven(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   string
	}{
		{"absent", "", ""},
		{"bearer_lowercase", "bearer abc123", "abc123"},
		{"bearer_titlecase", "Bearer abc123", "abc123"},
		{"bearer_uppercase", "BEARER abc123", "abc123"},
		{"wrong_scheme_basic", "Basic abc123", ""},
		{"no_space_malformed", "Bearertoken", ""},
		{"only_scheme_no_token", "Bearer", ""},
		{"multiple_spaces_trimmed", "Bearer  abc123", "abc123"},
		{"trailing_whitespace_trimmed", "Bearer abc123   ", "abc123"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)
			if tc.header != "" {
				c.Request.Header.Set("Authorization", tc.header)
			}
			got := extractBearerToken(c)
			if got != tc.want {
				t.Errorf("extractBearerToken(%q) = %q, want %q", tc.header, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// AuthMiddleware — branches not exercised by auth_test.go: PAT resolver
// generic (non-ErrInvalidPAT) error, JWKS fetch failure, tenantLookup error.
// ---------------------------------------------------------------------------

func zzGenRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return key
}

func zzBuildJWKS(t *testing.T, priv *rsa.PrivateKey, kid string) []byte {
	t.Helper()
	pub, err := jwk.FromRaw(priv.Public())
	if err != nil {
		t.Fatalf("jwk from raw: %v", err)
	}
	if err := pub.Set(jwk.KeyIDKey, kid); err != nil {
		t.Fatalf("set kid: %v", err)
	}
	if err := pub.Set(jwk.AlgorithmKey, jwa.RS256); err != nil {
		t.Fatalf("set alg: %v", err)
	}
	set := jwk.NewSet()
	if err := set.AddKey(pub); err != nil {
		t.Fatalf("add key: %v", err)
	}
	b, err := json.Marshal(set)
	if err != nil {
		t.Fatalf("marshal jwks: %v", err)
	}
	return b
}

func zzNewEngine(m gin.HandlerFunc) *gin.Engine {
	e := gin.New()
	e.GET("/protected", m, func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	return e
}

// TestZZAuth_PATResolverGenericError_Returns401 covers the branch where the
// PATResolver returns a non-ErrInvalidPAT error (e.g. a DB fault): the
// middleware must still respond 401 (not 500), but takes the "log it" path
// rather than the quiet path — business invariant: PAT failures never leak
// as 5xx to a caller.
func TestZZAuth_PATResolverGenericError_Returns401(t *testing.T) {
	resolver := func(_ context.Context, _ string) (uuid.UUID, error) {
		return uuid.Nil, errors.New("db exploded")
	}
	m := NewAuthMiddleware("http://unused.invalid/jwks", "https://identity.lurus.cn", "aud", nil, resolver)
	e := zzNewEngine(m)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer tally_pat_deadbeefdeadbeefdeadbeefdeadbeefdead")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("generic PAT resolver error: status = %d, want 401", rec.Code)
	}
}

// TestZZAuth_JWKSFetchFailure_Returns401 covers the branch where fetching the
// JWKS itself fails (provider down / 500) — must 401, not panic or 500.
func TestZZAuth_JWKSFetchFailure_Returns401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	m := NewAuthMiddleware(srv.URL, "https://identity.lurus.cn", "aud", nil, nil)
	e := zzNewEngine(m)

	// Use a syntactically JWT-shaped bearer so we reach the JWKS-fetch branch
	// (a totally empty/garbage token would still 401 via jwt.Parse, but we
	// specifically want cache.Get to be the thing that fails).
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer aaa.bbb.ccc")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("JWKS fetch failure: status = %d, want 401", rec.Code)
	}
}

// TestZZAuth_TenantLookupError_StillNext verifies that when tenantLookup
// returns an error, the middleware logs it but still calls Next() (does not
// abort the request) — tenant_id simply stays unset (uuid.Nil).
func TestZZAuth_TenantLookupError_StillNext(t *testing.T) {
	priv := zzGenRSAKey(t)
	jwksJSON := zzBuildJWKS(t, priv, "kid-1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwksJSON)
	}))
	defer srv.Close()

	lookup := func(_ context.Context, _ string) (uuid.UUID, error) {
		return uuid.Nil, errors.New("user_identity_mapping lookup failed")
	}

	m := NewAuthMiddleware(srv.URL, "https://identity.lurus.cn", "aud-x", lookup, nil)
	e := gin.New()
	var tenantSeen any
	var tenantOK bool
	e.GET("/protected", m, func(c *gin.Context) {
		tenantSeen, tenantOK = c.Get(CtxKeyTenantID)
		c.Status(http.StatusOK)
	})

	tok := jwt.New()
	_ = tok.Set(jwt.SubjectKey, "sub-err")
	_ = tok.Set(jwt.IssuerKey, "https://identity.lurus.cn")
	_ = tok.Set(jwt.ExpirationKey, time.Now().Add(time.Hour))
	_ = tok.Set("aud", "aud-x")
	privKey, _ := jwk.FromRaw(priv)
	_ = privKey.Set(jwk.KeyIDKey, "kid-1")
	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.RS256, privKey))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+string(signed))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("tenantLookup error must not abort request; status = %d", rec.Code)
	}
	if tenantOK {
		t.Errorf("tenant_id must stay unset when tenantLookup errors, got %v", tenantSeen)
	}
}

// TestZZAuth_EmailAndDisplayName_InjectedWhenPresent covers the "ok && s != ''"
// true branches for both the email and name claim injection, exercised via
// the exported GetEmail/GetDisplayName accessors.
func TestZZAuth_EmailAndDisplayName_InjectedWhenPresent(t *testing.T) {
	priv := zzGenRSAKey(t)
	jwksJSON := zzBuildJWKS(t, priv, "kid-2")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwksJSON)
	}))
	defer srv.Close()

	m := NewAuthMiddleware(srv.URL, "https://identity.lurus.cn", "aud-y", nil, nil)
	e := gin.New()
	var gotEmail, gotName string
	e.GET("/protected", m, func(c *gin.Context) {
		gotEmail = GetEmail(c)
		gotName = GetDisplayName(c)
		c.Status(http.StatusOK)
	})

	tok := jwt.New()
	_ = tok.Set(jwt.SubjectKey, "sub-with-claims")
	_ = tok.Set(jwt.IssuerKey, "https://identity.lurus.cn")
	_ = tok.Set(jwt.ExpirationKey, time.Now().Add(time.Hour))
	_ = tok.Set("aud", "aud-y")
	_ = tok.Set("email", "user@example.com")
	_ = tok.Set("name", "Jane Doe")
	privKey, _ := jwk.FromRaw(priv)
	_ = privKey.Set(jwk.KeyIDKey, "kid-2")
	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.RS256, privKey))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+string(signed))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if gotEmail != "user@example.com" {
		t.Errorf("GetEmail = %q, want user@example.com", gotEmail)
	}
	if gotName != "Jane Doe" {
		t.Errorf("GetDisplayName = %q, want Jane Doe", gotName)
	}
}

// TestZZGetEmail_GetDisplayName_WrongType_ReturnsEmpty covers the type-assert
// failure branch of both accessors (key present but not a string).
func TestZZGetEmail_GetDisplayName_WrongType_ReturnsEmpty(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)
	c.Set(CtxKeyEmail, 42)
	c.Set(CtxKeyDisplayName, 99)

	if got := GetEmail(c); got != "" {
		t.Errorf("GetEmail with non-string value = %q, want empty", got)
	}
	if got := GetDisplayName(c); got != "" {
		t.Errorf("GetDisplayName with non-string value = %q, want empty", got)
	}
}

// TestZZIdempotency_EmptyStringTenant_Skips covers the "case string: t == ''"
// branch specifically — a tenant key that IS present in context but holds an
// empty string (distinct from the key being altogether absent).
func TestZZIdempotency_EmptyStringTenant_Skips(t *testing.T) {
	idempotencySkipped.Reset()
	store := newZZIdemStore()
	var calls int
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(CtxKeyTenantID, "") // present but empty string
		c.Next()
	})
	r.Use(Idempotency(store))
	r.POST("/x", func(c *gin.Context) {
		calls++
		c.Status(http.StatusCreated)
	})

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/x", nil)
		req.Header.Set(HeaderIdempotencyKey, "k1")
		r.ServeHTTP(w, req)
	}
	if calls != 2 {
		t.Errorf("empty-string tenant must not dedupe; ran %d times, want 2", calls)
	}
}

// TestZZIdempotency_HandlerUsesWriteString_MirroredIntoBuffer covers the
// recorder.WriteString path (used by some Gin render paths instead of Write)
// to confirm the mirrored buffer still captures the body for caching.
func TestZZIdempotency_HandlerUsesWriteString_MirroredIntoBuffer(t *testing.T) {
	store := newZZIdemStore()
	tenantID := uuid.New()

	var calls int
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(CtxKeyTenantID, tenantID)
		c.Next()
	})
	r.Use(Idempotency(store))
	r.POST("/x", func(c *gin.Context) {
		calls++
		c.Status(http.StatusCreated)
		_, _ = c.Writer.WriteString("written-via-writestring")
	})

	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodPost, "/x", nil)
	req1.Header.Set(HeaderIdempotencyKey, "ws-key")
	r.ServeHTTP(w1, req1)

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/x", nil)
	req2.Header.Set(HeaderIdempotencyKey, "ws-key")
	r.ServeHTTP(w2, req2)

	if calls != 1 {
		t.Fatalf("handler should run once; ran %d times", calls)
	}
	if w2.Body.String() != "written-via-writestring" {
		t.Errorf("replayed body = %q, want the WriteString content mirrored into cache", w2.Body.String())
	}
}

// ---------------------------------------------------------------------------
// TenantDB — sqlmock-backed: success path (pin + RESET on return), set_config
// failure (503 + Close), db.Conn generic failure (503 fail-closed), and
// context.Canceled (quiet Abort, not 503).
// ---------------------------------------------------------------------------

func TestZZTenantDB_HappyPath_PinsAndResetsOnReturn(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	tenantID := uuid.New()
	mock.ExpectExec("SELECT set_config('app.tenant_id', $1, false)").
		WithArgs(tenantID.String()).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("RESET app.tenant_id").
		WillReturnResult(sqlmock.NewResult(0, 0))

	var pinnedQuerier dbscope.Querier
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(CtxKeyTenantID, tenantID)
		c.Next()
	})
	r.Use(TenantDB(db))
	r.GET("/x", func(c *gin.Context) {
		pinnedQuerier = dbscope.From(c.Request.Context(), db)
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if pinnedQuerier == nil {
		t.Fatal("expected a tenant-pinned connection injected via dbscope.With")
	}
	// The deferred RESET runs synchronously before ServeHTTP returns (Gin
	// handlers are not async), so by now the mock expectation must be met —
	// proving the connection was scrubbed of app.tenant_id before release.
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("RESET app.tenant_id was not executed before connection release: %v", err)
	}
}

// TestZZTenantDB_ResetFails_StillServesRequest covers the branch where the
// deferred RESET itself errors: the response to the CLIENT must already be
// committed (200) by the time RESET runs, so a RESET failure only produces a
// warning log and a discarded connection — it must never turn a successful
// request into an error after the fact.
func TestZZTenantDB_ResetFails_StillServesRequest(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	tenantID := uuid.New()
	mock.ExpectExec("SELECT set_config('app.tenant_id', $1, false)").
		WithArgs(tenantID.String()).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("RESET app.tenant_id").
		WillReturnError(errors.New("reset boom"))

	reached := false
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(CtxKeyTenantID, tenantID)
		c.Next()
	})
	r.Use(TenantDB(db))
	r.GET("/x", func(c *gin.Context) {
		reached = true
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	if w.Code != http.StatusOK || !reached {
		t.Fatalf("RESET failure must not affect the already-served response; status=%d reached=%v", w.Code, reached)
	}
}

func TestZZTenantDB_SetConfigFails_Returns503AndCloses(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	tenantID := uuid.New()
	mock.ExpectExec("SELECT set_config('app.tenant_id', $1, false)").
		WithArgs(tenantID.String()).
		WillReturnError(errors.New("set_config boom"))

	reached := false
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(CtxKeyTenantID, tenantID)
		c.Next()
	})
	r.Use(TenantDB(db))
	r.GET("/x", func(c *gin.Context) {
		reached = true
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("set_config failure: status = %d, want 503", w.Code)
	}
	if reached {
		t.Error("handler must not run when set_config fails (fail-closed)")
	}
}

func TestZZTenantDB_ConnGenericFailure_Returns503FailClosed(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	// Close the pool up front so db.Conn returns sql.ErrConnDone — a generic
	// (non-context.Canceled) failure that must fail CLOSED with 503, never
	// serve the request on an unscoped connection.
	_ = db.Close()

	tenantID := uuid.New()
	reached := false
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(CtxKeyTenantID, tenantID)
		c.Next()
	})
	r.Use(TenantDB(db))
	r.GET("/x", func(c *gin.Context) {
		reached = true
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("db.Conn generic failure: status = %d, want 503", w.Code)
	}
	if reached {
		t.Error("handler must not run when acquiring the pinned connection fails")
	}
}

func TestZZTenantDB_ContextCanceled_QuietAbortNot503(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	tenantID := uuid.New()
	reached := false
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(CtxKeyTenantID, tenantID)
		c.Next()
	})
	r.Use(TenantDB(db))
	r.GET("/x", func(c *gin.Context) {
		reached = true
		c.Status(http.StatusOK)
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel: database/sql returns ctx.Err() (context.Canceled)
	// before ever touching the driver.
	req := httptest.NewRequest(http.MethodGet, "/x", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if reached {
		t.Error("handler must not run when client disconnected")
	}
	// c.Abort() alone does not set a status code beyond gin's zero-value,
	// which the recorder reports as 200 — the key business invariant is that
	// it must NOT be the 503 the genuine-failure branch would emit.
	if w.Code == http.StatusServiceUnavailable {
		t.Error("client disconnect (context.Canceled) must not surface as 503")
	}
}

func TestZZTenantDB_NilTenant_NoOp(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	reached := false
	r := gin.New()
	// No tenant set at all → GetTenantID returns uuid.Nil.
	r.Use(TenantDB(db))
	r.GET("/x", func(c *gin.Context) {
		reached = true
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	if w.Code != http.StatusOK || !reached {
		t.Fatalf("uuid.Nil tenant must no-op through to handler; status=%d reached=%v", w.Code, reached)
	}
}

// ---------------------------------------------------------------------------
// Idempotency — classifyStatus table, wrong_type tenant, malformed cache
// entry fallthrough, SetNX-error degrade-open, concurrent race (409 loser),
// content-type replay.
// ---------------------------------------------------------------------------

func TestZZClassifyStatus_TableDriven(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{200, idempotencyKindOK},
		{201, idempotencyKindOK},
		{299, idempotencyKindOK},
		{300, ""}, // boundary: StatusMultipleChoices excluded from "ok"
		{302, ""}, // redirect — not cached
		{400, idempotencyKindClientError},
		{404, idempotencyKindClientError},
		{409, idempotencyKindClientError},
		{422, idempotencyKindClientError},
		{429, idempotencyKindTransient},
		{499, idempotencyKindClientError},
		{500, ""},
		{503, ""},
	}
	for _, tc := range cases {
		if got := classifyStatus(tc.status); got != tc.want {
			t.Errorf("classifyStatus(%d) = %q, want %q", tc.status, got, tc.want)
		}
	}
}

// zzIdemStore is a minimal in-memory IdempotencyStore with injectable faults,
// local to this file (distinct name from the memStore in idempotency_test.go
// which lives in package middleware_test — different package, but kept
// distinct anyway for clarity).
type zzIdemStore struct {
	mu       sync.Mutex
	data     map[string][]byte
	setNXErr error
}

func newZZIdemStore() *zzIdemStore {
	return &zzIdemStore{data: make(map[string][]byte)}
}

func (s *zzIdemStore) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[key]
	if !ok {
		return nil, ErrIdemNotFound
	}
	return v, nil
}

func (s *zzIdemStore) SetNX(_ context.Context, key string, value []byte, _ time.Duration) (bool, error) {
	if s.setNXErr != nil {
		return false, s.setNXErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.data[key]; exists {
		return false, nil
	}
	s.data[key] = value
	return true, nil
}

func (s *zzIdemStore) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
	return nil
}

func (s *zzIdemStore) Del(_ context.Context, keys ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range keys {
		delete(s.data, k)
	}
	return nil
}

// TestZZIdempotency_WrongTypeTenant_SkipsWithMetric verifies that a tenant
// context value of an unexpected type (neither uuid.UUID nor string, e.g. an
// int accidentally set upstream) is treated as "no usable tenant": the
// request passes through undeduped and the wrong_type skip metric increments.
func TestZZIdempotency_WrongTypeTenant_SkipsWithMetric(t *testing.T) {
	idempotencySkipped.Reset()
	store := newZZIdemStore()
	var calls int
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(CtxKeyTenantID, 12345) // wrong type: int
		c.Next()
	})
	r.Use(Idempotency(store))
	r.POST("/x", func(c *gin.Context) {
		calls++
		c.Status(http.StatusCreated)
	})

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/x", nil)
		req.Header.Set(HeaderIdempotencyKey, "k1")
		r.ServeHTTP(w, req)
	}
	if calls != 2 {
		t.Errorf("wrong-type tenant must not dedupe; handler ran %d times, want 2", calls)
	}
}

// TestZZIdempotency_MalformedCacheEntry_FallsThroughToReexecute verifies that
// a cache entry which fails to unmarshal (or has Status<=0) is treated as if
// it were a miss: the handler re-executes rather than replaying garbage.
func TestZZIdempotency_MalformedCacheEntry_FallsThroughToReexecute(t *testing.T) {
	store := newZZIdemStore()
	tenantID := uuid.New()
	storeKey := idempotencyKeyPrefix + tenantID.String() + ":bad-key"
	store.data[storeKey] = []byte("not valid json")

	var calls int
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(CtxKeyTenantID, tenantID)
		c.Next()
	})
	r.Use(Idempotency(store))
	r.POST("/x", func(c *gin.Context) {
		calls++
		c.JSON(http.StatusCreated, gin.H{"n": calls})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set(HeaderIdempotencyKey, "bad-key")
	r.ServeHTTP(w, req)

	if calls != 1 {
		t.Errorf("malformed cache entry must fall through to re-execute; ran %d times", calls)
	}
	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", w.Code)
	}
}

// TestZZIdempotency_SetNXError_DegradesOpen verifies that when acquiring the
// short-lived dedup lock itself errors (store fault, distinct from the Get
// fault already covered elsewhere), the middleware degrades open rather than
// blocking the request.
func TestZZIdempotency_SetNXError_DegradesOpen(t *testing.T) {
	store := newZZIdemStore()
	store.setNXErr = errors.New("lock store fault")
	tenantID := uuid.New()

	var calls int
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(CtxKeyTenantID, tenantID)
		c.Next()
	})
	r.Use(Idempotency(store))
	r.POST("/x", func(c *gin.Context) {
		calls++
		c.Status(http.StatusCreated)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set(HeaderIdempotencyKey, "k-lock-fault")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated || calls != 1 {
		t.Errorf("SetNX fault must degrade open; status=%d calls=%d", w.Code, calls)
	}
}

// TestZZIdempotency_ConcurrentRace_OneWinsOneGets409 exercises the SetNX-loser
// path under real concurrency: two goroutines race on the same (tenant, key)
// pair, both miss the Get (empty cache), and only one may acquire the lock —
// the other must receive 409 with Retry-After: 1. Run with -race.
func TestZZIdempotency_ConcurrentRace_OneWinsOneGets409(t *testing.T) {
	store := newZZIdemStore()
	tenantID := uuid.New()

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(CtxKeyTenantID, tenantID)
		c.Next()
	})
	r.Use(Idempotency(store))
	release := make(chan struct{})
	var handlerEntered int32Counter
	r.POST("/x", func(c *gin.Context) {
		handlerEntered.inc()
		<-release // hold the winner in-handler so the loser's SetNX truly races
		c.Status(http.StatusCreated)
	})

	var wg sync.WaitGroup
	codes := make([]int, 2)
	wg.Add(2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		go func(idx int) {
			defer wg.Done()
			<-start
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/x", nil)
			req.Header.Set(HeaderIdempotencyKey, "race-key")
			r.ServeHTTP(w, req)
			codes[idx] = w.Code
		}(i)
	}
	close(start)
	// Give the loser a moment to hit SetNX and get rejected, then release the
	// winner so its handler can finish.
	time.Sleep(100 * time.Millisecond)
	close(release)
	wg.Wait()

	var count201, count409 int
	for _, c := range codes {
		switch c {
		case http.StatusCreated:
			count201++
		case http.StatusConflict:
			count409++
		}
	}
	if count201 != 1 || count409 != 1 {
		t.Errorf("expected exactly one 201 and one 409, got codes=%v", codes)
	}
}

// int32Counter is a tiny race-safe counter (avoids importing sync/atomic just
// for one bump — kept local and trivial).
type int32Counter struct {
	mu sync.Mutex
	n  int
}

func (c *int32Counter) inc() {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
}

// TestZZIdempotency_ContentTypePreservedOnReplay verifies that a replayed
// response restores the original Content-Type header, not just status+body.
func TestZZIdempotency_ContentTypePreservedOnReplay(t *testing.T) {
	store := newZZIdemStore()
	tenantID := uuid.New()

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(CtxKeyTenantID, tenantID)
		c.Next()
	})
	r.Use(Idempotency(store))
	r.POST("/x", func(c *gin.Context) {
		c.Header("Content-Type", "application/vnd.custom+json")
		c.String(http.StatusCreated, `{"ok":true}`)
	})

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/x", nil)
		req.Header.Set(HeaderIdempotencyKey, "ct-key")
		r.ServeHTTP(w, req)
		if w.Header().Get("Content-Type") != "application/vnd.custom+json" {
			t.Errorf("call %d: Content-Type = %q, want application/vnd.custom+json", i, w.Header().Get("Content-Type"))
		}
	}
}

// ---------------------------------------------------------------------------
// ParseLimitQuery / ParseOffsetQuery — remaining edge branches: max<=0,
// def<=0, and non-numeric offset.
// ---------------------------------------------------------------------------

func TestZZParseLimitQuery_MaxLessEqualZero_UsesDefaultCeiling(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/?limit=999999", nil)

	got := ParseLimitQuery(c, "limit", 20, 0) // max<=0 → DefaultMaxPageLimit (500)
	if got != DefaultMaxPageLimit {
		t.Errorf("max<=0: got %d, want %d", got, DefaultMaxPageLimit)
	}
}

func TestZZParseLimitQuery_DefLessEqualZero_Uses20(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil) // no raw value → falls back to def

	got := ParseLimitQuery(c, "limit", 0, 500) // def<=0 → 20
	if got != 20 {
		t.Errorf("def<=0: got %d, want 20", got)
	}
	got2 := ParseLimitQuery(c, "limit", -5, 500)
	if got2 != 20 {
		t.Errorf("negative def: got %d, want 20", got2)
	}
}

func TestZZParseOffsetQuery_NonNumeric_DefaultsToZero(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/?offset=abc", nil)

	if got := ParseOffsetQuery(c, "offset"); got != 0 {
		t.Errorf("non-numeric offset: got %d, want 0", got)
	}
}

func TestZZGetTenantID_WrongTypeInContext_ReturnsNil(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)
	c.Set(CtxKeyTenantID, "not-a-uuid")

	if got := GetTenantID(c); got != uuid.Nil {
		t.Errorf("GetTenantID with wrong-typed context value = %v, want uuid.Nil", got)
	}
}

// ---------------------------------------------------------------------------
// SessionRecord + parseClientIP — entirely uncovered by the existing suite.
// ---------------------------------------------------------------------------

func TestZZSessionRecord_NilFn_IsInert(t *testing.T) {
	r := gin.New()
	r.Use(SessionRecord(nil))
	reached := false
	r.GET("/x", func(c *gin.Context) {
		reached = true
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	if w.Code != http.StatusOK || !reached {
		t.Fatalf("nil fn must be inert passthrough; status=%d reached=%v", w.Code, reached)
	}
}

func TestZZSessionRecord_NoTenantOrNoUser_FnNotCalled(t *testing.T) {
	cases := []struct {
		name     string
		tenantID uuid.UUID
		userID   string
	}{
		{"no_tenant", uuid.Nil, "user-1"},
		{"no_user", uuid.New(), ""},
		{"neither", uuid.Nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			fn := func(_ context.Context, _ uuid.UUID, _, _ string, _ net.IP) error {
				called = true
				return nil
			}
			r := gin.New()
			r.Use(func(c *gin.Context) {
				if tc.tenantID != uuid.Nil {
					c.Set(CtxKeyTenantID, tc.tenantID)
				}
				if tc.userID != "" {
					c.Set(CtxKeyIDPSubject, tc.userID)
				}
				c.Next()
			})
			r.Use(SessionRecord(fn))
			r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

			if called {
				t.Errorf("%s: fn must not be called without both tenant and user", tc.name)
			}
		})
	}
}

func TestZZSessionRecord_TenantAndUserPresent_FnCalledWithParsedIP(t *testing.T) {
	tenantID := uuid.New()
	var gotTenant uuid.UUID
	var gotUser, gotUA string
	var gotIP net.IP
	fn := func(_ context.Context, tid uuid.UUID, userID, ua string, ip net.IP) error {
		gotTenant, gotUser, gotUA, gotIP = tid, userID, ua, ip
		return errors.New("best-effort error must be swallowed")
	}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(CtxKeyTenantID, tenantID)
		c.Set(CtxKeyIDPSubject, "user-42")
		c.Next()
	})
	r.Use(SessionRecord(fn))
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("User-Agent", "test-agent/1.0")
	req.RemoteAddr = "203.0.113.5:12345"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("fn error must not fail the request; status = %d", w.Code)
	}
	if gotTenant != tenantID || gotUser != "user-42" || gotUA != "test-agent/1.0" {
		t.Errorf("fn called with tenant=%v user=%q ua=%q, want %v user-42 test-agent/1.0", gotTenant, gotUser, gotUA, tenantID)
	}
	if gotIP == nil || gotIP.String() != "203.0.113.5" {
		t.Errorf("parsed IP = %v, want 203.0.113.5", gotIP)
	}
}

func TestZZParseClientIP_TableDriven(t *testing.T) {
	cases := []struct {
		name       string
		xff        string
		remoteAddr string
		want       string // "" means nil expected
	}{
		{"xff_single", "198.51.100.7", "10.0.0.1:9999", "198.51.100.7"},
		{"xff_list_takes_first", "198.51.100.7, 10.0.0.2", "10.0.0.1:9999", "198.51.100.7"},
		{"xff_malformed_falls_back_to_remote_addr", "not-an-ip", "203.0.113.9:8080", "203.0.113.9"},
		{"no_xff_uses_remote_addr", "", "203.0.113.10:8080", "203.0.113.10"},
		{"remote_addr_no_port_returns_nil", "", "not-a-valid-remoteaddr", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)
			if tc.xff != "" {
				c.Request.Header.Set("X-Forwarded-For", tc.xff)
			}
			c.Request.RemoteAddr = tc.remoteAddr

			got := parseClientIP(c)
			if tc.want == "" {
				if got != nil {
					t.Errorf("%s: got %v, want nil", tc.name, got)
				}
				return
			}
			if got == nil || got.String() != tc.want {
				t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// IdempotencyRedisStore — miniredis-backed, covers the redis.Nil→ErrIdemNotFound
// translation and pass-through of a genuine transport fault.
// ---------------------------------------------------------------------------

func zzNewMiniredisStore(t *testing.T) (*IdempotencyRedisStore, *miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return NewIdempotencyRedisStore(rdb), mr, rdb
}

func TestZZIdempotencyRedisStore_Get_MissTranslatesToErrIdemNotFound(t *testing.T) {
	store, _, _ := zzNewMiniredisStore(t)
	_, err := store.Get(context.Background(), "missing-key")
	if !errors.Is(err, ErrIdemNotFound) {
		t.Errorf("Get on miss: err = %v, want ErrIdemNotFound", err)
	}
}

func TestZZIdempotencyRedisStore_SetGetDel_RoundTrip(t *testing.T) {
	store, _, _ := zzNewMiniredisStore(t)
	ctx := context.Background()

	if err := store.Set(ctx, "k1", []byte("payload"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := store.Get(ctx, "k1")
	if err != nil {
		t.Fatalf("Get after Set: %v", err)
	}
	if string(got) != "payload" {
		t.Errorf("Get = %q, want payload", got)
	}

	if err := store.Del(ctx, "k1"); err != nil {
		t.Fatalf("Del: %v", err)
	}
	if _, err := store.Get(ctx, "k1"); !errors.Is(err, ErrIdemNotFound) {
		t.Errorf("Get after Del: err = %v, want ErrIdemNotFound", err)
	}
}

func TestZZIdempotencyRedisStore_SetNX_FirstTrueSecondFalse(t *testing.T) {
	store, _, _ := zzNewMiniredisStore(t)
	ctx := context.Background()

	first, err := store.SetNX(ctx, "lock1", []byte("1"), time.Minute)
	if err != nil || !first {
		t.Fatalf("first SetNX: acquired=%v err=%v, want true nil", first, err)
	}
	second, err := store.SetNX(ctx, "lock1", []byte("1"), time.Minute)
	if err != nil || second {
		t.Fatalf("second SetNX on existing key: acquired=%v err=%v, want false nil", second, err)
	}
}

func TestZZIdempotencyRedisStore_Get_TransportFault_NotTranslated(t *testing.T) {
	store, mr, _ := zzNewMiniredisStore(t)
	mr.Close() // kill the backing server: subsequent calls hit a genuine transport error

	_, err := store.Get(context.Background(), "any-key")
	if err == nil {
		t.Fatal("expected a transport error after the backing redis died")
	}
	if errors.Is(err, ErrIdemNotFound) {
		t.Error("a genuine transport fault must NOT be reported as ErrIdemNotFound")
	}
}

// ---------------------------------------------------------------------------
// RequestMetrics — the "unmatched route" synthetic label (404/no FullPath).
// ---------------------------------------------------------------------------

func TestZZRequestMetrics_UnmatchedRoute_UsesUnmatchedLabel(t *testing.T) {
	httpRequests.Reset()
	r := gin.New()
	r.Use(RequestMetrics())
	// No routes registered — every request is a 404 with empty FullPath().

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/does-not-exist", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	got := testutil.ToFloat64(httpRequests.WithLabelValues("unmatched", "GET", "404"))
	if got != 1 {
		t.Errorf("tally_http_requests_total{unmatched GET 404} = %v, want 1", got)
	}
}

// ---------------------------------------------------------------------------
// classifyStatus / idempotency kind constants sanity (guards against a typo
// silently changing cache semantics).
// ---------------------------------------------------------------------------

func TestZZIdempotencyKindConstants_AreDistinct(t *testing.T) {
	kinds := map[string]bool{
		idempotencyKindOK:          true,
		idempotencyKindClientError: true,
		idempotencyKindTransient:   true,
	}
	if len(kinds) != 3 {
		t.Fatalf("expected 3 distinct idempotency kind constants, got %d", len(kinds))
	}
}
