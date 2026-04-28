package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	handlerAuth "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/auth"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appTenant "github.com/hanmahong5-arch/lurus-tally/internal/app/tenant"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/tenant"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// --- stubs ---

type stubChooseProfile struct {
	called bool
	err    error
	in     appTenant.ChooseProfileInput
}

func (s *stubChooseProfile) Execute(_ context.Context, in appTenant.ChooseProfileInput) (*domain.TenantProfile, error) {
	s.called = true
	s.in = in
	if s.err != nil {
		return nil, s.err
	}
	// Use a deterministic tenant ID for test assertions; use case derives this.
	p, _ := domain.NewTenantProfile(uuid.New(), domain.ProfileType(in.ProfileType))
	return p, nil
}

type stubGetMe struct {
	result *appTenant.GetMeOutput
	err    error
}

func (s *stubGetMe) Execute(_ context.Context, _ appTenant.GetMeInput) (*appTenant.GetMeOutput, error) {
	return s.result, s.err
}

// newTestEngine wires auth handler on a Gin engine with sub/email/name pre-injected.
func newTestEngine(h *handlerAuth.Handler, sub, email, name string) *gin.Engine {
	e := gin.New()
	e.Use(func(c *gin.Context) {
		if sub != "" {
			c.Set(middleware.CtxKeyZitadelSub, sub)
		}
		if email != "" {
			c.Set(middleware.CtxKeyEmail, email)
		}
		if name != "" {
			c.Set(middleware.CtxKeyDisplayName, name)
		}
		c.Next()
	})
	h.RegisterRoutes(e.Group("/api/v1"))
	return e
}

// TestAuthHandler_GetMe_Unauthenticated_Returns401 verifies /me without sub → 401.
func TestAuthHandler_GetMe_Unauthenticated_Returns401(t *testing.T) {
	h := handlerAuth.New(&stubChooseProfile{}, &stubGetMe{result: &appTenant.GetMeOutput{}})
	e := gin.New()
	h.RegisterRoutes(e.Group("/api/v1"))

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/me", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// TestAuthHandler_GetMe_Authenticated_Returns200 verifies authenticated /me → 200.
func TestAuthHandler_GetMe_Authenticated_Returns200(t *testing.T) {
	stub := &stubGetMe{
		result: &appTenant.GetMeOutput{
			UserSub:     "sub-abc",
			TenantID:    uuid.New().String(),
			ProfileType: "cross_border",
			IsFirstTime: false,
		},
	}
	h := handlerAuth.New(&stubChooseProfile{}, stub)
	e := newTestEngine(h, "sub-abc", "alice@x.com", "Alice")

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/me", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestAuthHandler_GetMe_FirstTimeUser_Returns200WithFlag verifies first-time
// users get IsFirstTime=true in the response so frontend redirects to /setup.
func TestAuthHandler_GetMe_FirstTimeUser_Returns200WithFlag(t *testing.T) {
	stub := &stubGetMe{
		result: &appTenant.GetMeOutput{
			UserSub:     "sub-new",
			IsFirstTime: true,
		},
	}
	h := handlerAuth.New(&stubChooseProfile{}, stub)
	e := newTestEngine(h, "sub-new", "", "")

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/me", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["is_first_time"] != true {
		t.Errorf("expected is_first_time=true, got %v", body["is_first_time"])
	}
}

// TestAuthHandler_ChooseProfile_ValidInput_Returns200 verifies bootstrap → 200
// with the profile body. Sub/email/name come from middleware-injected context.
func TestAuthHandler_ChooseProfile_ValidInput_Returns200(t *testing.T) {
	stub := &stubChooseProfile{}
	h := handlerAuth.New(stub, &stubGetMe{result: &appTenant.GetMeOutput{}})
	e := newTestEngine(h, "sub-xyz", "carol@x.com", "Carol")

	body := map[string]string{"profile_type": "cross_border"}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/tenant/profile", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !stub.called {
		t.Error("ChooseProfile use case was not called")
	}
	if stub.in.ZitadelSub != "sub-xyz" || stub.in.Email != "carol@x.com" || stub.in.DisplayName != "Carol" {
		t.Errorf("input not propagated correctly from context: %+v", stub.in)
	}
}

// TestAuthHandler_ChooseProfile_NoSub_Returns401 verifies missing auth → 401.
func TestAuthHandler_ChooseProfile_NoSub_Returns401(t *testing.T) {
	h := handlerAuth.New(&stubChooseProfile{}, &stubGetMe{result: &appTenant.GetMeOutput{}})
	e := gin.New()
	h.RegisterRoutes(e.Group("/api/v1"))

	body := map[string]string{"profile_type": "retail"}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/tenant/profile", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// TestAuthHandler_ChooseProfile_AlreadySet_Returns409 verifies conflict mapping.
func TestAuthHandler_ChooseProfile_AlreadySet_Returns409(t *testing.T) {
	stub := &stubChooseProfile{err: domain.ErrProfileAlreadySet}
	h := handlerAuth.New(stub, &stubGetMe{result: &appTenant.GetMeOutput{}})
	e := newTestEngine(h, "sub-dup", "", "")

	body := map[string]string{"profile_type": "retail"}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/tenant/profile", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestAuthHandler_ChooseProfile_InvalidType_Returns400 verifies validation.
func TestAuthHandler_ChooseProfile_InvalidType_Returns400(t *testing.T) {
	stub := &stubChooseProfile{err: domain.ErrInvalidProfileType}
	h := handlerAuth.New(stub, &stubGetMe{result: &appTenant.GetMeOutput{}})
	e := newTestEngine(h, "sub-bad", "", "")

	body := map[string]string{"profile_type": "garbage"}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/tenant/profile", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestAuthHandler_Logout_Returns200 verifies logout stub.
func TestAuthHandler_Logout_Returns200(t *testing.T) {
	h := handlerAuth.New(&stubChooseProfile{}, &stubGetMe{result: &appTenant.GetMeOutput{}})
	e := newTestEngine(h, "sub-out", "", "")

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}
