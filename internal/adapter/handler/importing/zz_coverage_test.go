package importing_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	handlerimporting "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/importing"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appimporting "github.com/hanmahong5-arch/lurus-tally/internal/app/importing"
)

// ----- fake use case ---------------------------------------------------------

// zzFakeUC is a self-contained fake ImportUseCase. Each test constructs its own
// instance (never shared across sub-tests) so there is no cross-goroutine
// mutable state to race on.
type zzFakeUC struct {
	execResult *appimporting.ImportResult
	execErr    error
	listResult []appimporting.SKUMapping
	listErr    error

	gotReq          appimporting.ImportRequest
	gotListTenant   uuid.UUID
	gotListPlatform string
}

func (f *zzFakeUC) Execute(_ context.Context, req appimporting.ImportRequest) (*appimporting.ImportResult, error) {
	f.gotReq = req
	if f.execErr != nil {
		return nil, f.execErr
	}
	if f.execResult != nil {
		return f.execResult, nil
	}
	return &appimporting.ImportResult{}, nil
}

func (f *zzFakeUC) ListMappings(_ context.Context, tenantID uuid.UUID, platform string) ([]appimporting.SKUMapping, error) {
	f.gotListTenant = tenantID
	f.gotListPlatform = platform
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listResult, nil
}

// ----- router builder ---------------------------------------------------------

// zzNewRouter mounts the handler with the given default warehouse, optionally
// seeding tenant_id and idp_subject context keys the way AuthMiddleware would.
func zzNewRouter(uc handlerimporting.ImportUseCase, tenant uuid.UUID, idpSub string, defaultWarehouse uuid.UUID) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	grp := r.Group("/api/v1")
	grp.Use(func(c *gin.Context) {
		if tenant != uuid.Nil {
			c.Set(middleware.CtxKeyTenantID, tenant)
		}
		if idpSub != "" {
			c.Set(middleware.CtxKeyIDPSubject, idpSub)
		}
		c.Next()
	})
	handlerimporting.New(uc, defaultWarehouse).RegisterRoutes(grp)
	return r
}

// zzBuildMultipart builds a multipart/form-data body. When platform/warehouse
// are empty strings the corresponding field is omitted entirely (matching the
// real client behaviour of not sending an unset form field). includeFile
// controls whether the "file" field is attached; fileBytes lets the caller
// force an oversized body.
func zzBuildMultipart(t *testing.T, platform, warehouse string, includeFile bool, fileBytes []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	if platform != "" {
		if err := w.WriteField("platform", platform); err != nil {
			t.Fatalf("WriteField platform: %v", err)
		}
	}
	if warehouse != "" {
		if err := w.WriteField("warehouse", warehouse); err != nil {
			t.Fatalf("WriteField warehouse: %v", err)
		}
	}
	if includeFile {
		if fileBytes == nil {
			fileBytes = []byte("order_no,sku,qty\nA-1,SKU-1,2\n")
		}
		fw, err := w.CreateFormFile("file", "orders.csv")
		if err != nil {
			t.Fatalf("CreateFormFile: %v", err)
		}
		if _, err := fw.Write(fileBytes); err != nil {
			t.Fatalf("write file bytes: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("multipart close: %v", err)
	}
	return body, w.FormDataContentType()
}

func zzDecodeBody(t *testing.T, rec *httptest.ResponseRecorder, v interface{}) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decode response body %q: %v", rec.Body.String(), err)
	}
}

// ----- ImportOrders: validation / auth / error status table -----------------

