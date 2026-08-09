package payment_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/shopspring/decimal"

	handlerpayment "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/payment"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/payment"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domainpayment "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
)

// ----- this file's own router builder, wired to set the OIDC-subject key too -----

// zcRouter is like newPaymentRouter but additionally propagates an X-Sub header
// into middleware.CtxKeyIDPSubject (as a string, or a non-string sentinel when
// requested), so we can drive resolveCreatorID's branches without touching the
// existing handler_test.go helpers.
func zcRouter(h *handlerpayment.Handler, subMode string, subValue string) *gin.Engine {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if raw := c.GetHeader("X-Tenant-ID"); raw != "" {
			if id, err := uuid.Parse(raw); err == nil {
				c.Set(middleware.CtxKeyTenantID, id)
			}
		}
		switch subMode {
		case "string":
			c.Set(middleware.CtxKeyIDPSubject, subValue)
		case "int":
			c.Set(middleware.CtxKeyIDPSubject, 42) // non-string type
		case "absent":
			// do nothing: key never set
		}
		c.Next()
	})
	api := r.Group("/api/v1")
	h.RegisterRoutes(api)
	return r
}

// noAuthRouter never injects tenant_id or idp_subject at all, regardless of headers,
// to exercise the tenantID == uuid.Nil -> 401 branch on both Record and List.
func noAuthRouter(h *handlerpayment.Handler) *gin.Engine {
	r := gin.New()
	api := r.Group("/api/v1")
	h.RegisterRoutes(api)
	return r
}

// ----- shared fakes (package-local names distinct from handler_test.go's ph* fakes) -----

type zcBillReader struct {
	bills map[uuid.UUID]*domain.BillHead
}

func newZCBillReader() *zcBillReader {
	return &zcBillReader{bills: make(map[uuid.UUID]*domain.BillHead)}
}

func (m *zcBillReader) WithTx(_ context.Context, fn func(tx *sql.Tx) error) error {
	return fn(nil) //nolint:staticcheck
}

func (m *zcBillReader) GetBillForUpdate(_ context.Context, _ *sql.Tx, _, billID uuid.UUID) (*domain.BillHead, error) {
	h, ok := m.bills[billID]
	if !ok {
		return nil, fmt.Errorf("bill not found")
	}
	return h, nil
}

func (m *zcBillReader) UpdatePaidAmount(_ context.Context, _ *sql.Tx, _, billID uuid.UUID, paidAmount decimal.Decimal) error {
	if h, ok := m.bills[billID]; ok {
		h.PaidAmount = paidAmount
	}
	return nil
}

var _ appbill.BillReader = (*zcBillReader)(nil)

// zcPaymentRepo captures every Record() call so tests can assert exactly what
// RecordPaymentRequest fields (Amount, CreatorID, TenantID) propagated through
// the handler and use case, satisfying the "not self-validating" requirement:
// expectations below are computed by hand from the request, not read back from
// the response body.
type zcPaymentRepo struct {
	recorded  []*domainpayment.Payment
	listErr   error
	listItems []*domainpayment.Payment
}

func (m *zcPaymentRepo) Record(_ context.Context, _ *sql.Tx, p *domainpayment.Payment) error {
	m.recorded = append(m.recorded, p)
	return nil
}

func (m *zcPaymentRepo) ListByBill(_ context.Context, _, _ uuid.UUID) ([]*domainpayment.Payment, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.listItems, nil
}

func (m *zcPaymentRepo) SumByBill(_ context.Context, _ *sql.Tx, _, _ uuid.UUID) (decimal.Decimal, error) {
	return decimal.Zero, nil
}

func (m *zcPaymentRepo) WithTx(_ context.Context, fn func(tx *sql.Tx) error) error {
	return fn(nil) //nolint:staticcheck
}

var _ appbill.PaymentRepo = (*zcPaymentRepo)(nil)

func zcBuildHandler(billReader *zcBillReader, payRepo *zcPaymentRepo) *handlerpayment.Handler {
	recordUC := appbill.NewRecordPaymentUseCase(billReader, payRepo)
	listUC := appbill.NewListPaymentsUseCase(payRepo)
	return handlerpayment.New(recordUC, listUC)
}

