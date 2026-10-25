package customer_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	handlercust "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/customer"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appcust "github.com/hanmahong5-arch/lurus-tally/internal/app/customer"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/customer"
)

func init() {
	gin.SetMode(gin.TestMode)
}

const testTenantID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"

// fakeRepo is an in-memory stub satisfying appcust.Repository.
type fakeRepo struct {
	createErr  error
	created    *domain.Customer
	byName     *domain.Customer
	byMobile   *domain.Customer
	getErr     error
	getResult  *domain.Customer
	listItems  []*domain.Customer
	listTotal  int
	listErr    error
	lastFilter domain.ListFilter
}

func (f *fakeRepo) Create(_ context.Context, c *domain.Customer) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.created = c
	return nil
}

func (f *fakeRepo) GetByID(_ context.Context, _, _ uuid.UUID) (*domain.Customer, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.getResult, nil
}

func (f *fakeRepo) List(_ context.Context, filter domain.ListFilter) ([]*domain.Customer, int, error) {
	f.lastFilter = filter
	if f.listErr != nil {
		return nil, 0, f.listErr
	}
	return f.listItems, f.listTotal, nil
}

func (f *fakeRepo) FindActiveByName(_ context.Context, _ uuid.UUID, _ string) (*domain.Customer, error) {
	if f.byName != nil {
		return f.byName, nil
	}
	return nil, appcust.ErrNotFound
}

func (f *fakeRepo) FindActiveByMobile(_ context.Context, _ uuid.UUID, _ string) (*domain.Customer, error) {
	if f.byMobile != nil {
		return f.byMobile, nil
	}
	return nil, appcust.ErrNotFound
}

func (f *fakeRepo) Update(_ context.Context, c *domain.Customer) error {
	if f.getErr != nil {
		return f.getErr
	}
	f.getResult = c
	return nil
}

func (f *fakeRepo) Delete(_ context.Context, _, _ uuid.UUID) error {
	if f.getErr != nil {
		return f.getErr
	}
	return nil
}

func (f *fakeRepo) Restore(_ context.Context, _, _ uuid.UUID) (*domain.Customer, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.getResult, nil
}

var _ appcust.Repository = (*fakeRepo)(nil)

func makeRouter(repo appcust.Repository) *gin.Engine {
	h := handlercust.New(
		appcust.NewCreateUseCase(repo),
		appcust.NewGetByIDUseCase(repo),
		appcust.NewListUseCase(repo),
		appcust.NewUpdateUseCase(repo),
		appcust.NewDeleteUseCase(repo),
		appcust.NewRestoreUseCase(repo),
	)
	r := gin.New()
	r.Use(gin.Recovery())
	// Inject tenant_id for tests.
	r.Use(func(c *gin.Context) {
		if tid := c.GetHeader("X-Tenant-ID"); tid != "" {
			if id, err := uuid.Parse(tid); err == nil {
				c.Set(middleware.CtxKeyTenantID, id)
			}
		}
		c.Next()
	})
	api := r.Group("/api/v1")
	h.RegisterRoutes(api)
	return r
}

