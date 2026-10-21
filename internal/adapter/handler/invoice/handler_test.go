package invoice

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appinv "github.com/hanmahong5-arch/lurus-tally/internal/app/invoice"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/invoice"
)

// memRepo is a minimal in-memory appinv.Repository (no bills: linking is covered in the app tests).
type memRepo struct {
	byID map[uuid.UUID]*domain.Invoice
	nos  map[string]bool
	last domain.ListFilter
}

func newMem() *memRepo {
	return &memRepo{byID: map[uuid.UUID]*domain.Invoice{}, nos: map[string]bool{}}
}

func (m *memRepo) WithTx(_ context.Context, fn func(*sql.Tx) error) error { return fn(nil) }
func (m *memRepo) PartnerName(context.Context, *sql.Tx, uuid.UUID, uuid.UUID) (string, error) {
	return "客户甲", nil
}
func (m *memRepo) LockBills(context.Context, *sql.Tx, uuid.UUID, []uuid.UUID) (map[uuid.UUID]appinv.BillRef, error) {
	return map[uuid.UUID]appinv.BillRef{}, nil
}
func (m *memRepo) Insert(_ context.Context, _ *sql.Tx, inv *domain.Invoice) error {
	k := string(inv.Direction) + inv.InvoiceNo
	if m.nos[k] {
		return appinv.ErrDuplicateNo
	}
	m.nos[k] = true
	m.byID[inv.ID] = inv
	return nil
}
func (m *memRepo) GetForUpdate(_ context.Context, _ *sql.Tx, _ uuid.UUID, id uuid.UUID) (*domain.Invoice, error) {
	if inv, ok := m.byID[id]; ok {
		return inv, nil
	}
	return nil, appinv.ErrNotFound
}
func (m *memRepo) SetStatus(_ context.Context, _ *sql.Tx, _ uuid.UUID, id uuid.UUID, from, to domain.Status) (bool, error) {
	if inv := m.byID[id]; inv != nil && inv.Status == from {
		inv.Status = to
		return true, nil
	}
	return false, nil
}
func (m *memRepo) GetByID(_ context.Context, _ uuid.UUID, id uuid.UUID) (*domain.Invoice, error) {
	if inv, ok := m.byID[id]; ok {
		return inv, nil
	}
	return nil, appinv.ErrNotFound
}
func (m *memRepo) List(_ context.Context, f domain.ListFilter) ([]*domain.Invoice, int, error) {
	m.last = f
	var out []*domain.Invoice
	for _, v := range m.byID {
		out = append(out, v)
	}
	return out, len(out), nil
}
func (m *memRepo) Summarize(context.Context, domain.ListFilter) (domain.Summary, error) {
	return domain.Summary{}, nil
}

func setup(tenant uuid.UUID) (*gin.Engine, *memRepo) {
	return setupAs(tenant, "")
}

// setupAs is setup with an authenticated IdP subject, as the auth middleware would inject it.
func setupAs(tenant uuid.UUID, sub string) (*gin.Engine, *memRepo) {
	gin.SetMode(gin.TestMode)
	m := newMem()
	h := New(appinv.NewCreateUseCase(m), appinv.NewGetUseCase(m), appinv.NewListUseCase(m), appinv.NewVoidUseCase(m), appinv.NewRedUseCase(m))
	r := gin.New()
	g := r.Group("/api/v1")
	g.Use(func(c *gin.Context) {
		if tenant != uuid.Nil {
			c.Set(middleware.CtxKeyTenantID, tenant)
		}
		if sub != "" {
			c.Set(middleware.CtxKeyIDPSubject, sub)
		}
	})
	h.RegisterRoutes(g)
	return r, m
}

func do(r *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func validBody(no string) map[string]any {
	return map[string]any{
		"direction": "out", "invoice_no": no, "invoice_type": "数电普票", "partner_id": uuid.New().String(),
		"issue_date": "2026-10-01", "amount_excl_tax": "100", "tax_amount": "13",
	}
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("body %s: %v", w.Body.String(), err)
	}
	return m
}

func TestUnauthorizedWithoutTenant(t *testing.T) {
	r, _ := setup(uuid.Nil)
	for _, c := range [][2]string{{"GET", "/api/v1/invoices"}, {"POST", "/api/v1/invoices"}, {"GET", "/api/v1/invoices/" + uuid.NewString()},
		{"POST", "/api/v1/invoices/" + uuid.NewString() + "/void"}, {"POST", "/api/v1/invoices/" + uuid.NewString() + "/red"}} {
		if w := do(r, c[0], c[1], nil); w.Code != http.StatusUnauthorized {
			t.Errorf("%v: %d", c, w.Code)
		}
	}
}

