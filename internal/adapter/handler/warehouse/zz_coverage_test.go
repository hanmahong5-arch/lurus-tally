package warehouse

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appwarehouse "github.com/hanmahong5-arch/lurus-tally/internal/app/warehouse"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/warehouse"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// covFakeRepo is a white-box in-memory stub satisfying appwarehouse.Repository,
// wired through the *real* use cases so this file exercises the genuine
// handler -> use case -> domain.Validate path; only persistence is faked.
type covFakeRepo struct {
	t *testing.T

	// forbid, when true, fails the test immediately if ANY repo method is
	// invoked — used to prove the tenant-isolation gate returns 401 BEFORE
	// any use-case/repo call happens.
	forbid bool

	createFn  func(ctx context.Context, w *domain.Warehouse) error
	getByIDFn func(ctx context.Context, tenantID, id uuid.UUID) (*domain.Warehouse, error)
	listFn    func(ctx context.Context, f domain.ListFilter) ([]*domain.Warehouse, int, error)
	updateFn  func(ctx context.Context, w *domain.Warehouse) error
	deleteFn  func(ctx context.Context, tenantID, id uuid.UUID) error
	restoreFn func(ctx context.Context, tenantID, id uuid.UUID) (*domain.Warehouse, error)
}

func (f *covFakeRepo) guard(method string) {
	if f.forbid {
		f.t.Fatalf("repo.%s must not be called (tenant gate should short-circuit)", method)
	}
}

func (f *covFakeRepo) Create(ctx context.Context, w *domain.Warehouse) error {
	f.guard("Create")
	if f.createFn != nil {
		return f.createFn(ctx, w)
	}
	return nil
}

func (f *covFakeRepo) GetByID(ctx context.Context, tenantID, id uuid.UUID) (*domain.Warehouse, error) {
	f.guard("GetByID")
	if f.getByIDFn != nil {
		return f.getByIDFn(ctx, tenantID, id)
	}
	return nil, appwarehouse.ErrNotFound
}

func (f *covFakeRepo) List(ctx context.Context, flt domain.ListFilter) ([]*domain.Warehouse, int, error) {
	f.guard("List")
	if f.listFn != nil {
		return f.listFn(ctx, flt)
	}
	return nil, 0, nil
}

func (f *covFakeRepo) Update(ctx context.Context, w *domain.Warehouse) error {
	f.guard("Update")
	if f.updateFn != nil {
		return f.updateFn(ctx, w)
	}
	return nil
}

func (f *covFakeRepo) Delete(ctx context.Context, tenantID, id uuid.UUID) error {
	f.guard("Delete")
	if f.deleteFn != nil {
		return f.deleteFn(ctx, tenantID, id)
	}
	return nil
}

func (f *covFakeRepo) Restore(ctx context.Context, tenantID, id uuid.UUID) (*domain.Warehouse, error) {
	f.guard("Restore")
	if f.restoreFn != nil {
		return f.restoreFn(ctx, tenantID, id)
	}
	return nil, appwarehouse.ErrNotFound
}

var _ appwarehouse.Repository = (*covFakeRepo)(nil)

// covTenant is the fixed caller tenant used across these tests.
var covTenant = uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")

// covAttackerTenant is a distinct tenant id occasionally smuggled in request
// bodies to prove the handler never trusts client-supplied tenant data.
var covAttackerTenant = uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")

// newCovRouter wires a real Handler (backed by real use cases) to repo.
// When tenant is non-nil, a shim middleware injects it via the same context
// key AuthMiddleware uses in production (middleware.CtxKeyTenantID); when
// nil, no key is set at all, so middleware.GetTenantID returns uuid.Nil,
// mirroring an unauthenticated request.
func newCovRouter(repo appwarehouse.Repository, tenant *uuid.UUID) *gin.Engine {
	h := New(
		appwarehouse.NewCreateUseCase(repo),
		appwarehouse.NewGetByIDUseCase(repo),
		appwarehouse.NewListUseCase(repo),
		appwarehouse.NewUpdateUseCase(repo),
		appwarehouse.NewDeleteUseCase(repo),
		appwarehouse.NewRestoreUseCase(repo),
	)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(func(c *gin.Context) {
		if tenant != nil {
			c.Set(middleware.CtxKeyTenantID, *tenant)
		}
		c.Next()
	})
	rg := r.Group("/api/v1")
	h.RegisterRoutes(rg)
	return r
}

