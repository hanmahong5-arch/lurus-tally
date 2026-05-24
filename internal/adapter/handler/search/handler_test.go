package search_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	handlersearch "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/search"
	appsearch "github.com/hanmahong5-arch/lurus-tally/internal/app/search"
)

func init() { gin.SetMode(gin.TestMode) }

// stubRepo is a minimal EntityRepo for handler tests.
type stubRepo struct {
	results []appsearch.EntityResult
	err     error
}

func (s *stubRepo) SearchProducts(_ context.Context, _ uuid.UUID, _ string, _ int) ([]appsearch.EntityResult, error) {
	return s.results, s.err
}
func (s *stubRepo) SearchSuppliers(_ context.Context, _ uuid.UUID, _ string, _ int) ([]appsearch.EntityResult, error) {
	return nil, s.err
}
func (s *stubRepo) SearchCustomers(_ context.Context, _ uuid.UUID, _ string, _ int) ([]appsearch.EntityResult, error) {
	return nil, s.err
}
func (s *stubRepo) SearchBills(_ context.Context, _ uuid.UUID, _ string, _ int) ([]appsearch.EntityResult, error) {
	return nil, s.err
}

var _ appsearch.EntityRepo = (*stubRepo)(nil)

func buildRouter(repo appsearch.EntityRepo) *gin.Engine {
	uc := appsearch.NewSearchEntitiesUseCase(repo)
	h := handlersearch.New(uc)
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(func(c *gin.Context) {
		// inject tenant_id into context (mirrors AuthMiddleware behaviour)
		tid := uuid.MustParse("00000000-0000-0000-0000-000000000001")
		c.Set("tenant_id", tid)
		c.Next()
	})
	h.RegisterRoutes(api)
	return r
}

func buildRouterNoTenant(repo appsearch.EntityRepo) *gin.Engine {
	uc := appsearch.NewSearchEntitiesUseCase(repo)
	h := handlersearch.New(uc)
	r := gin.New()
	api := r.Group("/api/v1")
	// No tenant middleware → uuid.Nil → 401
	h.RegisterRoutes(api)
	return r
}

// ── tests ──────────────────────────────────────────────────────────────────────

func TestSearchHandler_BlankQuery_Returns200EmptyGroups(t *testing.T) {
	r := buildRouter(&stubRepo{})
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/search?q=", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var body appsearch.SearchResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(body.Groups) != 0 {
		t.Fatalf("want 0 groups for blank q, got %d", len(body.Groups))
	}
}

func TestSearchHandler_WithQuery_Returns200WithGroups(t *testing.T) {
	repo := &stubRepo{
		results: []appsearch.EntityResult{
			{Type: appsearch.EntityProduct, ID: "prod-1", Label: "Widget", Sublabel: "W-001"},
		},
	}
	r := buildRouter(repo)
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/search?q=widget", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var body appsearch.SearchResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(body.Groups) == 0 {
		t.Fatal("expected at least one group")
	}
}

func TestSearchHandler_NoTenant_Returns401(t *testing.T) {
	r := buildRouterNoTenant(&stubRepo{})
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/search?q=test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestSearchHandler_LimitClamped(t *testing.T) {
	// limit=999 → clamped to 20; no error expected.
	r := buildRouter(&stubRepo{})
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/search?q=x&limit=999", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
}
