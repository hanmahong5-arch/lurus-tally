package product

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	repoproduct "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/product"
	appproduct "github.com/hanmahong5-arch/lurus-tally/internal/app/product"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/product"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ---- fake repository (in-memory, satisfies appproduct.Repository) ----

type covFakeRepo struct {
	products map[uuid.UUID]*domain.Product

	// per-call error overrides; when set, the corresponding method returns it
	// unconditionally instead of consulting the map.
	getErr     error
	listErr    error
	updateErr  error
	deleteErr  error
	restoreErr error

	lastListFilter domain.ListFilter
}

func newCovFakeRepo() *covFakeRepo {
	return &covFakeRepo{products: make(map[uuid.UUID]*domain.Product)}
}

func (f *covFakeRepo) Create(_ context.Context, p *domain.Product) error {
	f.products[p.ID] = p
	return nil
}

func (f *covFakeRepo) GetByID(_ context.Context, tenantID, id uuid.UUID) (*domain.Product, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	p, ok := f.products[id]
	if !ok || p.TenantID != tenantID {
		return nil, repoproduct.ErrNotFound
	}
	return p, nil
}

func (f *covFakeRepo) List(_ context.Context, filter domain.ListFilter) ([]*domain.Product, int, error) {
	f.lastListFilter = filter
	if f.listErr != nil {
		return nil, 0, f.listErr
	}
	var items []*domain.Product
	for _, p := range f.products {
		if p.TenantID == filter.TenantID {
			items = append(items, p)
		}
	}
	return items, len(items), nil
}

func (f *covFakeRepo) Update(_ context.Context, p *domain.Product) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.products[p.ID] = p
	return nil
}

func (f *covFakeRepo) Delete(_ context.Context, tenantID, id uuid.UUID) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	p, ok := f.products[id]
	if !ok || p.TenantID != tenantID {
		return repoproduct.ErrNotFound
	}
	delete(f.products, id)
	return nil
}

func (f *covFakeRepo) Restore(_ context.Context, tenantID, id uuid.UUID) (*domain.Product, error) {
	if f.restoreErr != nil {
		return nil, f.restoreErr
	}
	p, ok := f.products[id]
	if !ok || p.TenantID != tenantID {
		return nil, repoproduct.ErrNotFound
	}
	return p, nil
}

var _ appproduct.Repository = (*covFakeRepo)(nil)

// ---- wiring helpers ----

func newCovHandler(repo *covFakeRepo) *Handler {
	return New(
		appproduct.NewCreateUseCase(repo),
		appproduct.NewListUseCase(repo),
		appproduct.NewGetUseCase(repo),
		appproduct.NewUpdateUseCase(repo),
		appproduct.NewDeleteUseCase(repo),
		appproduct.NewRestoreUseCase(repo),
	)
}

// newCovRouter wires the handler behind a minimal middleware that sets the
// tenant context key from a test-only header, mirroring how AuthMiddleware
// would inject middleware.CtxKeyTenantID in production. When the header is
// absent, no key is set, so resolveTenantID (handler.go:311-316) falls
// through to uuid.Nil exactly as it would for an unauthenticated request.
func newCovRouter(h *Handler) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(func(c *gin.Context) {
		if raw := c.GetHeader("X-Cov-Tenant"); raw != "" {
			if id, err := uuid.Parse(raw); err == nil {
				c.Set(middleware.CtxKeyTenantID, id)
			}
		}
		c.Next()
	})
	api := r.Group("/api/v1")
	api.POST("/products", h.Create)
	api.GET("/products", h.List)
	api.GET("/products/:id", h.GetByID)
	api.PUT("/products/:id", h.Update)
	api.DELETE("/products/:id", h.Delete)
	api.POST("/products/:id/restore", h.Restore)
	return r
}

const covTenantStr = "22222222-2222-2222-2222-222222222222"

var covTenant = uuid.MustParse(covTenantStr)

func covSeed(repo *covFakeRepo, tenantID uuid.UUID) *domain.Product {
	id := uuid.New()
	p := &domain.Product{
		ID:                  id,
		TenantID:            tenantID,
		Code:                "COV-001",
		Name:                "Coverage Widget",
		MeasurementStrategy: domain.StrategyIndividual,
		Enabled:             true,
		Attributes:          json.RawMessage("{}"),
	}
	repo.products[id] = p
	return p
}

