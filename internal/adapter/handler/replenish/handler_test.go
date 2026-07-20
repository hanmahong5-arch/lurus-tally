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
	"github.com/shopspring/decimal"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// stubUseCase is a test double for ListSuggestionsUseCase.
type stubUseCase struct {
	rows []appreplenish.SuggestionRow
	err  error
}

func (s *stubUseCase) Execute(_ context.Context, _ uuid.UUID, _ int) ([]appreplenish.SuggestionRow, error) {
	return s.rows, s.err
}

func newTestEngine(h *handlerreplenish.Handler, tenantID uuid.UUID) *gin.Engine {
	e := gin.New()
	e.Use(func(c *gin.Context) {
		if tenantID != uuid.Nil {
			c.Set(middleware.CtxKeyTenantID, tenantID)
		}
		c.Next()
	})
	h.RegisterRoutes(e.Group("/api/v1"))
	return e
}

// TestReplenishHandler_NoTenant_Returns401 verifies auth guard.
func TestReplenishHandler_NoTenant_Returns401(t *testing.T) {
	h := handlerreplenish.New(&stubUseCase{})
	e := newTestEngine(h, uuid.Nil)

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/replenish/suggestions", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestReplenishHandler_HappyPath_ReturnsItems verifies successful suggestions response.
func TestReplenishHandler_HappyPath_ReturnsItems(t *testing.T) {
	tenantID := uuid.New()
	pid := uuid.New()
	sid := uuid.New()

	rows := []appreplenish.SuggestionRow{
		{
			ProductID:     pid,
			ProductName:   "Widget A",
			ProductCode:   "W-001",
			AvailableQty:  decimal.NewFromInt(10),
			SafetyQty:     decimal.NewFromInt(5),
			AvgDailySales: decimal.NewFromInt(3),
			SuggestedQty:  decimal.NewFromInt(32),
			EstAmountCNY:  decimal.NewFromInt(800),
			SupplierID:    &sid,
			SupplierName:  "Acme Corp",
			UrgencyScore:  decimal.NewFromFloat(3.33),
		},
	}

	h := handlerreplenish.New(&stubUseCase{rows: rows})
	e := newTestEngine(h, tenantID)

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/replenish/suggestions?weeks=2", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Items []struct {
			ProductID    string  `json:"product_id"`
			SupplierID   *string `json:"supplier_id"`
			SupplierName string  `json:"supplier_name"`
			SuggestedQty string  `json:"suggested_qty"`
		} `json:"items"`
		Count int `json:"count"`
		Weeks int `json:"weeks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Count != 1 || len(resp.Items) != 1 {
		t.Errorf("expected 1 item, got count=%d items=%d", resp.Count, len(resp.Items))
	}
	if resp.Items[0].ProductID != pid.String() {
		t.Errorf("product_id mismatch: got %s want %s", resp.Items[0].ProductID, pid.String())
	}
	if resp.Items[0].SupplierID == nil || *resp.Items[0].SupplierID != sid.String() {
		t.Errorf("supplier_id mismatch")
	}
	if resp.Items[0].SupplierName != "Acme Corp" {
		t.Errorf("supplier_name mismatch: %s", resp.Items[0].SupplierName)
	}
	if resp.Weeks != 2 {
		t.Errorf("weeks mismatch: got %d want 2", resp.Weeks)
	}
}

// TestReplenishHandler_EmptyList_ReturnsEmptyItems verifies nil items serialise to [].
func TestReplenishHandler_EmptyList_ReturnsEmptyItems(t *testing.T) {
	h := handlerreplenish.New(&stubUseCase{rows: nil})
	e := newTestEngine(h, uuid.New())

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/replenish/suggestions", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []json.RawMessage `json:"items"`
		Count int               `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Count != 0 || len(resp.Items) != 0 {
		t.Errorf("expected 0 items, got count=%d items=%d", resp.Count, len(resp.Items))
	}
}

