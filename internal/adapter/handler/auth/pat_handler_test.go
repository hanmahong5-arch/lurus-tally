package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	handlerAuth "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/auth"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appauth "github.com/hanmahong5-arch/lurus-tally/internal/app/auth"
	domainauth "github.com/hanmahong5-arch/lurus-tally/internal/domain/auth"
)

// --- stub Repository --------------------------------------------------------

type stubRepo struct {
	createErr  error
	created    *domainauth.PAT
	listResult []*domainauth.PAT
	listErr    error
	revokeErr  error
	revokedID  uuid.UUID
	revokedTen uuid.UUID
}

func (s *stubRepo) Create(_ context.Context, p *domainauth.PAT) error {
	if s.createErr != nil {
		return s.createErr
	}
	s.created = p
	return nil
}
func (s *stubRepo) GetByPrefix(_ context.Context, _ string) (*domainauth.PAT, error) {
	return nil, appauth.ErrNotFound
}
func (s *stubRepo) ListByTenant(_ context.Context, _ uuid.UUID) ([]*domainauth.PAT, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.listResult, nil
}
func (s *stubRepo) Revoke(_ context.Context, tenantID, id uuid.UUID) error {
	s.revokedTen = tenantID
	s.revokedID = id
	return s.revokeErr
}
func (s *stubRepo) TouchLastUsed(_ context.Context, _ uuid.UUID) error { return nil }

// --- helpers ----------------------------------------------------------------

func newPATEngine(repo appauth.Repository) *gin.Engine {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(func(c *gin.Context) {
		if raw := c.GetHeader("X-Tenant-ID"); raw != "" {
			if id, err := uuid.Parse(raw); err == nil {
				c.Set(middleware.CtxKeyTenantID, id)
			}
		}
		c.Next()
	})
	api := e.Group("/api/v1")
	handlerAuth.NewPATHandler(repo).RegisterRoutes(api)
	return e
}

const testTenantHeader = "00000000-0000-0000-0000-000000000abc"

// --- Create -----------------------------------------------------------------

func TestPATHandler_Create_HappyPath(t *testing.T) {
	repo := &stubRepo{}
	r := newPATEngine(repo)

	body, _ := json.Marshal(map[string]any{"name": "tally-mcp-laptop"})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/pats", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantHeader)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID    uuid.UUID `json:"id"`
		Name  string    `json:"name"`
		Token string    `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.HasPrefix(resp.Token, domainauth.Scheme) {
		t.Errorf("token = %q, want prefix %q", resp.Token, domainauth.Scheme)
	}
	if len(resp.Token) != len(domainauth.Scheme)+domainauth.PrefixLen+domainauth.SecretLen {
		t.Errorf("token len = %d, want %d", len(resp.Token), len(domainauth.Scheme)+domainauth.PrefixLen+domainauth.SecretLen)
	}
	if repo.created == nil {
		t.Fatalf("repo.Create never called")
	}
	if repo.created.Name != "tally-mcp-laptop" {
		t.Errorf("name = %q, want tally-mcp-laptop", repo.created.Name)
	}
	if repo.created.Hash == "" || strings.Contains(rec.Body.String(), repo.created.Hash) {
		t.Errorf("hash leaked into response or unset: hash=%q body=%s", repo.created.Hash, rec.Body.String())
	}
}

func TestPATHandler_Create_NoTenant_Returns401(t *testing.T) {
	r := newPATEngine(&stubRepo{})
	body, _ := json.Marshal(map[string]any{"name": "x"})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/pats", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestPATHandler_Create_EmptyName_Returns400(t *testing.T) {
	r := newPATEngine(&stubRepo{})
	body, _ := json.Marshal(map[string]any{"name": "  "})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/pats", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantHeader)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestPATHandler_Create_LimitExceeded_Returns409(t *testing.T) {
	// 20 active tokens exist → 21st rejected.
	full := make([]*domainauth.PAT, 20)
	for i := range full {
		full[i] = &domainauth.PAT{ID: uuid.New()}
	}
	repo := &stubRepo{listResult: full}
	r := newPATEngine(repo)

	body, _ := json.Marshal(map[string]any{"name": "n"})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/pats", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantHeader)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
	if repo.created != nil {
		t.Errorf("Create called despite limit")
	}
}

// --- List -------------------------------------------------------------------

func TestPATHandler_List_OmitsHash(t *testing.T) {
	hash := strings.Repeat("a", 64)
	repo := &stubRepo{listResult: []*domainauth.PAT{
		{ID: uuid.New(), Name: "one", Prefix: "abcd1234", Hash: hash},
	}}
	r := newPATEngine(repo)

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/auth/pats", nil)
	req.Header.Set("X-Tenant-ID", testTenantHeader)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), hash) {
		t.Errorf("hash leaked into list response: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "abcd1234") {
		t.Errorf("prefix missing from list response: %s", rec.Body.String())
	}
}

// --- Revoke -----------------------------------------------------------------

func TestPATHandler_Revoke_HappyPath(t *testing.T) {
	repo := &stubRepo{}
	r := newPATEngine(repo)

	id := uuid.New()
	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/auth/pats/%s", id), nil)
	req.Header.Set("X-Tenant-ID", testTenantHeader)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
	if repo.revokedID != id {
		t.Errorf("revoked id = %s, want %s", repo.revokedID, id)
	}
}

func TestPATHandler_Revoke_NotFound_StillReturns204(t *testing.T) {
	// 204 even when the row doesn't exist — avoid leaking which IDs are valid.
	repo := &stubRepo{revokeErr: appauth.ErrNotFound}
	r := newPATEngine(repo)

	req, _ := http.NewRequest(http.MethodDelete, "/api/v1/auth/pats/"+uuid.NewString(), nil)
	req.Header.Set("X-Tenant-ID", testTenantHeader)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
}

func TestPATHandler_Revoke_BadID_Returns400(t *testing.T) {
	r := newPATEngine(&stubRepo{})
	req, _ := http.NewRequest(http.MethodDelete, "/api/v1/auth/pats/not-a-uuid", nil)
	req.Header.Set("X-Tenant-ID", testTenantHeader)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
