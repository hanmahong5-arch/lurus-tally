package middleware_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// generateTestRSAKey creates an in-memory RSA key pair for tests.
func generateTestRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return key
}

// buildJWKS returns a minimal JWKS JSON doc exposing the public key with given kid.
func buildJWKS(t *testing.T, priv *rsa.PrivateKey, kid string) []byte {
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

// signToken builds a signed RS256 JWT with the given claims.
func signToken(t *testing.T, priv *rsa.PrivateKey, kid, sub, issuer string, expiresAt time.Time, extra map[string]any) string {
	t.Helper()
	tok := jwt.New()
	_ = tok.Set(jwt.SubjectKey, sub)
	_ = tok.Set(jwt.IssuerKey, issuer)
	_ = tok.Set(jwt.IssuedAtKey, time.Now())
	_ = tok.Set(jwt.ExpirationKey, expiresAt)
	for k, v := range extra {
		_ = tok.Set(k, v)
	}

	privKey, err := jwk.FromRaw(priv)
	if err != nil {
		t.Fatalf("jwk from raw priv: %v", err)
	}
	if err := privKey.Set(jwk.KeyIDKey, kid); err != nil {
		t.Fatalf("set kid on priv: %v", err)
	}

	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.RS256, privKey))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return string(signed)
}

// mockJWKSServer spins up an httptest server that returns jwksJSON on GET /.
func mockJWKSServer(t *testing.T, jwksJSON []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwksJSON)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newEngineWithAuth(t *testing.T, m gin.HandlerFunc) *gin.Engine {
	t.Helper()
	e := gin.New()
	e.Use(gin.Recovery())
	e.GET("/protected", m, func(c *gin.Context) {
		sub, _ := c.Get(middleware.CtxKeyZitadelSub)
		tid, _ := c.Get(middleware.CtxKeyTenantID)
		c.JSON(http.StatusOK, gin.H{
			"sub":       fmt.Sprintf("%v", sub),
			"tenant_id": fmt.Sprintf("%v", tid),
		})
	})
	return e
}