func doJSON(t *testing.T, r *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ---------------------------------------------------------------------------
// Business invariant: tenant isolation gate — every handler must 401 BEFORE
// touching the use case/repo when middleware.GetTenantID == uuid.Nil.
// ---------------------------------------------------------------------------

func TestHandlers_TenantGate401_BeforeUseCase(t *testing.T) {
	someID := uuid.New().String()
	cases := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"List", http.MethodGet, "/api/v1/warehouses", nil},
		{"Create", http.MethodPost, "/api/v1/warehouses", map[string]any{"name": "x"}},
		{"GetByID", http.MethodGet, "/api/v1/warehouses/" + someID, nil},
		{"Update", http.MethodPut, "/api/v1/warehouses/" + someID, map[string]any{}},
		{"Delete", http.MethodDelete, "/api/v1/warehouses/" + someID, nil},
		{"Restore", http.MethodPost, "/api/v1/warehouses/" + someID + "/restore", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &covFakeRepo{t: t, forbid: true}
			r := newCovRouter(repo, nil)
			w := doJSON(t, r, tc.method, tc.path, tc.body)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401; body: %s", w.Code, w.Body.String())
			}
			var payload map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if payload["error"] != "tenant not identified" {
				t.Errorf("error = %q, want %q", payload["error"], "tenant not identified")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Create
// ---------------------------------------------------------------------------

func TestCreate_BindingBoundaries(t *testing.T) {
	max128 := strings.Repeat("a", 128)
	over128 := strings.Repeat("a", 129)
	max500 := strings.Repeat("b", 500)
	over500 := strings.Repeat("b", 501)

	cases := []struct {
		name       string
		body       map[string]any
		wantStatus int
	}{
		{"missing name", map[string]any{"code": "WH1"}, http.StatusBadRequest},
		{"name at max 128 ok", map[string]any{"name": max128}, http.StatusCreated},
		{"name over max 129 fails", map[string]any{"name": over128}, http.StatusBadRequest},
		{"address at max 500 ok", map[string]any{"name": "n", "address": max500}, http.StatusCreated},
		{"address over max 501 fails", map[string]any{"name": "n", "address": over500}, http.StatusBadRequest},
		{"manager at max 128 ok", map[string]any{"name": "n", "manager": max128}, http.StatusCreated},
		{"manager over max 129 fails", map[string]any{"name": "n", "manager": over128}, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &covFakeRepo{t: t}
			r := newCovRouter(repo, &covTenant)
			w := doJSON(t, r, http.MethodPost, "/api/v1/warehouses", tc.body)
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", w.Code, tc.wantStatus, w.Body.String())
			}
		})
	}
}

func TestCreate_DuplicateName_Returns409(t *testing.T) {
	repo := &covFakeRepo{t: t, createFn: func(_ context.Context, _ *domain.Warehouse) error {
		return appwarehouse.ErrDuplicateName
	}}
	r := newCovRouter(repo, &covTenant)
	w := doJSON(t, r, http.MethodPost, "/api/v1/warehouses", map[string]any{"name": "重复仓库"})
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body: %s", w.Code, w.Body.String())
	}
	var payload map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &payload)
	if payload["error"] != "duplicate warehouse name" {
		t.Errorf("error = %q, want %q", payload["error"], "duplicate warehouse name")
	}
}

