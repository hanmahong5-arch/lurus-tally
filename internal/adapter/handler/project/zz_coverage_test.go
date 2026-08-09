package project

// Internal (white-box) test file — same package as handler.go, so it can reach
// the unexported helpers (parseOptionalDate, queryInt) directly. Coexists with
// the external handler_test.go (package project_test) in the same directory;
// this file must not duplicate any identifier declared there.

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
	appproject "github.com/hanmahong5-arch/lurus-tally/internal/app/project"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/project"
)

func init() {
	gin.SetMode(gin.TestMode)
}

const zzTenant = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"

// ---- fake repository (distinct name/fields from handler_test.go's fakeHandlerRepo) ----

type zzRepo struct {
	createErr error
	created   *domain.Project

	getProject *domain.Project
	getErr     error

	listItems  []*domain.Project
	listTotal  int
	listErr    error
	lastFilter domain.ListFilter

	updateErr error
	updated   *domain.Project

	deleteErr error

	restoreProject *domain.Project
	restoreErr     error
}

func (r *zzRepo) Create(_ context.Context, p *domain.Project) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.created = p
	return nil
}

func (r *zzRepo) GetByID(_ context.Context, _, _ uuid.UUID) (*domain.Project, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	if r.getProject != nil {
		return r.getProject, nil
	}
	return &domain.Project{
		ID:        uuid.New(),
		TenantID:  uuid.MustParse(zzTenant),
		Code:      "Z001",
		Name:      "默认项目",
		Status:    domain.StatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}, nil
}

func (r *zzRepo) List(_ context.Context, f domain.ListFilter) ([]*domain.Project, int, error) {
	r.lastFilter = f
	if r.listErr != nil {
		return nil, 0, r.listErr
	}
	return r.listItems, r.listTotal, nil
}

func (r *zzRepo) Update(_ context.Context, p *domain.Project) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	r.updated = p
	return nil
}

func (r *zzRepo) Delete(_ context.Context, _, _ uuid.UUID) error {
	return r.deleteErr
}

func (r *zzRepo) Restore(_ context.Context, _, _ uuid.UUID) (*domain.Project, error) {
	if r.restoreErr != nil {
		return nil, r.restoreErr
	}
	if r.restoreProject != nil {
		return r.restoreProject, nil
	}
	return &domain.Project{
		ID:        uuid.New(),
		TenantID:  uuid.MustParse(zzTenant),
		Code:      "Z001",
		Name:      "默认项目",
		Status:    domain.StatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}, nil
}

var _ appproject.Repository = (*zzRepo)(nil)

// zzRouter wires a ProjectHandler over repo behind a tenant-injecting shim
// middleware (mirrors the production AuthMiddleware contract: sets
// middleware.CtxKeyTenantID from a header, so the handlers' uuid.Nil guard
// sees a real tenant when the test supplies X-Tenant-ID).
func zzRouter(repo appproject.Repository) *gin.Engine {
	h := NewProjectHandler(
		appproject.NewCreateUseCase(repo),
		appproject.NewGetByIDUseCase(repo),
		appproject.NewListUseCase(repo),
		appproject.NewUpdateUseCase(repo),
		appproject.NewDeleteUseCase(repo),
		appproject.NewRestoreUseCase(repo),
	)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if tid := c.GetHeader("X-Tenant-ID"); tid != "" {
			if id, err := uuid.Parse(tid); err == nil {
				c.Set(middleware.CtxKeyTenantID, id)
			}
		}
		c.Next()
	})
	grp := r.Group("/api/v1")
	h.RegisterRoutes(grp)
	return r
}

