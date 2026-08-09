package unit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	repounit "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/unit"
	appunit "github.com/hanmahong5-arch/lurus-tally/internal/app/unit"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/unit"
)

// fakeUnitRepo implements appunit.Repository for exercising the handlers through
// real *appunit.CreateUseCase / *appunit.ListUseCase / *appunit.DeleteUseCase
// instances, without touching a real database.
type fakeUnitRepo struct {
	mu sync.Mutex

	createErr       error
	createCalled    bool
	lastCreateInput *domain.UnitDef

	listResult     []*domain.UnitDef
	listErr        error
	listCalled     bool
	lastListFilter domain.ListFilter

	getByIDResult *domain.UnitDef
	getByIDErr    error
	lastGetTenant uuid.UUID
	lastGetID     uuid.UUID

	deleteErr        error
	deleteCalled     bool
	lastDeleteTenant uuid.UUID
	lastDeleteID     uuid.UUID
}

func (f *fakeUnitRepo) Create(_ context.Context, u *domain.UnitDef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalled = true
	f.lastCreateInput = u
	return f.createErr
}

func (f *fakeUnitRepo) List(_ context.Context, filter domain.ListFilter) ([]*domain.UnitDef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalled = true
	f.lastListFilter = filter
	return f.listResult, f.listErr
}

func (f *fakeUnitRepo) GetByID(_ context.Context, tenantID, id uuid.UUID) (*domain.UnitDef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastGetTenant = tenantID
	f.lastGetID = id
	return f.getByIDResult, f.getByIDErr
}

func (f *fakeUnitRepo) Delete(_ context.Context, tenantID, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteCalled = true
	f.lastDeleteTenant = tenantID
	f.lastDeleteID = id
	return f.deleteErr
}

var errRepoBoom = errors.New("repo boom")

func init() {
	gin.SetMode(gin.TestMode)
}

// newTestContext builds a gin.Context wired to a fresh httptest recorder.
// When tenantID != uuid.Nil it is injected the same way AuthMiddleware would.
func newTestContext(method, target string, body []byte, tenantID uuid.UUID) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	var reader *bytes.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, target, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.Request = req

	if tenantID != uuid.Nil {
		c.Set(middleware.CtxKeyTenantID, tenantID)
	}
	return c, w
}

// flushStatus forces gin to flush a status set via c.Status() (no body written)
// to the underlying httptest recorder. gin normally does this in
// (*Engine).ServeHTTP after the handler chain returns; since these tests call
// handler methods directly, WriteHeaderNow never fires on its own for 204s.
func flushStatus(c *gin.Context) {
	c.Writer.WriteHeaderNow()
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if w.Body.Len() == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("response body is not valid JSON: %v (body=%q)", err, w.Body.String())
	}
	return m
}

// ---- Create --------------------------------------------------------------

func TestHandler_Create_NilTenantUnauthorized(t *testing.T) {
	repo := &fakeUnitRepo{}
	h := New(appunit.NewCreateUseCase(repo), appunit.NewListUseCase(repo), appunit.NewDeleteUseCase(repo))

	c, w := newTestContext(http.MethodPost, "/api/v1/units", []byte(`{"code":"PCS","name":"Piece","unit_type":"count"}`), uuid.Nil)
	h.Create(c)
	flushStatus(c)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	body := decodeBody(t, w)
	if body["error"] != "tenant_id required" {
		t.Fatalf("body = %+v, want error=tenant_id required", body)
	}
	if repo.createCalled {
		t.Fatal("cross-tenant guard breached: repo.Create must NOT be called when tenant_id is Nil")
	}
}

