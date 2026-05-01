package horticulture_test

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
	handlerhort "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/horticulture"
	apphort "github.com/hanmahong5-arch/lurus-tally/internal/app/horticulture"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/horticulture"
)

func init() {
	gin.SetMode(gin.TestMode)
}

const handlerTestTenantID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"

// ---- fake repository for handler tests ----

type fakeHandlerRepo struct {
	createErr  error
	getErr     error
	listItems  []*domain.NurseryDict
	listTotal  int
	listErr    error
	updateErr  error
	deleteErr  error
	restoreErr error
	created    *domain.NurseryDict
}

func (f *fakeHandlerRepo) Create(_ context.Context, d *domain.NurseryDict) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.created = d
	return nil
}

func (f *fakeHandlerRepo) GetByID(_ context.Context, _, _ uuid.UUID) (*domain.NurseryDict, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return &domain.NurseryDict{
		ID:           uuid.New(),
		TenantID:     uuid.MustParse(handlerTestTenantID),
		Name:         "红枫",
		LatinName:    "Acer palmatum",
		Type:         domain.NurseryTypeTree,
		SpecTemplate: json.RawMessage("{}"),
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}, nil
}

func (f *fakeHandlerRepo) List(_ context.Context, _ domain.ListFilter) ([]*domain.NurseryDict, int, error) {
	if f.listErr != nil {
		return nil, 0, f.listErr
	}
	return f.listItems, f.listTotal, nil
}

func (f *fakeHandlerRepo) Update(_ context.Context, d *domain.NurseryDict) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	return nil
}

func (f *fakeHandlerRepo) Delete(_ context.Context, _, _ uuid.UUID) error {
	return f.deleteErr
}

func (f *fakeHandlerRepo) Restore(_ context.Context, _, _ uuid.UUID) (*domain.NurseryDict, error) {
	if f.restoreErr != nil {
		return nil, f.restoreErr
	}
	return &domain.NurseryDict{
		ID:           uuid.New(),
		TenantID:     uuid.MustParse(handlerTestTenantID),
		Name:         "红枫",
		Type:         domain.NurseryTypeTree,
		SpecTemplate: json.RawMessage("{}"),
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		DeletedAt:    nil,
	}, nil
}

// makeDictHandler wires the handler with a fake repo.
func makeDictHandler(repo apphort.Repository) (*handlerhort.DictHandler, *gin.Engine) {
	h := handlerhort.NewDictHandler(
		apphort.NewCreateUseCase(repo),
		apphort.NewGetByIDUseCase(repo),
		apphort.NewListUseCase(repo),
		apphort.NewUpdateUseCase(repo),
		apphort.NewDeleteUseCase(repo),
		apphort.NewRestoreUseCase(repo),
	)
	r := gin.New()
	r.Use(gin.Recovery())
	api := r.Group("/api/v1")
	h.RegisterRoutes(api)
	return h, r
}

func TestDictHandler_List_Returns200WithItems(t *testing.T) {
	items := []*domain.NurseryDict{
		{ID: uuid.New(), TenantID: uuid.MustParse(handlerTestTenantID), Name: "红枫", Type: domain.NurseryTypeTree, SpecTemplate: json.RawMessage("{}"), CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: uuid.New(), TenantID: uuid.MustParse(handlerTestTenantID), Name: "银杏", Type: domain.NurseryTypeTree, SpecTemplate: json.RawMessage("{}"), CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: uuid.New(), TenantID: uuid.MustParse(handlerTestTenantID), Name: "水杉", Type: domain.NurseryTypeTree, SpecTemplate: json.RawMessage("{}"), CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}
	repo := &fakeHandlerRepo{listItems: items, listTotal: 3}
	_, r := makeDictHandler(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nursery-dict", nil)
	req.Header.Set("X-Tenant-ID", handlerTestTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp handlerhort.ListResponse
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

func TestDictHandler_Create_Returns201(t *testing.T) {
	repo := &fakeHandlerRepo{}
	_, r := makeDictHandler(repo)

	body, _ := json.Marshal(map[string]any{
		"name": "测试苗木",
		"type": "shrub",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/nursery-dict", bytes.NewReader(body))
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

func TestDictHandler_Create_DuplicateName_Returns409(t *testing.T) {
	repo := &fakeHandlerRepo{createErr: apphort.ErrDuplicateName}
	_, r := makeDictHandler(repo)

	body, _ := json.Marshal(map[string]any{"name": "红枫", "type": "tree"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/nursery-dict", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", handlerTestTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409; body: %s", w.Code, w.Body.String())
	}
}

func TestDictHandler_GetByID_Returns404ForUnknown(t *testing.T) {
	repo := &fakeHandlerRepo{getErr: apphort.ErrNotFound}
	_, r := makeDictHandler(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nursery-dict/"+uuid.New().String(), nil)
	req.Header.Set("X-Tenant-ID", handlerTestTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

func TestDictHandler_Update_Returns200(t *testing.T) {
	repo := &fakeHandlerRepo{}
	_, r := makeDictHandler(repo)

	newName := "更新苗木"
	body, _ := json.Marshal(map[string]any{"name": newName})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/nursery-dict/"+uuid.New().String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", handlerTestTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestDictHandler_Delete_Returns204(t *testing.T) {
	repo := &fakeHandlerRepo{}
	_, r := makeDictHandler(repo)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/nursery-dict/"+uuid.New().String(), nil)
	req.Header.Set("X-Tenant-ID", handlerTestTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204; body: %s", w.Code, w.Body.String())
	}
}

func TestDictHandler_Restore_Returns200(t *testing.T) {
	repo := &fakeHandlerRepo{}
	_, r := makeDictHandler(repo)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/nursery-dict/"+uuid.New().String()+"/restore", nil)
	req.Header.Set("X-Tenant-ID", handlerTestTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

// Compile-time check.
var _ apphort.Repository = (*fakeHandlerRepo)(nil)