func zzDo(r *gin.Engine, method, path string, body []byte, withTenant bool) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	} else {
		reader = bytes.NewReader([]byte{})
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if withTenant {
		req.Header.Set("X-Tenant-ID", zzTenant)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ---- pure helper: parseOptionalDate ----

func TestZZParseOptionalDate(t *testing.T) {
	tests := []struct {
		name    string
		in      *string
		wantNil bool
		wantErr bool
		wantYMD [3]int
	}{
		{name: "nil pointer", in: nil, wantNil: true},
		{name: "empty string", in: strPtr(""), wantNil: true},
		{name: "invalid calendar date", in: strPtr("2025-13-40"), wantErr: true},
		{name: "garbage", in: strPtr("not-a-date"), wantErr: true},
		{name: "valid date", in: strPtr("2024-05-01"), wantYMD: [3]int{2024, 5, 1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseOptionalDate(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (result=%v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantNil {
				if got != nil {
					t.Fatalf("got = %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("got nil, want non-nil *time.Time")
			}
			y, m, d := got.Date()
			if y != tc.wantYMD[0] || int(m) != tc.wantYMD[1] || d != tc.wantYMD[2] {
				t.Errorf("got %d-%d-%d, want %d-%d-%d", y, m, d, tc.wantYMD[0], tc.wantYMD[1], tc.wantYMD[2])
			}
		})
	}
}

func strPtr(s string) *string { return &s }

// ---- pure helper: queryInt ----

func TestZZQueryInt(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		defaultVal int
		want       int
	}{
		{name: "missing param uses default", url: "/", defaultVal: 20, want: 20},
		{name: "non-numeric falls back to default", url: "/?limit=abc", defaultVal: 20, want: 20},
		{name: "negative falls back to default", url: "/?limit=-5", defaultVal: 20, want: 20},
		{name: "valid positive is used", url: "/?limit=50", defaultVal: 20, want: 50},
		{name: "zero is accepted (not negative)", url: "/?limit=0", defaultVal: 20, want: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, tc.url, nil)
			got := queryInt(c, "limit", tc.defaultVal)
			if got != tc.want {
				t.Errorf("queryInt() = %d, want %d", got, tc.want)
			}
		})
	}
}

// ---- tenant guard: every route must 401 before touching the use case ----

func TestZZ_TenantNilGuard_Returns401(t *testing.T) {
	someID := uuid.New().String()
	tests := []struct {
		name   string
		method string
		path   string
		body   []byte
	}{
		{"List", http.MethodGet, "/api/v1/projects", nil},
		{"Create", http.MethodPost, "/api/v1/projects", []byte(`{"code":"C1","name":"N1"}`)},
		{"GetByID", http.MethodGet, "/api/v1/projects/" + someID, nil},
		{"Update", http.MethodPut, "/api/v1/projects/" + someID, []byte(`{"name":"N2"}`)},
		{"Delete", http.MethodDelete, "/api/v1/projects/" + someID, nil},
		{"Restore", http.MethodPost, "/api/v1/projects/" + someID + "/restore", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &zzRepo{}
			r := zzRouter(repo)
			w := zzDo(r, tc.method, tc.path, tc.body, false)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401; body: %s", w.Code, w.Body.String())
			}
		})
	}
}

// ---- invalid :id path param → 400 ----

func TestZZ_InvalidIDParam_Returns400(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   []byte
	}{
		{"GetByID", http.MethodGet, "/api/v1/projects/not-a-uuid", nil},
		{"Update", http.MethodPut, "/api/v1/projects/not-a-uuid", []byte(`{"name":"N2"}`)},
		{"Delete", http.MethodDelete, "/api/v1/projects/not-a-uuid", nil},
		{"Restore", http.MethodPost, "/api/v1/projects/not-a-uuid/restore", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &zzRepo{}
			r := zzRouter(repo)
			w := zzDo(r, tc.method, tc.path, tc.body, true)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
			}
		})
	}
}

// ---- Create: field-length caps enforced by ShouldBindJSON ----

func TestZZ_Create_FieldCaps_Returns400(t *testing.T) {
	tests := []struct {
		name string
		body map[string]any
	}{
		{"code too long", map[string]any{"code": strings.Repeat("a", 129), "name": "N", "address": "x", "manager": "x", "remark": "x"}},
		{"address too long", map[string]any{"code": "C1", "name": "N", "address": strings.Repeat("a", 2001)}},
		{"manager too long", map[string]any{"code": "C1", "name": "N", "manager": strings.Repeat("a", 129)}},
		{"remark too long", map[string]any{"code": "C1", "name": "N", "remark": strings.Repeat("a", 501)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &zzRepo{}
			r := zzRouter(repo)
			body, _ := json.Marshal(tc.body)
			w := zzDo(r, http.MethodPost, "/api/v1/projects", body, true)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
			}
		})
	}
}

// ---- Create: invalid start_date / end_date → 400 with the documented prefix ----

