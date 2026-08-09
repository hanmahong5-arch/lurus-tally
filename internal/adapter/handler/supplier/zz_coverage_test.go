package supplier

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
	appsupp "github.com/hanmahong5-arch/lurus-tally/internal/app/supplier"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/supplier"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ---- fakeRepo: white-box double for appsupp.Repository -------------------

type fakeRepo struct {
	createErr   error
	createdSupp *domain.Supplier
	createCalls int

	getErr   error
	getSupp  *domain.Supplier
	getCalls int

	listErr    error
	listItems  []*domain.Supplier
	listTotal  int
	listFilter domain.ListFilter
	listCalls  int

	updateErr   error
	updatedSupp *domain.Supplier
	updateCalls int

	deleteErr   error
	deleteCalls int

	restoreErr   error
	restoreSupp  *domain.Supplier
	restoreCalls int
}

func (f *fakeRepo) Create(_ context.Context, s *domain.Supplier) error {
	f.createCalls++
	if f.createErr != nil {
		return f.createErr
	}
	f.createdSupp = s
	return nil
}

func (f *fakeRepo) GetByID(_ context.Context, _, _ uuid.UUID) (*domain.Supplier, error) {
	f.getCalls++
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.getSupp != nil {
		return f.getSupp, nil
	}
	return &domain.Supplier{
		ID:        uuid.New(),
		TenantID:  uuid.New(),
		Name:      "default-supplier",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}, nil
}

func (f *fakeRepo) List(_ context.Context, filt domain.ListFilter) ([]*domain.Supplier, int, error) {
	f.listCalls++
	f.listFilter = filt
	if f.listErr != nil {
		return nil, 0, f.listErr
	}
	return f.listItems, f.listTotal, nil
}

func (f *fakeRepo) Update(_ context.Context, s *domain.Supplier) error {
	f.updateCalls++
	if f.updateErr != nil {
		return f.updateErr
	}
	f.updatedSupp = s
	return nil
}

func (f *fakeRepo) Delete(_ context.Context, _, _ uuid.UUID) error {
	f.deleteCalls++
	return f.deleteErr
}

func (f *fakeRepo) Restore(_ context.Context, _, _ uuid.UUID) (*domain.Supplier, error) {
	f.restoreCalls++
	if f.restoreErr != nil {
		return nil, f.restoreErr
	}
	if f.restoreSupp != nil {
		return f.restoreSupp, nil
	}
	return &domain.Supplier{
		ID:        uuid.New(),
		TenantID:  uuid.New(),
		Name:      "restored-supplier",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}, nil
}

var _ appsupp.Repository = (*fakeRepo)(nil)

func newTestHandler(repo *fakeRepo) *Handler {
	return New(
		appsupp.NewCreateUseCase(repo),
		appsupp.NewGetByIDUseCase(repo),
		appsupp.NewListUseCase(repo),
		appsupp.NewUpdateUseCase(repo),
		appsupp.NewDeleteUseCase(repo),
		appsupp.NewRestoreUseCase(repo),
	)
}

// buildCtx builds a bare gin.Context (no router/middleware) so each handler
// method can be invoked directly, giving precise control over the tenant id
// and the raw HTTP request (method/target/body/params).
func buildCtx(method, target string, body []byte, tenantID *uuid.UUID, idParam string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, target, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	c.Request = req

	if tenantID != nil {
		c.Set(middleware.CtxKeyTenantID, *tenantID)
	}
	if idParam != "" {
		c.Params = gin.Params{{Key: "id", Value: idParam}}
	}
	return c, w
}

func decodeMap(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode body %q: %v", w.Body.String(), err)
	}
	return m
}

// ---------------------------------------------------------------------------
// Cross-cutting invariant: tenantID == uuid.Nil -> 401, use case NOT invoked.
// ---------------------------------------------------------------------------