func covDo(r *gin.Engine, method, path, tenant, body string) *httptest.ResponseRecorder {
	var reqBody *bytes.Reader
	if body != "" {
		reqBody = bytes.NewReader([]byte(body))
	} else {
		reqBody = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	if tenant != "" {
		req.Header.Set("X-Cov-Tenant", tenant)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ---- Create ----

func TestHandlerCreate_Cov(t *testing.T) {
	cases := []struct {
		name       string
		tenant     string
		body       string
		wantStatus int
		wantBody   string // substring expected in response body
	}{
		{
			name:       "no tenant -> 401 cross-tenant gate",
			tenant:     "",
			body:       `{"code":"C1","name":"N1"}`,
			wantStatus: http.StatusUnauthorized,
			wantBody:   "tenant_id required",
		},
		{
			name:       "malformed JSON -> 400 invalid request body",
			tenant:     covTenantStr,
			body:       `{not-json`,
			wantStatus: http.StatusBadRequest,
			wantBody:   "invalid request body",
		},
		{
			name:       "field exceeds binding max=128 -> 400",
			tenant:     covTenantStr,
			body:       `{"code":"` + strings.Repeat("a", 129) + `","name":"N"}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   "invalid request body",
		},
		{
			name:       "use-case validation error -> 400 with err.Error(), not 500",
			tenant:     covTenantStr,
			body:       `{"code":"","name":"N"}`, // domain: code is required
			wantStatus: http.StatusBadRequest,
			wantBody:   "code is required",
		},
		{
			name:       "success -> 201",
			tenant:     covTenantStr,
			body:       `{"code":"C-OK","name":"OK Product"}`,
			wantStatus: http.StatusCreated,
			wantBody:   `"code":"C-OK"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newCovFakeRepo()
			h := newCovHandler(repo)
			r := newCovRouter(h)

			w := covDo(r, http.MethodPost, "/api/v1/products", tc.tenant, tc.body)

			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tc.wantStatus, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), tc.wantBody) {
				t.Errorf("body = %s, want substring %q", w.Body.String(), tc.wantBody)
			}
		})
	}
}

// ---- List ----

func TestHandlerList_Cov(t *testing.T) {
	t.Run("no tenant -> 401", func(t *testing.T) {
		repo := newCovFakeRepo()
		h := newCovHandler(repo)
		r := newCovRouter(h)

		w := covDo(r, http.MethodGet, "/api/v1/products", "", "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401; body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("use-case error -> 500 via httperr, generic body", func(t *testing.T) {
		repo := newCovFakeRepo()
		repo.listErr = errors.New("boom: pg connection reset")
		h := newCovHandler(repo)
		r := newCovRouter(h)

		w := covDo(r, http.MethodGet, "/api/v1/products", covTenantStr, "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500; body=%s", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "boom") {
			t.Errorf("5xx body leaked internal cause: %s", w.Body.String())
		}
	})

	t.Run("attributes_filter query param passed through to ListFilter", func(t *testing.T) {
		repo := newCovFakeRepo()
		covSeed(repo, covTenant)
		h := newCovHandler(repo)
		r := newCovRouter(h)

		w := covDo(r, http.MethodGet, `/api/v1/products?attributes_filter={"hs_code":"1234"}`, covTenantStr, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(string(repo.lastListFilter.AttributesFilter), "hs_code") {
			t.Errorf("filter.AttributesFilter = %s, want it to carry the query value", repo.lastListFilter.AttributesFilter)
		}
	})

	enabledCases := []struct {
		name        string
		enabledStr  string // query value; "" means param omitted entirely
		wantEnabled *bool  // nil means filter.Enabled must be nil
	}{
		{name: "enabled=true -> &true", enabledStr: "true", wantEnabled: boolPtr(true)},
		{name: "enabled=false -> &false (only 'true' matches strictly)", enabledStr: "false", wantEnabled: boolPtr(false)},
		{name: "enabled=1 -> &false (non-'true' string maps false)", enabledStr: "1", wantEnabled: boolPtr(false)},
		{name: "enabled omitted -> nil (no filter)", enabledStr: "", wantEnabled: nil},
	}

	for _, tc := range enabledCases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newCovFakeRepo()
			covSeed(repo, covTenant)
			h := newCovHandler(repo)
			r := newCovRouter(h)

			path := "/api/v1/products"
			if tc.enabledStr != "" {
				path += "?enabled=" + tc.enabledStr
			}
			w := covDo(r, http.MethodGet, path, covTenantStr, "")

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
			}

			// Business invariant: List must scope the query by the caller's
			// tenant (handler.go:136-141) — no cross-tenant leak.
			if repo.lastListFilter.TenantID != covTenant {
				t.Errorf("filter.TenantID = %s, want %s (tenant scoping leak)", repo.lastListFilter.TenantID, covTenant)
			}

			got := repo.lastListFilter.Enabled
			switch {
			case tc.wantEnabled == nil:
				if got != nil {
					t.Errorf("filter.Enabled = %v, want nil", *got)
				}
			case got == nil:
				t.Errorf("filter.Enabled = nil, want %v", *tc.wantEnabled)
			case *got != *tc.wantEnabled:
				t.Errorf("filter.Enabled = %v, want %v", *got, *tc.wantEnabled)
			}
		})
	}
}