func TestCreateGetVoidRedFlow(t *testing.T) {
	r, _ := setup(uuid.New())
	w := do(r, "POST", "/api/v1/invoices", validBody("A1"))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	m := decode(t, w)
	id := m["id"].(string)
	if m["total_amount"] != "113" || m["status"] != "issued" || m["partner_name"] != "客户甲" || m["issue_date"] != "2026-10-01" {
		t.Fatalf("dto: %v", m)
	}
	if w := do(r, "POST", "/api/v1/invoices", validBody("A1")); w.Code != http.StatusConflict {
		t.Fatalf("dup: %d", w.Code)
	}
	if w := do(r, "GET", "/api/v1/invoices/"+id, nil); w.Code != http.StatusOK {
		t.Fatalf("get: %d", w.Code)
	}
	if w := do(r, "POST", "/api/v1/invoices/"+id+"/red", map[string]any{"invoice_no": ""}); w.Code != http.StatusBadRequest {
		t.Fatalf("red empty no: %d %s", w.Code, w.Body.String())
	}
	w = do(r, "POST", "/api/v1/invoices/"+id+"/red", map[string]any{"invoice_no": "R1"})
	if w.Code != http.StatusCreated {
		t.Fatalf("red: %d %s", w.Code, w.Body.String())
	}
	red := decode(t, w)
	if red["total_amount"] != "-113" || red["red_of_id"] != id {
		t.Fatalf("red dto: %v", red)
	}
	if w := do(r, "POST", "/api/v1/invoices/"+id+"/void", nil); w.Code != http.StatusConflict {
		t.Fatalf("void red'd: %d", w.Code)
	}
	if w := do(r, "POST", "/api/v1/invoices/"+uuid.NewString()+"/void", nil); w.Code != http.StatusNotFound {
		t.Fatalf("void missing: %d", w.Code)
	}
	w = do(r, "POST", "/api/v1/invoices", validBody("A2"))
	id2 := decode(t, w)["id"].(string)
	w = do(r, "POST", "/api/v1/invoices/"+id2+"/void", nil)
	if w.Code != http.StatusOK || decode(t, w)["status"] != "void" {
		t.Fatalf("void: %d %s", w.Code, w.Body.String())
	}
}

func TestCreateValidation(t *testing.T) {
	r, _ := setup(uuid.New())
	mut := func(f func(map[string]any)) map[string]any { b := validBody("V1"); f(b); return b }
	cases := map[string]map[string]any{
		"bad date":   mut(func(b map[string]any) { b["issue_date"] = "2026/10/01" }),
		"bad type":   mut(func(b map[string]any) { b["invoice_type"] = "收据" }),
		"zero excl":  mut(func(b map[string]any) { b["amount_excl_tax"] = "0" }),
		"bad amount": mut(func(b map[string]any) { b["amount_excl_tax"] = "abc" }),
		"bad bill id": mut(func(b map[string]any) {
			b["links"] = []map[string]any{{"bill_id": "x", "amount": "1"}}
		}),
		"no partner": mut(func(b map[string]any) { delete(b, "partner_id") }),
	}
	for name, body := range cases {
		w := do(r, "POST", "/api/v1/invoices", body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
	if w := do(r, "GET", "/api/v1/invoices/not-a-uuid", nil); w.Code != http.StatusBadRequest {
		t.Errorf("bad id: %d", w.Code)
	}
}

func TestListFilterParsing(t *testing.T) {
	r, m := setup(uuid.New())
	pid := uuid.NewString()
	w := do(r, "GET", "/api/v1/invoices?direction=in&status=issued&partner_id="+pid+"&date_from=2026-10-01&date_to=2026-10-31&q=AB&limit=5&offset=10", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	f := m.last
	if f.Direction != domain.DirectionIn || f.Status != domain.StatusIssued || f.PartnerID == nil || f.Keyword != "AB" || f.Limit != 5 || f.Offset != 10 {
		t.Fatalf("filter: %+v", f)
	}
	if f.DateFrom.Format("2006-01-02") != "2026-10-01" || f.DateTo.Format("2006-01-02") != "2026-11-01" {
		t.Fatalf("dates: %v %v", f.DateFrom, f.DateTo)
	}
	if _, ok := decode(t, w)["summary"]; !ok {
		t.Fatal("summary missing")
	}
	for _, q := range []string{"direction=x", "status=x", "partner_id=x", "date_from=bad", "date_to=bad", "date_from=2026-10-02&date_to=2026-10-01", "offset=-1", "bill_id=x"} {
		if w := do(r, "GET", "/api/v1/invoices?"+q, nil); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", q, w.Code)
		}
	}
}

func TestListBillIDFilter(t *testing.T) {
	r, m := setup(uuid.New())
	bid := uuid.New()
	if w := do(r, "GET", "/api/v1/invoices?bill_id="+bid.String(), nil); w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if m.last.BillID == nil || *m.last.BillID != bid {
		t.Fatalf("bill filter: %+v", m.last.BillID)
	}
}

func TestCreateAndRed_RecordCreatedBy(t *testing.T) {
	sub := uuid.New()
	r, m := setupAs(uuid.New(), sub.String())
	w := do(r, "POST", "/api/v1/invoices", validBody("C1"))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	id := decode(t, w)["id"].(string)
	var created *domain.Invoice
	for _, inv := range m.byID {
		created = inv
	}
	if created == nil || created.CreatedBy == nil || *created.CreatedBy != sub {
		t.Fatalf("created invoice CreatedBy = %v, want %s", created, sub)
	}

	w = do(r, "POST", "/api/v1/invoices/"+id+"/red", map[string]any{"invoice_no": "RC1"})
	if w.Code != http.StatusCreated {
		t.Fatalf("red: %d %s", w.Code, w.Body.String())
	}
	if len(m.byID) != 2 {
		t.Fatalf("want original + red, got %d invoices", len(m.byID))
	}
	for _, inv := range m.byID {
		if inv.CreatedBy == nil || *inv.CreatedBy != sub {
			t.Errorf("invoice %s CreatedBy = %v, want %s", inv.InvoiceNo, inv.CreatedBy, sub)
		}
	}
}

func TestCreate_NoSubjectLeavesCreatedByEmpty(t *testing.T) {
	r, m := setup(uuid.New())
	if w := do(r, "POST", "/api/v1/invoices", validBody("C2")); w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	for _, inv := range m.byID {
		if inv.CreatedBy != nil {
			t.Fatalf("CreatedBy = %v, want nil without an authenticated subject", inv.CreatedBy)
		}
	}
}
