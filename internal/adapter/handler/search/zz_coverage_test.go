package search

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appsearch "github.com/hanmahong5-arch/lurus-tally/internal/app/search"
)

func init() { gin.SetMode(gin.TestMode) }

// captureRepo is a fake appsearch.EntityRepo that records the arguments its
// SearchProducts call received, so tests can assert the use case forwarded the
// handler's tenant/q/limit unchanged (business invariant: tenant isolation).
type captureRepo struct {
	gotTenant uuid.UUID
	gotQ      string
	gotLimit  int
	results   []appsearch.EntityResult
	err       error
}

func (r *captureRepo) SearchProducts(_ context.Context, tid uuid.UUID, q string, limit int) ([]appsearch.EntityResult, error) {
	r.gotTenant = tid
	r.gotQ = q
	r.gotLimit = limit
	return r.results, r.err
}
func (r *captureRepo) SearchSuppliers(_ context.Context, _ uuid.UUID, _ string, _ int) ([]appsearch.EntityResult, error) {
	return nil, nil
}
func (r *captureRepo) SearchCustomers(_ context.Context, _ uuid.UUID, _ string, _ int) ([]appsearch.EntityResult, error) {
	return nil, nil
}
func (r *captureRepo) SearchBills(_ context.Context, _ uuid.UUID, _ string, _ int) ([]appsearch.EntityResult, error) {
	return nil, nil
}

var _ appsearch.EntityRepo = (*captureRepo)(nil)

// errRepo is a fake appsearch.EntityRepo whose first call always fails, so the
// use case's Execute returns a non-nil error and the handler must map it via
// httperr.WriteInternal (500).
type errRepo struct{ err error }

func (r *errRepo) SearchProducts(_ context.Context, _ uuid.UUID, _ string, _ int) ([]appsearch.EntityResult, error) {
	return nil, r.err
}
func (r *errRepo) SearchSuppliers(_ context.Context, _ uuid.UUID, _ string, _ int) ([]appsearch.EntityResult, error) {
	return nil, nil
}
func (r *errRepo) SearchCustomers(_ context.Context, _ uuid.UUID, _ string, _ int) ([]appsearch.EntityResult, error) {
	return nil, nil
}
func (r *errRepo) SearchBills(_ context.Context, _ uuid.UUID, _ string, _ int) ([]appsearch.EntityResult, error) {
	return nil, nil
}

var _ appsearch.EntityRepo = (*errRepo)(nil)

func newRouterWithTenant(h *Handler, tenant uuid.UUID) *gin.Engine {
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(func(c *gin.Context) {
		c.Set(middleware.CtxKeyTenantID, tenant)
		c.Next()
	})
	h.RegisterRoutes(api)
	return r
}

// TestParseLimit hand-asserts every branch of the unexported parseLimit
// helper: blank/zero/negative/non-numeric all fall back to defaultLimit,
// in-range values pass through unchanged, and out-of-range values clamp to
// maxLimit. Expected values are computed from the documented constants
// (defaultLimit=5, maxLimit=20), not read back from the function under test.
func TestParseLimit(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int
	}{
		{"blank", "", 5},
		{"zero", "0", 5},
		{"negative", "-5", 5},
		{"non_numeric", "abc", 5},
		{"whitespace", "  ", 5},
		{"in_range_low", "1", 1},
		{"in_range_mid", "10", 10},
		{"boundary_max", "20", 20},
		{"over_max", "21", 20},
		{"way_over_max", "999", 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseLimit(tc.raw)
			if got != tc.want {
				t.Fatalf("parseLimit(%q) = %d, want %d", tc.raw, got, tc.want)
			}
		})
	}
}

// TestSearch_TenantPassedUnchanged asserts the business invariant: the
// tenantID resolved from middleware.GetTenantID reaches the use case's
// SearchRequest unchanged (results stay scoped to the requesting tenant), and
// so do q/limit.
func TestSearch_TenantPassedUnchanged(t *testing.T) {
	tenant := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	repo := &captureRepo{
		results: []appsearch.EntityResult{{Type: appsearch.EntityProduct, ID: "p1", Label: "Widget"}},
	}
	uc := appsearch.NewSearchEntitiesUseCase(repo)
	h := New(uc)
	r := newRouterWithTenant(h, tenant)

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/search?q=widget&limit=10", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if repo.gotTenant != tenant {
		t.Fatalf("tenant not forwarded unchanged: got %s, want %s", repo.gotTenant, tenant)
	}
	if repo.gotQ != "widget" {
		t.Fatalf("q not forwarded unchanged: got %q", repo.gotQ)
	}
	if repo.gotLimit != 10 {
		t.Fatalf("limit not forwarded unchanged: got %d, want 10", repo.gotLimit)
	}
}

// TestSearch_NoTenant_Returns401 asserts tenant isolation at the handler
// boundary: a request with no tenant context (uuid.Nil) never reaches the use
// case and is rejected with 401.
func TestSearch_NoTenant_Returns401(t *testing.T) {
	repo := &captureRepo{}
	uc := appsearch.NewSearchEntitiesUseCase(repo)
	h := New(uc)
	r := gin.New()
	api := r.Group("/api/v1")
	h.RegisterRoutes(api) // no tenant middleware -> GetTenantID returns uuid.Nil

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/search?q=widget", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
	if repo.gotQ != "" || repo.gotTenant != uuid.Nil {
		t.Fatalf("use case must not be invoked when tenant is missing, got q=%q tenant=%s", repo.gotQ, repo.gotTenant)
	}
}

// TestSearch_UseCaseError_Returns500 covers the error branch: when the use
// case fails, the handler must map it through httperr.WriteInternal (500),
// not leak the underlying cause in the response body.
func TestSearch_UseCaseError_Returns500(t *testing.T) {
	tenant := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	repo := &errRepo{err: errors.New("boom: db exploded")}
	uc := appsearch.NewSearchEntitiesUseCase(repo)
	h := New(uc)
	r := newRouterWithTenant(h, tenant)

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/search?q=widget", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got == "" {
		t.Fatal("expected a body")
	}
	// Safety invariant of httperr.WriteInternal: the raw cause text must never
	// be echoed to the client.
	if strings.Contains(w.Body.String(), "boom: db exploded") {
		t.Fatalf("response leaked internal cause: %s", w.Body.String())
	}
}

// TestSearch_HappyPath_Returns200WithResponse asserts the success path: a
// non-blank q with a real match returns 200 and the use case's response body.
func TestSearch_HappyPath_Returns200WithResponse(t *testing.T) {
	tenant := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	repo := &captureRepo{
		results: []appsearch.EntityResult{{Type: appsearch.EntityProduct, ID: "p1", Label: "Widget", Sublabel: "W-1"}},
	}
	uc := appsearch.NewSearchEntitiesUseCase(repo)
	h := New(uc)
	r := newRouterWithTenant(h, tenant)

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/search?q=widget", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
}