// TestAuthMiddleware_NoToken_Returns401 verifies that a missing Authorization header → 401.
func TestAuthMiddleware_NoToken_Returns401(t *testing.T) {
	priv := generateTestRSAKey(t)
	jwksJSON := buildJWKS(t, priv, "test-kid")
	srv := mockJWKSServer(t, jwksJSON)

	m := middleware.NewAuthMiddleware(srv.URL, "https://auth.lurus.cn", nil)
	engine := newEngineWithAuth(t, m)

	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// TestAuthMiddleware_InvalidJWT_Returns401 verifies that a tampered/invalid token → 401.
func TestAuthMiddleware_InvalidJWT_Returns401(t *testing.T) {
	priv := generateTestRSAKey(t)
	jwksJSON := buildJWKS(t, priv, "test-kid")
	srv := mockJWKSServer(t, jwksJSON)

	m := middleware.NewAuthMiddleware(srv.URL, "https://auth.lurus.cn", nil)
	engine := newEngineWithAuth(t, m)

	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer this.is.not.a.valid.jwt")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// TestAuthMiddleware_ExpiredToken_Returns401 verifies that an expired token → 401.
func TestAuthMiddleware_ExpiredToken_Returns401(t *testing.T) {
	priv := generateTestRSAKey(t)
	jwksJSON := buildJWKS(t, priv, "test-kid")
	srv := mockJWKSServer(t, jwksJSON)

	m := middleware.NewAuthMiddleware(srv.URL, "https://auth.lurus.cn", nil)
	engine := newEngineWithAuth(t, m)

	token := signToken(t, priv, "test-kid", "user-sub-123", "https://auth.lurus.cn",
		time.Now().Add(-1*time.Hour), nil)

	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// TestAuthMiddleware_ValidJWT_InjectsUserID verifies that a valid token injects sub into context.
func TestAuthMiddleware_ValidJWT_InjectsUserID(t *testing.T) {
	priv := generateTestRSAKey(t)
	jwksJSON := buildJWKS(t, priv, "test-kid")
	srv := mockJWKSServer(t, jwksJSON)

	m := middleware.NewAuthMiddleware(srv.URL, "https://auth.lurus.cn", nil)
	engine := newEngineWithAuth(t, m)

	tenantID := uuid.New().String()
	token := signToken(t, priv, "test-kid", "user-sub-abc", "https://auth.lurus.cn",
		time.Now().Add(1*time.Hour),
		map[string]any{"tally_tenant_id": tenantID})

	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: body=%s", rec.Code, rec.Body.String())
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["sub"] != "user-sub-abc" {
		t.Errorf("expected sub=user-sub-abc, got %q", body["sub"])
	}
	if body["tenant_id"] != tenantID {
		t.Errorf("expected tenant_id=%s, got %q", tenantID, body["tenant_id"])
	}
}

// TestAuthMiddleware_TenantLookupFallback_InjectsTenantID verifies that when
// the JWT lacks the tally_tenant_id custom claim, the middleware uses the
// tenantLookup callback to resolve tenant_id from sub.
func TestAuthMiddleware_TenantLookupFallback_InjectsTenantID(t *testing.T) {
	priv := generateTestRSAKey(t)
	jwksJSON := buildJWKS(t, priv, "test-kid")
	srv := mockJWKSServer(t, jwksJSON)

	expectedTenant := uuid.New()
	lookupCalledWithSub := ""
	lookup := func(_ context.Context, sub string) (uuid.UUID, error) {
		lookupCalledWithSub = sub
		return expectedTenant, nil
	}

	m := middleware.NewAuthMiddleware(srv.URL, "https://auth.lurus.cn", lookup)
	engine := newEngineWithAuth(t, m)

	// Token has NO tally_tenant_id claim — lookup must fill the gap.
	token := signToken(t, priv, "test-kid", "user-sub-abc", "https://auth.lurus.cn",
		time.Now().Add(1*time.Hour), nil)

	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: body=%s", rec.Code, rec.Body.String())
	}
	if lookupCalledWithSub != "user-sub-abc" {
		t.Errorf("expected lookup called with sub=user-sub-abc, got %q", lookupCalledWithSub)
	}
	var body map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body["tenant_id"] != expectedTenant.String() {
		t.Errorf("expected tenant_id=%s injected from lookup, got %q",
			expectedTenant, body["tenant_id"])
	}
}

// TestAuthMiddleware_TenantLookup_FirstTimeUser_NoTenantInjected verifies that
// when the lookup returns uuid.Nil (first-time user, no mapping yet), the
// request still succeeds but tenant_id stays empty so handlers can return 401.
func TestAuthMiddleware_TenantLookup_FirstTimeUser_NoTenantInjected(t *testing.T) {
	priv := generateTestRSAKey(t)
	jwksJSON := buildJWKS(t, priv, "test-kid")
	srv := mockJWKSServer(t, jwksJSON)

	lookup := func(_ context.Context, _ string) (uuid.UUID, error) {
		return uuid.Nil, nil
	}

	m := middleware.NewAuthMiddleware(srv.URL, "https://auth.lurus.cn", lookup)
	engine := newEngineWithAuth(t, m)

	token := signToken(t, priv, "test-kid", "first-time-user", "https://auth.lurus.cn",
		time.Now().Add(1*time.Hour), nil)

	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&body)
	// Test handler reads c.Get → returns "<nil>" string when missing.
	if body["tenant_id"] != "<nil>" {
		t.Errorf("expected tenant_id=<nil> for first-time user, got %q", body["tenant_id"])
	}
}

// TestAuthMiddleware_WrongIssuer_Returns401 verifies wrong issuer → 401.
func TestAuthMiddleware_WrongIssuer_Returns401(t *testing.T) {
	priv := generateTestRSAKey(t)
	jwksJSON := buildJWKS(t, priv, "test-kid")
	srv := mockJWKSServer(t, jwksJSON)

	m := middleware.NewAuthMiddleware(srv.URL, "https://auth.lurus.cn", nil)
	engine := newEngineWithAuth(t, m)

	// Token signed with correct key but wrong issuer.
	token := signToken(t, priv, "test-kid", "user-sub-xyz", "https://evil.example.com",
		time.Now().Add(1*time.Hour), nil)

	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}