func do(r *gin.Engine, method, path, tenant, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	if tenant != "" {
		req.Header.Set("X-Tenant-ID", tenant)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func sampleCustomer() *domain.Customer {
	ts := time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)
	return &domain.Customer{
		ID:          uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
		TenantID:    uuid.MustParse(testTenantID),
		Code:        "C001",
		Name:        "杭州客户甲",
		ContactName: "张三",
		Mobile:      "13800000000",
		Phone:       "0571-12345678",
		Address:     "杭州市西湖区",
		Remark:      "老客户",
		CreatedAt:   ts,
		UpdatedAt:   ts,
	}
}

// ---- Create ----

func TestCustomerHandler_Create_Returns201(t *testing.T) {
	repo := &fakeRepo{}
	r := makeRouter(repo)

	body := `{"name":"杭州客户甲","code":"C001","contact_name":"张三","mobile":"13800000000","phone":"0571-12345678","address":"杭州市西湖区","remark":"老客户"}`
	w := do(r, http.MethodPost, "/api/v1/customers", testTenantID, body)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := map[string]string{
		"tenant_id":    testTenantID,
		"name":         "杭州客户甲",
		"code":         "C001",
		"contact_name": "张三",
		"mobile":       "13800000000",
		"phone":        "0571-12345678",
		"address":      "杭州市西湖区",
		"remark":       "老客户",
	}
	for k, v := range want {
		if resp[k] != v {
			t.Errorf("%s = %v, want %q", k, resp[k], v)
		}
	}
	for _, k := range []string{"id", "created_at", "updated_at"} {
		if s, _ := resp[k].(string); s == "" {
			t.Errorf("%s missing from response", k)
		}
	}
	if repo.created == nil || repo.created.TenantID.String() != testTenantID {
		t.Fatalf("repo.Create not called with the request tenant: %+v", repo.created)
	}
	if loc := w.Header().Get("Location"); loc != "/api/v1/customers/"+repo.created.ID.String() {
		t.Errorf("Location = %q, want /api/v1/customers/%s", loc, repo.created.ID)
	}
}

func TestCustomerHandler_Create_OptionalFieldsOmittedCodeGenerated(t *testing.T) {
	r := makeRouter(&fakeRepo{})

	w := do(r, http.MethodPost, "/api/v1/customers", testTenantID, `{"name":"散客"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	for _, k := range []string{"contact_name", "mobile", "phone", "address", "remark"} {
		if _, ok := resp[k]; ok {
			t.Errorf("empty optional field %q should be omitted, got %v", k, resp[k])
		}
	}
	if code, _ := resp["code"].(string); !regexp.MustCompile(`^C\d{6}[2-9A-HJ-NP-Z]{4}$`).MatchString(code) {
		t.Errorf("code = %v, want a generated C-prefixed code", resp["code"])
	}
}

func TestCustomerHandler_Create_BadRequests(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"malformed JSON", `{not-json`},
		{"missing name", `{"code":"C1"}`},
		{"blank name", `{"name":"   "}`},
		{"name over 128", `{"name":"` + strings.Repeat("a", 129) + `"}`},
		{"code over 64", `{"name":"N","code":"` + strings.Repeat("c", 65) + `"}`},
		{"remark over 500", `{"name":"N","remark":"` + strings.Repeat("r", 501) + `"}`},
		{"mobile over 30", `{"name":"N","mobile":"` + strings.Repeat("1", 31) + `"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{}
			r := makeRouter(repo)
			w := do(r, http.MethodPost, "/api/v1/customers", testTenantID, tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
			}
			if repo.created != nil {
				t.Error("repo.Create must not be reached on a bad request")
			}
		})
	}
}

func TestCustomerHandler_Create_NoTenant_Returns401(t *testing.T) {
	r := makeRouter(&fakeRepo{})
	w := do(r, http.MethodPost, "/api/v1/customers", "", `{"name":"N"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestCustomerHandler_Create_DuplicateCode_Returns409(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23505", Message: `duplicate key value violates unique constraint "idx_partner_code"`}
	repo := &fakeRepo{createErr: fmt.Errorf("customer repo create: %w", pgErr)}
	r := makeRouter(repo)

	w := do(r, http.MethodPost, "/api/v1/customers", testTenantID, `{"name":"N","code":"C001"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "duplicate" {
		t.Errorf("error = %v, want duplicate", resp["error"])
	}
	if strings.Contains(w.Body.String(), "idx_partner_code") {
		t.Errorf("409 body leaked the constraint name: %s", w.Body.String())
	}
}

func TestCustomerHandler_Create_RepoError_Returns500WithoutLeak(t *testing.T) {
	repo := &fakeRepo{createErr: errors.New("boom: pg connection reset")}
	r := makeRouter(repo)

	w := do(r, http.MethodPost, "/api/v1/customers", testTenantID, `{"name":"N"}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "boom") {
		t.Errorf("5xx body leaked internal cause: %s", w.Body.String())
	}
}

// ---- List ----

func TestCustomerHandler_List_Returns200(t *testing.T) {
	repo := &fakeRepo{listItems: []*domain.Customer{sampleCustomer()}, listTotal: 41}
	r := makeRouter(repo)

	w := do(r, http.MethodGet, "/api/v1/customers?q=138&limit=10&offset=20", testTenantID, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 41 || len(resp.Items) != 1 {
		t.Fatalf("total=%d items=%d, want 41/1", resp.Total, len(resp.Items))
	}
	if resp.Items[0]["mobile"] != "13800000000" || resp.Items[0]["created_at"] != "2026-10-10T08:00:00Z" {
		t.Errorf("item = %v", resp.Items[0])
	}
	f := repo.lastFilter
	if f.TenantID.String() != testTenantID || f.Query != "138" || f.Limit != 10 || f.Offset != 20 {
		t.Errorf("filter = %+v, want tenant/q=138/limit=10/offset=20", f)
	}
}

func TestCustomerHandler_List_DefaultsAndEmptyItemsArray(t *testing.T) {
	repo := &fakeRepo{}
	r := makeRouter(repo)

	w := do(r, http.MethodGet, "/api/v1/customers?limit=abc&offset=-3", testTenantID, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Errorf("empty list must serialise items as [], got %s", w.Body.String())
	}
	if repo.lastFilter.Limit != 20 || repo.lastFilter.Offset != 0 {
		t.Errorf("filter = %+v, want limit=20 offset=0", repo.lastFilter)
	}
}

func TestCustomerHandler_List_NoTenant_Returns401(t *testing.T) {
	r := makeRouter(&fakeRepo{})
	w := do(r, http.MethodGet, "/api/v1/customers", "", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestCustomerHandler_List_RepoError_Returns500(t *testing.T) {
	r := makeRouter(&fakeRepo{listErr: errors.New("boom: timeout")})
	w := do(r, http.MethodGet, "/api/v1/customers", testTenantID, "")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if strings.Contains(w.Body.String(), "boom") {
		t.Errorf("5xx body leaked internal cause: %s", w.Body.String())
	}
}

// ---- GetByID ----

func TestCustomerHandler_GetByID_Returns200(t *testing.T) {
	c := sampleCustomer()
	r := makeRouter(&fakeRepo{getResult: c})

	w := do(r, http.MethodGet, "/api/v1/customers/"+c.ID.String(), testTenantID, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["id"] != c.ID.String() || resp["name"] != c.Name {
		t.Errorf("resp = %v", resp)
	}
}

func TestCustomerHandler_GetByID_Errors(t *testing.T) {
	cases := []struct {
		name   string
		tenant string
		id     string
		getErr error
		want   int
	}{
		{"no tenant", "", uuid.NewString(), nil, http.StatusUnauthorized},
		{"invalid id", testTenantID, "not-a-uuid", nil, http.StatusBadRequest},
		{"not found", testTenantID, uuid.NewString(), appcust.ErrNotFound, http.StatusNotFound},
		{"repo error", testTenantID, uuid.NewString(), errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := makeRouter(&fakeRepo{getErr: tc.getErr, getResult: sampleCustomer()})
			w := do(r, http.MethodGet, "/api/v1/customers/"+tc.id, tc.tenant, "")
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestCustomerHandler_Create_DuplicateName_Returns409WithExistingID(t *testing.T) {
	existing := sampleCustomer()
	repo := &fakeRepo{byName: existing}
	w := do(makeRouter(repo), http.MethodPost, "/api/v1/customers", testTenantID, `{"name":" 杭州客户甲 "}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "duplicate_customer_name" || resp["existing_id"] != existing.ID.String() ||
		resp["message"] != "已存在同名客户「杭州客户甲」" {
		t.Errorf("body = %v", resp)
	}
	if repo.created != nil {
		t.Error("duplicate must not be inserted")
	}
}

func TestCustomerHandler_Create_DuplicateMobile_Returns409(t *testing.T) {
	existing := sampleCustomer()
	w := do(makeRouter(&fakeRepo{byMobile: existing}), http.MethodPost, "/api/v1/customers", testTenantID,
		`{"name":"新客户","mobile":"13800000000"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "duplicate_customer_mobile" || resp["existing_id"] != existing.ID.String() {
		t.Errorf("body = %v", resp)
	}
}

func TestCustomerHandler_Update_Returns200(t *testing.T) {
	c := sampleCustomer()
	r := makeRouter(&fakeRepo{getResult: c})
	w := do(r, http.MethodPut, "/api/v1/customers/"+c.ID.String(), testTenantID, `{"contact_name":"李四"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["contact_name"] != "李四" {
		t.Errorf("resp = %v", resp)
	}
}

func TestCustomerHandler_Update_DuplicateName_Returns409(t *testing.T) {
	existing := sampleCustomer()
	other := sampleCustomer()
	other.ID = uuid.New()
	other.Name = "别家"
	repo := &fakeRepo{getResult: other, byName: existing}
	w := do(makeRouter(repo), http.MethodPut, "/api/v1/customers/"+other.ID.String(), testTenantID, `{"name":"杭州客户甲"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", w.Code, w.Body.String())
	}
}

func TestCustomerHandler_Delete_Returns204(t *testing.T) {
	c := sampleCustomer()
	w := do(makeRouter(&fakeRepo{getResult: c}), http.MethodDelete, "/api/v1/customers/"+c.ID.String(), testTenantID, "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", w.Code, w.Body.String())
	}
}

func TestCustomerHandler_Restore_Returns200(t *testing.T) {
	c := sampleCustomer()
	w := do(makeRouter(&fakeRepo{getResult: c}), http.MethodPost, "/api/v1/customers/"+c.ID.String()+"/restore", testTenantID, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
}