func zcApprovedBill(billID, tenantID uuid.UUID, total decimal.Decimal) *domain.BillHead {
	return &domain.BillHead{
		ID:          billID,
		TenantID:    tenantID,
		Status:      domain.StatusApproved,
		TotalAmount: total,
		CreatedAt:   time.Now(),
	}
}

func doRecord(r *gin.Engine, tenantID uuid.UUID, bodyJSON []byte, setTenantHeader bool) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/payments", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	if setTenantHeader {
		req.Header.Set("X-Tenant-ID", tenantID.String())
	}
	r.ServeHTTP(w, req)
	return w
}

func doList(r *gin.Engine, tenantID uuid.UUID, billIDQuery string, setTenantHeader bool) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	url := "/api/v1/payments"
	if billIDQuery != "" {
		url += "?bill_id=" + billIDQuery
	}
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if setTenantHeader {
		req.Header.Set("X-Tenant-ID", tenantID.String())
	}
	r.ServeHTTP(w, req)
	return w
}

// ===================== Record: tenant isolation =====================

// TestRecord_NoTenant_Returns401Unauthorized asserts the tenant-isolation
// invariant: tenantID == uuid.Nil must short-circuit with 401 and the use
// case's Record must never be invoked (no side effect on the repo).
func TestRecord_NoTenant_Returns401Unauthorized(t *testing.T) {
	billReader := newZCBillReader()
	payRepo := &zcPaymentRepo{}
	h := zcBuildHandler(billReader, payRepo)
	r := noAuthRouter(h)

	body, _ := json.Marshal(map[string]any{
		"bill_id": uuid.New().String(),
		"amount":  "100",
	})
	w := doRecord(r, uuid.Nil, body, false)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body: %s", w.Code, w.Body.String())
	}
	if len(payRepo.recorded) != 0 {
		t.Fatalf("recordUC.Execute must not be called when tenant is missing; got %d records", len(payRepo.recorded))
	}
}

// ===================== Record: malformed JSON =====================

func TestRecord_MalformedJSON_Returns400(t *testing.T) {
	billReader := newZCBillReader()
	payRepo := &zcPaymentRepo{}
	h := zcBuildHandler(billReader, payRepo)
	tenantID := uuid.New()
	r := zcRouter(h, "absent", "")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/payments", bytes.NewReader([]byte("{not-json")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantID.String())
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for malformed JSON; body: %s", w.Code, w.Body.String())
	}
	if len(payRepo.recorded) != 0 {
		t.Fatalf("recordUC.Execute must not be called on bind failure; got %d records", len(payRepo.recorded))
	}
}

// ===================== Record: bill_id validation =====================

func TestRecord_InvalidBillID_Returns400(t *testing.T) {
	billReader := newZCBillReader()
	payRepo := &zcPaymentRepo{}
	h := zcBuildHandler(billReader, payRepo)
	tenantID := uuid.New()
	r := zcRouter(h, "absent", "")

	body, _ := json.Marshal(map[string]any{
		"bill_id": "not-a-uuid",
		"amount":  "50",
	})
	w := doRecord(r, tenantID, body, true)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for invalid bill_id; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["message"] != "bill_id must be a valid UUID" {
		t.Errorf("message = %v, want exact bill_id UUID error", resp["message"])
	}
}

// ===================== Record: amount validation (table-driven) =====================

