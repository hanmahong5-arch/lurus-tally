package digest_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	handlerdigest "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/digest"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appdigest "github.com/hanmahong5-arch/lurus-tally/internal/app/digest"
	"github.com/shopspring/decimal"
)

func init() {
	gin.SetMode(gin.TestMode)
}

type stubUseCase struct {
	summary appdigest.Summary
	err     error
}

func (s *stubUseCase) Execute(_ context.Context, _ uuid.UUID) (appdigest.Summary, error) {
	return s.summary, s.err
}

func newTestEngine(h *handlerdigest.Handler, tenantID uuid.UUID) *gin.Engine {
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

// TestDigestHandler_NoTenant_Returns401 verifies the auth guard.
func TestDigestHandler_NoTenant_Returns401(t *testing.T) {
	h := handlerdigest.New(&stubUseCase{})
	e := newTestEngine(h, uuid.Nil)

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/weekly-summary", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestDigestHandler_HappyPath_ReturnsAllFields verifies the full response shape.
func TestDigestHandler_HappyPath_ReturnsAllFields(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	stub := &stubUseCase{
		summary: appdigest.Summary{
			ReplenishCount:     3,
			ReplenishAmountCNY: decimal.NewFromFloat(15000.50),
			OversellCount:      2,
			DeadStockCount:     7,
			GeneratedAt:        now,
		},
	}

	h := handlerdigest.New(stub)
	e := newTestEngine(h, uuid.New())

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/weekly-summary", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Replenish struct {
			Count     int    `json:"count"`
			AmountCNY string `json:"amount_cny"`
		} `json:"replenish"`
		Oversell struct {
			Count int `json:"count"`
		} `json:"oversell"`
		DeadStock struct {
			Count int `json:"count"`
		} `json:"dead_stock"`
		GeneratedAt time.Time `json:"generated_at"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Replenish.Count != 3 {
		t.Errorf("replenish.count: want 3 got %d", resp.Replenish.Count)
	}
	if resp.Replenish.AmountCNY != "15000.50" {
		t.Errorf("replenish.amount_cny: want 15000.50 got %s", resp.Replenish.AmountCNY)
	}
	if resp.Oversell.Count != 2 {
		t.Errorf("oversell.count: want 2 got %d", resp.Oversell.Count)
	}
	if resp.DeadStock.Count != 7 {
		t.Errorf("dead_stock.count: want 7 got %d", resp.DeadStock.Count)
	}
}

// TestDigestHandler_UsecaseError_Returns500 verifies error propagation.
func TestDigestHandler_UsecaseError_Returns500(t *testing.T) {
	h := handlerdigest.New(&stubUseCase{err: errors.New("db down")})
	e := newTestEngine(h, uuid.New())

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/weekly-summary", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestDigestHandler_ZeroCounts_ReturnsEmptyButValid verifies zero-value summary.
func TestDigestHandler_ZeroCounts_ReturnsEmptyButValid(t *testing.T) {
	h := handlerdigest.New(&stubUseCase{})
	e := newTestEngine(h, uuid.New())

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/weekly-summary", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Replenish struct {
			Count     int    `json:"count"`
			AmountCNY string `json:"amount_cny"`
		} `json:"replenish"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Replenish.Count != 0 {
		t.Errorf("want count=0 got %d", resp.Replenish.Count)
	}
	if resp.Replenish.AmountCNY != "0.00" {
		t.Errorf("want amount_cny=0.00 got %s", resp.Replenish.AmountCNY)
	}
}
