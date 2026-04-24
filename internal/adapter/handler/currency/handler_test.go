package currency_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	handlercurrency "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/currency"
	appcurrency "github.com/hanmahong5-arch/lurus-tally/internal/app/currency"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/currency"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// --- mock repo ---

type mockRepo struct {
	currencies []domain.Currency
	rates      []domain.ExchangeRate
	saveErr    error
}

func (m *mockRepo) ListCurrencies(_ context.Context) ([]domain.Currency, error) {
	return m.currencies, nil
}

func (m *mockRepo) GetRateOn(_ context.Context, _ uuid.UUID, from, to string, date time.Time) (*domain.ExchangeRate, error) {
	var best *domain.ExchangeRate
	for i := range m.rates {
		r := &m.rates[i]
		if r.FromCurrency == from && r.ToCurrency == to && !r.EffectiveAt.After(date) {
			if best == nil || r.EffectiveAt.After(best.EffectiveAt) {
				best = r
			}
		}
	}
	return best, nil
}

func (m *mockRepo) SaveRate(_ context.Context, r *domain.ExchangeRate) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.rates = append(m.rates, *r)
	return nil
}

func (m *mockRepo) ListRateHistory(_ context.Context, _ uuid.UUID, from, to string, days int) ([]domain.ExchangeRate, error) {
	cutoff := time.Now().AddDate(0, 0, -days)
	var result []domain.ExchangeRate
	for _, r := range m.rates {
		if r.FromCurrency == from && r.ToCurrency == to && !r.EffectiveAt.Before(cutoff) {
			result = append(result, r)
		}
	}
	return result, nil
}

func newHandler(repo *mockRepo) *handlercurrency.Handler {
	return handlercurrency.New(
		appcurrency.NewListCurrenciesUseCase(repo),
		appcurrency.NewGetRateUseCase(repo),
		appcurrency.NewCreateRateUseCase(repo),
		appcurrency.NewListRateHistoryUseCase(repo),
	)
}

func newRouter(h *handlercurrency.Handler) *gin.Engine {
	r := gin.New()
	api := r.Group("/api/v1")
	h.RegisterRoutes(api)
	return r
}

var testTenantID = uuid.New()

func TestCurrencyHandler_ListCurrencies_Returns200(t *testing.T) {
	repo := &mockRepo{
		currencies: []domain.Currency{
			{Code: "CNY", Name: "人民币", Symbol: "¥", Enabled: true},
			{Code: "USD", Name: "美元", Symbol: "$", Enabled: true},
		},
	}
	r := newRouter(newHandler(repo))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/currencies", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	currencies, ok := resp["currencies"].([]any)
	if !ok {
		t.Fatalf("currencies field missing or not array")
	}
	if len(currencies) != 2 {
		t.Errorf("expected 2 currencies, got %d", len(currencies))
	}
}

func TestCurrencyHandler_GetRate_NoData_Returns200WithDefault(t *testing.T) {
	repo := &mockRepo{}
	r := newRouter(newHandler(repo))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/exchange-rates?from=USD&to=CNY&date=2026-04-23", nil)
	req.Header.Set("X-Tenant-ID", testTenantID.String())
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if resp["warning"] != "no_rate_found" {
		t.Errorf("warning = %v, want no_rate_found", resp["warning"])
	}
	if resp["source"] != "default" {
		t.Errorf("source = %v, want default", resp["source"])
	}
}

func TestCurrencyHandler_GetRate_ExactDate_Returns200(t *testing.T) {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	repo := &mockRepo{
		rates: []domain.ExchangeRate{
			{
				ID:           uuid.New(),
				TenantID:     testTenantID,
				FromCurrency: "USD",
				ToCurrency:   "CNY",
				Rate:         decimal.RequireFromString("7.25"),
				Source:       domain.SourceManual,
				EffectiveAt:  today,
			},
		},
	}
	r := newRouter(newHandler(repo))

	dateStr := today.Format("2006-01-02")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/exchange-rates?from=USD&to=CNY&date="+dateStr, nil)
	req.Header.Set("X-Tenant-ID", testTenantID.String())
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp appcurrency.RateResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if resp.Warning != "" {
		t.Errorf("unexpected warning: %s", resp.Warning)
	}
}

func TestCurrencyHandler_CreateRate_ValidBody_Returns201(t *testing.T) {
	repo := &mockRepo{}
	r := newRouter(newHandler(repo))

	body := map[string]string{
		"from_currency": "USD",
		"to_currency":   "CNY",
		"rate":          "7.25",
		"effective_at":  "2026-04-23T00:00:00Z",
	}
	b, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/exchange-rates", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantID.String())
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp domain.ExchangeRate
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if resp.Source != domain.SourceManual {
		t.Errorf("source = %s, want manual", resp.Source)
	}
}

func TestCurrencyHandler_CreateRate_ZeroRate_Returns400(t *testing.T) {
	repo := &mockRepo{}
	r := newRouter(newHandler(repo))

	body := map[string]string{
		"from_currency": "USD",
		"to_currency":   "CNY",
		"rate":          "0",
		"effective_at":  "2026-04-23T00:00:00Z",
	}
	b, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/exchange-rates", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantID.String())
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCurrencyHandler_CreateRate_NoTenantID_Returns401(t *testing.T) {
	repo := &mockRepo{}
	r := newRouter(newHandler(repo))

	body := map[string]string{
		"from_currency": "USD",
		"to_currency":   "CNY",
		"rate":          "7.25",
		"effective_at":  "2026-04-23T00:00:00Z",
	}
	b, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/exchange-rates", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	// No X-Tenant-ID header
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}