// TestRecord_AmountValidation_TableDriven hand-computes the expected status for
// every amount branch: parse failure, zero, negative, tiny-positive-ok,
// exactly-at-max-ok (GreaterThan is strict >, so equality must pass validation
// and proceed to the use case), and over-max-rejected.
func TestRecord_AmountValidation_TableDriven(t *testing.T) {
	cases := []struct {
		name       string
		amount     string
		wantStatus int
	}{
		{"unparseable_garbage", "not-a-number", http.StatusBadRequest},
		{"zero", "0", http.StatusBadRequest},
		{"negative", "-5.00", http.StatusBadRequest},
		{"tiny_positive_ok", "0.01", http.StatusCreated},
		{"exactly_at_max_ok", "10000000000", http.StatusCreated}, // == 1e10, GreaterThan is strict
		{"one_over_max_rejected", "10000000001", http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			billReader := newZCBillReader()
			payRepo := &zcPaymentRepo{}
			tenantID := uuid.New()
			billID := uuid.New()
			// Bill total large enough to absorb the max-amount case without
			// tripping the "exceeds bill total" use-case error, so we isolate
			// the handler's own validation branch.
			billReader.bills[billID] = zcApprovedBill(billID, tenantID, decimal.NewFromFloat(1e10).Add(decimal.NewFromInt(1000)))

			h := zcBuildHandler(billReader, payRepo)
			r := zcRouter(h, "absent", "")

			body, _ := json.Marshal(map[string]any{
				"bill_id": billID.String(),
				"amount":  tc.amount,
			})
			w := doRecord(r, tenantID, body, true)

			if w.Code != tc.wantStatus {
				t.Fatalf("amount=%q: status = %d, want %d; body: %s", tc.amount, w.Code, tc.wantStatus, w.Body.String())
			}
			wantRecorded := 0
			if tc.wantStatus == http.StatusCreated {
				wantRecorded = 1
			}
			if len(payRepo.recorded) != wantRecorded {
				t.Fatalf("amount=%q: recorded = %d, want %d", tc.amount, len(payRepo.recorded), wantRecorded)
			}
		})
	}
}

// ===================== Record: recordUC.Execute error -> 422 =====================

// TestRecord_UseCaseError_Returns422 drives the use case's own validation
// (bill not found in the repo -> GetBillForUpdate error -> Execute returns a
// wrapped error) and asserts the handler surfaces it as 422 payment_error,
// echoing the use-case error text (not a generic message).
func TestRecord_UseCaseError_Returns422(t *testing.T) {
	billReader := newZCBillReader() // no bills registered -> GetBillForUpdate fails
	payRepo := &zcPaymentRepo{}
	tenantID := uuid.New()
	billID := uuid.New()

	h := zcBuildHandler(billReader, payRepo)
	r := zcRouter(h, "absent", "")

	body, _ := json.Marshal(map[string]any{
		"bill_id": billID.String(),
		"amount":  "100",
	})
	w := doRecord(r, tenantID, body, true)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["error"] != "payment_error" {
		t.Errorf("error code = %v, want payment_error", resp["error"])
	}
	if len(payRepo.recorded) != 0 {
		t.Errorf("no payment should be persisted when the use case fails")
	}
}

// ===================== resolveCreatorID branches (table-driven) =====================

// TestRecord_CreatorIDResolution_TableDriven exercises every resolveCreatorID
// branch through the full HTTP path and asserts the CreatorID that actually
// reached the persisted domain Payment (captured by the fake repo), computed
// by hand per case rather than read back from the HTTP response.
func TestRecord_CreatorIDResolution_TableDriven(t *testing.T) {
	validSub := uuid.New()

	cases := []struct {
		name    string
		subMode string
		subVal  string
		// wantCreatorFromSub: when true, expected CreatorID == validSub (or
		// whatever subVal parses to); when false, expected CreatorID falls
		// back to tenantID (per resolveCreatorID + Record's fallback line).
		wantCreatorFromSub bool
	}{
		{"sub_absent_falls_back_to_tenant", "absent", "", false},
		{"sub_non_string_falls_back_to_tenant", "int", "", false},
		{"sub_invalid_uuid_falls_back_to_tenant", "string", "not-a-uuid", false},
		{"sub_valid_uuid_is_used", "string", validSub.String(), true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			billReader := newZCBillReader()
			payRepo := &zcPaymentRepo{}
			tenantID := uuid.New()
			billID := uuid.New()
			billReader.bills[billID] = zcApprovedBill(billID, tenantID, decimal.NewFromInt(1000))

			h := zcBuildHandler(billReader, payRepo)
			subVal := tc.subVal
			if tc.subMode == "string" && tc.wantCreatorFromSub {
				subVal = validSub.String()
			}
			r := zcRouter(h, tc.subMode, subVal)

			body, _ := json.Marshal(map[string]any{
				"bill_id": billID.String(),
				"amount":  "10",
			})
			w := doRecord(r, tenantID, body, true)

			if w.Code != http.StatusCreated {
				t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
			}
			if len(payRepo.recorded) != 1 {
				t.Fatalf("expected exactly 1 recorded payment, got %d", len(payRepo.recorded))
			}
			got := payRepo.recorded[0].CreatorID

			var want uuid.UUID
			if tc.wantCreatorFromSub {
				want = validSub
			} else {
				want = tenantID
			}
			if got != want {
				t.Errorf("CreatorID = %s, want %s (business invariant: creator resolved from OIDC sub, falling back to tenant, never from header/body)", got, want)
			}
			// TenantID must always propagate from context, never from the body.
			if payRepo.recorded[0].TenantID != tenantID {
				t.Errorf("TenantID = %s, want %s", payRepo.recorded[0].TenantID, tenantID)
			}
		})
	}
}

