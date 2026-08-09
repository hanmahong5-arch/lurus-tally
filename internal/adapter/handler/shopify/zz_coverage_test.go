package shopify_test

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
	handlershopify "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/shopify"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appshopify "github.com/hanmahong5-arch/lurus-tally/internal/app/shopify"
)

// This file supplements handler_test.go with the branches not yet exercised:
// Bind malformed/missing-field JSON, invalid warehouse_id, default(500) app
// error mapping, creator_id anti-spoofing fallback; List error + empty-slice
// JSON shape + no-tenant 401; Unbind invalid-id 400. It does not duplicate
// any case already present in handler_test.go.

// ---- capturing fake (for creator_id anti-spoofing assertions) --------------

// capturingBinder records the BindInput it was called with so tests can
// assert what CreatorID the handler actually resolved (never taken from the
// request body — the wire format has no creator_id field at all).
type capturingBinder struct {
	got appshopify.BindInput
	out *appshopify.ShopMapping
	err error
}

func (f *capturingBinder) Execute(_ context.Context, in appshopify.BindInput) (*appshopify.ShopMapping, error) {
	f.got = in
	if f.err != nil {
		return nil, f.err
	}
	if f.out != nil {
		return f.out, nil
	}
	return &appshopify.ShopMapping{
		ID:          uuid.New(),
		TenantID:    in.TenantID,
		ShopDomain:  in.ShopDomain,
		WarehouseID: in.WarehouseID,
		CreatorID:   in.CreatorID,
	}, nil
}

type fakeBinderZZ struct {
	out *appshopify.ShopMapping
	err error
}

func (f *fakeBinderZZ) Execute(_ context.Context, _ appshopify.BindInput) (*appshopify.ShopMapping, error) {
	return f.out, f.err
}

type fakeListerZZ struct {
	items []appshopify.ShopMapping
	err   error
}

func (f *fakeListerZZ) Execute(_ context.Context, _ uuid.UUID) ([]appshopify.ShopMapping, error) {
	return f.items, f.err
}

type fakeUnbinderZZ struct {
	err error
}

func (f *fakeUnbinderZZ) Execute(_ context.Context, _, _ uuid.UUID) error {
	return f.err
}

// ---- router helpers (mirrors handler_test.go; kept local to this file) ----

const zzTenantID = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"

func zzRouter(h *handlershopify.Handler, sub string) *gin.Engine {
	r := gin.New()
	rg := r.Group("/api/v1")
	rg.Use(func(c *gin.Context) {
		c.Set(middleware.CtxKeyTenantID, uuid.MustParse(zzTenantID))
		if sub != "" {
			c.Set(middleware.CtxKeyIDPSubject, sub)
		}
		c.Next()
	})
	h.RegisterRoutes(rg)
	return r
}

func zzRouterNoTenant(h *handlershopify.Handler) *gin.Engine {
	r := gin.New()
	rg := r.Group("/api/v1")
	h.RegisterRoutes(rg)
	return r
}

func zzMarshal(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewBuffer(b)
}

// ---- Bind: malformed / missing-required JSON → 400 -------------------------

func TestBind_MalformedJSON_Returns400(t *testing.T) {
	h := handlershopify.New(&fakeBinderZZ{}, &fakeListerZZ{}, &fakeUnbinderZZ{})
	r := zzRouter(h, "")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/shopify/shops", bytes.NewBufferString("{not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400: body=%s", w.Code, w.Body.String())
	}
}

func TestBind_MissingRequiredFields_Returns400(t *testing.T) {
	tests := []struct {
		name string
		body map[string]string
	}{
		{"missing shop_domain", map[string]string{"warehouse_id": uuid.New().String()}},
		{"missing warehouse_id", map[string]string{"shop_domain": "abc.myshopify.com"}},
		{"empty body", map[string]string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := handlershopify.New(&fakeBinderZZ{}, &fakeListerZZ{}, &fakeUnbinderZZ{})
			r := zzRouter(h, "")

			req := httptest.NewRequest(http.MethodPost, "/api/v1/shopify/shops", zzMarshal(t, tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d; want 400: body=%s", w.Code, w.Body.String())
			}
		})
	}
}

