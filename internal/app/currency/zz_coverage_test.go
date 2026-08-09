package currency_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appcurrency "github.com/hanmahong5-arch/lurus-tally/internal/app/currency"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/currency"
)

// covRepo is a dedicated fake for this file, distinct from mockCurrencyRepo
// (defined in list_currencies_test.go) so we can inject errors on every
// method and capture the exact arguments each use case passes down.
type covRepo struct {
	// ListCurrencies
	currencies    []domain.Currency
	listCurErr    error
	listCurCalled bool

	// GetRateOn
	getRateResult   *domain.ExchangeRate
	getRateErr      error
	getRateOnCalled bool

	// SaveRate
	saveErr    error
	saved      *domain.ExchangeRate
	saveCalled bool

	// ListRateHistory
	historyResult []domain.ExchangeRate
	historyErr    error
	historyCalled bool
	lastDaysArg   int
}

func (m *covRepo) ListCurrencies(_ context.Context) ([]domain.Currency, error) {
	m.listCurCalled = true
	if m.listCurErr != nil {
		return nil, m.listCurErr
	}
	return m.currencies, nil
}

func (m *covRepo) GetRateOn(_ context.Context, _ uuid.UUID, _, _ string, _ time.Time) (*domain.ExchangeRate, error) {
	m.getRateOnCalled = true
	if m.getRateErr != nil {
		return nil, m.getRateErr
	}
	return m.getRateResult, nil
}

func (m *covRepo) SaveRate(_ context.Context, r *domain.ExchangeRate) error {
	m.saveCalled = true
	if m.saveErr != nil {
		return m.saveErr
	}
	m.saved = r
	return nil
}

func (m *covRepo) ListRateHistory(_ context.Context, _ uuid.UUID, _, _ string, days int) ([]domain.ExchangeRate, error) {
	m.historyCalled = true
	m.lastDaysArg = days
	if m.historyErr != nil {
		return nil, m.historyErr
	}
	return m.historyResult, nil
}

// --- CreateRate: full validation table (every invalid-field branch) ---

func TestCreateRate_Validation_TableDriven(t *testing.T) {
	validEffective := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		req  appcurrency.CreateRateRequest
	}{
		{
			name: "tenant_nil",
			req: appcurrency.CreateRateRequest{
				TenantID: uuid.Nil, FromCurrency: "USD", ToCurrency: "CNY",
				Rate: decimal.NewFromInt(1), EffectiveAt: validEffective,
			},
		},
		{
			name: "rate_zero",
			req: appcurrency.CreateRateRequest{
				TenantID: testTenantID, FromCurrency: "USD", ToCurrency: "CNY",
				Rate: decimal.Zero, EffectiveAt: validEffective,
			},
		},
		{
			name: "rate_negative",
			req: appcurrency.CreateRateRequest{
				TenantID: testTenantID, FromCurrency: "USD", ToCurrency: "CNY",
				Rate: decimal.NewFromInt(-1), EffectiveAt: validEffective,
			},
		},
		{
			name: "from_empty",
			req: appcurrency.CreateRateRequest{
				TenantID: testTenantID, FromCurrency: "", ToCurrency: "CNY",
				Rate: decimal.NewFromInt(1), EffectiveAt: validEffective,
			},
		},
		{
			name: "to_empty",
			req: appcurrency.CreateRateRequest{
				TenantID: testTenantID, FromCurrency: "USD", ToCurrency: "",
				Rate: decimal.NewFromInt(1), EffectiveAt: validEffective,
			},
		},
		{
			name: "from_equals_to",
			req: appcurrency.CreateRateRequest{
				TenantID: testTenantID, FromCurrency: "USD", ToCurrency: "USD",
				Rate: decimal.NewFromInt(1), EffectiveAt: validEffective,
			},
		},
		{
			name: "effective_at_zero",
			req: appcurrency.CreateRateRequest{
				TenantID: testTenantID, FromCurrency: "USD", ToCurrency: "CNY",
				Rate: decimal.NewFromInt(1), EffectiveAt: time.Time{},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &covRepo{}
			uc := appcurrency.NewCreateRateUseCase(repo)

			r, err := uc.Execute(context.Background(), tc.req)

			if r != nil {
				t.Errorf("expected nil result, got %+v", r)
			}
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, appcurrency.ErrValidation) {
				t.Errorf("expected ErrValidation, got %v", err)
			}
			if repo.saveCalled {
				t.Error("SaveRate must not be called on validation failure")
			}
		})
	}
}