// TestRecord_CreatorID_NeverFromHeaderOrBody is the explicit anti-spoofing
// regression for UAT-3 Bug 2: even if a caller sets X-User-ID or a creator_id
// field in the JSON body, the persisted CreatorID must come only from the
// resolved OIDC subject / tenant fallback.
func TestRecord_CreatorID_NeverFromHeaderOrBody(t *testing.T) {
	billReader := newZCBillReader()
	payRepo := &zcPaymentRepo{}
	tenantID := uuid.New()
	billID := uuid.New()
	billReader.bills[billID] = zcApprovedBill(billID, tenantID, decimal.NewFromInt(1000))

	h := zcBuildHandler(billReader, payRepo)
	r := zcRouter(h, "absent", "") // no OIDC subject set -> must fall back to tenantID

	spoofedCreator := uuid.New()
	bodyMap := map[string]any{
		"bill_id":    billID.String(),
		"amount":     "10",
		"creator_id": spoofedCreator.String(), // recordRequest has no such field; must be ignored
	}
	b, _ := json.Marshal(bodyMap)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/payments", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantID.String())
	req.Header.Set("X-User-ID", spoofedCreator.String())
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	if len(payRepo.recorded) != 1 {
		t.Fatalf("expected 1 recorded payment, got %d", len(payRepo.recorded))
	}
	if payRepo.recorded[0].CreatorID == spoofedCreator {
		t.Fatalf("CreatorID must never be taken from header/body; got the spoofed value %s", spoofedCreator)
	}
	if payRepo.recorded[0].CreatorID != tenantID {
		t.Errorf("CreatorID = %s, want fallback tenantID %s", payRepo.recorded[0].CreatorID, tenantID)
	}
}

// ===================== Record success -> IncPaymentCreated metric fired =====================

// TestRecord_Success_IncrementsPaymentCreatedMetric asserts the counter
// tally_payment_created_total{currency="CNY",tenant_id=<tenant>} increments by
// exactly 1 on a successful record, computed as a before/after delta read off
// the real process-wide Prometheus default registry (middleware.IncPaymentCreated
// registers onto prometheus.MustRegister at package init) — production code
// produces the number, the +1 delta is hand-computed, not read-back-as-expectation.
func TestRecord_Success_IncrementsPaymentCreatedMetric(t *testing.T) {
	billReader := newZCBillReader()
	payRepo := &zcPaymentRepo{}
	tenantID := uuid.New()
	billID := uuid.New()
	billReader.bills[billID] = zcApprovedBill(billID, tenantID, decimal.NewFromInt(1000))

	h := zcBuildHandler(billReader, payRepo)
	r := zcRouter(h, "absent", "")

	before := gatherPaymentCreatedCounter(t, "CNY", tenantID.String())

	body, _ := json.Marshal(map[string]any{
		"bill_id": billID.String(),
		"amount":  "10",
	})
	w := doRecord(r, tenantID, body, true)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}

	after := gatherPaymentCreatedCounter(t, "CNY", tenantID.String())
	if after != before+1 {
		t.Errorf("tally_payment_created_total delta = %v, want +1 (before=%v after=%v)", after-before, before, after)
	}
}

// gatherPaymentCreatedCounter reads the current value of
// tally_payment_created_total{currency=currency,tenant_id=tenantID} straight
// off prometheus.DefaultGatherer (the same registry middleware.IncPaymentCreated
// writes to). Returns 0 if the series does not exist yet (first observation).
func gatherPaymentCreatedCounter(t *testing.T, currency, tenantID string) float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, mf := range families {
		if mf.GetName() != "tally_payment_created_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			if metricHasLabels(m, map[string]string{"currency": currency, "tenant_id": tenantID}) {
				return m.GetCounter().GetValue()
			}
		}
	}
	return 0
}