// ---- Bind: invalid (non-UUID) warehouse_id → 422 "invalid warehouse_id" ----

func TestBind_InvalidWarehouseIDFormat_Returns422(t *testing.T) {
	h := handlershopify.New(&fakeBinderZZ{}, &fakeListerZZ{}, &fakeUnbinderZZ{})
	r := zzRouter(h, "")

	body := zzMarshal(t, map[string]string{
		"shop_domain":  "abc.myshopify.com",
		"warehouse_id": "not-a-uuid",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/shopify/shops", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d; want 422: body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["error"] != "invalid warehouse_id" {
		t.Errorf("error = %q; want %q", resp["error"], "invalid warehouse_id")
	}
}

// ---- Bind: default/unmapped app error → 500 "internal error" ---------------

func TestBind_UnmappedAppError_Returns500(t *testing.T) {
	h := handlershopify.New(
		&fakeBinderZZ{err: errors.New("db connection reset")},
		&fakeListerZZ{},
		&fakeUnbinderZZ{},
	)
	r := zzRouter(h, "")

	body := zzMarshal(t, map[string]string{
		"shop_domain":  "abc.myshopify.com",
		"warehouse_id": uuid.New().String(),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/shopify/shops", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d; want 500: body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["error"] != "internal error" {
		t.Errorf("error = %q; want %q (must not leak internal detail)", resp["error"], "internal error")
	}
}

// ---- Bind: success response body maps ShopMapping -> shopDTO correctly ----

func TestBind_Happy_ResponseBodyFields(t *testing.T) {
	tenant := uuid.MustParse(zzTenantID)
	warehouseID := uuid.New()
	shopID := uuid.New()
	want := &appshopify.ShopMapping{
		ID:          shopID,
		TenantID:    tenant,
		ShopDomain:  "myshop.myshopify.com",
		WarehouseID: warehouseID,
		CreatorID:   tenant,
	}
	h := handlershopify.New(&fakeBinderZZ{out: want}, &fakeListerZZ{}, &fakeUnbinderZZ{})
	r := zzRouter(h, "")

	body := zzMarshal(t, map[string]string{
		"shop_domain":  "myshop.myshopify.com",
		"warehouse_id": warehouseID.String(),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/shopify/shops", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d; want 201: body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	// hand-computed expectations from toDTO's field-by-field .String() mapping.
	if resp["id"] != shopID.String() {
		t.Errorf("id = %q; want %q", resp["id"], shopID.String())
	}
	if resp["shop_domain"] != "myshop.myshopify.com" {
		t.Errorf("shop_domain = %q; want %q", resp["shop_domain"], "myshop.myshopify.com")
	}
	if resp["warehouse_id"] != warehouseID.String() {
		t.Errorf("warehouse_id = %q; want %q", resp["warehouse_id"], warehouseID.String())
	}
	if resp["creator_id"] != tenant.String() {
		t.Errorf("creator_id = %q; want %q", resp["creator_id"], tenant.String())
	}
}

// ---- Bind: creator_id anti-spoofing -----------------------------------------
//
// Business invariant: creator_id is always resolved server-side from the
// IDP subject (or falls back to tenant), never taken from the request body
// (the wire bindRequest type does not even expose a creator_id field).

func TestBind_CreatorID_ResolvesFromValidIDPSubject(t *testing.T) {
	tenant := uuid.MustParse(zzTenantID)
	validSub := uuid.New() // a subject that happens to parse as a UUID
	capB := &capturingBinder{}
	h := handlershopify.New(capB, &fakeListerZZ{}, &fakeUnbinderZZ{})
	r := zzRouter(h, validSub.String())

	body := zzMarshal(t, map[string]string{
		"shop_domain":  "abc.myshopify.com",
		"warehouse_id": uuid.New().String(),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/shopify/shops", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d; want 201: body=%s", w.Code, w.Body.String())
	}
	if capB.got.CreatorID != validSub {
		t.Errorf("CreatorID = %s; want resolved IDP subject %s (not tenant %s)", capB.got.CreatorID, validSub, tenant)
	}
	if capB.got.TenantID != tenant {
		t.Errorf("TenantID = %s; want %s", capB.got.TenantID, tenant)
	}
}

func TestBind_CreatorID_FallsBackToTenant_WhenSubjectNotUUID(t *testing.T) {
	tenant := uuid.MustParse(zzTenantID)
	capB := &capturingBinder{}
	h := handlershopify.New(capB, &fakeListerZZ{}, &fakeUnbinderZZ{})
	r := zzRouter(h, "not-a-parseable-uuid-subject")

	body := zzMarshal(t, map[string]string{
		"shop_domain":  "abc.myshopify.com",
		"warehouse_id": uuid.New().String(),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/shopify/shops", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d; want 201: body=%s", w.Code, w.Body.String())
	}
	if capB.got.CreatorID != tenant {
		t.Errorf("CreatorID = %s; want fallback to tenant %s when sub unparsable", capB.got.CreatorID, tenant)
	}
}

func TestBind_CreatorID_FallsBackToTenant_WhenSubjectAbsent(t *testing.T) {
	tenant := uuid.MustParse(zzTenantID)
	capB := &capturingBinder{}
	h := handlershopify.New(capB, &fakeListerZZ{}, &fakeUnbinderZZ{})
	r := zzRouter(h, "") // no CtxKeyIDPSubject set at all

	body := zzMarshal(t, map[string]string{
		"shop_domain":  "abc.myshopify.com",
		"warehouse_id": uuid.New().String(),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/shopify/shops", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d; want 201: body=%s", w.Code, w.Body.String())
	}
	if capB.got.CreatorID != tenant {
		t.Errorf("CreatorID = %s; want fallback to tenant %s when sub absent", capB.got.CreatorID, tenant)
	}
}

// ---- List: tenant isolation, error, empty-slice JSON shape -----------------

func TestList_NoTenant_Returns401(t *testing.T) {
	h := handlershopify.New(&fakeBinderZZ{}, &fakeListerZZ{}, &fakeUnbinderZZ{})
	r := zzRouterNoTenant(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/shopify/shops", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d; want 401: body=%s", w.Code, w.Body.String())
	}
}

func TestList_UseCaseError_Returns500(t *testing.T) {
	h := handlershopify.New(&fakeBinderZZ{}, &fakeListerZZ{err: errors.New("db down")}, &fakeUnbinderZZ{})
	r := zzRouter(h, "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/shopify/shops", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d; want 500: body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["error"] != "internal error" {
		t.Errorf("error = %q; want %q", resp["error"], "internal error")
	}
}

// TestList_EmptyResult_JSONIsEmptyArrayNotNull asserts the make([]shopDTO, 0,
// len(items)) guarantee: an empty items slice must serialize to `"items":[]`,
// never `"items":null`, so API consumers can always range over it safely.
func TestList_EmptyResult_JSONIsEmptyArrayNotNull(t *testing.T) {
	h := handlershopify.New(&fakeBinderZZ{}, &fakeListerZZ{items: []appshopify.ShopMapping{}}, &fakeUnbinderZZ{})
	r := zzRouter(h, "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/shopify/shops", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200: body=%s", w.Code, w.Body.String())
	}
	got := strings.TrimSpace(w.Body.String())
	want := `{"items":[]}`
	if got != want {
		t.Errorf("body = %q; want %q (must be [] not null)", got, want)
	}
}

// TestList_EmptyResult_NilItemsAlsoSerializesToEmptyArray covers the case
// where the use case returns a nil slice (not just an already-empty one) —
// the handler's make(..., 0, len(items)) must still normalize it to [].
func TestList_NilItems_JSONIsEmptyArrayNotNull(t *testing.T) {
	h := handlershopify.New(&fakeBinderZZ{}, &fakeListerZZ{items: nil}, &fakeUnbinderZZ{})
	r := zzRouter(h, "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/shopify/shops", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200: body=%s", w.Code, w.Body.String())
	}
	got := strings.TrimSpace(w.Body.String())
	want := `{"items":[]}`
	if got != want {
		t.Errorf("body = %q; want %q (must be [] not null)", got, want)
	}
}

func TestList_MultipleItems_AllFieldsMapped(t *testing.T) {
	m1 := appshopify.ShopMapping{ID: uuid.New(), ShopDomain: "a.myshopify.com", WarehouseID: uuid.New(), CreatorID: uuid.New()}
	m2 := appshopify.ShopMapping{ID: uuid.New(), ShopDomain: "b.myshopify.com", WarehouseID: uuid.New(), CreatorID: uuid.New()}
	h := handlershopify.New(&fakeBinderZZ{}, &fakeListerZZ{items: []appshopify.ShopMapping{m1, m2}}, &fakeUnbinderZZ{})
	r := zzRouter(h, "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/shopify/shops", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200: body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Items []struct {
			ID          string `json:"id"`
			ShopDomain  string `json:"shop_domain"`
			WarehouseID string `json:"warehouse_id"`
			CreatorID   string `json:"creator_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("len(items) = %d; want 2", len(resp.Items))
	}
	if resp.Items[0].ID != m1.ID.String() || resp.Items[0].ShopDomain != m1.ShopDomain ||
		resp.Items[0].WarehouseID != m1.WarehouseID.String() || resp.Items[0].CreatorID != m1.CreatorID.String() {
		t.Errorf("items[0] = %+v; want mapped from %+v", resp.Items[0], m1)
	}
	if resp.Items[1].ID != m2.ID.String() || resp.Items[1].ShopDomain != m2.ShopDomain ||
		resp.Items[1].WarehouseID != m2.WarehouseID.String() || resp.Items[1].CreatorID != m2.CreatorID.String() {
		t.Errorf("items[1] = %+v; want mapped from %+v", resp.Items[1], m2)
	}
}

// ---- Unbind: tenant isolation + invalid id + idempotent success -----------

func TestUnbind_NoTenant_Returns401(t *testing.T) {
	h := handlershopify.New(&fakeBinderZZ{}, &fakeListerZZ{}, &fakeUnbinderZZ{})
	r := zzRouterNoTenant(h)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/shopify/shops/"+uuid.New().String(), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d; want 401: body=%s", w.Code, w.Body.String())
	}
}

func TestUnbind_InvalidID_Returns400(t *testing.T) {
	h := handlershopify.New(&fakeBinderZZ{}, &fakeListerZZ{}, &fakeUnbinderZZ{})
	r := zzRouter(h, "")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/shopify/shops/not-a-uuid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400: body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["error"] != "invalid id" {
		t.Errorf("error = %q; want %q", resp["error"], "invalid id")
	}
}

// TestUnbind_NonExistentID_StillReturns204 asserts idempotency: deleting a
// binding that does not exist (fake returns no error, mirroring a repo whose
// DELETE affected 0 rows) must still succeed with 204, not 404 — repeated
// unbind calls from a flaky client must never error.
func TestUnbind_NonExistentID_StillReturns204(t *testing.T) {
	h := handlershopify.New(&fakeBinderZZ{}, &fakeListerZZ{}, &fakeUnbinderZZ{err: nil})
	r := zzRouter(h, "")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/shopify/shops/"+uuid.New().String(), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d; want 204 (unbind must be idempotent): body=%s", w.Code, w.Body.String())
	}
}

// ---- RegisterRoutes wiring sanity ------------------------------------------
//
// Verifies all three routes are actually mounted at the documented paths
// (a routing typo here would silently 404 every request in production).
func TestRegisterRoutes_AllThreeRoutesMounted(t *testing.T) {
	h := handlershopify.New(&fakeBinderZZ{}, &fakeListerZZ{}, &fakeUnbinderZZ{})
	r := zzRouter(h, "")

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/shopify/shops"},
		{http.MethodDelete, "/api/v1/shopify/shops/" + uuid.New().String()},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Errorf("%s %s: got 404, route not registered", tc.method, tc.path)
		}
	}
}