// TestCreateRate_Success_FieldsPersistedCorrectly asserts the full set of
// fields written to the repo: tenant ownership, generated ID, forced
// Source=manual, and CreatedAt stamped in UTC.
func TestCreateRate_Success_FieldsPersistedCorrectly(t *testing.T) {
	repo := &covRepo{}
	uc := appcurrency.NewCreateRateUseCase(repo)

	effective := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	req := appcurrency.CreateRateRequest{
		TenantID:     testTenantID,
		FromCurrency: "USD",
		ToCurrency:   "CNY",
		Rate:         decimal.NewFromInt(7),
		EffectiveAt:  effective,
	}

	before := time.Now().UTC()
	got, err := uc.Execute(context.Background(), req)
	after := time.Now().UTC()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !repo.saveCalled {
		t.Fatal("expected SaveRate to be called exactly once")
	}
	if repo.saved != got {
		t.Fatal("expected returned record to be the same pointer saved to repo")
	}
	if repo.saved.TenantID != testTenantID {
		t.Errorf("TenantID = %v, want %v (tenant isolation)", repo.saved.TenantID, testTenantID)
	}
	if repo.saved.ID == uuid.Nil {
		t.Error("expected a freshly generated non-nil ID")
	}
	if repo.saved.Source != domain.SourceManual {
		t.Errorf("Source = %q, want %q", repo.saved.Source, domain.SourceManual)
	}
	if !repo.saved.Rate.Equal(decimal.NewFromInt(7)) {
		t.Errorf("Rate = %s, want 7", repo.saved.Rate)
	}
	if repo.saved.CreatedAt.Location() != time.UTC {
		t.Errorf("CreatedAt location = %v, want UTC", repo.saved.CreatedAt.Location())
	}
	if repo.saved.CreatedAt.Before(before) || repo.saved.CreatedAt.After(after) {
		t.Errorf("CreatedAt = %v, want between %v and %v", repo.saved.CreatedAt, before, after)
	}
}

func TestCreateRate_RepoSaveError_WrapsWithPrefix(t *testing.T) {
	repoErr := errors.New("unique constraint violated")
	repo := &covRepo{saveErr: repoErr}
	uc := appcurrency.NewCreateRateUseCase(repo)

	req := appcurrency.CreateRateRequest{
		TenantID:     testTenantID,
		FromCurrency: "USD",
		ToCurrency:   "CNY",
		Rate:         decimal.NewFromInt(1),
		EffectiveAt:  time.Now().UTC(),
	}
	_, err := uc.Execute(context.Background(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, repoErr) {
		t.Errorf("expected wrapped repoErr, got %v", err)
	}
	if err.Error() != "create rate: unique constraint violated" {
		t.Errorf("error message = %q, want prefixed with 'create rate: '", err.Error())
	}
}

// --- GetRate: same-currency short-circuit, repo error, default fallback, passthrough ---

func TestGetRate_SameCurrency_ShortCircuitsWithoutCallingRepo(t *testing.T) {
	repo := &covRepo{getRateErr: errors.New("must not be called")}
	uc := appcurrency.NewGetRateUseCase(repo)

	result, err := uc.Execute(context.Background(), testTenantID, "USD", "USD", time.Now())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if repo.getRateOnCalled {
		t.Error("GetRateOn must not be called when from==to")
	}
	if !result.Rate.Equal(decimal.NewFromInt(1)) {
		t.Errorf("Rate = %s, want 1", result.Rate)
	}
	if result.Source != domain.SourceManual {
		t.Errorf("Source = %q, want %q", result.Source, domain.SourceManual)
	}
	if result.Warning != "" {
		t.Errorf("Warning = %q, want empty", result.Warning)
	}
}

func TestGetRate_RepoError_Wrapped(t *testing.T) {
	repoErr := errors.New("connection reset")
	repo := &covRepo{getRateErr: repoErr}
	uc := appcurrency.NewGetRateUseCase(repo)

	result, err := uc.Execute(context.Background(), testTenantID, "USD", "CNY", time.Now())
	if result != nil {
		t.Errorf("expected nil result on error, got %+v", result)
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, repoErr) {
		t.Errorf("expected wrapped repoErr, got %v", err)
	}
}

func TestGetRate_NilResult_FallsBackToDefault(t *testing.T) {
	repo := &covRepo{getRateResult: nil}
	uc := appcurrency.NewGetRateUseCase(repo)

	result, err := uc.Execute(context.Background(), testTenantID, "USD", "CNY", time.Now())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !repo.getRateOnCalled {
		t.Error("expected GetRateOn to be called for differing currencies")
	}
	if !result.Rate.Equal(decimal.NewFromInt(1)) {
		t.Errorf("Rate = %s, want 1 (default fallback, not 0)", result.Rate)
	}
	if result.Source != "default" {
		t.Errorf("Source = %q, want \"default\"", result.Source)
	}
	if result.Warning != "no_rate_found" {
		t.Errorf("Warning = %q, want \"no_rate_found\"", result.Warning)
	}
}

func TestGetRate_RepoHasRate_PassesThroughRateAndSource(t *testing.T) {
	stored := &domain.ExchangeRate{
		Rate:   decimal.RequireFromString("6.88"),
		Source: domain.SourcePBoC,
	}
	repo := &covRepo{getRateResult: stored}
	uc := appcurrency.NewGetRateUseCase(repo)

	result, err := uc.Execute(context.Background(), testTenantID, "USD", "CNY", time.Now())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.Rate.Equal(decimal.RequireFromString("6.88")) {
		t.Errorf("Rate = %s, want 6.88", result.Rate)
	}
	if result.Source != domain.SourcePBoC {
		t.Errorf("Source = %q, want %q", result.Source, domain.SourcePBoC)
	}
	if result.Warning != "" {
		t.Errorf("Warning = %q, want empty on found rate", result.Warning)
	}
}

// --- ListCurrencies: repo error branch ---

func TestListCurrencies_RepoError_WrapsWithPrefix(t *testing.T) {
	repoErr := errors.New("table locked")
	repo := &covRepo{listCurErr: repoErr}
	uc := appcurrency.NewListCurrenciesUseCase(repo)

	result, err := uc.Execute(context.Background())
	if result != nil {
		t.Errorf("expected nil result on error, got %+v", result)
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, repoErr) {
		t.Errorf("expected wrapped repoErr, got %v", err)
	}
	if err.Error() != "list currencies: table locked" {
		t.Errorf("error message = %q, want prefixed with 'list currencies: '", err.Error())
	}
}

func TestListCurrencies_Success_PassesThroughSlice(t *testing.T) {
	want := sixCurrencies()
	repo := &covRepo{currencies: want}
	uc := appcurrency.NewListCurrenciesUseCase(repo)

	got, err := uc.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	if got[0].Code != want[0].Code {
		t.Errorf("got[0].Code = %q, want %q", got[0].Code, want[0].Code)
	}
}

// --- ListRateHistory: days-clamp table (captures the actual arg passed to repo) ---

func TestListRateHistory_DaysClamp_TableDriven(t *testing.T) {
	cases := []struct {
		name     string
		input    int
		wantDays int
	}{
		{"negative_clamps_to_30", -1, 30},
		{"zero_clamps_to_30", 0, 30},
		{"exactly_30_unchanged", 30, 30},
		{"exactly_365_unchanged", 365, 365},
		{"over_365_clamps_to_365", 400, 365},
		{"far_over_clamps_to_365", 10000, 365},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &covRepo{}
			uc := appcurrency.NewListRateHistoryUseCase(repo)

			_, err := uc.Execute(context.Background(), testTenantID, "USD", "CNY", tc.input)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if !repo.historyCalled {
				t.Fatal("expected ListRateHistory to be called")
			}
			if repo.lastDaysArg != tc.wantDays {
				t.Errorf("days passed to repo = %d, want %d (input %d)", repo.lastDaysArg, tc.wantDays, tc.input)
			}
		})
	}
}

