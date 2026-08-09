package replenish_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	handlerreplenish "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/replenish"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appreplenish "github.com/hanmahong5-arch/lurus-tally/internal/app/replenish"
)

// newTestEngineWithSub is like newTestEngine (defined in handler_test.go) but
// also injects an OIDC subject into the gin context when subVal is non-empty,
// covering the resolveCreatorID JWT-sub branch (vs the tenant-fallback branch
// already exercised by handler_test.go).
func newTestEngineWithSub(h *handlerreplenish.Handler, tenantID uuid.UUID, subVal string, setSub bool) *gin.Engine {
	e := gin.New()
	e.Use(func(c *gin.Context) {
		if tenantID != uuid.Nil {
			c.Set(middleware.CtxKeyTenantID, tenantID)
		}
		if setSub {
			c.Set(middleware.CtxKeyIDPSubject, subVal)
		}
		c.Next()
	})
	h.RegisterRoutes(e.Group("/api/v1"))
	return e
}

// ----- PostDraftBatch: 501 when h.batch == nil (constructed via New, no batch wired) -----

func TestPostDraftBatch_NotWired_Returns501(t *testing.T) {
	h := handlerreplenish.New(&stubUseCase{}) // no NewWithBatch → h.batch is nil
	e := newTestEngine(h, uuid.New())

	body := `{"lines":[{"product_id":"` + uuid.New().String() + `","qty":"5"}]}`
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/replenish/draft-batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Errorf("expected 501, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// ----- PostDraftBatch: 401 when tenant is missing (checked before binding) -----

func TestPostDraftBatch_NoTenant_Returns401(t *testing.T) {
	batch := &stubBatchUC{}
	h := handlerreplenish.NewWithBatch(&stubUseCase{}, batch)
	e := newTestEngine(h, uuid.Nil)

	body := `{"lines":[{"product_id":"` + uuid.New().String() + `","qty":"5"}]}`
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/replenish/draft-batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// ----- PostDraftBatch: qty validation (business invariant: no zero/negative qty) -----

func TestPostDraftBatch_QtyValidation_RejectsNonPositive(t *testing.T) {
	tests := []struct {
		name string
		qty  string
	}{
		{"negative", "-1"},
		{"zero", "0"},
		{"unparseable", "not-a-number"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			batch := &stubBatchUC{}
			h := handlerreplenish.NewWithBatch(&stubUseCase{}, batch)
			e := newTestEngine(h, uuid.New())

			body := `{"lines":[{"product_id":"` + uuid.New().String() + `","qty":"` + tt.qty + `"}]}`
			req, _ := http.NewRequest(http.MethodPost, "/api/v1/replenish/draft-batch", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("qty=%s: expected 400, got %d body=%s", tt.qty, rec.Code, rec.Body.String())
			}
			var resp map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if resp["detail"] != "qty must be a positive number" {
				t.Errorf("qty=%s: detail = %q, want %q", tt.qty, resp["detail"], "qty must be a positive number")
			}
			// The use case must never be invoked when validation fails — no side effect.
			if batch.gotCreator != uuid.Nil {
				t.Errorf("qty=%s: use case should not have been invoked", tt.qty)
			}
		})
	}
}

// ----- PostDraftBatch: request-body binding failures -----

func TestPostDraftBatch_EmptyLines_Returns400(t *testing.T) {
	batch := &stubBatchUC{}
	h := handlerreplenish.NewWithBatch(&stubUseCase{}, batch)
	e := newTestEngine(h, uuid.New())

	body := `{"lines":[]}`
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/replenish/draft-batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty lines, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPostDraftBatch_TooManyLines_Returns400(t *testing.T) {
	batch := &stubBatchUC{}
	h := handlerreplenish.NewWithBatch(&stubUseCase{}, batch)
	e := newTestEngine(h, uuid.New())

	var sb strings.Builder
	sb.WriteString(`{"lines":[`)
	for i := 0; i < 201; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"product_id":"` + uuid.New().String() + `","qty":"1"}`)
	}
	sb.WriteString(`]}`)

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/replenish/draft-batch", strings.NewReader(sb.String()))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for >200 lines, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPostDraftBatch_MissingProductID_Returns400(t *testing.T) {
	batch := &stubBatchUC{}
	h := handlerreplenish.NewWithBatch(&stubUseCase{}, batch)
	e := newTestEngine(h, uuid.New())

	body := `{"lines":[{"qty":"5"}]}`
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/replenish/draft-batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing product_id, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPostDraftBatch_NonUUIDProductID_Returns400(t *testing.T) {
	batch := &stubBatchUC{}
	h := handlerreplenish.NewWithBatch(&stubUseCase{}, batch)
	e := newTestEngine(h, uuid.New())

	body := `{"lines":[{"product_id":"not-a-uuid","qty":"5"}]}`
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/replenish/draft-batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-uuid product_id (binding tag), got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPostDraftBatch_InvalidSupplierID_Returns400(t *testing.T) {
	batch := &stubBatchUC{}
	h := handlerreplenish.NewWithBatch(&stubUseCase{}, batch)
	e := newTestEngine(h, uuid.New())

	body := `{"lines":[{"product_id":"` + uuid.New().String() + `","supplier_id":"not-a-uuid","qty":"5"}]}`
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/replenish/draft-batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid supplier_id, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(resp["detail"], "invalid supplier_id") {
		t.Errorf("detail = %q, want it to mention invalid supplier_id", resp["detail"])
	}
}

func TestPostDraftBatch_MalformedJSON_Returns400(t *testing.T) {
	batch := &stubBatchUC{}
	h := handlerreplenish.NewWithBatch(&stubUseCase{}, batch)
	e := newTestEngine(h, uuid.New())

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/replenish/draft-batch", strings.NewReader(`{not json`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for malformed JSON, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// ----- resolveCreatorID branches -----

// TestPostDraftBatch_CreatorID_UsesValidSub verifies that when the OIDC sub is
// a valid UUID, it is used as creator_id (not overridden by the tenant
// fallback). handler_test.go already covers the "sub absent" fallback path.
func TestPostDraftBatch_CreatorID_UsesValidSub(t *testing.T) {
	tenantID := uuid.New()
	sub := uuid.New()
	batch := &stubBatchUC{}
	h := handlerreplenish.NewWithBatch(&stubUseCase{}, batch)
	e := newTestEngineWithSub(h, tenantID, sub.String(), true)

	body := `{"lines":[{"product_id":"` + uuid.New().String() + `","qty":"5"}]}`
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/replenish/draft-batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if batch.gotCreator != sub {
		t.Errorf("creator_id: got %v, want sub %v", batch.gotCreator, sub)
	}
}

// TestPostDraftBatch_CreatorID_NonUUIDSubFallsBackToTenant verifies that a
// present-but-non-UUID sub (malformed claim) is treated as uuid.Nil and falls
// back to the tenant id — the same as an absent sub.
func TestPostDraftBatch_CreatorID_NonUUIDSubFallsBackToTenant(t *testing.T) {
	tenantID := uuid.New()
	batch := &stubBatchUC{}
	h := handlerreplenish.NewWithBatch(&stubUseCase{}, batch)
	e := newTestEngineWithSub(h, tenantID, "not-a-uuid-sub", true)

	body := `{"lines":[{"product_id":"` + uuid.New().String() + `","qty":"5"}]}`
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/replenish/draft-batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if batch.gotCreator != tenantID {
		t.Errorf("creator_id: got %v, want tenant fallback %v", batch.gotCreator, tenantID)
	}
}

// ----- PostDraftBatch: use case error → 500 -----

// stubBatchUCErr is a CreateDraftBatchUseCase double that always errors.
type stubBatchUCErr struct{}

func (s *stubBatchUCErr) Execute(_ context.Context, _ appreplenish.DraftBatchRequest) (*appreplenish.DraftBatchOutput, error) {
	return nil, context.DeadlineExceeded
}

func TestPostDraftBatch_UsecaseError_Returns500(t *testing.T) {
	h := handlerreplenish.NewWithBatch(&stubUseCase{}, &stubBatchUCErr{})
	e := newTestEngine(h, uuid.New())

	body := `{"lines":[{"product_id":"` + uuid.New().String() + `","qty":"5"}]}`
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/replenish/draft-batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// ----- PostDraftBatch: response mapping — SupplierID/SupplierName present vs nil -----

// stubBatchUCFixed returns a fixed DraftBatchOutput regardless of input, so
// the test can assert the exact JSON shape produced from DraftResult mapping.
type stubBatchUCFixed struct {
	out *appreplenish.DraftBatchOutput
}

func (s *stubBatchUCFixed) Execute(_ context.Context, _ appreplenish.DraftBatchRequest) (*appreplenish.DraftBatchOutput, error) {
	return s.out, nil
}

func TestPostDraftBatch_ResponseMapping_SupplierIDPresentAndNil(t *testing.T) {
	billIDWithSupplier := uuid.New()
	billIDNoSupplier := uuid.New()
	supplierID := uuid.New()

	out := &appreplenish.DraftBatchOutput{
		Drafts: []appreplenish.DraftResult{
			{
				BillID:       billIDWithSupplier,
				BillNo:       "PO-0001",
				SupplierID:   &supplierID,
				SupplierName: "Acme Corp",
				LineCount:    2,
			},
			{
				BillID:       billIDNoSupplier,
				BillNo:       "PO-0002",
				SupplierID:   nil,
				SupplierName: "",
				LineCount:    1,
			},
		},
	}

	h := handlerreplenish.NewWithBatch(&stubUseCase{}, &stubBatchUCFixed{out: out})
	e := newTestEngine(h, uuid.New())

	body := `{"lines":[{"product_id":"` + uuid.New().String() + `","qty":"3"},{"product_id":"` + uuid.New().String() + `","qty":"2"}]}`
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/replenish/draft-batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Drafts []map[string]json.RawMessage `json:"drafts"`
		Count  int                          `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Count != 2 || len(resp.Drafts) != 2 {
		t.Fatalf("expected 2 drafts, got count=%d drafts=%d", resp.Count, len(resp.Drafts))
	}

	// First draft has a supplier — supplier_id key present.
	if _, present := resp.Drafts[0]["supplier_id"]; !present {
		t.Error("drafts[0]: expected supplier_id key present when SupplierID != nil")
	}
	var sid string
	if err := json.Unmarshal(resp.Drafts[0]["supplier_id"], &sid); err != nil {
		t.Fatalf("unmarshal supplier_id: %v", err)
	}
	if sid != supplierID.String() {
		t.Errorf("supplier_id = %s, want %s", sid, supplierID.String())
	}

	// Second draft has no supplier — omitempty should drop the key entirely.
	if _, present := resp.Drafts[1]["supplier_id"]; present {
		t.Error("drafts[1]: expected supplier_id key absent when SupplierID == nil")
	}
}

// ----- Also cover the accept-with-supplier_id happy path (valid UUID) -----

func TestPostDraftBatch_ValidSupplierID_Accepted(t *testing.T) {
	tenantID := uuid.New()
	batch := &stubBatchUC{}
	h := handlerreplenish.NewWithBatch(&stubUseCase{}, batch)
	e := newTestEngine(h, tenantID)

	supplierID := uuid.New()
	body := `{"lines":[{"product_id":"` + uuid.New().String() + `","supplier_id":"` + supplierID.String() + `","qty":"5"}]}`
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/replenish/draft-batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// ----- weeks query param edge cases -----

func TestGetSuggestions_WeeksParam_EdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  int
	}{
		{"absent", "", 2},
		{"zero", "weeks=0", 2},
		{"negative", "weeks=-3", 2},
		{"explicit_five", "weeks=5", 5},
		{"non_numeric", "weeks=abc", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := handlerreplenish.New(&stubUseCase{rows: nil})
			e := newTestEngine(h, uuid.New())

			url := "/api/v1/replenish/suggestions"
			if tt.query != "" {
				url += "?" + tt.query
			}
			req, _ := http.NewRequest(http.MethodGet, url, nil)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("query=%s: expected 200, got %d body=%s", tt.query, rec.Code, rec.Body.String())
			}
			var resp struct {
				Weeks int `json:"weeks"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if resp.Weeks != tt.want {
				t.Errorf("query=%s: weeks = %d, want %d", tt.query, resp.Weeks, tt.want)
			}
		})
	}
}
