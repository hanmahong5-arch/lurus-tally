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
	result *domain.TenantProfile
}

func (s *stubChooseProfile) Execute(ctx context.Context, in appTenant.ChooseProfileInput) (*domain.TenantProfile, error) {
	s.called = true
	if s.err != nil {
		return nil, s.err
	}
	p, _ := domain.NewTenantProfile(in.TenantID, domain.ProfileType(in.ProfileType))
	s.result = p
	return p, nil
}

type stubGetMe struct {
	result *appTenant.GetMeOutput
	err    error
}

func (s *stubGetMe) Execute(ctx context.Context, in appTenant.GetMeInput) (*appTenant.GetMeOutput, error) {
	return s.result, s.err
}

// newTestEngine wires auth handler on a Gin engine with tenant_id pre-injected.
func newTestEngine(h *handlerAuth.Handler, tenantID uuid.UUID, sub string) *gin.Engine {
	e := gin.New()
	e.Use(func(c *gin.Context) {
		if tenantID != uuid.Nil {
			c.Set(middleware.CtxKeyTenantID, tenantID)
		}
		if sub != "" {
			c.Set(middleware.CtxKeyZitadelSub, sub)
		}
		c.Next()
	})
	h.RegisterRoutes(e.Group("/api/v1"))
	return e
}

// TestAuthHandler_GetMe_Unauthenticated_Returns401 tests that /me without context → 401.
func TestAuthHandler_GetMe_Unauthenticated_Returns401(t *testing.T) {
	stub := &stubGetMe{result: &appTenant.GetMeOutput{}}
	h := handlerAuth.New(
		&stubChooseProfile{},
		stub,
	)
	e := gin.New()
	h.RegisterRoutes(e.Group("/api/v1"))

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/me", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// TestAuthHandler_GetMe_Authenticated_Returns200 tests that authenticated /me returns 200.
func TestAuthHandler_GetMe_Authenticated_Returns200(t *testing.T) {
	tenantID := uuid.New()
	stub := &stubGetMe{
		result: &appTenant.GetMeOutput{
			UserSub:     "sub-abc",
			TenantID:    tenantID.String(),
			ProfileType: "cross_border",
		},
	}
	h := handlerAuth.New(&stubChooseProfile{}, stub)
	e := newTestEngine(h, tenantID, "sub-abc")

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/me", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestAuthHandler_ChooseProfile_ValidInput_Returns201 tests profile creation → 201.
func TestAuthHandler_ChooseProfile_ValidInput_Returns201(t *testing.T) {
	tenantID := uuid.New()
	stub := &stubChooseProfile{}
	h := handlerAuth.New(stub, &stubGetMe{result: &appTenant.GetMeOutput{}})
	e := newTestEngine(h, tenantID, "sub-xyz")

	body := map[string]string{"profile_type": "cross_border"}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/tenant/profile", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if !stub.called {
		t.Error("ChooseProfile use case was not called")
	}
}

// TestAuthHandler_ChooseProfile_NoTenantID_Returns401 tests missing tenant → 401.
func TestAuthHandler_ChooseProfile_NoTenantID_Returns401(t *testing.T) {
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

// TestAuthHandler_ChooseProfile_AlreadySet_Returns409 tests duplicate profile → 409.
func TestAuthHandler_ChooseProfile_AlreadySet_Returns409(t *testing.T) {
	tenantID := uuid.New()
	stub := &stubChooseProfile{err: domain.ErrProfileAlreadySet}
	h := handlerAuth.New(stub, &stubGetMe{result: &appTenant.GetMeOutput{}})
	e := newTestEngine(h, tenantID, "sub-duplicate")

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