func TestImportOrders_ValidationAndAuthTable(t *testing.T) {
	validWarehouse := uuid.New()

	cases := []struct {
		name             string
		tenant           uuid.UUID // uuid.Nil => unauthenticated request
		defaultWarehouse uuid.UUID
		platform         string
		warehouse        string
		includeFile      bool
		execErr          error
		wantStatus       int
		wantDetailSubstr string
	}{
		{
			name:             "no tenant -> 401",
			tenant:           uuid.Nil,
			defaultWarehouse: validWarehouse,
			platform:         "amazon",
			includeFile:      true,
			wantStatus:       http.StatusUnauthorized,
		},
		{
			name:             "invalid platform -> 400 validation_error",
			tenant:           uuid.New(),
			defaultWarehouse: validWarehouse,
			platform:         "ebay",
			includeFile:      true,
			wantStatus:       http.StatusBadRequest,
			wantDetailSubstr: "unknown platform",
		},
		{
			name:             "warehouse form field non-UUID -> 400",
			tenant:           uuid.New(),
			defaultWarehouse: uuid.Nil,
			platform:         "amazon",
			warehouse:        "not-a-uuid",
			includeFile:      true,
			wantStatus:       http.StatusBadRequest,
			wantDetailSubstr: "invalid warehouse UUID",
		},
		{
			name:             "no default warehouse and no field -> 400 warehouse required",
			tenant:           uuid.New(),
			defaultWarehouse: uuid.Nil,
			platform:         "amazon",
			includeFile:      true,
			wantStatus:       http.StatusBadRequest,
			wantDetailSubstr: "warehouse is required",
		},
		{
			name:             "missing file field -> 400",
			tenant:           uuid.New(),
			defaultWarehouse: validWarehouse,
			platform:         "amazon",
			includeFile:      false,
			wantStatus:       http.StatusBadRequest,
			wantDetailSubstr: "missing form field 'file'",
		},
		{
			name:             "use case Execute error -> 422 import_failed",
			tenant:           uuid.New(),
			defaultWarehouse: validWarehouse,
			platform:         "amazon",
			includeFile:      true,
			execErr:          errors.New("warehouse does not belong to tenant"),
			wantStatus:       http.StatusUnprocessableEntity,
			wantDetailSubstr: "warehouse does not belong to tenant",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uc := &zzFakeUC{execErr: tc.execErr}
			r := zzNewRouter(uc, tc.tenant, "", tc.defaultWarehouse)

			body, contentType := zzBuildMultipart(t, tc.platform, tc.warehouse, tc.includeFile, nil)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/imports/orders", body)
			req.Header.Set("Content-Type", contentType)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantDetailSubstr != "" && !strings.Contains(rec.Body.String(), tc.wantDetailSubstr) {
				t.Fatalf("body %q does not contain %q", rec.Body.String(), tc.wantDetailSubstr)
			}
		})
	}
}

// TestImportOrders_MultipartTooLarge asserts the 10MB MaxBytesReader guard:
// a body whose file field alone exceeds maxUploadBytes must fail
// ParseMultipartForm and surface a 400, never reach the use case.
func TestImportOrders_MultipartTooLarge(t *testing.T) {
	uc := &zzFakeUC{}
	r := zzNewRouter(uc, uuid.New(), "", uuid.New())

	oversized := bytes.Repeat([]byte("x"), 11*1024*1024) // 11MiB > 10MiB cap
	body, contentType := zzBuildMultipart(t, "amazon", "", true, oversized)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/imports/orders", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if uc.gotReq.TenantID != uuid.Nil {
		t.Fatalf("use case should never have been invoked for an oversized body")
	}
}

// TestImportOrders_CreatorIDFallback verifies the creator_id fallback business
// invariant (handler.go:69-72): PAT-style requests (no IDP subject) must pass
// creatorID == tenantID so the use case never rejects with "creator_id is
// required"; requests carrying a valid IDP subject must pass the parsed UUID.
func TestImportOrders_CreatorIDFallback(t *testing.T) {
	validSub := uuid.New()

	cases := []struct {
		name   string
		idpSub string
		wantFn func(tenant uuid.UUID) uuid.UUID
	}{
		{
			name:   "no idp subject falls back to tenant id",
			idpSub: "",
			wantFn: func(tenant uuid.UUID) uuid.UUID { return tenant },
		},
		{
			name:   "valid idp subject uuid is used as creator id",
			idpSub: validSub.String(),
			wantFn: func(uuid.UUID) uuid.UUID { return validSub },
		},
		{
			name:   "non-uuid idp subject falls back to tenant id",
			idpSub: "not-a-uuid-subject",
			wantFn: func(tenant uuid.UUID) uuid.UUID { return tenant },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tenant := uuid.New()
			warehouse := uuid.New()
			uc := &zzFakeUC{}
			r := zzNewRouter(uc, tenant, tc.idpSub, warehouse)

			body, contentType := zzBuildMultipart(t, "amazon", "", true, nil)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/imports/orders", body)
			req.Header.Set("Content-Type", contentType)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			want := tc.wantFn(tenant)
			if uc.gotReq.CreatorID != want {
				t.Fatalf("CreatorID = %s, want %s", uc.gotReq.CreatorID, want)
			}
			if uc.gotReq.TenantID != tenant {
				t.Fatalf("TenantID = %s, want %s", uc.gotReq.TenantID, tenant)
			}
		})
	}
}