// TestReplenishHandler_InvalidWeeks_UsesDefault verifies invalid weeks param is ignored.
func TestReplenishHandler_InvalidWeeks_UsesDefault(t *testing.T) {
	h := handlerreplenish.New(&stubUseCase{rows: nil})
	e := newTestEngine(h, uuid.New())

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/replenish/suggestions?weeks=abc", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp struct {
		Weeks int `json:"weeks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Weeks != 2 {
		t.Errorf("expected default weeks=2, got %d", resp.Weeks)
	}
}

// TestReplenishHandler_UsecaseError_Returns500 verifies error propagation.
func TestReplenishHandler_UsecaseError_Returns500(t *testing.T) {
	h := handlerreplenish.New(&stubUseCase{err: context.DeadlineExceeded})
	e := newTestEngine(h, uuid.New())

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/replenish/suggestions", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestReplenishHandler_GetSuggestions_SerializesForecastAndLearningFields
// verifies the JSON body carries the forecast fields (lead_time_days,
// in_transit, rop, safety_stock, reason) and the learning fields
// (last_purchase_price, lead_time_source, lead_time_samples).
func TestReplenishHandler_GetSuggestions_SerializesForecastAndLearningFields(t *testing.T) {
	lastPrice := decimal.NewFromFloat(72.5)
	rows := []appreplenish.SuggestionRow{
		{
			ProductID:         uuid.New(),
			ProductName:       "Widget L",
			ProductCode:       "L-001",
			AvailableQty:      decimal.NewFromInt(10),
			SuggestedQty:      decimal.NewFromInt(20),
			EstAmountCNY:      decimal.NewFromInt(1450),
			UrgencyScore:      decimal.NewFromInt(2),
			LeadTimeDays:      5,
			InTransit:         decimal.NewFromInt(8),
			ROP:               decimal.NewFromInt(33),
			SafetyStock:       decimal.NewFromInt(6),
			Reason:            "日均5×提前期5天；基于最近3次实际到货,中位交期5天",
			LastPurchasePrice: &lastPrice,
			LeadTimeSource:    appreplenish.LeadTimeSourceLearned,
			LeadTimeSamples:   3,
		},
	}
	h := handlerreplenish.New(&stubUseCase{rows: rows})
	e := newTestEngine(h, uuid.New())

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/replenish/suggestions", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Items []struct {
			LeadTimeDays      int     `json:"lead_time_days"`
			InTransit         string  `json:"in_transit"`
			ROP               string  `json:"rop"`
			SafetyStock       string  `json:"safety_stock"`
			Reason            string  `json:"reason"`
			LastPurchasePrice *string `json:"last_purchase_price"`
			LeadTimeSource    string  `json:"lead_time_source"`
			LeadTimeSamples   int     `json:"lead_time_samples"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Items))
	}
	it := resp.Items[0]

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"lead_time_days", it.LeadTimeDays, 5},
		{"in_transit", it.InTransit, "8"},
		{"rop", it.ROP, "33"},
		{"safety_stock", it.SafetyStock, "6"},
		{"reason", it.Reason, "日均5×提前期5天；基于最近3次实际到货,中位交期5天"},
		{"lead_time_source", it.LeadTimeSource, "learned"},
		{"lead_time_samples", it.LeadTimeSamples, 3},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	if it.LastPurchasePrice == nil || *it.LastPurchasePrice != "72.5" {
		t.Errorf("last_purchase_price = %v, want 72.5", it.LastPurchasePrice)
	}
}

// stubBatchUC records the DraftBatchRequest it receives so a test can assert the
// resolved creator_id. It always succeeds with an empty output.
type stubBatchUC struct {
	gotCreator uuid.UUID
}

func (s *stubBatchUC) Execute(_ context.Context, req appreplenish.DraftBatchRequest) (*appreplenish.DraftBatchOutput, error) {
	s.gotCreator = req.CreatorID
	return &appreplenish.DraftBatchOutput{}, nil
}

