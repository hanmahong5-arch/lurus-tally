package replenish_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
