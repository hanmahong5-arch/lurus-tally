package currency_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/currency"
)

// TestCurrency_Validate_TableDriven closes the remaining Currency.Validate
// branches (empty Name with Code set, and the fully-valid case) that
// currency_test.go does not exercise.
func TestCurrency_Validate_TableDriven(t *testing.T) {
	cases := []struct {
		name      string
		currency  domain.Currency
		wantErr   bool
		wantMatch string // substring expected in the error message
	}{
		{
			name:      "empty code and name -> code error fires first",
			currency:  domain.Currency{Code: "", Name: ""},
			wantErr:   true,
			wantMatch: "code is required",
		},
		{
			name:      "empty code only",
			currency:  domain.Currency{Code: "", Name: "US Dollar"},
			wantErr:   true,
			wantMatch: "code is required",
		},
		{
			name:      "empty name only",
			currency:  domain.Currency{Code: "USD", Name: ""},
			wantErr:   true,
			wantMatch: "name is required",
		},
		{
			name:      "both set -> valid",
			currency:  domain.Currency{Code: "CNY", Name: "Chinese Yuan", Symbol: "¥", Enabled: true},
			wantErr:   false,
			wantMatch: "",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := tc.currency.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !errors.Is(err, domain.ErrValidation) {
					t.Errorf("expected errors.Is(err, ErrValidation) to hold, got %v", err)
				}
				if !strings.Contains(err.Error(), tc.wantMatch) {
					t.Errorf("expected error message to contain %q, got %q", tc.wantMatch, err.Error())
				}
				return
			}
			if err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

// TestExchangeRate_Validate_TableDriven exercises every branch of
// ExchangeRate.Validate individually (rate zero/negative/exactly-one,
// missing from/to, self-conversion, zero EffectiveAt, and the fully-valid
// path), each asserted against errors.Is(ErrValidation) plus a field-specific
// message substring per the business invariants in the test brief.
func TestExchangeRate_Validate_TableDriven(t *testing.T) {
	validTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	base := func() domain.ExchangeRate {
		return domain.ExchangeRate{
			ID:           uuid.New(),
			TenantID:     uuid.New(),
			FromCurrency: "USD",
			ToCurrency:   "CNY",
			Rate:         decimal.NewFromFloat(6.8123),
			Source:       domain.SourceManual,
			EffectiveAt:  validTime,
		}
	}

	cases := []struct {
		name      string
		mutate    func(r *domain.ExchangeRate)
		wantErr   bool
		wantMatch string
	}{
		{
			name:      "rate zero",
			mutate:    func(r *domain.ExchangeRate) { r.Rate = decimal.Zero },
			wantErr:   true,
			wantMatch: "rate must be positive",
		},
		{
			name:      "rate negative",
			mutate:    func(r *domain.ExchangeRate) { r.Rate = decimal.NewFromInt(-1) },
			wantErr:   true,
			wantMatch: "rate must be positive",
		},
		{
			name:      "rate exactly one is valid boundary",
			mutate:    func(r *domain.ExchangeRate) { r.Rate = decimal.NewFromInt(1) },
			wantErr:   false,
			wantMatch: "",
		},
		{
			name:      "from_currency empty",
			mutate:    func(r *domain.ExchangeRate) { r.FromCurrency = "" },
			wantErr:   true,
			wantMatch: "from_currency is required",
		},
		{
			name:      "to_currency empty",
			mutate:    func(r *domain.ExchangeRate) { r.ToCurrency = "" },
			wantErr:   true,
			wantMatch: "to_currency is required",
		},
		{
			name: "from equals to -> self-conversion rejected",
			mutate: func(r *domain.ExchangeRate) {
				r.FromCurrency = "USD"
				r.ToCurrency = "USD"
			},
			wantErr:   true,
			wantMatch: "must differ",
		},
		{
			name:      "effective_at zero",
			mutate:    func(r *domain.ExchangeRate) { r.EffectiveAt = time.Time{} },
			wantErr:   true,
			wantMatch: "effective_at is required",
		},
		{
			name:      "fully valid",
			mutate:    func(r *domain.ExchangeRate) {},
			wantErr:   false,
			wantMatch: "",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			r := base()
			tc.mutate(&r)
			err := r.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !errors.Is(err, domain.ErrValidation) {
					t.Errorf("expected errors.Is(err, ErrValidation) to hold, got %v", err)
				}
				if !strings.Contains(err.Error(), tc.wantMatch) {
					t.Errorf("expected error message to contain %q, got %q", tc.wantMatch, err.Error())
				}
				return
			}
			if err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

// TestExchangeRate_Rate_PrecisionRoundTrip asserts the business invariant
// that Rate is a decimal.Decimal (not a float), so a precision-sensitive
// literal like "6.8123" round-trips exactly through parsing, validation,
// and string rendering without float rounding drift.
func TestExchangeRate_Rate_PrecisionRoundTrip(t *testing.T) {
	const literal = "6.8123"
	rate, err := decimal.NewFromString(literal)
	if err != nil {
		t.Fatalf("decimal.NewFromString(%q) failed: %v", literal, err)
	}

	r := &domain.ExchangeRate{
		FromCurrency: "USD",
		ToCurrency:   "CNY",
		Rate:         rate,
		EffectiveAt:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	if err := r.Validate(); err != nil {
		t.Fatalf("expected valid exchange rate, got error: %v", err)
	}

	if got := r.Rate.String(); got != literal {
		t.Errorf("expected rate to round-trip as %q, got %q (precision loss)", literal, got)
	}

	// A float64-based representation of 6.8123 is well known to not be exact;
	// this guards that Rate is never silently coerced to a lossy float path.
	if r.Rate.InexactFloat64() == 0 {
		t.Fatalf("sanity check failed: rate float projection is zero")
	}
}

// TestCurrency_Validate_Concurrent exercises Validate concurrently under
// -race to confirm the read-only validation logic has no shared mutable
// state / data races across goroutines.
func TestCurrency_Validate_Concurrent(t *testing.T) {
	c := &domain.Currency{Code: "USD", Name: "US Dollar", Symbol: "$", Enabled: true}

	done := make(chan error, 32)
	for i := 0; i < 32; i++ {
		go func() {
			done <- c.Validate()
		}()
	}
	for i := 0; i < 32; i++ {
		if err := <-done; err != nil {
			t.Errorf("expected no error from concurrent Validate, got %v", err)
		}
	}
}

// TestExchangeRate_Validate_Concurrent does the same for ExchangeRate.Validate.
func TestExchangeRate_Validate_Concurrent(t *testing.T) {
	r := &domain.ExchangeRate{
		FromCurrency: "USD",
		ToCurrency:   "CNY",
		Rate:         decimal.NewFromFloat(6.8123),
		EffectiveAt:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	done := make(chan error, 32)
	for i := 0; i < 32; i++ {
		go func() {
			done <- r.Validate()
		}()
	}
	for i := 0; i < 32; i++ {
		if err := <-done; err != nil {
			t.Errorf("expected no error from concurrent Validate, got %v", err)
		}
	}
}
