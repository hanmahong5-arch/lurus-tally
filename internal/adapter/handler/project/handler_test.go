package project_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	handlerproject "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/project"
	appproject "github.com/hanmahong5-arch/lurus-tally/internal/app/project"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/project"
)

func init() {
	gin.SetMode(gin.TestMode)
}

const handlerTestTenantID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"

// ---- fake repository for handler tests ----

type fakeHandlerRepo struct {
	createErr  error
	getErr     error
	listItems  []*domain.Project
	listTotal  int
	listErr    error
	updateErr  error
	deleteErr  error
	restoreErr error
	created    *domain.Project
}

func (f *fakeHandlerRepo) Create(_ context.Context, p *domain.Project) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.created = p
	return nil
}

func (f *fakeHandlerRepo) GetByID(_ context.Context, _, _ uuid.UUID) (*domain.Project, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return &domain.Project{
		ID:        uuid.New(),
		TenantID:  uuid.MustParse(handlerTestTenantID),
		Code:      "P001",
		Name:      "河道绿化",
		Status:    domain.StatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}, nil
}

func (f *fakeHandlerRepo) List(_ context.Context, _ domain.ListFilter) ([]*domain.Project, int, error) {
	if f.listErr != nil {
		return nil, 0, f.listErr
	}
	return f.listItems, f.listTotal, nil
}

func (f *fakeHandlerRepo) Update(_ context.Context, _ *domain.Project) error {
	return f.updateErr
}

func (f *fakeHandlerRepo) Delete(_ context.Context, _, _ uuid.UUID) error {
	return f.deleteErr
}

func (f *fakeHandlerRepo) Restore(_ context.Context, _, _ uuid.UUID) (*domain.Project, error) {
	if f.restoreErr != nil {
		return nil, f.restoreErr
	}
	return &domain.Project{
		ID:        uuid.New(),
		TenantID:  uuid.MustParse(handlerTestTenantID),
		Code:      "P001",
		Name:      "河道绿化",
		Status:    domain.StatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		DeletedAt: nil,
	}, nil
}

// makeProjectHandler wires the handler with a fake repo.
func makeProjectHandler(repo appproject.Repository) (*handlerproject.ProjectHandler, *gin.Engine) {
	h := handlerproject.NewProjectHandler(
		appproject.NewCreateUseCase(repo),
		appproject.NewGetByIDUseCase(repo),
		appproject.NewListUseCase(repo),
		appproject.NewUpdateUseCase(repo),
		appproject.NewDeleteUseCase(repo),
		appproject.NewRestoreUseCase(repo),
	)
	r := gin.New()
	r.Use(gin.Recovery())
	api := r.Group("/api/v1")
	h.RegisterRoutes(api)
	return h, r
}

func TestProjectHandler_List_Returns200WithItems(t *testing.T) {
	items := []*domain.Project{
		{ID: uuid.New(), TenantID: uuid.MustParse(handlerTestTenantID), Code: "P001", Name: "河道绿化", Status: domain.StatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: uuid.New(), TenantID: uuid.MustParse(handlerTestTenantID), Code: "P002", Name: "道路修缮", Status: domain.StatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: uuid.New(), TenantID: uuid.MustParse(handlerTestTenantID), Code: "P003", Name: "公园绿化", Status: domain.StatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}
	repo := &fakeHandlerRepo{listItems: items, listTotal: 3}
	_, r := makeProjectHandler(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	req.Header.Set("X-Tenant-ID", handlerTestTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp handlerproject.ProjectListResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 3 {
		t.Errorf("total = %d, want 3", resp.Total)
	}
	if len(resp.Items) != 3 {
		t.Errorf("items len = %d, want 3", len(resp.Items))
	}
}

func TestProjectHandler_Create_Returns201(t *testing.T) {
	repo := &fakeHandlerRepo{}
	_, r := makeProjectHandler(repo)

	body, _ := json.Marshal(map[string]any{
		"code": "P001",
		"name": "河道绿化",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", handlerTestTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc == "" {
		t.Error("missing Location header")
	}
}

func TestProjectHandler_Create_DuplicateCode_Returns409(t *testing.T) {
	repo := &fakeHandlerRepo{createErr: appproject.ErrDuplicateCode}
	_, r := makeProjectHandler(repo)

	body, _ := json.Marshal(map[string]any{"code": "P001", "name": "河道绿化"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", handlerTestTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "duplicate code" {
		t.Errorf("error = %q, want 'duplicate code'", resp["error"])
	}
}

func TestProjectHandler_Create_MissingName_Returns400(t *testing.T) {
	repo := &fakeHandlerRepo{}
	_, r := makeProjectHandler(repo)

	body, _ := json.Marshal(map[string]any{"code": "P001"}) // no name
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", handlerTestTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestProjectHandler_GetByID_Returns404ForUnknown(t *testing.T) {
	repo := &fakeHandlerRepo{getErr: appproject.ErrNotFound}
	_, r := makeProjectHandler(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+uuid.New().String(), nil)
	req.Header.Set("X-Tenant-ID", handlerTestTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

func TestProjectHandler_Update_Returns200(t *testing.T) {
	repo := &fakeHandlerRepo{}
	_, r := makeProjectHandler(repo)

	newName := "更新项目"
	body, _ := json.Marshal(map[string]any{"name": newName})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/projects/"+uuid.New().String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", handlerTestTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestProjectHandler_Delete_Returns204(t *testing.T) {
	repo := &fakeHandlerRepo{}
	_, r := makeProjectHandler(repo)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/"+uuid.New().String(), nil)
	req.Header.Set("X-Tenant-ID", handlerTestTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204; body: %s", w.Code, w.Body.String())
	}
}

func TestProjectHandler_Restore_Returns200(t *testing.T) {
	repo := &fakeHandlerRepo{}
	_, r := makeProjectHandler(repo)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+uuid.New().String()+"/restore", nil)
	req.Header.Set("X-Tenant-ID", handlerTestTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

// Compile-time check.
var _ appproject.Repository = (*fakeHandlerRepo)(nil)