// ----- ImportOrders: status-code business invariant (preview vs create) ------

// TestImportOrders_PreviewNeverCreates asserts: preview=true always returns 200
// even when Imported is non-empty (no bill creation happened); only a
// non-preview request with a non-empty Imported slice returns 201.
func TestImportOrders_PreviewNeverCreates(t *testing.T) {
	nonEmptyResult := &appimporting.ImportResult{
		Imported: []appimporting.ImportedOrder{{PlatformOrderNo: "A-1", BillNo: "(preview)"}},
	}

	cases := []struct {
		name       string
		preview    bool
		result     *appimporting.ImportResult
		wantStatus int
	}{
		{
			name:       "preview=true with non-empty Imported -> 200, never 201",
			preview:    true,
			result:     nonEmptyResult,
			wantStatus: http.StatusOK,
		},
		{
			name:       "preview=false with non-empty Imported -> 201",
			preview:    false,
			result:     nonEmptyResult,
			wantStatus: http.StatusCreated,
		},
		{
			name:       "preview=false with empty Imported -> 200",
			preview:    false,
			result:     &appimporting.ImportResult{},
			wantStatus: http.StatusOK,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uc := &zzFakeUC{execResult: tc.result}
			r := zzNewRouter(uc, uuid.New(), "", uuid.New())

			body, contentType := zzBuildMultipart(t, "amazon", "", true, nil)
			target := "/api/v1/imports/orders"
			if tc.preview {
				target += "?preview=true"
			}
			req := httptest.NewRequest(http.MethodPost, target, body)
			req.Header.Set("Content-Type", contentType)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.preview != (uc.gotReq.DryRun) {
				t.Fatalf("DryRun passed to use case = %v, want %v", uc.gotReq.DryRun, tc.preview)
			}
		})
	}
}

// zzResultDTO mirrors the unexported importResultDTO shape so the test can
// decode and assert on it without depending on unexported types.
type zzResultDTO struct {
	Imported []struct {
		PlatformOrderNo string `json:"platform_order_no"`
		BillID          string `json:"bill_id"`
		BillNo          string `json:"bill_no"`
	} `json:"imported"`
	Skipped []struct {
		PlatformOrderNo string `json:"platform_order_no"`
		Reason          string `json:"reason"`
	} `json:"skipped"`
	Oversells []struct {
		PlatformOrderNo string `json:"platform_order_no"`
		PlatformSKU     string `json:"platform_sku"`
		ProductID       string `json:"product_id"`
		Requested       string `json:"requested_qty"`
		Available       string `json:"available_qty"`
	} `json:"oversells"`
	UnknownSKUs []struct {
		Platform    string `json:"platform"`
		PlatformSKU string `json:"platform_sku"`
	} `json:"unknown_skus"`
	Summary struct {
		TotalParsed  int `json:"total_parsed"`
		Imported     int `json:"imported"`
		Skipped      int `json:"skipped"`
		OversellRows int `json:"oversell_rows"`
		UnknownSKUs  int `json:"unknown_skus"`
	} `json:"summary"`
}

