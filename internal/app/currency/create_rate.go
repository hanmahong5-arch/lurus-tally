package currency

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/currency"
)

// ErrValidation is the sentinel for use-case level validation failures.
var ErrValidation = errors.New("currency: validation error")

// CreateRateRequest is the input to CreateRateUseCase.
type CreateRateRequest struct {
	TenantID     uuid.UUID
	FromCurrency string
	ToCurrency   string
	Rate         decimal.Decimal
	EffectiveAt  time.Time
}

// CreateRateUseCase persists a manual exchange rate record.
type CreateRateUseCase struct {
	repo CurrencyRepo
}

// NewCreateRateUseCase constructs the use case.
func NewCreateRateUseCase(repo CurrencyRepo) *CreateRateUseCase {
	return &CreateRateUseCase{repo: repo}
}

// Execute validates and saves a manual exchange rate. Source is always "manual".
func (uc *CreateRateUseCase) Execute(ctx context.Context, req CreateRateRequest) (*domain.ExchangeRate, error) {
	if req.TenantID == uuid.Nil {
		return nil, fmt.Errorf("%w: tenant_id is required", ErrValidation)
	}
	if req.Rate.IsZero() || req.Rate.IsNegative() {
		return nil, fmt.Errorf("%w: rate must be positive", ErrValidation)
	}
	if req.FromCurrency == "" {
		return nil, fmt.Errorf("%w: from_currency is required", ErrValidation)
	}
	if req.ToCurrency == "" {
		return nil, fmt.Errorf("%w: to_currency is required", ErrValidation)
	}
	if req.FromCurrency == req.ToCurrency {
		return nil, fmt.Errorf("%w: from_currency and to_currency must differ", ErrValidation)
	}
	if req.EffectiveAt.IsZero() {
		return nil, fmt.Errorf("%w: effective_at is required", ErrValidation)
	}

	r := &domain.ExchangeRate{
		ID:           uuid.New(),
		TenantID:     req.TenantID,
		FromCurrency: req.FromCurrency,
		ToCurrency:   req.ToCurrency,
		Rate:         req.Rate,
		Source:       domain.SourceManual,
		EffectiveAt:  req.EffectiveAt,
		CreatedAt:    time.Now().UTC(),
	}

	if err := uc.repo.SaveRate(ctx, r); err != nil {
		return nil, fmt.Errorf("create rate: %w", err)
	}
	return r, nil
}