// TestReplenishHandler_DraftBatch_PATFallsBackToTenantCreator verifies that when
// no OIDC subject is present (PAT auth carries no user identity), the handler
// falls back creator_id to the tenant id instead of passing uuid.Nil — which the
// use case rejected as "creator_id is required", surfacing a confusing 500.
// Matches the payment handler's existing tenant-as-actor fallback.
func TestReplenishHandler_DraftBatch_PATFallsBackToTenantCreator(t *testing.T) {
	tenantID := uuid.New()
	batch := &stubBatchUC{}
	h := handlerreplenish.NewWithBatch(&stubUseCase{}, batch)
	e := newTestEngine(h, tenantID) // sets tenant only; no OIDC subject (PAT scenario)

	body := `{"lines":[{"product_id":"` + uuid.New().String() + `","qty":"5"}]}`
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/replenish/draft-batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (PAT draft-batch should succeed), got %d body=%s", rec.Code, rec.Body.String())
	}
	if batch.gotCreator != tenantID {
		t.Errorf("creator_id: got %v, want tenant fallback %v", batch.gotCreator, tenantID)
	}
}

// stubScorecardUC is a test double for GetScorecardUseCase.
type stubScorecardUC struct {
	out *appreplenish.Scorecard
	err error
}

func (s *stubScorecardUC) Execute(_ context.Context, _ uuid.UUID) (*appreplenish.Scorecard, error) {
	return s.out, s.err
}

// TestReplenishHandler_GetScorecard_HappyPath_SerializesFields verifies the
// JSON contract of GET /replenish/scorecard.
func TestReplenishHandler_GetScorecard_HappyPath_SerializesFields(t *testing.T) {
	h := handlerreplenish.New(&stubUseCase{}).WithScorecard(&stubScorecardUC{out: &appreplenish.Scorecard{
		WindowDays:       28,
		SuggestionsCount: 8,
		AdoptedCount:     4,
		AdoptionRate:     0.5,
		StockoutMisses:   2,
	}})
	e := newTestEngine(h, uuid.New())

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/replenish/scorecard", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		WindowDays       int     `json:"window_days"`
		SuggestionsCount int     `json:"suggestions_count"`
		AdoptedCount     int     `json:"adopted_count"`
		AdoptionRate     float64 `json:"adoption_rate"`
		StockoutMisses   int     `json:"stockout_misses"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"window_days", resp.WindowDays, 28},
		{"suggestions_count", resp.SuggestionsCount, 8},
		{"adopted_count", resp.AdoptedCount, 4},
		{"adoption_rate", resp.AdoptionRate, 0.5},
		{"stockout_misses", resp.StockoutMisses, 2},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

// TestReplenishHandler_GetScorecard_NoTenant_Returns401 verifies auth guard.
func TestReplenishHandler_GetScorecard_NoTenant_Returns401(t *testing.T) {
	h := handlerreplenish.New(&stubUseCase{}).WithScorecard(&stubScorecardUC{out: &appreplenish.Scorecard{}})
	e := newTestEngine(h, uuid.Nil)

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/replenish/scorecard", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestReplenishHandler_GetScorecard_UsecaseError_Returns500 verifies 5xx goes
// through httperr (generic body, no raw error leak).
func TestReplenishHandler_GetScorecard_UsecaseError_Returns500(t *testing.T) {
	h := handlerreplenish.New(&stubUseCase{}).WithScorecard(&stubScorecardUC{err: context.DeadlineExceeded})
	e := newTestEngine(h, uuid.New())

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/replenish/scorecard", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestReplenishHandler_GetScorecard_NotWired_Returns501 verifies the route
// degrades gracefully when the use case is not configured.
func TestReplenishHandler_GetScorecard_NotWired_Returns501(t *testing.T) {
	h := handlerreplenish.New(&stubUseCase{}) // no WithScorecard
	e := newTestEngine(h, uuid.New())

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/replenish/scorecard", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Errorf("expected 501, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestReplenishHandler_GetSuggestions_OmitsLastPriceWhenNil verifies the
// last_purchase_price key is absent (omitempty) when there is no history.
func TestReplenishHandler_GetSuggestions_OmitsLastPriceWhenNil(t *testing.T) {
	rows := []appreplenish.SuggestionRow{
		{
			ProductID:      uuid.New(),
			ProductName:    "Widget N",
			ProductCode:    "N-001",
			LeadTimeSource: appreplenish.LeadTimeSourceDefault,
		},
	}
	h := handlerreplenish.New(&stubUseCase{rows: rows})
	e := newTestEngine(h, uuid.New())

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/replenish/suggestions", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, present := resp.Items[0]["last_purchase_price"]; present {
		t.Error("last_purchase_price should be omitted when nil")
	}
}