func TestCreate_OtherRepoError_Returns400(t *testing.T) {
	repo := &covFakeRepo{t: t, createFn: func(_ context.Context, _ *domain.Warehouse) error {
		return errors.New("boom: constraint violated")
	}}
	r := newCovRouter(repo, &covTenant)
	w := doJSON(t, r, http.MethodPost, "/api/v1/warehouses", map[string]any{"name": "仓库"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestCreate_Success_LocationAndTenantEcho(t *testing.T) {
	repo := &covFakeRepo{t: t}
	r := newCovRouter(repo, &covTenant)

	// Attacker attempts to smuggle a foreign tenant_id in the JSON body;
	// createRequest has no such field so it must be silently ignored.
	rawBody := []byte(`{"name":"广州主仓库","tenant_id":"` + covAttackerTenant.String() + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/warehouses", bytes.NewReader(rawBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	var dto WarehouseDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dto.TenantID != covTenant.String() {
		t.Errorf("tenant_id = %q, want caller tenant %q (must not honour attacker-supplied tenant)", dto.TenantID, covTenant.String())
	}
	wantLoc := "/api/v1/warehouses/" + dto.ID
	if got := w.Header().Get("Location"); got != wantLoc {
		t.Errorf("Location = %q, want %q", got, wantLoc)
	}
}

// ---------------------------------------------------------------------------
// GetByID
// ---------------------------------------------------------------------------

func TestGetByID_InvalidID_Returns400(t *testing.T) {
	repo := &covFakeRepo{t: t, forbid: true}
	r := newCovRouter(repo, &covTenant)
	w := doJSON(t, r, http.MethodGet, "/api/v1/warehouses/not-a-uuid", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
	var payload map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &payload)
	if payload["error"] != "invalid id" {
		t.Errorf("error = %q, want %q", payload["error"], "invalid id")
	}
}

func TestGetByID_NotFound_Returns404(t *testing.T) {
	repo := &covFakeRepo{t: t, getByIDFn: func(_ context.Context, _, _ uuid.UUID) (*domain.Warehouse, error) {
		return nil, appwarehouse.ErrNotFound
	}}
	r := newCovRouter(repo, &covTenant)
	w := doJSON(t, r, http.MethodGet, "/api/v1/warehouses/"+uuid.New().String(), nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

func TestGetByID_RepoInternalError_Returns500(t *testing.T) {
	repo := &covFakeRepo{t: t, getByIDFn: func(_ context.Context, _, _ uuid.UUID) (*domain.Warehouse, error) {
		return nil, errors.New("connection reset")
	}}
	r := newCovRouter(repo, &covTenant)
	w := doJSON(t, r, http.MethodGet, "/api/v1/warehouses/"+uuid.New().String(), nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
	var payload map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &payload)
	if payload["error"] != "internal_error" {
		t.Errorf("error code = %q, want %q (safe generic body)", payload["error"], "internal_error")
	}
	if strings.Contains(w.Body.String(), "connection reset") {
		t.Error("500 body leaked internal cause text")
	}
}

func TestGetByID_Success_Returns200(t *testing.T) {
	created := time.Date(2024, 3, 4, 5, 6, 7, 0, time.UTC)
	updated := time.Date(2024, 3, 4, 8, 9, 10, 0, time.UTC)
	wantID := uuid.New()
	repo := &covFakeRepo{t: t, getByIDFn: func(_ context.Context, tenantID, id uuid.UUID) (*domain.Warehouse, error) {
		if tenantID != covTenant {
			t.Errorf("tenantID passed to repo = %v, want %v", tenantID, covTenant)
		}
		return &domain.Warehouse{
			ID: wantID, TenantID: covTenant, Name: "中心仓", Code: "C1",
			CreatedAt: created, UpdatedAt: updated,
		}, nil
	}}
	r := newCovRouter(repo, &covTenant)
	w := doJSON(t, r, http.MethodGet, "/api/v1/warehouses/"+wantID.String(), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var dto WarehouseDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dto.ID != wantID.String() || dto.Name != "中心仓" || dto.CreatedAt != "2024-03-04T05:06:07Z" || dto.UpdatedAt != "2024-03-04T08:09:10Z" {
		t.Errorf("dto = %+v, unexpected fields", dto)
	}
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

func TestUpdate_InvalidID_Returns400(t *testing.T) {
	repo := &covFakeRepo{t: t, forbid: true}
	r := newCovRouter(repo, &covTenant)
	w := doJSON(t, r, http.MethodPut, "/api/v1/warehouses/not-a-uuid", map[string]any{})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestUpdate_BindingBoundaries(t *testing.T) {
	over128 := strings.Repeat("a", 129)
	over500 := strings.Repeat("b", 501)
	cases := []struct {
		name string
		body map[string]any
	}{
		{"name over 128", map[string]any{"name": over128}},
		{"address over 500", map[string]any{"address": over500}},
		{"manager over 128", map[string]any{"manager": over128}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &covFakeRepo{t: t, forbid: true}
			r := newCovRouter(repo, &covTenant)
			w := doJSON(t, r, http.MethodPut, "/api/v1/warehouses/"+uuid.New().String(), tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestUpdate_FetchNotFound_Returns404(t *testing.T) {
	repo := &covFakeRepo{t: t, getByIDFn: func(_ context.Context, _, _ uuid.UUID) (*domain.Warehouse, error) {
		return nil, appwarehouse.ErrNotFound
	}}
	r := newCovRouter(repo, &covTenant)
	w := doJSON(t, r, http.MethodPut, "/api/v1/warehouses/"+uuid.New().String(), map[string]any{})
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

func TestUpdate_ValidateFailure_Returns400(t *testing.T) {
	empty := ""
	existing := &domain.Warehouse{ID: uuid.New(), TenantID: covTenant, Name: "旧仓库", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	repo := &covFakeRepo{
		t: t,
		getByIDFn: func(_ context.Context, _, _ uuid.UUID) (*domain.Warehouse, error) {
			return existing, nil
		},
	}
	r := newCovRouter(repo, &covTenant)
	w := doJSON(t, r, http.MethodPut, "/api/v1/warehouses/"+existing.ID.String(), map[string]any{"name": empty})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (domain.Validate: name required); body: %s", w.Code, w.Body.String())
	}
}

func TestUpdate_RepoUpdateNotFound_Returns404(t *testing.T) {
	existing := &domain.Warehouse{ID: uuid.New(), TenantID: covTenant, Name: "旧仓库", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	repo := &covFakeRepo{
		t: t,
		getByIDFn: func(_ context.Context, _, _ uuid.UUID) (*domain.Warehouse, error) {
			return existing, nil
		},
		updateFn: func(_ context.Context, _ *domain.Warehouse) error {
			return appwarehouse.ErrNotFound
		},
	}
	r := newCovRouter(repo, &covTenant)
	newName := "新名字"
	w := doJSON(t, r, http.MethodPut, "/api/v1/warehouses/"+existing.ID.String(), map[string]any{"name": newName})
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (deleted between fetch and update); body: %s", w.Code, w.Body.String())
	}
}

func TestUpdate_RepoUpdateOtherError_Returns400(t *testing.T) {
	existing := &domain.Warehouse{ID: uuid.New(), TenantID: covTenant, Name: "旧仓库", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	repo := &covFakeRepo{
		t: t,
		getByIDFn: func(_ context.Context, _, _ uuid.UUID) (*domain.Warehouse, error) {
			return existing, nil
		},
		updateFn: func(_ context.Context, _ *domain.Warehouse) error {
			return errors.New("db write failed")
		},
	}
	r := newCovRouter(repo, &covTenant)
	newName := "新名字"
	w := doJSON(t, r, http.MethodPut, "/api/v1/warehouses/"+existing.ID.String(), map[string]any{"name": newName})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestUpdate_Success_Returns200(t *testing.T) {
	created := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	existing := &domain.Warehouse{ID: uuid.New(), TenantID: covTenant, Name: "旧仓库", CreatedAt: created, UpdatedAt: created}
	repo := &covFakeRepo{
		t: t,
		getByIDFn: func(_ context.Context, _, _ uuid.UUID) (*domain.Warehouse, error) {
			return existing, nil
		},
		updateFn: func(_ context.Context, w *domain.Warehouse) error {
			existing = w
			return nil
		},
	}
	r := newCovRouter(repo, &covTenant)
	newName := "新仓库名"
	isDefault := true
	w := doJSON(t, r, http.MethodPut, "/api/v1/warehouses/"+existing.ID.String(), map[string]any{
		"name": newName, "is_default": isDefault,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var dto WarehouseDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dto.Name != newName {
		t.Errorf("name = %q, want %q", dto.Name, newName)
	}
	if !dto.IsDefault {
		t.Error("is_default = false, want true")
	}
	if dto.CreatedAt != "2023-01-01T00:00:00Z" {
		t.Errorf("created_at should be preserved from fetch, got %q", dto.CreatedAt)
	}
}

// ---------------------------------------------------------------------------
// Delete
// ---------------------------------------------------------------------------

func TestDelete_InvalidID_Returns400(t *testing.T) {
	repo := &covFakeRepo{t: t, forbid: true}
	r := newCovRouter(repo, &covTenant)
	w := doJSON(t, r, http.MethodDelete, "/api/v1/warehouses/not-a-uuid", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestDelete_NotFound_Returns404(t *testing.T) {
	repo := &covFakeRepo{t: t, deleteFn: func(_ context.Context, _, _ uuid.UUID) error {
		return appwarehouse.ErrNotFound
	}}
	r := newCovRouter(repo, &covTenant)
	w := doJSON(t, r, http.MethodDelete, "/api/v1/warehouses/"+uuid.New().String(), nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

func TestDelete_RepoInternalError_Returns500(t *testing.T) {
	repo := &covFakeRepo{t: t, deleteFn: func(_ context.Context, _, _ uuid.UUID) error {
		return errors.New("db down")
	}}
	r := newCovRouter(repo, &covTenant)
	w := doJSON(t, r, http.MethodDelete, "/api/v1/warehouses/"+uuid.New().String(), nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
}

func TestDelete_Success_Returns204(t *testing.T) {
	repo := &covFakeRepo{t: t}
	r := newCovRouter(repo, &covTenant)
	w := doJSON(t, r, http.MethodDelete, "/api/v1/warehouses/"+uuid.New().String(), nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body: %s", w.Code, w.Body.String())
	}
	if w.Body.Len() != 0 {
		t.Errorf("204 body should be empty, got %q", w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Restore
// ---------------------------------------------------------------------------

func TestRestore_InvalidID_Returns400(t *testing.T) {
	repo := &covFakeRepo{t: t, forbid: true}
	r := newCovRouter(repo, &covTenant)
	w := doJSON(t, r, http.MethodPost, "/api/v1/warehouses/not-a-uuid/restore", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestRestore_NotFound_Returns404(t *testing.T) {
	repo := &covFakeRepo{t: t, restoreFn: func(_ context.Context, _, _ uuid.UUID) (*domain.Warehouse, error) {
		return nil, appwarehouse.ErrNotFound
	}}
	r := newCovRouter(repo, &covTenant)
	w := doJSON(t, r, http.MethodPost, "/api/v1/warehouses/"+uuid.New().String()+"/restore", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

func TestRestore_RepoInternalError_Returns500(t *testing.T) {
	repo := &covFakeRepo{t: t, restoreFn: func(_ context.Context, _, _ uuid.UUID) (*domain.Warehouse, error) {
		return nil, errors.New("db down")
	}}
	r := newCovRouter(repo, &covTenant)
	w := doJSON(t, r, http.MethodPost, "/api/v1/warehouses/"+uuid.New().String()+"/restore", nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
}

func TestRestore_Success_Returns200(t *testing.T) {
	wantID := uuid.New()
	repo := &covFakeRepo{t: t, restoreFn: func(_ context.Context, _, _ uuid.UUID) (*domain.Warehouse, error) {
		return &domain.Warehouse{ID: wantID, TenantID: covTenant, Name: "恢复仓库", CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
	}}
	r := newCovRouter(repo, &covTenant)
	w := doJSON(t, r, http.MethodPost, "/api/v1/warehouses/"+wantID.String()+"/restore", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var dto WarehouseDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dto.ID != wantID.String() {
		t.Errorf("id = %q, want %q", dto.ID, wantID.String())
	}
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

func TestList_RepoError_Returns500(t *testing.T) {
	repo := &covFakeRepo{t: t, listFn: func(_ context.Context, _ domain.ListFilter) ([]*domain.Warehouse, int, error) {
		return nil, 0, errors.New("query failed")
	}}
	r := newCovRouter(repo, &covTenant)
	w := doJSON(t, r, http.MethodGet, "/api/v1/warehouses", nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
}

func TestList_Success_EmptyItemsIsEmptyArrayNotNull(t *testing.T) {
	repo := &covFakeRepo{t: t, listFn: func(_ context.Context, _ domain.ListFilter) ([]*domain.Warehouse, int, error) {
		return nil, 0, nil
	}}
	r := newCovRouter(repo, &covTenant)
	w := doJSON(t, r, http.MethodGet, "/api/v1/warehouses", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Errorf("body = %s, want items to serialise as [] not null", w.Body.String())
	}
}

func TestList_Success_WithItems(t *testing.T) {
	created := time.Date(2022, 6, 15, 10, 0, 0, 0, time.UTC)
	items := []*domain.Warehouse{
		{ID: uuid.New(), TenantID: covTenant, Name: "仓库A", CreatedAt: created, UpdatedAt: created},
		{ID: uuid.New(), TenantID: covTenant, Name: "仓库B", CreatedAt: created, UpdatedAt: created},
	}
	repo := &covFakeRepo{t: t, listFn: func(_ context.Context, _ domain.ListFilter) ([]*domain.Warehouse, int, error) {
		return items, 2, nil
	}}
	r := newCovRouter(repo, &covTenant)
	w := doJSON(t, r, http.MethodGet, "/api/v1/warehouses", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp WarehouseListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 2 || len(resp.Items) != 2 {
		t.Fatalf("resp = %+v, want total=2 items=2", resp)
	}
	if resp.Items[0].Name != "仓库A" || resp.Items[1].Name != "仓库B" {
		t.Errorf("items = %+v, names mismatch", resp.Items)
	}
}

func TestList_QueryParams_PassedToUseCase(t *testing.T) {
	var captured domain.ListFilter
	repo := &covFakeRepo{t: t, listFn: func(_ context.Context, f domain.ListFilter) ([]*domain.Warehouse, int, error) {
		captured = f
		return nil, 0, nil
	}}
	r := newCovRouter(repo, &covTenant)
	w := doJSON(t, r, http.MethodGet, "/api/v1/warehouses?q=abc&limit=5&offset=10", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if captured.TenantID != covTenant {
		t.Errorf("filter.TenantID = %v, want %v", captured.TenantID, covTenant)
	}
	if captured.Query != "abc" {
		t.Errorf("filter.Query = %q, want %q", captured.Query, "abc")
	}
	if captured.Limit != 5 {
		t.Errorf("filter.Limit = %d, want 5", captured.Limit)
	}
	if captured.Offset != 10 {
		t.Errorf("filter.Offset = %d, want 10", captured.Offset)
	}
}

// ---------------------------------------------------------------------------
// queryInt (pure helper) — table-driven against a synthetic *gin.Context.
// ---------------------------------------------------------------------------

func TestQueryInt_TableDriven(t *testing.T) {
	cases := []struct {
		name       string
		query      string
		key        string
		defaultVal int
		want       int
	}{
		{"empty string uses default", "", "limit", 20, 20},
		{"missing key uses default", "", "limit", 20, 20},
		{"negative uses default", "limit=-1", "limit", 20, 20},
		{"non-numeric uses default", "limit=abc", "limit", 20, 20},
		{"valid positive parses", "limit=5", "limit", 20, 5},
		{"zero is valid, not defaulted", "offset=0", "offset", 99, 0},
		{"large valid value parses", "offset=1000", "offset", 0, 1000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			target := "/x"
			if tc.query != "" {
				target += "?" + tc.query
			}
			c.Request = httptest.NewRequest(http.MethodGet, target, nil)
			got := queryInt(c, tc.key, tc.defaultVal)
			if got != tc.want {
				t.Errorf("queryInt(%q,%d) = %d, want %d", tc.query, tc.defaultVal, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// toDTO — deterministic RFC3339 time formatting, hand-computed expectations.
// ---------------------------------------------------------------------------

func TestToDTO_FieldMappingAndTimeFormatting(t *testing.T) {
	id := uuid.New()
	tenantID := uuid.New()
	created := time.Date(2024, 3, 4, 5, 6, 7, 0, time.UTC)
	updated := time.Date(2024, 3, 4, 8, 9, 10, 0, time.UTC)
	w := &domain.Warehouse{
		ID: id, TenantID: tenantID, Code: "C1", Name: "N1",
		Address: "A1", Manager: "M1", IsDefault: true, Remark: "R1",
		CreatedAt: created, UpdatedAt: updated,
	}
	dto := toDTO(w)

	if dto.ID != id.String() {
		t.Errorf("ID = %q, want %q", dto.ID, id.String())
	}
	if dto.TenantID != tenantID.String() {
		t.Errorf("TenantID = %q, want %q", dto.TenantID, tenantID.String())
	}
	if dto.Code != "C1" || dto.Name != "N1" || dto.Address != "A1" || dto.Manager != "M1" || dto.Remark != "R1" {
		t.Errorf("dto = %+v, field mismatch", dto)
	}
	if !dto.IsDefault {
		t.Error("IsDefault = false, want true")
	}
	// Hand-computed RFC3339 strings for the fixed timestamps above — NOT
	// derived by reading back toDTO's own output.
	if dto.CreatedAt != "2024-03-04T05:06:07Z" {
		t.Errorf("CreatedAt = %q, want %q", dto.CreatedAt, "2024-03-04T05:06:07Z")
	}
	if dto.UpdatedAt != "2024-03-04T08:09:10Z" {
		t.Errorf("UpdatedAt = %q, want %q", dto.UpdatedAt, "2024-03-04T08:09:10Z")
	}
}
