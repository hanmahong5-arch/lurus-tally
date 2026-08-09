package currency_test

// Additional coverage for internal/adapter/handler/currency, filling gaps left by
// handler_test.go: error branches (500s), validation branches (400/401), date
// end-of-day boundary computation, and the from==to short-circuit invariant.
//
// This file must NOT be edited alongside handler_test.go's mockRepo — it defines
// its own fakeRepo so both files can coexist without touching each other.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	handlercurrency "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/currency"
	appcurrency "github.com/hanmahong5-arch/lurus-tally/internal/app/currency"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/currency"
)

// fakeRepo is a controllable CurrencyRepo double: every method's error/result is
// injectable, and GetRateOn/ListRateHistory capture the arguments they were
// called with so tests can assert on what the handler actually passed down
// (not just what came back).
type fakeRepo struct {
	currencies    []domain.Currency
	currenciesErr error

	getRateResult *domain.ExchangeRate
	getRateErr    error
	getRateCalled bool
	getRateDate   time.Time

	saveErr error
	saved   []domain.ExchangeRate

	historyResult []domain.ExchangeRate
	historyErr    error
	historyDays   int
}

func (f *fakeRepo) ListCurrencies(_ context.Context) ([]domain.Currency, error) {
	return f.currencies, f.currenciesErr
}

func (f *fakeRepo) GetRateOn(_ context.Context, _ uuid.UUID, _, _ string, date time.Time) (*domain.ExchangeRate, error) {
	f.getRateCalled = true
	f.getRateDate = date
	if f.getRateErr != nil {
		return nil, f.getRateErr
	}
	return f.getRateResult, nil
}

func (f *fakeRepo) SaveRate(_ context.Context, r *domain.ExchangeRate) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = append(f.saved, *r)
	return nil
}

func (f *fakeRepo) ListRateHistory(_ context.Context, _ uuid.UUID, _, _ string, days int) ([]domain.ExchangeRate, error) {
	f.historyDays = days
	if f.historyErr != nil {
		return nil, f.historyErr
	}
	return f.historyResult, nil
}

func newFakeHandler(repo *fakeRepo) *handlercurrency.Handler {
	return handlercurrency.New(
		appcurrency.NewListCurrenciesUseCase(repo),
		appcurrency.NewGetRateUseCase(repo),
		appcurrency.NewCreateRateUseCase(repo),
		appcurrency.NewListRateHistoryUseCase(repo),
	)
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode response body %q: %v", w.Body.String(), err)
	}
	return m
}

// ----- ListCurrencies -----

func TestZZ_ListCurrencies_UseCaseErr_Returns500(t *testing.T) {
	repo := &fakeRepo{currenciesErr: errors.New("db unreachable")}
	r := newRouter(newFakeHandler(repo))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/currencies", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
	body := decodeBody(t, w)
	// httperr.Internal always emits this exact safe/static code+message — the
	// underlying cause ("db unreachable") must never leak to the client.
	if body["error"] != "internal_error" {
		t.Errorf("error code = %v, want internal_error", body["error"])
	}
	if body["message"] != "an internal error occurred" {
		t.Errorf("message = %v, want generic safe message (cause must not leak)", body["message"])
	}
}

func TestZZ_ListCurrencies_NoTenantConstraint(t *testing.T) {
	// Business invariant: ListCurrencies is not tenant-scoped — it must succeed
	// with no X-Tenant-ID header at all.
	repo := &fakeRepo{currencies: []domain.Currency{{Code: "USD", Name: "US Dollar", Enabled: true}}}
	r := newRouter(newFakeHandler(repo))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/currencies", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with no tenant header, got %d: %s", w.Code, w.Body.String())
	}
}

// ----- GetRate -----

func TestZZ_GetRate_MissingFromOrTo_Returns400(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{"missing both", ""},
		{"missing to", "?from=USD"},
		{"missing from", "?to=CNY"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{}
			r := newRouter(newFakeHandler(repo))
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodGet, "/api/v1/exchange-rates"+tc.query, nil)
			r.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
			body := decodeBody(t, w)
			if body["message"] != "from and to are required" {
				t.Errorf("message = %v, want 'from and to are required'", body["message"])
			}
			if repo.getRateCalled {
				t.Errorf("repo must not be queried when from/to missing")
			}
		})
	}
}

func TestZZ_GetRate_InvalidDateFormat_Returns400(t *testing.T) {
	repo := &fakeRepo{}
	r := newRouter(newFakeHandler(repo))
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/exchange-rates?from=USD&to=CNY&date=2026/04/23", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	body := decodeBody(t, w)
	if body["message"] != "date must be YYYY-MM-DD" {
		t.Errorf("message = %v, want 'date must be YYYY-MM-DD'", body["message"])
	}
	if repo.getRateCalled {
		t.Errorf("repo must not be queried when date is malformed")
	}
}