// TestImportOrders_DTOMapping_OversellAndSummary drives a fixture ImportResult
// (Imported, Skipped, Oversells, UnknownSKUs all populated) through the real
// toResultDTO mapping and hand-computes the expected summary counts from the
// fixture — NOT by re-reading the DTO under test (anti self-validating).
func TestImportOrders_DTOMapping_OversellAndSummary(t *testing.T) {
	billID := uuid.New()
	oversellProductID := uuid.New()

	fixture := &appimporting.ImportResult{
		Imported: []appimporting.ImportedOrder{
			{PlatformOrderNo: "A-1", BillID: billID, BillNo: "SO-0001"},
		},
		Skipped: []appimporting.SkippedOrder{
			{PlatformOrderNo: "A-2", Reason: "duplicate:bill_id=deadbeef"},
		},
		Oversells: []appimporting.OversellRow{
			{
				PlatformOrderNo: "A-3",
				PlatformSKU:     "SKU-3",
				ProductID:       oversellProductID,
				Requested:       decimal.NewFromInt(5),
				Available:       decimal.NewFromInt(2),
			},
		},
		UnknownSKUs: []appimporting.UnknownSKU{
			{Platform: "amazon", PlatformSKU: "SKU-X"},
		},
	}
	// Hand-computed expectations from the fixture above (fixed constants, not
	// derived from the handler's own output).
	const wantTotalParsed = 2 // len(Imported)=1 + len(Skipped)=1
	const wantImported = 1
	const wantSkipped = 1
	const wantOversellRows = 1
	const wantUnknownSKUs = 1

	uc := &zzFakeUC{execResult: fixture}
	r := zzNewRouter(uc, uuid.New(), "", uuid.New())

	body, contentType := zzBuildMultipart(t, "amazon", "", true, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/imports/orders?preview=true", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var dto zzResultDTO
	zzDecodeBody(t, rec, &dto)

	if dto.Summary.TotalParsed != wantTotalParsed {
		t.Errorf("Summary.TotalParsed = %d, want %d", dto.Summary.TotalParsed, wantTotalParsed)
	}
	if dto.Summary.Imported != wantImported {
		t.Errorf("Summary.Imported = %d, want %d", dto.Summary.Imported, wantImported)
	}
	if dto.Summary.Skipped != wantSkipped {
		t.Errorf("Summary.Skipped = %d, want %d", dto.Summary.Skipped, wantSkipped)
	}
	if dto.Summary.OversellRows != wantOversellRows {
		t.Errorf("Summary.OversellRows = %d, want %d", dto.Summary.OversellRows, wantOversellRows)
	}
	if dto.Summary.UnknownSKUs != wantUnknownSKUs {
		t.Errorf("Summary.UnknownSKUs = %d, want %d", dto.Summary.UnknownSKUs, wantUnknownSKUs)
	}

	// Business invariant (F06 fix): oversell rows must not be dropped, and
	// PlatformSKU must be populated so the UI can display which SKU oversold.
	if len(dto.Oversells) != 1 {
		t.Fatalf("Oversells len = %d, want 1", len(dto.Oversells))
	}
	ov := dto.Oversells[0]
	if ov.PlatformSKU != "SKU-3" {
		t.Errorf("Oversells[0].PlatformSKU = %q, want %q", ov.PlatformSKU, "SKU-3")
	}
	if ov.PlatformOrderNo != "A-3" {
		t.Errorf("Oversells[0].PlatformOrderNo = %q, want %q", ov.PlatformOrderNo, "A-3")
	}
	if ov.ProductID != oversellProductID.String() {
		t.Errorf("Oversells[0].ProductID = %q, want %q", ov.ProductID, oversellProductID.String())
	}
	if ov.Requested != "5" {
		t.Errorf("Oversells[0].Requested = %q, want %q", ov.Requested, "5")
	}
	if ov.Available != "2" {
		t.Errorf("Oversells[0].Available = %q, want %q", ov.Available, "2")
	}

	if len(dto.Imported) != 1 || dto.Imported[0].BillID != billID.String() || dto.Imported[0].BillNo != "SO-0001" {
		t.Errorf("Imported[0] = %+v, want BillID=%s BillNo=SO-0001", dto.Imported, billID.String())
	}
	if len(dto.Skipped) != 1 || dto.Skipped[0].Reason != "duplicate:bill_id=deadbeef" {
		t.Errorf("Skipped[0] = %+v", dto.Skipped)
	}
	if len(dto.UnknownSKUs) != 1 || dto.UnknownSKUs[0].PlatformSKU != "SKU-X" {
		t.Errorf("UnknownSKUs = %+v", dto.UnknownSKUs)
	}
}

// TestImportOrders_ImportedOrder_ZeroBillID covers the branch where BillID is
// uuid.Nil (e.g. a preview-mode ImportedOrder) — toResultDTO must leave
// bill_id empty rather than emitting the nil UUID string.
func TestImportOrders_ImportedOrder_ZeroBillID(t *testing.T) {
	fixture := &appimporting.ImportResult{
		Imported: []appimporting.ImportedOrder{
			{PlatformOrderNo: "A-1", BillID: uuid.Nil, BillNo: "(preview)"},
		},
	}
	uc := &zzFakeUC{execResult: fixture}
	r := zzNewRouter(uc, uuid.New(), "", uuid.New())

	body, contentType := zzBuildMultipart(t, "amazon", "", true, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/imports/orders?preview=true", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	var dto zzResultDTO
	zzDecodeBody(t, rec, &dto)
	if len(dto.Imported) != 1 {
		t.Fatalf("Imported len = %d, want 1", len(dto.Imported))
	}
	if dto.Imported[0].BillID != "" {
		t.Errorf("BillID = %q, want empty for uuid.Nil", dto.Imported[0].BillID)
	}
}

// ----- ListMappings -----------------------------------------------------------

func TestListMappings_Unauthorized(t *testing.T) {
	uc := &zzFakeUC{}
	r := zzNewRouter(uc, uuid.Nil, "", uuid.Nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/imports/mappings", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

func TestListMappings_UseCaseError_Returns500(t *testing.T) {
	uc := &zzFakeUC{listErr: errors.New("db connection lost")}
	r := zzNewRouter(uc, uuid.New(), "", uuid.Nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/imports/mappings", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
	// httperr.WriteInternal must never leak the raw cause to the client.
	if strings.Contains(rec.Body.String(), "db connection lost") {
		t.Errorf("response body leaked internal cause: %s", rec.Body.String())
	}
}

func TestListMappings_Empty(t *testing.T) {
	uc := &zzFakeUC{listResult: nil}
	tenant := uuid.New()
	r := zzNewRouter(uc, tenant, "", uuid.Nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/imports/mappings?platform=amazon", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Items []json.RawMessage `json:"items"`
		Total int               `json:"total"`
	}
	zzDecodeBody(t, rec, &got)
	if got.Total != 0 {
		t.Errorf("total = %d, want 0", got.Total)
	}
	if got.Items == nil {
		t.Errorf("items must serialise as [] (empty slice), not null")
	}
	if len(got.Items) != 0 {
		t.Errorf("items len = %d, want 0", len(got.Items))
	}
	if uc.gotListTenant != tenant {
		t.Errorf("ListMappings called with tenant %s, want %s", uc.gotListTenant, tenant)
	}
	if uc.gotListPlatform != "amazon" {
		t.Errorf("ListMappings called with platform %q, want %q", uc.gotListPlatform, "amazon")
	}
}

func TestListMappings_NonEmpty(t *testing.T) {
	mappingID := uuid.New()
	productID := uuid.New()
	updatedAt := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

	uc := &zzFakeUC{listResult: []appimporting.SKUMapping{
		{
			ID:          mappingID,
			Platform:    "shopify",
			PlatformSKU: "SKU-9",
			ProductID:   productID,
			UpdatedAt:   updatedAt,
		},
	}}
	r := zzNewRouter(uc, uuid.New(), "", uuid.Nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/imports/mappings", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Items []struct {
			ID          string `json:"id"`
			Platform    string `json:"platform"`
			PlatformSKU string `json:"platform_sku"`
			ProductID   string `json:"product_id"`
			UpdatedAt   string `json:"updated_at"`
		} `json:"items"`
		Total int `json:"total"`
	}
	zzDecodeBody(t, rec, &got)

	if got.Total != 1 || len(got.Items) != 1 {
		t.Fatalf("got %+v, want exactly 1 item", got)
	}
	item := got.Items[0]
	if item.ID != mappingID.String() {
		t.Errorf("ID = %q, want %q", item.ID, mappingID.String())
	}
	if item.Platform != "shopify" {
		t.Errorf("Platform = %q, want shopify", item.Platform)
	}
	if item.PlatformSKU != "SKU-9" {
		t.Errorf("PlatformSKU = %q, want SKU-9", item.PlatformSKU)
	}
	if item.ProductID != productID.String() {
		t.Errorf("ProductID = %q, want %q", item.ProductID, productID.String())
	}
	if item.UpdatedAt != "2026-03-04T05:06:07Z" {
		t.Errorf("UpdatedAt = %q, want 2026-03-04T05:06:07Z", item.UpdatedAt)
	}
}