func TestZZ_Create_InvalidDates_Returns400(t *testing.T) {
	tests := []struct {
		name      string
		body      map[string]any
		wantMsgHas string
	}{
		{"bad start_date", map[string]any{"code": "C1", "name": "N", "start_date": "2025-13-40"}, "invalid start_date"},
		{"bad end_date", map[string]any{"code": "C1", "name": "N", "end_date": "not-a-date"}, "invalid end_date"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &zzRepo{}
			r := zzRouter(repo)
			body, _ := json.Marshal(tc.body)
			w := zzDo(r, http.MethodPost, "/api/v1/projects", body, true)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
			}
			var resp map[string]string
			_ = json.NewDecoder(w.Body).Decode(&resp)
			if !strings.Contains(resp["error"], tc.wantMsgHas) {
				t.Errorf("error = %q, want prefix containing %q", resp["error"], tc.wantMsgHas)
			}
		})
	}
}

// ---- Create: ContractAmount round-trips byte-identical (decimal-as-string, never float) ----

func TestZZ_Create_ContractAmountAndCustomerID_RoundTrip(t *testing.T) {
	repo := &zzRepo{}
	r := zzRouter(repo)
	customerID := uuid.New().String()
	body, _ := json.Marshal(map[string]any{
		"code":            "C1",
		"name":            "N1",
		"customer_id":     customerID,
		"contract_amount": "12345.67",
	})
	w := zzDo(r, http.MethodPost, "/api/v1/projects", body, true)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	var dto ProjectDTO
	if err := json.NewDecoder(w.Body).Decode(&dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dto.ContractAmount == nil || *dto.ContractAmount != "12345.67" {
		t.Errorf("ContractAmount = %v, want \"12345.67\" (byte-identical, no float parsing)", dto.ContractAmount)
	}
	if dto.CustomerID == nil || *dto.CustomerID != customerID {
		t.Errorf("CustomerID = %v, want %q", dto.CustomerID, customerID)
	}
	// Verify the repo actually received the raw string, never a parsed float.
	if repo.created == nil || repo.created.ContractAmount == nil || *repo.created.ContractAmount != "12345.67" {
		t.Errorf("repo received ContractAmount = %v, want \"12345.67\"", repo.created.ContractAmount)
	}
}

// ---- Create: malformed customer_id is silently ignored (lenient parse), create still succeeds ----

func TestZZ_Create_MalformedCustomerID_SilentlyIgnored_StillSucceeds(t *testing.T) {
	repo := &zzRepo{}
	r := zzRouter(repo)
	body, _ := json.Marshal(map[string]any{
		"code":        "C1",
		"name":        "N1",
		"customer_id": "not-a-uuid",
	})
	w := zzDo(r, http.MethodPost, "/api/v1/projects", body, true)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	var dto ProjectDTO
	if err := json.NewDecoder(w.Body).Decode(&dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dto.CustomerID != nil {
		t.Errorf("CustomerID = %v, want nil (malformed input silently dropped)", *dto.CustomerID)
	}
}

// ---- GetByID: success path exercises toDTO's optional-field branches ----

func TestZZ_GetByID_Success_RendersOptionalFields(t *testing.T) {
	cid := uuid.New()
	sd := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)
	ed := time.Date(2024, 6, 30, 0, 0, 0, 0, time.UTC)
	amount := "999.99"
	repo := &zzRepo{getProject: &domain.Project{
		ID:             uuid.New(),
		TenantID:       uuid.MustParse(zzTenant),
		Code:           "C2",
		Name:           "N2",
		CustomerID:     &cid,
		ContractAmount: &amount,
		StartDate:      &sd,
		EndDate:        &ed,
		Status:         domain.StatusActive,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}}
	r := zzRouter(repo)
	w := zzDo(r, http.MethodGet, "/api/v1/projects/"+uuid.New().String(), nil, true)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var dto ProjectDTO
	if err := json.NewDecoder(w.Body).Decode(&dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dto.CustomerID == nil || *dto.CustomerID != cid.String() {
		t.Errorf("CustomerID = %v, want %q", dto.CustomerID, cid.String())
	}
	if dto.StartDate == nil || *dto.StartDate != "2024-03-15" {
		t.Errorf("StartDate = %v, want 2024-03-15", dto.StartDate)
	}
	if dto.EndDate == nil || *dto.EndDate != "2024-06-30" {
		t.Errorf("EndDate = %v, want 2024-06-30", dto.EndDate)
	}
	if dto.ContractAmount == nil || *dto.ContractAmount != "999.99" {
		t.Errorf("ContractAmount = %v, want 999.99", dto.ContractAmount)
	}
}

// ---- GetByID: non-NotFound repo error maps to 500 via httperr.WriteInternal ----

func TestZZ_GetByID_InternalError_Returns500(t *testing.T) {
	repo := &zzRepo{getErr: errors.New("connection reset")}
	r := zzRouter(repo)
	w := zzDo(r, http.MethodGet, "/api/v1/projects/"+uuid.New().String(), nil, true)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
}

// ---- Update: malformed JSON body → 400 ----

func TestZZ_Update_BadJSON_Returns400(t *testing.T) {
	repo := &zzRepo{}
	r := zzRouter(repo)
	w := zzDo(r, http.MethodPut, "/api/v1/projects/"+uuid.New().String(), []byte(`{`), true)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

// ---- Update: fetch NotFound → 404 ----

func TestZZ_Update_NotFound_Returns404(t *testing.T) {
	repo := &zzRepo{getErr: appproject.ErrNotFound}
	r := zzRouter(repo)
	body, _ := json.Marshal(map[string]any{"name": "N2"})
	w := zzDo(r, http.MethodPut, "/api/v1/projects/"+uuid.New().String(), body, true)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

// ---- Update: invalid start_date / end_date → 400 ----

func TestZZ_Update_InvalidDates_Returns400(t *testing.T) {
	tests := []struct {
		name string
		body map[string]any
	}{
		{"bad start_date", map[string]any{"start_date": "2025-13-40"}},
		{"bad end_date", map[string]any{"end_date": "not-a-date"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &zzRepo{}
			r := zzRouter(repo)
			body, _ := json.Marshal(tc.body)
			w := zzDo(r, http.MethodPut, "/api/v1/projects/"+uuid.New().String(), body, true)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
			}
		})
	}
}

// ---- Update: status-machine invariant (app/project.UpdateUseCase + domain CanTransitionTo) ----
//
// completed → active is illegal (not in the defined state machine) and must
// surface as 400; active → archived is legal and must surface as 200 with the
// new status persisted.

func TestZZ_Update_StatusTransition_IllegalReturns400(t *testing.T) {
	repo := &zzRepo{getProject: &domain.Project{
		ID:        uuid.New(),
		TenantID:  uuid.MustParse(zzTenant),
		Code:      "C3",
		Name:      "N3",
		Status:    domain.StatusCompleted,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}}
	r := zzRouter(repo)
	body, _ := json.Marshal(map[string]any{"status": "active"})
	w := zzDo(r, http.MethodPut, "/api/v1/projects/"+uuid.New().String(), body, true)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (completed->active illegal); body: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if !strings.Contains(resp["error"], "illegal status transition") {
		t.Errorf("error = %q, want to contain 'illegal status transition'", resp["error"])
	}
}

func TestZZ_Update_StatusTransition_LegalReturns200(t *testing.T) {
	repo := &zzRepo{getProject: &domain.Project{
		ID:        uuid.New(),
		TenantID:  uuid.MustParse(zzTenant),
		Code:      "C4",
		Name:      "N4",
		Status:    domain.StatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}}
	r := zzRouter(repo)
	body, _ := json.Marshal(map[string]any{"status": "archived"})
	w := zzDo(r, http.MethodPut, "/api/v1/projects/"+uuid.New().String(), body, true)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (active->archived legal); body: %s", w.Code, w.Body.String())
	}
	var dto ProjectDTO
	if err := json.NewDecoder(w.Body).Decode(&dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dto.Status != string(domain.StatusArchived) {
		t.Errorf("Status = %q, want %q", dto.Status, domain.StatusArchived)
	}
}

// ---- Update: customer_id valid vs malformed ----

func TestZZ_Update_CustomerID_ValidAppliedMalformedIgnored(t *testing.T) {
	t.Run("valid uuid applied", func(t *testing.T) {
		repo := &zzRepo{getProject: &domain.Project{
			ID: uuid.New(), TenantID: uuid.MustParse(zzTenant), Code: "C5", Name: "N5",
			Status: domain.StatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}}
		r := zzRouter(repo)
		newCID := uuid.New().String()
		body, _ := json.Marshal(map[string]any{"customer_id": newCID})
		w := zzDo(r, http.MethodPut, "/api/v1/projects/"+uuid.New().String(), body, true)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
		}
		var dto ProjectDTO
		_ = json.NewDecoder(w.Body).Decode(&dto)
		if dto.CustomerID == nil || *dto.CustomerID != newCID {
			t.Errorf("CustomerID = %v, want %q", dto.CustomerID, newCID)
		}
	})

	t.Run("malformed uuid ignored, prior value unchanged (nil)", func(t *testing.T) {
		repo := &zzRepo{getProject: &domain.Project{
			ID: uuid.New(), TenantID: uuid.MustParse(zzTenant), Code: "C6", Name: "N6",
			Status: domain.StatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now(),
			// CustomerID starts nil.
		}}
		r := zzRouter(repo)
		body, _ := json.Marshal(map[string]any{"customer_id": "not-a-uuid"})
		w := zzDo(r, http.MethodPut, "/api/v1/projects/"+uuid.New().String(), body, true)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
		}
		var dto ProjectDTO
		_ = json.NewDecoder(w.Body).Decode(&dto)
		if dto.CustomerID != nil {
			t.Errorf("CustomerID = %v, want nil (malformed value must not overwrite)", *dto.CustomerID)
		}
	})
}

// ---- Delete ----

func TestZZ_Delete_NotFound_Returns404(t *testing.T) {
	repo := &zzRepo{deleteErr: appproject.ErrNotFound}
	r := zzRouter(repo)
	w := zzDo(r, http.MethodDelete, "/api/v1/projects/"+uuid.New().String(), nil, true)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

func TestZZ_Delete_InternalError_Returns500(t *testing.T) {
	repo := &zzRepo{deleteErr: errors.New("db unavailable")}
	r := zzRouter(repo)
	w := zzDo(r, http.MethodDelete, "/api/v1/projects/"+uuid.New().String(), nil, true)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
}

// ---- Restore ----

func TestZZ_Restore_NotFound_Returns404(t *testing.T) {
	repo := &zzRepo{restoreErr: appproject.ErrNotFound}
	r := zzRouter(repo)
	w := zzDo(r, http.MethodPost, "/api/v1/projects/"+uuid.New().String()+"/restore", nil, true)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

func TestZZ_Restore_InternalError_Returns500(t *testing.T) {
	repo := &zzRepo{restoreErr: errors.New("db unavailable")}
	r := zzRouter(repo)
	w := zzDo(r, http.MethodPost, "/api/v1/projects/"+uuid.New().String()+"/restore", nil, true)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
}

// ---- List: status/customer_id query filters, and non-NotFound error → 500 ----

func TestZZ_List_StatusAndCustomerIDFilters(t *testing.T) {
	t.Run("valid status and customer_id are both applied", func(t *testing.T) {
		repo := &zzRepo{}
		r := zzRouter(repo)
		cid := uuid.New()
		w := zzDo(r, http.MethodGet, "/api/v1/projects?status=active&customer_id="+cid.String(), nil, true)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
		}
		if repo.lastFilter.Status == nil || *repo.lastFilter.Status != domain.StatusActive {
			t.Errorf("filter.Status = %v, want %q", repo.lastFilter.Status, domain.StatusActive)
		}
		if repo.lastFilter.CustomerID == nil || *repo.lastFilter.CustomerID != cid {
			t.Errorf("filter.CustomerID = %v, want %v", repo.lastFilter.CustomerID, cid)
		}
	})

	t.Run("invalid customer_id is dropped (no filter applied)", func(t *testing.T) {
		repo := &zzRepo{}
		r := zzRouter(repo)
		w := zzDo(r, http.MethodGet, "/api/v1/projects?customer_id=not-a-uuid", nil, true)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
		}
		if repo.lastFilter.CustomerID != nil {
			t.Errorf("filter.CustomerID = %v, want nil", repo.lastFilter.CustomerID)
		}
	})

	t.Run("no status query leaves filter.Status nil", func(t *testing.T) {
		repo := &zzRepo{}
		r := zzRouter(repo)
		w := zzDo(r, http.MethodGet, "/api/v1/projects", nil, true)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
		}
		if repo.lastFilter.Status != nil {
			t.Errorf("filter.Status = %v, want nil", repo.lastFilter.Status)
		}
	})
}

func TestZZ_List_InternalError_Returns500(t *testing.T) {
	repo := &zzRepo{listErr: errors.New("query failed")}
	r := zzRouter(repo)
	w := zzDo(r, http.MethodGet, "/api/v1/projects", nil, true)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
}