func TestZZ_GetRate_DateEndOfDay_PassedToUseCase(t *testing.T) {
	// Technical case: date=YYYY-MM-DD must be converted to end-of-day
	// (+23:59:59) before being handed to the use case, so rates effective
	// earlier that same day are included. Compute the expected value by hand
	// from the same parse the handler uses — do not read it back from the
	// handler's own output.
	repo := &fakeRepo{getRateResult: nil}
	r := newRouter(newFakeHandler(repo))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/exchange-rates?from=USD&to=CNY&date=2026-04-23", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !repo.getRateCalled {
		t.Fatalf("expected repo.GetRateOn to be called")
	}
	base := time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC)
	expected := base.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
	if !repo.getRateDate.Equal(expected) {
		t.Errorf("date passed to use case = %v, want %v (end-of-day)", repo.getRateDate, expected)
	}
}

func TestZZ_GetRate_SameCurrency_ShortCircuits(t *testing.T) {
	// Business invariant: from==to must short-circuit to rate=1/source=manual
	// without ever touching the repo — no possibility of a wrong conversion
	// being computed from stale/missing data for a same-currency "conversion".
	repo := &fakeRepo{getRateErr: errors.New("must not be called")}
	r := newRouter(newFakeHandler(repo))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/exchange-rates?from=USD&to=USD", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if repo.getRateCalled {
		t.Errorf("GetRateOn must not be called when from==to")
	}
	var resp appcurrency.RateResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if !resp.Rate.Equal(decimal.NewFromInt(1)) {
		t.Errorf("rate = %s, want 1", resp.Rate)
	}
	if resp.Source != domain.SourceManual {
		t.Errorf("source = %s, want manual", resp.Source)
	}
	if resp.Warning != "" {
		t.Errorf("warning = %q, want empty", resp.Warning)
	}
}

func TestZZ_GetRate_UseCaseErr_Returns500(t *testing.T) {
	repo := &fakeRepo{getRateErr: errors.New("query failed")}
	r := newRouter(newFakeHandler(repo))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/exchange-rates?from=USD&to=CNY", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
	body := decodeBody(t, w)
	if body["error"] != "internal_error" {
		t.Errorf("error = %v, want internal_error", body["error"])
	}
}

// ----- CreateRate -----

func TestZZ_CreateRate_MalformedJSON_Returns400(t *testing.T) {
	repo := &fakeRepo{}
	r := newRouter(newFakeHandler(repo))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/exchange-rates", bytes.NewReader([]byte("{not-json")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantID.String())
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	body := decodeBody(t, w)
	if body["error"] != "validation_error" {
		t.Errorf("error = %v, want validation_error", body["error"])
	}
}

func TestZZ_CreateRate_InvalidRate_Table(t *testing.T) {
	// Business invariant: rate must be a positive decimal. Parsed via
	// decimalutil.Parse (string in, not float), and IsZero()/IsNegative()
	// both reject — table drives every rejecting branch through one assertion.
	cases := []struct {
		name string
		rate string
	}{
		{"unparseable", "not-a-number"},
		{"zero", "0"},
		{"negative", "-3.5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{}
			r := newRouter(newFakeHandler(repo))

			body := map[string]string{
				"from_currency": "USD",
				"to_currency":   "CNY",
				"rate":          tc.rate,
				"effective_at":  "2026-04-23T00:00:00Z",
			}
			b, _ := json.Marshal(body)

			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodPost, "/api/v1/exchange-rates", bytes.NewReader(b))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Tenant-ID", testTenantID.String())
			r.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
			resp := decodeBody(t, w)
			if resp["error"] != "invalid_rate" {
				t.Errorf("error = %v, want invalid_rate", resp["error"])
			}
			if resp["message"] != "rate must be a positive decimal" {
				t.Errorf("message = %v, want 'rate must be a positive decimal'", resp["message"])
			}
			if len(repo.saved) != 0 {
				t.Errorf("SaveRate must not be called for a rejected rate")
			}
		})
	}
}

func TestZZ_CreateRate_EffectiveAtDefaultsToTodayTruncated(t *testing.T) {
	repo := &fakeRepo{}
	r := newRouter(newFakeHandler(repo))

	body := map[string]string{
		"from_currency": "USD",
		"to_currency":   "CNY",
		"rate":          "7.1",
		// effective_at omitted entirely.
	}
	b, _ := json.Marshal(body)

	before := time.Now().UTC().Truncate(24 * time.Hour)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/exchange-rates", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantID.String())
	r.ServeHTTP(w, req)
	after := time.Now().UTC().Truncate(24 * time.Hour)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if len(repo.saved) != 1 {
		t.Fatalf("expected 1 saved rate, got %d", len(repo.saved))
	}
	got := repo.saved[0].EffectiveAt
	if !got.Equal(before) && !got.Equal(after) {
		t.Errorf("effective_at = %v, want today truncated to 24h (%v)", got, before)
	}
}