func metricHasLabels(m *dto.Metric, want map[string]string) bool {
	got := make(map[string]string, len(m.GetLabel()))
	for _, lp := range m.GetLabel() {
		got[lp.GetName()] = lp.GetValue()
	}
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
}

// ===================== List: tenant isolation and validation =====================

func TestList_NoTenant_Returns401(t *testing.T) {
	billReader := newZCBillReader()
	payRepo := &zcPaymentRepo{}
	h := zcBuildHandler(billReader, payRepo)
	r := noAuthRouter(h)

	w := doList(r, uuid.Nil, uuid.New().String(), false)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body: %s", w.Code, w.Body.String())
	}
}

func TestList_MissingBillID_Returns400(t *testing.T) {
	billReader := newZCBillReader()
	payRepo := &zcPaymentRepo{}
	tenantID := uuid.New()
	h := zcBuildHandler(billReader, payRepo)
	r := zcRouter(h, "absent", "")

	w := doList(r, tenantID, "", true)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["message"] != "bill_id query parameter is required" {
		t.Errorf("message = %v, want required-param message", resp["message"])
	}
}

func TestList_InvalidBillID_Returns400(t *testing.T) {
	billReader := newZCBillReader()
	payRepo := &zcPaymentRepo{}
	tenantID := uuid.New()
	h := zcBuildHandler(billReader, payRepo)
	r := zcRouter(h, "absent", "")

	w := doList(r, tenantID, "not-a-uuid", true)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

// TestList_UseCaseError_Returns500 forces listUC.Execute to return an error
// (via the fake repo's ListByBill) and asserts httperr.WriteInternal maps it
// to a generic 500 internal_error, per the httperr safe-body invariant.
func TestList_UseCaseError_Returns500(t *testing.T) {
	billReader := newZCBillReader()
	payRepo := &zcPaymentRepo{listErr: fmt.Errorf("boom: connection reset")}
	tenantID := uuid.New()
	billID := uuid.New()
	h := zcBuildHandler(billReader, payRepo)
	r := zcRouter(h, "absent", "")

	w := doList(r, tenantID, billID.String(), true)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "internal_error" {
		t.Errorf("error code = %v, want internal_error", resp["error"])
	}
	if msg, _ := resp["message"].(string); msg == "" || msg == "boom: connection reset" {
		t.Errorf("message = %q must be the safe static string, never the raw cause", msg)
	}
}

// TestList_Success_ReturnsItems asserts the happy path returns 200 with the
// items the fake repo held for that specific bill_id/tenant pair (bill-scoped
// isolation: the repo call itself is scoped by tenantID+billID upstream).
func TestList_Success_ReturnsItems(t *testing.T) {
	billReader := newZCBillReader()
	tenantID := uuid.New()
	billID := uuid.New()
	want := []*domainpayment.Payment{
		{ID: uuid.New(), TenantID: tenantID, BillID: billID, Amount: decimal.NewFromFloat(42.5), PayType: domainpayment.PayTypeWechat, PayDate: time.Now()},
	}
	payRepo := &zcPaymentRepo{listItems: want}
	h := zcBuildHandler(billReader, payRepo)
	r := zcRouter(h, "absent", "")

	w := doList(r, tenantID, billID.String(), true)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Items []domainpayment.Payment `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(resp.Items))
	}
	if resp.Items[0].ID != want[0].ID {
		t.Errorf("item ID = %s, want %s", resp.Items[0].ID, want[0].ID)
	}
}

// TestList_Success_EmptyResult covers the "no payments yet" branch (repo
// returns an empty, non-nil slice; List use case normalises nil -> []).
func TestList_Success_EmptyResult(t *testing.T) {
	billReader := newZCBillReader()
	tenantID := uuid.New()
	billID := uuid.New()
	payRepo := &zcPaymentRepo{listItems: nil}
	h := zcBuildHandler(billReader, payRepo)
	r := zcRouter(h, "absent", "")

	w := doList(r, tenantID, billID.String(), true)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	items, ok := resp["items"].([]any)
	if !ok {
		t.Fatalf("items field missing or wrong type: %v", resp["items"])
	}
	if len(items) != 0 {
		t.Errorf("items = %d, want 0", len(items))
	}
}