func TestHandler_TenantNil_Returns401AndSkipsUseCase(t *testing.T) {
	someID := uuid.New().String()

	cases := []struct {
		name string
		call func(h *Handler, c *gin.Context)
	}{
		{"List", func(h *Handler, c *gin.Context) { h.List(c) }},
		{"Create", func(h *Handler, c *gin.Context) { h.Create(c) }},
		{"GetByID", func(h *Handler, c *gin.Context) { h.GetByID(c) }},
		{"Update", func(h *Handler, c *gin.Context) { h.Update(c) }},
		{"Delete", func(h *Handler, c *gin.Context) { h.Delete(c) }},
		{"Restore", func(h *Handler, c *gin.Context) { h.Restore(c) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{}
			h := newTestHandler(repo)
			body, _ := json.Marshal(map[string]any{"name": "x"})
			c, w := buildCtx(http.MethodGet, "/api/v1/suppliers", body, nil, someID)
			// No tenant set at all (uuid.Nil default from GetTenantID).
			tc.call(h, c)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401; body=%s", w.Code, w.Body.String())
			}
			if repo.createCalls+repo.getCalls+repo.listCalls+repo.updateCalls+repo.deleteCalls+repo.restoreCalls != 0 {
				t.Fatalf("expected zero repo calls, got create=%d get=%d list=%d update=%d delete=%d restore=%d",
					repo.createCalls, repo.getCalls, repo.listCalls, repo.updateCalls, repo.deleteCalls, repo.restoreCalls)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Create
// ---------------------------------------------------------------------------

func TestHandler_Create_MissingName_400(t *testing.T) {
	tenantID := uuid.New()
	repo := &fakeRepo{}
	h := newTestHandler(repo)
	body, _ := json.Marshal(map[string]any{"code": "S1"})
	c, w := buildCtx(http.MethodPost, "/api/v1/suppliers", body, &tenantID, "")

	h.Create(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	if repo.createCalls != 0 {
		t.Fatalf("expected Create not invoked when binding fails, got %d calls", repo.createCalls)
	}
}

func TestHandler_Create_NameOverMax128_400(t *testing.T) {
	tenantID := uuid.New()
	repo := &fakeRepo{}
	h := newTestHandler(repo)
	body, _ := json.Marshal(map[string]any{"name": strings.Repeat("a", 129)})
	c, w := buildCtx(http.MethodPost, "/api/v1/suppliers", body, &tenantID, "")

	h.Create(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestHandler_Create_DuplicateName_409(t *testing.T) {
	tenantID := uuid.New()
	repo := &fakeRepo{createErr: appsupp.ErrDuplicateName}
	h := newTestHandler(repo)
	body, _ := json.Marshal(map[string]any{"name": "dup-name"})
	c, w := buildCtx(http.MethodPost, "/api/v1/suppliers", body, &tenantID, "")

	h.Create(c)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", w.Code, w.Body.String())
	}
	m := decodeMap(t, w)
	if m["error"] != "duplicate supplier name" {
		t.Errorf("error = %v, want %q", m["error"], "duplicate supplier name")
	}
}

func TestHandler_Create_OtherRepoErr_400Echoes(t *testing.T) {
	tenantID := uuid.New()
	repo := &fakeRepo{createErr: errors.New("boom: constraint xyz")}
	h := newTestHandler(repo)
	body, _ := json.Marshal(map[string]any{"name": "n"})
	c, w := buildCtx(http.MethodPost, "/api/v1/suppliers", body, &tenantID, "")

	h.Create(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	m := decodeMap(t, w)
	errMsg, _ := m["error"].(string)
	if !strings.Contains(errMsg, "boom: constraint xyz") {
		t.Errorf("error = %q, want it to echo underlying cause", errMsg)
	}
}

func TestHandler_Create_Success_201LocationAndBody(t *testing.T) {
	tenantID := uuid.New()
	repo := &fakeRepo{}
	h := newTestHandler(repo)
	body, _ := json.Marshal(map[string]any{
		"code":    "S001",
		"name":    "深圳供应商A",
		"contact": "张三",
		"phone":   "13800000000",
		"email":   "a@example.com",
		"address": "深圳市南山区",
		"remark":  "vip",
	})
	c, w := buildCtx(http.MethodPost, "/api/v1/suppliers", body, &tenantID, "")

	h.Create(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	if repo.createCalls != 1 {
		t.Fatalf("createCalls = %d, want 1", repo.createCalls)
	}

	m := decodeMap(t, w)
	id, _ := m["id"].(string)
	if id == "" {
		t.Fatal("missing id in response body")
	}
	wantLocation := "/api/v1/suppliers/" + id
	if loc := w.Header().Get("Location"); loc != wantLocation {
		t.Errorf("Location = %q, want %q", loc, wantLocation)
	}
	if m["tenant_id"] != tenantID.String() {
		t.Errorf("tenant_id = %v, want %s", m["tenant_id"], tenantID.String())
	}
	if m["code"] != "S001" || m["name"] != "深圳供应商A" || m["contact"] != "张三" {
		t.Errorf("unexpected echoed fields: %+v", m)
	}
	if _, ok := m["created_at"]; !ok {
		t.Error("missing created_at")
	}
	if _, ok := m["updated_at"]; !ok {
		t.Error("missing updated_at")
	}
}

func TestHandler_Create_OptionalFieldsEmpty_OmittedFromJSON(t *testing.T) {
	tenantID := uuid.New()
	repo := &fakeRepo{}
	h := newTestHandler(repo)
	body, _ := json.Marshal(map[string]any{"name": "仅名称"})
	c, w := buildCtx(http.MethodPost, "/api/v1/suppliers", body, &tenantID, "")

	h.Create(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	m := decodeMap(t, w)
	for _, key := range []string{"code", "contact", "phone", "email", "address", "remark"} {
		if _, present := m[key]; present {
			t.Errorf("expected omitempty field %q to be absent, got %v", key, m[key])
		}
	}
}

// ---------------------------------------------------------------------------
// GetByID
// ---------------------------------------------------------------------------

func TestHandler_GetByID_InvalidID_400(t *testing.T) {
	tenantID := uuid.New()
	repo := &fakeRepo{}
	h := newTestHandler(repo)
	c, w := buildCtx(http.MethodGet, "/api/v1/suppliers/not-a-uuid", nil, &tenantID, "not-a-uuid")

	h.GetByID(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	m := decodeMap(t, w)
	if m["error"] != "invalid id" {
		t.Errorf("error = %v, want 'invalid id'", m["error"])
	}
	if repo.getCalls != 0 {
		t.Errorf("expected GetByID use case not invoked, got %d calls", repo.getCalls)
	}
}

func TestHandler_GetByID_NotFound_404(t *testing.T) {
	tenantID := uuid.New()
	repo := &fakeRepo{getErr: appsupp.ErrNotFound}
	h := newTestHandler(repo)
	id := uuid.New().String()
	c, w := buildCtx(http.MethodGet, "/api/v1/suppliers/"+id, nil, &tenantID, id)

	h.GetByID(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", w.Code, w.Body.String())
	}
}

func TestHandler_GetByID_OtherErr_500(t *testing.T) {
	tenantID := uuid.New()
	repo := &fakeRepo{getErr: errors.New("db down")}
	h := newTestHandler(repo)
	id := uuid.New().String()
	c, w := buildCtx(http.MethodGet, "/api/v1/suppliers/"+id, nil, &tenantID, id)

	h.GetByID(c)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", w.Code, w.Body.String())
	}
	m := decodeMap(t, w)
	if m["error"] != "internal_error" {
		t.Errorf("error code = %v, want internal_error", m["error"])
	}
}

func TestHandler_GetByID_Success_200(t *testing.T) {
	tenantID := uuid.New()
	fixed := time.Date(2024, 3, 15, 10, 30, 0, 0, time.UTC)
	want := &domain.Supplier{
		ID:        uuid.New(),
		TenantID:  tenantID,
		Name:      "供应商X",
		CreatedAt: fixed,
		UpdatedAt: fixed,
	}
	repo := &fakeRepo{getSupp: want}
	h := newTestHandler(repo)
	c, w := buildCtx(http.MethodGet, "/api/v1/suppliers/"+want.ID.String(), nil, &tenantID, want.ID.String())

	h.GetByID(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	m := decodeMap(t, w)
	if m["id"] != want.ID.String() {
		t.Errorf("id = %v, want %s", m["id"], want.ID.String())
	}
	if m["created_at"] != fixed.Format(time.RFC3339) {
		t.Errorf("created_at = %v, want %s", m["created_at"], fixed.Format(time.RFC3339))
	}
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

func TestHandler_Update_InvalidID_400(t *testing.T) {
	tenantID := uuid.New()
	repo := &fakeRepo{}
	h := newTestHandler(repo)
	body, _ := json.Marshal(map[string]any{})
	c, w := buildCtx(http.MethodPut, "/api/v1/suppliers/bad", body, &tenantID, "bad")

	h.Update(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	m := decodeMap(t, w)
	if m["error"] != "invalid id" {
		t.Errorf("error = %v, want 'invalid id'", m["error"])
	}
}

func TestHandler_Update_BindFail_NameOverMax_400(t *testing.T) {
	tenantID := uuid.New()
	repo := &fakeRepo{}
	h := newTestHandler(repo)
	id := uuid.New().String()
	body, _ := json.Marshal(map[string]any{"name": strings.Repeat("z", 129)})
	c, w := buildCtx(http.MethodPut, "/api/v1/suppliers/"+id, body, &tenantID, id)

	h.Update(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	if repo.getCalls != 0 {
		t.Errorf("expected use case not invoked on bind failure, got getCalls=%d", repo.getCalls)
	}
}

func TestHandler_Update_FetchNotFound_404(t *testing.T) {
	tenantID := uuid.New()
	repo := &fakeRepo{getErr: appsupp.ErrNotFound}
	h := newTestHandler(repo)
	id := uuid.New().String()
	body, _ := json.Marshal(map[string]any{"name": "new-name"})
	c, w := buildCtx(http.MethodPut, "/api/v1/suppliers/"+id, body, &tenantID, id)

	h.Update(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", w.Code, w.Body.String())
	}
}

func TestHandler_Update_FetchOtherErr_400NotInternal(t *testing.T) {
	// Per handler source: Update's non-ErrNotFound errors ALL fall to 400,
	// unlike Get/Delete/Restore which use httperr.WriteInternal (500).
	tenantID := uuid.New()
	repo := &fakeRepo{getErr: errors.New("db unreachable")}
	h := newTestHandler(repo)
	id := uuid.New().String()
	body, _ := json.Marshal(map[string]any{"name": "new-name"})
	c, w := buildCtx(http.MethodPut, "/api/v1/suppliers/"+id, body, &tenantID, id)

	h.Update(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (not 500); body=%s", w.Code, w.Body.String())
	}
}

func TestHandler_Update_ValidateFail_EmptyName_400(t *testing.T) {
	tenantID := uuid.New()
	base := &domain.Supplier{ID: uuid.New(), TenantID: tenantID, Name: "orig", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	repo := &fakeRepo{getSupp: base}
	h := newTestHandler(repo)
	empty := ""
	body, _ := json.Marshal(map[string]any{"name": &empty})
	c, w := buildCtx(http.MethodPut, "/api/v1/suppliers/"+base.ID.String(), body, &tenantID, base.ID.String())

	h.Update(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	if repo.updateCalls != 0 {
		t.Errorf("expected repo.Update not reached after domain Validate() fails, got %d calls", repo.updateCalls)
	}
}

func TestHandler_Update_RepoUpdateOtherErr_400(t *testing.T) {
	tenantID := uuid.New()
	base := &domain.Supplier{ID: uuid.New(), TenantID: tenantID, Name: "orig", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	repo := &fakeRepo{getSupp: base, updateErr: errors.New("write failed")}
	h := newTestHandler(repo)
	body, _ := json.Marshal(map[string]any{"name": "renamed"})
	c, w := buildCtx(http.MethodPut, "/api/v1/suppliers/"+base.ID.String(), body, &tenantID, base.ID.String())

	h.Update(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestHandler_Update_Success_200(t *testing.T) {
	tenantID := uuid.New()
	base := &domain.Supplier{ID: uuid.New(), TenantID: tenantID, Name: "orig", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	repo := &fakeRepo{getSupp: base}
	h := newTestHandler(repo)
	body, _ := json.Marshal(map[string]any{"name": "renamed-ok"})
	c, w := buildCtx(http.MethodPut, "/api/v1/suppliers/"+base.ID.String(), body, &tenantID, base.ID.String())

	h.Update(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	m := decodeMap(t, w)
	if m["name"] != "renamed-ok" {
		t.Errorf("name = %v, want renamed-ok", m["name"])
	}
	if repo.updateCalls != 1 {
		t.Errorf("updateCalls = %d, want 1", repo.updateCalls)
	}
}

// ---------------------------------------------------------------------------
// Delete
// ---------------------------------------------------------------------------

func TestHandler_Delete_InvalidID_400(t *testing.T) {
	tenantID := uuid.New()
	repo := &fakeRepo{}
	h := newTestHandler(repo)
	c, w := buildCtx(http.MethodDelete, "/api/v1/suppliers/bad", nil, &tenantID, "bad")

	h.Delete(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestHandler_Delete_NotFound_404(t *testing.T) {
	tenantID := uuid.New()
	repo := &fakeRepo{deleteErr: appsupp.ErrNotFound}
	h := newTestHandler(repo)
	id := uuid.New().String()
	c, w := buildCtx(http.MethodDelete, "/api/v1/suppliers/"+id, nil, &tenantID, id)

	h.Delete(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", w.Code, w.Body.String())
	}
}

func TestHandler_Delete_OtherErr_500(t *testing.T) {
	tenantID := uuid.New()
	repo := &fakeRepo{deleteErr: errors.New("db down")}
	h := newTestHandler(repo)
	id := uuid.New().String()
	c, w := buildCtx(http.MethodDelete, "/api/v1/suppliers/"+id, nil, &tenantID, id)

	h.Delete(c)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

func TestHandler_Delete_Success_204(t *testing.T) {
	tenantID := uuid.New()
	repo := &fakeRepo{}
	h := newTestHandler(repo)
	id := uuid.New().String()
	c, w := buildCtx(http.MethodDelete, "/api/v1/suppliers/"+id, nil, &tenantID, id)

	h.Delete(c)
	// c.Status() alone only buffers the header inside gin's responseWriter;
	// it is normally flushed by the engine after all handlers run. Since this
	// test invokes the handler directly (no engine.ServeHTTP), flush explicitly
	// so the underlying httptest.ResponseRecorder reflects the real status.
	c.Writer.WriteHeaderNow()

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", w.Code, w.Body.String())
	}
	if repo.deleteCalls != 1 {
		t.Errorf("deleteCalls = %d, want 1", repo.deleteCalls)
	}
}

// ---------------------------------------------------------------------------
// Restore
// ---------------------------------------------------------------------------

func TestHandler_Restore_InvalidID_400(t *testing.T) {
	tenantID := uuid.New()
	repo := &fakeRepo{}
	h := newTestHandler(repo)
	c, w := buildCtx(http.MethodPost, "/api/v1/suppliers/bad/restore", nil, &tenantID, "bad")

	h.Restore(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestHandler_Restore_NotFound_404(t *testing.T) {
	tenantID := uuid.New()
	repo := &fakeRepo{restoreErr: appsupp.ErrNotFound}
	h := newTestHandler(repo)
	id := uuid.New().String()
	c, w := buildCtx(http.MethodPost, "/api/v1/suppliers/"+id+"/restore", nil, &tenantID, id)

	h.Restore(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", w.Code, w.Body.String())
	}
}

func TestHandler_Restore_OtherErr_500(t *testing.T) {
	tenantID := uuid.New()
	repo := &fakeRepo{restoreErr: errors.New("db down")}
	h := newTestHandler(repo)
	id := uuid.New().String()
	c, w := buildCtx(http.MethodPost, "/api/v1/suppliers/"+id+"/restore", nil, &tenantID, id)

	h.Restore(c)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

func TestHandler_Restore_Success_200(t *testing.T) {
	tenantID := uuid.New()
	restored := &domain.Supplier{ID: uuid.New(), TenantID: tenantID, Name: "back-again", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	repo := &fakeRepo{restoreSupp: restored}
	h := newTestHandler(repo)
	c, w := buildCtx(http.MethodPost, "/api/v1/suppliers/"+restored.ID.String()+"/restore", nil, &tenantID, restored.ID.String())

	h.Restore(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	m := decodeMap(t, w)
	if m["name"] != "back-again" {
		t.Errorf("name = %v, want back-again", m["name"])
	}
	if repo.restoreCalls != 1 {
		t.Errorf("restoreCalls = %d, want 1", repo.restoreCalls)
	}
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

func TestHandler_List_Success_TotalIndependentOfItemsLen(t *testing.T) {
	tenantID := uuid.New()
	items := []*domain.Supplier{
		{ID: uuid.New(), TenantID: tenantID, Name: "a", CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}
	repo := &fakeRepo{listItems: items, listTotal: 42}
	h := newTestHandler(repo)
	c, w := buildCtx(http.MethodGet, "/api/v1/suppliers", nil, &tenantID, "")

	h.List(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp SupplierListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Errorf("len(items) = %d, want 1", len(resp.Items))
	}
	if resp.Total != 42 {
		t.Errorf("total = %d, want 42 (independent of items length)", resp.Total)
	}
}

func TestHandler_List_EmptyItems_ReturnsEmptySliceNotNull(t *testing.T) {
	tenantID := uuid.New()
	repo := &fakeRepo{listItems: nil, listTotal: 0}
	h := newTestHandler(repo)
	c, w := buildCtx(http.MethodGet, "/api/v1/suppliers", nil, &tenantID, "")

	h.List(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"items":null`) {
		t.Errorf("items should be an empty array, not null; body=%s", w.Body.String())
	}
}

func TestHandler_List_OtherErr_500(t *testing.T) {
	tenantID := uuid.New()
	repo := &fakeRepo{listErr: errors.New("db down")}
	h := newTestHandler(repo)
	c, w := buildCtx(http.MethodGet, "/api/v1/suppliers", nil, &tenantID, "")

	h.List(c)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

func TestHandler_List_QueryPassthroughAndPagination(t *testing.T) {
	tenantID := uuid.New()
	repo := &fakeRepo{}
	h := newTestHandler(repo)
	c, w := buildCtx(http.MethodGet, "/api/v1/suppliers?q=foo&limit=5&offset=10", nil, &tenantID, "")

	h.List(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if repo.listFilter.Query != "foo" {
		t.Errorf("Query = %q, want foo", repo.listFilter.Query)
	}
	if repo.listFilter.Limit != 5 {
		t.Errorf("Limit = %d, want 5", repo.listFilter.Limit)
	}
	if repo.listFilter.Offset != 10 {
		t.Errorf("Offset = %d, want 10", repo.listFilter.Offset)
	}
	if repo.listFilter.TenantID != tenantID {
		t.Errorf("TenantID = %v, want %v (tenant must propagate to use case)", repo.listFilter.TenantID, tenantID)
	}
}

func TestHandler_List_PaginationDefaultsWhenMissing(t *testing.T) {
	tenantID := uuid.New()
	repo := &fakeRepo{}
	h := newTestHandler(repo)
	c, w := buildCtx(http.MethodGet, "/api/v1/suppliers", nil, &tenantID, "")

	h.List(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if repo.listFilter.Limit != 20 {
		t.Errorf("Limit = %d, want default 20", repo.listFilter.Limit)
	}
	if repo.listFilter.Offset != 0 {
		t.Errorf("Offset = %d, want default 0", repo.listFilter.Offset)
	}
}

func TestHandler_List_PaginationInvalidOrNegative_FallsBackToDefault(t *testing.T) {
	tenantID := uuid.New()
	repo := &fakeRepo{}
	h := newTestHandler(repo)
	c, w := buildCtx(http.MethodGet, "/api/v1/suppliers?limit=not-a-number&offset=-5", nil, &tenantID, "")

	h.List(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (bad pagination params must not error)", w.Code)
	}
	if repo.listFilter.Limit != 20 {
		t.Errorf("Limit = %d, want default 20 on non-numeric input", repo.listFilter.Limit)
	}
	if repo.listFilter.Offset != 0 {
		t.Errorf("Offset = %d, want default 0 on negative input", repo.listFilter.Offset)
	}
}

// ---------------------------------------------------------------------------
// queryInt: direct table-driven coverage of the unexported helper.
// ---------------------------------------------------------------------------

func TestQueryInt_TableDriven(t *testing.T) {
	cases := []struct {
		name  string
		query string
		key   string
		def   int
		want  int
	}{
		{"missing_key_uses_default", "", "limit", 20, 20},
		{"valid_positive", "limit=5", "limit", 20, 5},
		{"zero_is_a_valid_value_not_default", "limit=0", "limit", 20, 0},
		{"non_numeric_falls_back_to_default", "limit=abc", "limit", 20, 20},
		{"negative_falls_back_to_default", "limit=-1", "limit", 20, 20},
		{"large_value_passthrough", "limit=1000", "limit", 20, 1000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := "/api/v1/suppliers"
			if tc.query != "" {
				target += "?" + tc.query
			}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, target, nil)

			got := queryInt(c, tc.key, tc.def)
			if got != tc.want {
				t.Errorf("queryInt(%q) = %d, want %d", tc.query, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// toDTO: direct coverage of RFC3339 time formatting and omitempty behaviour.
// ---------------------------------------------------------------------------

func TestToDTO_TimeFormattingAndOmitempty(t *testing.T) {
	created := time.Date(2024, 3, 15, 10, 30, 0, 0, time.UTC)
	updated := created.Add(2 * time.Hour)

	full := &domain.Supplier{
		ID:        uuid.New(),
		TenantID:  uuid.New(),
		Code:      "C1",
		Name:      "N1",
		Contact:   "Ct1",
		Phone:     "P1",
		Email:     "E1",
		Address:   "A1",
		Remark:    "R1",
		CreatedAt: created,
		UpdatedAt: updated,
	}
	dtoFull := toDTO(full)
	if dtoFull.CreatedAt != created.Format(time.RFC3339) {
		t.Errorf("CreatedAt = %q, want %q", dtoFull.CreatedAt, created.Format(time.RFC3339))
	}
	if dtoFull.UpdatedAt != updated.Format(time.RFC3339) {
		t.Errorf("UpdatedAt = %q, want %q", dtoFull.UpdatedAt, updated.Format(time.RFC3339))
	}
	fullJSON, err := json.Marshal(dtoFull)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fullMap map[string]any
	if err := json.Unmarshal(fullJSON, &fullMap); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"code", "contact", "phone", "email", "address", "remark"} {
		if _, ok := fullMap[key]; !ok {
			t.Errorf("expected populated field %q present in JSON, got %s", key, fullJSON)
		}
	}

	sparse := &domain.Supplier{
		ID:        uuid.New(),
		TenantID:  uuid.New(),
		Name:      "N2",
		CreatedAt: created,
		UpdatedAt: updated,
	}
	dtoSparse := toDTO(sparse)
	sparseJSON, err := json.Marshal(dtoSparse)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var sparseMap map[string]any
	if err := json.Unmarshal(sparseJSON, &sparseMap); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"code", "contact", "phone", "email", "address", "remark"} {
		if _, ok := sparseMap[key]; ok {
			t.Errorf("expected empty omitempty field %q absent from JSON, got %s", key, sparseJSON)
		}
	}
	// Required fields (no omitempty) must always be present, even if empty.
	for _, key := range []string{"id", "tenant_id", "name", "created_at", "updated_at"} {
		if _, ok := sparseMap[key]; !ok {
			t.Errorf("expected required field %q present in JSON, got %s", key, sparseJSON)
		}
	}
}