func TestZZ_CreateRate_EffectiveAtDateOnly_Accepted(t *testing.T) {
	repo := &fakeRepo{}
	r := newRouter(newFakeHandler(repo))

	body := map[string]string{
		"from_currency": "USD",
		"to_currency":   "CNY",
		"rate":          "7.1",
		"effective_at":  "2026-04-23",
	}
	b, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/exchange-rates", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantID.String())
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	want := time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC)
	if len(repo.saved) != 1 || !repo.saved[0].EffectiveAt.Equal(want) {
		t.Fatalf("effective_at not parsed as date-only: %+v", repo.saved)
	}
}

func TestZZ_CreateRate_EffectiveAtUnparseable_Returns400(t *testing.T) {
	repo := &fakeRepo{}
	r := newRouter(newFakeHandler(repo))

	body := map[string]string{
		"from_currency": "USD",
		"to_currency":   "CNY",
		"rate":          "7.1",
		"effective_at":  "not-a-date-at-all",
	}
	b, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/exchange-rates", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantID.String())
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeBody(t, w)
	if resp["message"] != "effective_at must be RFC3339 or YYYY-MM-DD" {
		t.Errorf("message = %v, want effective_at format error", resp["message"])
	}
	if len(repo.saved) != 0 {
		t.Errorf("SaveRate must not be called when effective_at is unparseable")
	}
}

func TestZZ_CreateRate_UseCaseValidationErr_Returns400(t *testing.T) {
	// Business invariant: appcurrency.ErrValidation (from_currency==to_currency
	// caught inside the use case, not the handler) must classify as a 400
	// validation_error, not a 500.
	repo := &fakeRepo{}
	r := newRouter(newFakeHandler(repo))

	body := map[string]string{
		"from_currency": "USD",
		"to_currency":   "USD",
		"rate":          "1.5",
		"effective_at":  "2026-04-23T00:00:00Z",
	}
	b, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/exchange-rates", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantID.String())
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeBody(t, w)
	if resp["error"] != "validation_error" {
		t.Errorf("error = %v, want validation_error", resp["error"])
	}
}

func TestZZ_CreateRate_UseCaseInternalErr_Returns500(t *testing.T) {
	repo := &fakeRepo{saveErr: errors.New("write failed")}
	r := newRouter(newFakeHandler(repo))

	body := map[string]string{
		"from_currency": "USD",
		"to_currency":   "CNY",
		"rate":          "7.1",
		"effective_at":  "2026-04-23T00:00:00Z",
	}
	b, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/exchange-rates", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantID.String())
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeBody(t, w)
	if resp["error"] != "internal_error" {
		t.Errorf("error = %v, want internal_error", resp["error"])
	}
	if resp["message"] != "an internal error occurred" {
		t.Errorf("message must be the generic safe message, got %v (cause must not leak)", resp["message"])
	}
}

// ----- ListRateHistory -----

func TestZZ_ListRateHistory_MissingFromOrTo_Returns400(t *testing.T) {
	cases := []string{"", "?from=USD", "?to=CNY"}
	for _, q := range cases {
		t.Run(q, func(t *testing.T) {
			repo := &fakeRepo{}
			r := newRouter(newFakeHandler(repo))
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodGet, "/api/v1/exchange-rates/history"+q, nil)
			r.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestZZ_ListRateHistory_DaysParsing_Table(t *testing.T) {
	// Technical case: non-numeric or <=0 "days" must fall back to the default
	// of 30 rather than propagating a bad value to the use case.
	cases := []struct {
		name     string
		daysArg  string
		wantDays int
	}{
		{"non-numeric falls back to 30", "abc", 30},
		{"zero falls back to 30", "0", 30},
		{"negative falls back to 30", "-5", 30},
		{"valid override honored", "7", 7},
		{"absent uses default 30", "", 30},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{historyResult: []domain.ExchangeRate{}}
			r := newRouter(newFakeHandler(repo))

			q := "?from=USD&to=CNY"
			if tc.daysArg != "" {
				q += "&days=" + tc.daysArg
			}
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodGet, "/api/v1/exchange-rates/history"+q, nil)
			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
			}
			if repo.historyDays != tc.wantDays {
				t.Errorf("days passed to use case = %d, want %d", repo.historyDays, tc.wantDays)
			}
			resp := decodeBody(t, w)
			rates, ok := resp["rates"].([]any)
			if !ok {
				t.Fatalf("rates field missing or not an array: %v", resp)
			}
			if len(rates) != 0 {
				t.Errorf("expected empty rates, got %d", len(rates))
			}
		})
	}
}

func TestZZ_ListRateHistory_UseCaseErr_Returns500(t *testing.T) {
	repo := &fakeRepo{historyErr: errors.New("scan failed")}
	r := newRouter(newFakeHandler(repo))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/exchange-rates/history?from=USD&to=CNY", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeBody(t, w)
	if resp["error"] != "internal_error" {
		t.Errorf("error = %v, want internal_error", resp["error"])
	}
}