func TestHandler_Create_InvalidJSONBody(t *testing.T) {
	repo := &fakeUnitRepo{}
	h := New(appunit.NewCreateUseCase(repo), appunit.NewListUseCase(repo), appunit.NewDeleteUseCase(repo))

	c, w := newTestContext(http.MethodPost, "/api/v1/units", []byte(`{not-json`), uuid.New())
	h.Create(c)
	flushStatus(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	body := decodeBody(t, w)
	errMsg, _ := body["error"].(string)
	if !strings.HasPrefix(errMsg, "invalid request body: ") {
		t.Fatalf("error = %q, want prefix %q", errMsg, "invalid request body: ")
	}
	if repo.createCalled {
		t.Fatal("repo.Create must NOT be called on malformed JSON")
	}
}

func TestHandler_Create_CodeExceedsMaxLengthValidation(t *testing.T) {
	repo := &fakeUnitRepo{}
	h := New(appunit.NewCreateUseCase(repo), appunit.NewListUseCase(repo), appunit.NewDeleteUseCase(repo))

	overlong := strings.Repeat("A", 129) // binding:"max=128"
	payload, err := json.Marshal(map[string]string{
		"code": overlong,
		"name": "Piece",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	c, w := newTestContext(http.MethodPost, "/api/v1/units", payload, uuid.New())
	h.Create(c)
	flushStatus(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	body := decodeBody(t, w)
	errMsg, _ := body["error"].(string)
	if !strings.HasPrefix(errMsg, "invalid request body: ") {
		t.Fatalf("error = %q, want prefix %q", errMsg, "invalid request body: ")
	}
	if repo.createCalled {
		t.Fatal("repo.Create must NOT be called when field validation fails")
	}
}

func TestHandler_Create_UseCaseErrorEchoed(t *testing.T) {
	repo := &fakeUnitRepo{createErr: errRepoBoom}
	h := New(appunit.NewCreateUseCase(repo), appunit.NewListUseCase(repo), appunit.NewDeleteUseCase(repo))

	c, w := newTestContext(http.MethodPost, "/api/v1/units", []byte(`{"code":"PCS","name":"Piece","unit_type":"count"}`), uuid.New())
	h.Create(c)
	flushStatus(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	body := decodeBody(t, w)
	errMsg, _ := body["error"].(string)
	// CreateUseCase.Execute wraps repo errors as "create unit: <cause>: repo boom".
	if !strings.Contains(errMsg, "repo boom") {
		t.Fatalf("error = %q, want it to echo the use case error (contains %q)", errMsg, "repo boom")
	}
	if !repo.createCalled {
		t.Fatal("repo.Create should have been invoked before failing")
	}
}

func TestHandler_Create_SuccessUsesResolvedTenantIDNotBody(t *testing.T) {
	repo := &fakeUnitRepo{}
	h := New(appunit.NewCreateUseCase(repo), appunit.NewListUseCase(repo), appunit.NewDeleteUseCase(repo))

	tenantID := uuid.New()
	// The request body deliberately carries no tenant field: createRequest has none,
	// so there is no way for the client to smuggle a foreign tenant_id in.
	c, w := newTestContext(http.MethodPost, "/api/v1/units", []byte(`{"code":"PCS","name":"Piece","unit_type":"count"}`), tenantID)
	h.Create(c)
	flushStatus(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if repo.lastCreateInput == nil {
		t.Fatal("repo.Create was not invoked")
	}
	// Business invariant: the persisted TenantID is the handler-resolved value,
	// never anything from the JSON body.
	if repo.lastCreateInput.TenantID != tenantID {
		t.Fatalf("persisted TenantID = %v, want the context-injected %v", repo.lastCreateInput.TenantID, tenantID)
	}

	var resp domain.UnitDef
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not a valid UnitDef JSON: %v", err)
	}
	if resp.TenantID != tenantID {
		t.Fatalf("response TenantID = %v, want %v", resp.TenantID, tenantID)
	}
	if resp.Code != "PCS" || resp.Name != "Piece" || resp.UnitType != domain.UnitTypeCount {
		t.Fatalf("response = %+v, want code/name/unit_type echoed", resp)
	}
	if resp.IsSystem {
		t.Fatal("a unit created via this handler must never be system-owned")
	}
}

// ---- List ------------------------------------------------------------------

func TestHandler_List_NilTenantUnauthorized(t *testing.T) {
	repo := &fakeUnitRepo{}
	h := New(appunit.NewCreateUseCase(repo), appunit.NewListUseCase(repo), appunit.NewDeleteUseCase(repo))

	c, w := newTestContext(http.MethodGet, "/api/v1/units", nil, uuid.Nil)
	h.List(c)
	flushStatus(c)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if repo.listCalled {
		t.Fatal("repo.List must NOT be called when tenant_id is Nil")
	}
}

func TestHandler_List_UnitTypeQueryPassthrough(t *testing.T) {
	tests := []struct {
		name     string
		rawQuery string
		want     domain.UnitType
	}{
		{name: "no filter", rawQuery: "", want: domain.UnitType("")},
		{name: "weight filter", rawQuery: "?unit_type=weight", want: domain.UnitTypeWeight},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeUnitRepo{listResult: []*domain.UnitDef{}}
			h := New(appunit.NewCreateUseCase(repo), appunit.NewListUseCase(repo), appunit.NewDeleteUseCase(repo))
			tenantID := uuid.New()

			c, w := newTestContext(http.MethodGet, "/api/v1/units"+tc.rawQuery, nil, tenantID)
			h.List(c)
			flushStatus(c)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
			}
			if !repo.listCalled {
				t.Fatal("repo.List was not invoked")
			}
			if repo.lastListFilter.TenantID != tenantID {
				t.Fatalf("filter.TenantID = %v, want %v", repo.lastListFilter.TenantID, tenantID)
			}
			if repo.lastListFilter.UnitType != tc.want {
				t.Fatalf("filter.UnitType = %q, want %q", repo.lastListFilter.UnitType, tc.want)
			}
		})
	}
}

func TestHandler_List_SuccessEnvelope(t *testing.T) {
	tenantID := uuid.New()
	want := []*domain.UnitDef{
		{ID: uuid.New(), TenantID: tenantID, Code: "PCS", Name: "Piece", UnitType: domain.UnitTypeCount, IsSystem: true},
	}
	repo := &fakeUnitRepo{listResult: want}
	h := New(appunit.NewCreateUseCase(repo), appunit.NewListUseCase(repo), appunit.NewDeleteUseCase(repo))

	c, w := newTestContext(http.MethodGet, "/api/v1/units", nil, tenantID)
	h.List(c)
	flushStatus(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var resp struct {
		Items []domain.UnitDef `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not the expected envelope: %v (body=%s)", err, w.Body.String())
	}
	if len(resp.Items) != 1 || resp.Items[0].Code != "PCS" {
		t.Fatalf("resp.Items = %+v, want one item with Code=PCS", resp.Items)
	}
}

func TestHandler_List_UseCaseErrorMapsTo500ViaHttperr(t *testing.T) {
	repo := &fakeUnitRepo{listErr: errRepoBoom}
	h := New(appunit.NewCreateUseCase(repo), appunit.NewListUseCase(repo), appunit.NewDeleteUseCase(repo))

	c, w := newTestContext(http.MethodGet, "/api/v1/units", nil, uuid.New())
	h.List(c)
	flushStatus(c)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	body := decodeBody(t, w)
	if body["error"] != "internal_error" {
		t.Fatalf("body = %+v, want error=internal_error (httperr.WriteInternal contract)", body)
	}
	if body["message"] != "an internal error occurred" {
		t.Fatalf("body = %+v, want the generic safe message (cause must not leak)", body)
	}
}

// ---- Delete -----------------------------------------------------------------

func newDeleteContext(idParam string, tenantID uuid.UUID) (*gin.Context, *httptest.ResponseRecorder) {
	c, w := newTestContext(http.MethodDelete, "/api/v1/units/"+idParam, nil, tenantID)
	c.Params = gin.Params{{Key: "id", Value: idParam}}
	return c, w
}

func TestHandler_Delete_NilTenantUnauthorized(t *testing.T) {
	repo := &fakeUnitRepo{}
	h := New(appunit.NewCreateUseCase(repo), appunit.NewListUseCase(repo), appunit.NewDeleteUseCase(repo))

	c, w := newDeleteContext(uuid.New().String(), uuid.Nil)
	h.Delete(c)
	flushStatus(c)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if repo.deleteCalled {
		t.Fatal("cross-tenant guard breached: repo.Delete must NEVER be called when tenant_id is Nil")
	}
}

func TestHandler_Delete_TenantIDPropagatesToExecute(t *testing.T) {
	repo := &fakeUnitRepo{
		getByIDResult: &domain.UnitDef{ID: uuid.New(), IsSystem: false, Code: "CUSTOM"},
	}
	h := New(appunit.NewCreateUseCase(repo), appunit.NewListUseCase(repo), appunit.NewDeleteUseCase(repo))

	tenantID := uuid.New()
	id := uuid.New()
	c, w := newDeleteContext(id.String(), tenantID)
	h.Delete(c)
	flushStatus(c)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusNoContent, w.Body.String())
	}
	if w.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty on 204", w.Body.String())
	}
	if repo.lastGetTenant != tenantID || repo.lastDeleteTenant != tenantID {
		t.Fatalf("tenantID not propagated: getTenant=%v deleteTenant=%v, want %v", repo.lastGetTenant, repo.lastDeleteTenant, tenantID)
	}
	if repo.lastGetID != id || repo.lastDeleteID != id {
		t.Fatalf("id not propagated: getID=%v deleteID=%v, want %v", repo.lastGetID, repo.lastDeleteID, id)
	}
}

func TestHandler_Delete_InvalidUUIDParam(t *testing.T) {
	repo := &fakeUnitRepo{}
	h := New(appunit.NewCreateUseCase(repo), appunit.NewListUseCase(repo), appunit.NewDeleteUseCase(repo))

	c, w := newDeleteContext("not-a-uuid", uuid.New())
	h.Delete(c)
	flushStatus(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	body := decodeBody(t, w)
	if body["error"] != "invalid unit id: must be a UUID" {
		t.Fatalf("body = %+v, want the exact UUID validation message", body)
	}
	if repo.deleteCalled {
		t.Fatal("repo.Delete must NOT be called for a malformed id")
	}
}

func TestHandler_Delete_NotFound(t *testing.T) {
	repo := &fakeUnitRepo{getByIDErr: repounit.ErrNotFound}
	h := New(appunit.NewCreateUseCase(repo), appunit.NewListUseCase(repo), appunit.NewDeleteUseCase(repo))

	c, w := newDeleteContext(uuid.New().String(), uuid.New())
	h.Delete(c)
	flushStatus(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
	body := decodeBody(t, w)
	if body["error"] != "unit not found" {
		t.Fatalf("body = %+v, want error=unit not found", body)
	}
}

func TestHandler_Delete_SystemUnitForbidden(t *testing.T) {
	tests := []struct {
		name string
		repo *fakeUnitRepo
	}{
		{
			name: "GetByID returns a live system unit (real DeleteUseCase system-unit refusal)",
			repo: &fakeUnitRepo{
				getByIDResult: &domain.UnitDef{ID: uuid.New(), IsSystem: true, Code: "KG"},
			},
		},
		{
			name: "repo error wraps the sentinel repounit.ErrSystemUnit",
			repo: &fakeUnitRepo{getByIDErr: repounit.ErrSystemUnit},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := New(appunit.NewCreateUseCase(tc.repo), appunit.NewListUseCase(tc.repo), appunit.NewDeleteUseCase(tc.repo))

			c, w := newDeleteContext(uuid.New().String(), uuid.New())
			h.Delete(c)
			flushStatus(c)

			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusForbidden, w.Body.String())
			}
			body := decodeBody(t, w)
			errMsg, _ := body["error"].(string)
			if !strings.Contains(errMsg, "system unit cannot be deleted") {
				t.Fatalf("error = %q, want it to contain %q", errMsg, "system unit cannot be deleted")
			}
			if tc.repo.deleteCalled {
				t.Fatal("repo.Delete must NEVER be called for a system unit")
			}
		})
	}
}

func TestHandler_Delete_OtherErrorMapsTo500(t *testing.T) {
	repo := &fakeUnitRepo{getByIDErr: errRepoBoom}
	h := New(appunit.NewCreateUseCase(repo), appunit.NewListUseCase(repo), appunit.NewDeleteUseCase(repo))

	c, w := newDeleteContext(uuid.New().String(), uuid.New())
	h.Delete(c)
	flushStatus(c)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusInternalServerError, w.Body.String())
	}
	body := decodeBody(t, w)
	if body["error"] != "internal_error" {
		t.Fatalf("body = %+v, want error=internal_error", body)
	}
}

func TestHandler_Delete_Success(t *testing.T) {
	repo := &fakeUnitRepo{
		getByIDResult: &domain.UnitDef{ID: uuid.New(), IsSystem: false, Code: "CUSTOM"},
	}
	h := New(appunit.NewCreateUseCase(repo), appunit.NewListUseCase(repo), appunit.NewDeleteUseCase(repo))

	c, w := newDeleteContext(uuid.New().String(), uuid.New())
	h.Delete(c)
	flushStatus(c)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", w.Body.String())
	}
	if !repo.deleteCalled {
		t.Fatal("repo.Delete should have been invoked for a tenant-custom unit")
	}
}

// ---- isSystemUnitError / containsString / indexString (table-driven, unexported helpers) --

func TestIsSystemUnitError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil error", err: nil, want: false},
		{name: "wraps sentinel ErrSystemUnit", err: repounit.ErrSystemUnit, want: true},
		{name: "message substring match", err: errors.New("delete unit: system unit \"KG\" cannot be deleted"), want: true},
		{name: "unrelated error", err: errRepoBoom, want: false},
		{name: "not found sentinel is not a system unit error", err: repounit.ErrNotFound, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSystemUnitError(tc.err); got != tc.want {
				t.Fatalf("isSystemUnitError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestContainsString(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		substr string
		want   bool
	}{
		{name: "substr longer than s", s: "ab", substr: "abc", want: false},
		{name: "s equals substr", s: "system unit", substr: "system unit", want: true},
		{name: "substr in the middle", s: "delete unit: system unit cannot be deleted", substr: "system unit", want: true},
		{name: "both empty", s: "", substr: "", want: true},
		{name: "empty substr non-empty s", s: "abc", substr: "", want: true},
		{name: "empty s non-empty substr", s: "", substr: "a", want: false},
		{name: "no match at all", s: "hello world", substr: "xyz", want: false},
		{name: "match at very start", s: "system unit cannot", substr: "system", want: true},
		{name: "match at very end", s: "cannot delete system", substr: "system", want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := containsString(tc.s, tc.substr); got != tc.want {
				t.Fatalf("containsString(%q, %q) = %v, want %v", tc.s, tc.substr, got, tc.want)
			}
		})
	}
}

func TestIndexString(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		substr string
		want   int
	}{
		{name: "found at start", s: "system unit", substr: "system", want: 0},
		{name: "found in middle", s: "a system unit b", substr: "system", want: 2},
		{name: "not found", s: "hello", substr: "xyz", want: -1},
		{name: "substr longer than s", s: "ab", substr: "abcd", want: -1},
		{name: "empty substr matches at 0", s: "abc", substr: "", want: 0},
		{name: "both empty", s: "", substr: "", want: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := indexString(tc.s, tc.substr); got != tc.want {
				t.Fatalf("indexString(%q, %q) = %d, want %d", tc.s, tc.substr, got, tc.want)
			}
		})
	}
}

// ---- resolveTenantID --------------------------------------------------------

func TestResolveTenantID(t *testing.T) {
	t.Run("absent tenant yields Nil", func(t *testing.T) {
		c, _ := newTestContext(http.MethodGet, "/api/v1/units", nil, uuid.Nil)
		if got := resolveTenantID(c); got != uuid.Nil {
			t.Fatalf("resolveTenantID = %v, want uuid.Nil", got)
		}
	})
	t.Run("injected tenant is returned verbatim", func(t *testing.T) {
		tenantID := uuid.New()
		c, _ := newTestContext(http.MethodGet, "/api/v1/units", nil, tenantID)
		if got := resolveTenantID(c); got != tenantID {
			t.Fatalf("resolveTenantID = %v, want %v", got, tenantID)
		}
	})
}