func TestListRateHistory_RepoError_WrapsWithPrefix(t *testing.T) {
	repoErr := errors.New("timeout")
	repo := &covRepo{historyErr: repoErr}
	uc := appcurrency.NewListRateHistoryUseCase(repo)

	result, err := uc.Execute(context.Background(), testTenantID, "USD", "CNY", 30)
	if result != nil {
		t.Errorf("expected nil result on error, got %+v", result)
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, repoErr) {
		t.Errorf("expected wrapped repoErr, got %v", err)
	}
	if err.Error() != "list rate history: timeout" {
		t.Errorf("error message = %q, want prefixed with 'list rate history: '", err.Error())
	}
}

func TestListRateHistory_Success_PassesThroughSlice(t *testing.T) {
	now := time.Now().UTC()
	want := []domain.ExchangeRate{
		makeRate("USD", "CNY", "7.10", now),
		makeRate("USD", "CNY", "7.20", now.AddDate(0, 0, -1)),
	}
	repo := &covRepo{historyResult: want}
	uc := appcurrency.NewListRateHistoryUseCase(repo)

	got, err := uc.Execute(context.Background(), testTenantID, "USD", "CNY", 30)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
}

// --- Concurrency: use cases hold no mutable state; parallel Execute calls
// against independent repos must not race.

func TestCreateRate_ConcurrentExecute_NoRace(t *testing.T) {
	// Each goroutine gets its own use case + repo instance: CreateRateUseCase
	// holds no shared mutable state, so this exercises Execute concurrently
	// without the fake repo itself becoming a shared-write race target.
	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func(n int) {
			defer func() { done <- struct{}{} }()
			uc := appcurrency.NewCreateRateUseCase(&covRepo{})
			req := appcurrency.CreateRateRequest{
				TenantID:     uuid.New(),
				FromCurrency: "USD",
				ToCurrency:   "CNY",
				Rate:         decimal.NewFromInt(int64(n + 1)),
				EffectiveAt:  time.Now().UTC(),
			}
			if _, err := uc.Execute(context.Background(), req); err != nil {
				t.Errorf("Execute: %v", err)
			}
		}(i)
	}
	for i := 0; i < 8; i++ {
		<-done
	}
}