func boolPtr(b bool) *bool { return &b }

// ---- GetByID ----

func TestHandlerGetByID_Cov(t *testing.T) {
	t.Run("no tenant -> 401", func(t *testing.T) {
		repo := newCovFakeRepo()
		h := newCovHandler(repo)
		r := newCovRouter(h)

		w := covDo(r, http.MethodGet, "/api/v1/products/"+uuid.New().String(), "", "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401; body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("non-UUID :id -> 400", func(t *testing.T) {
		repo := newCovFakeRepo()
		h := newCovHandler(repo)
		r := newCovRouter(h)

		w := covDo(r, http.MethodGet, "/api/v1/products/not-a-uuid", covTenantStr, "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "must be a UUID") {
			t.Errorf("body = %s, want 'must be a UUID'", w.Body.String())
		}
	})

	t.Run("ErrNotFound -> 404", func(t *testing.T) {
		repo := newCovFakeRepo()
		h := newCovHandler(repo)
		r := newCovRouter(h)

		w := covDo(r, http.MethodGet, "/api/v1/products/"+uuid.New().String(), covTenantStr, "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404; body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("non-NotFound repo error -> 500, not 400", func(t *testing.T) {
		repo := newCovFakeRepo()
		repo.getErr = errors.New("db timeout")
		h := newCovHandler(repo)
		r := newCovRouter(h)

		w := covDo(r, http.MethodGet, "/api/v1/products/"+uuid.New().String(), covTenantStr, "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500; body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("success -> 200", func(t *testing.T) {
		repo := newCovFakeRepo()
		p := covSeed(repo, covTenant)
		h := newCovHandler(repo)
		r := newCovRouter(h)

		w := covDo(r, http.MethodGet, "/api/v1/products/"+p.ID.String(), covTenantStr, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), p.ID.String()) {
			t.Errorf("body = %s, want product id %s", w.Body.String(), p.ID)
		}
	})
}

// ---- Update ----

func TestHandlerUpdate_Cov(t *testing.T) {
	t.Run("no tenant -> 401", func(t *testing.T) {
		repo := newCovFakeRepo()
		h := newCovHandler(repo)
		r := newCovRouter(h)

		w := covDo(r, http.MethodPut, "/api/v1/products/"+uuid.New().String(), "", `{"name":"X"}`)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401; body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("non-UUID :id -> 400", func(t *testing.T) {
		repo := newCovFakeRepo()
		h := newCovHandler(repo)
		r := newCovRouter(h)

		w := covDo(r, http.MethodPut, "/api/v1/products/not-a-uuid", covTenantStr, `{"name":"X"}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("malformed JSON -> 400 invalid request body", func(t *testing.T) {
		repo := newCovFakeRepo()
		h := newCovHandler(repo)
		r := newCovRouter(h)

		w := covDo(r, http.MethodPut, "/api/v1/products/"+uuid.New().String(), covTenantStr, `{not-json`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "invalid request body") {
			t.Errorf("body = %s, want 'invalid request body'", w.Body.String())
		}
	})

	t.Run("ErrNotFound -> 404", func(t *testing.T) {
		repo := newCovFakeRepo()
		h := newCovHandler(repo)
		r := newCovRouter(h)

		w := covDo(r, http.MethodPut, "/api/v1/products/"+uuid.New().String(), covTenantStr, `{"name":"X"}`)
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404; body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("non-NotFound use-case error -> 400, distinct from GetByID's 500 routing", func(t *testing.T) {
		repo := newCovFakeRepo()
		p := covSeed(repo, covTenant)
		h := newCovHandler(repo)
		r := newCovRouter(h)

		w := covDo(r, http.MethodPut, "/api/v1/products/"+p.ID.String(), covTenantStr,
			`{"measurement_strategy":"bogus"}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "invalid measurement_strategy") {
			t.Errorf("body = %s, want 'invalid measurement_strategy'", w.Body.String())
		}
	})

	t.Run("success -> 200 with updated field", func(t *testing.T) {
		repo := newCovFakeRepo()
		p := covSeed(repo, covTenant)
		h := newCovHandler(repo)
		r := newCovRouter(h)

		w := covDo(r, http.MethodPut, "/api/v1/products/"+p.ID.String(), covTenantStr,
			`{"name":"Updated Name"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "Updated Name") {
			t.Errorf("body = %s, want 'Updated Name'", w.Body.String())
		}
	})
}

// ---- Delete ----

func TestHandlerDelete_Cov(t *testing.T) {
	t.Run("no tenant -> 401", func(t *testing.T) {
		repo := newCovFakeRepo()
		h := newCovHandler(repo)
		r := newCovRouter(h)

		w := covDo(r, http.MethodDelete, "/api/v1/products/"+uuid.New().String(), "", "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401; body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("non-UUID :id -> 400", func(t *testing.T) {
		repo := newCovFakeRepo()
		h := newCovHandler(repo)
		r := newCovRouter(h)

		w := covDo(r, http.MethodDelete, "/api/v1/products/not-a-uuid", covTenantStr, "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("ErrNotFound -> 404", func(t *testing.T) {
		repo := newCovFakeRepo()
		h := newCovHandler(repo)
		r := newCovRouter(h)

		w := covDo(r, http.MethodDelete, "/api/v1/products/"+uuid.New().String(), covTenantStr, "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404; body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("non-NotFound repo error -> 500", func(t *testing.T) {
		repo := newCovFakeRepo()
		repo.deleteErr = errors.New("db fail")
		h := newCovHandler(repo)
		r := newCovRouter(h)

		w := covDo(r, http.MethodDelete, "/api/v1/products/"+uuid.New().String(), covTenantStr, "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500; body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("success -> 204 No Content, empty body", func(t *testing.T) {
		repo := newCovFakeRepo()
		p := covSeed(repo, covTenant)
		h := newCovHandler(repo)
		r := newCovRouter(h)

		w := covDo(r, http.MethodDelete, "/api/v1/products/"+p.ID.String(), covTenantStr, "")
		if w.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204; body=%s", w.Code, w.Body.String())
		}
		if w.Body.Len() != 0 {
			t.Errorf("body = %q, want empty for 204", w.Body.String())
		}
	})
}

// ---- Restore ----

func TestHandlerRestore_Cov(t *testing.T) {
	t.Run("no tenant -> 401", func(t *testing.T) {
		repo := newCovFakeRepo()
		h := newCovHandler(repo)
		r := newCovRouter(h)

		w := covDo(r, http.MethodPost, "/api/v1/products/"+uuid.New().String()+"/restore", "", "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401; body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("non-UUID :id -> 400", func(t *testing.T) {
		repo := newCovFakeRepo()
		h := newCovHandler(repo)
		r := newCovRouter(h)

		w := covDo(r, http.MethodPost, "/api/v1/products/not-a-uuid/restore", covTenantStr, "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("ErrNotFound -> 404 'product not found or already active'", func(t *testing.T) {
		repo := newCovFakeRepo()
		h := newCovHandler(repo)
		r := newCovRouter(h)

		w := covDo(r, http.MethodPost, "/api/v1/products/"+uuid.New().String()+"/restore", covTenantStr, "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404; body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "product not found or already active") {
			t.Errorf("body = %s, want restore-specific 404 message", w.Body.String())
		}
	})

	t.Run("non-NotFound repo error -> 500", func(t *testing.T) {
		repo := newCovFakeRepo()
		repo.restoreErr = errors.New("db fail")
		h := newCovHandler(repo)
		r := newCovRouter(h)

		w := covDo(r, http.MethodPost, "/api/v1/products/"+uuid.New().String()+"/restore", covTenantStr, "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500; body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("success -> 200", func(t *testing.T) {
		repo := newCovFakeRepo()
		p := covSeed(repo, covTenant)
		h := newCovHandler(repo)
		r := newCovRouter(h)

		w := covDo(r, http.MethodPost, "/api/v1/products/"+p.ID.String()+"/restore", covTenantStr, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
		}
	})
}
