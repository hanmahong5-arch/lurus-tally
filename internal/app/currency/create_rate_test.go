package currency_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appcurrency "github.com/hanmahong5-arch/lurus-tally/internal/app/currency"
)

func TestCreateRate_ValidInput_PersistsRecord(t *testing.T) {
	repo := &mockCurrencyRepo{}
	uc := appcurrency.NewCreateRateUseCase(repo)

	req := appcurrency.CreateRateRequest{
		TenantID:     testTenantID,
		FromCurrency: "USD",
		ToCurrency:   "CNY",
		Rate:         decimal.NewFromFloat(7.25),
		EffectiveAt:  time.Now().UTC(),
	}
	r, err := uc.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if r == nil {
		t.Fatal("expected non-nil result")
	}
	if repo.saved == nil {
		t.Fatal("expected repo.SaveRate to be called")
	}
	if repo.saved.Source != "manual" {
		t.Errorf("source = %s, want manual", repo.saved.Source)
	}
	if !repo.saved.Rate.Equal(decimal.NewFromFloat(7.25)) {
		t.Errorf("rate = %s, want 7.25", repo.saved.Rate)
	}
}

func TestCreateRate_RateZero_ReturnsValidationError(t *testing.T) {
	repo := &mockCurrencyRepo{}
	uc := appcurrency.NewCreateRateUseCase(repo)

	req := appcurrency.CreateRateRequest{
		TenantID:     testTenantID,
		FromCurrency: "USD",
		ToCurrency:   "CNY",
		Rate:         decimal.Zero,
		EffectiveAt:  time.Now(),
	}
	_, err := uc.Execute(context.Background(), req)
	if err == nil {
		t.Fatal("expected validation error for zero rate, got nil")
	}
	if !errors.Is(err, appcurrency.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}
}

func TestCreateRate_MissingTenantID_ReturnsError(t *testing.T) {
	repo := &mockCurrencyRepo{}
	uc := appcurrency.NewCreateRateUseCase(repo)

	req := appcurrency.CreateRateRequest{
		TenantID:     uuid.Nil,
		FromCurrency: "USD",
		ToCurrency:   "CNY",
		Rate:         decimal.NewFromFloat(7.25),
		EffectiveAt:  time.Now(),
	}
	_, err := uc.Execute(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for nil TenantID, got nil")
	}
}

func TestCreateRate_SameCurrency_ReturnsError(t *testing.T) {
	repo := &mockCurrencyRepo{}
	uc := appcurrency.NewCreateRateUseCase(repo)

	req := appcurrency.CreateRateRequest{
		TenantID:     testTenantID,
		FromCurrency: "USD",
		ToCurrency:   "USD",
		Rate:         decimal.NewFromFloat(1),
		EffectiveAt:  time.Now(),
	}
	_, err := uc.Execute(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for same currency pair, got nil")
	}
}

func TestCreateRate_RepoError_Propagated(t *testing.T) {
	repoErr := errors.New("db constraint violation")
	repo := &mockCurrencyRepo{saveErr: repoErr}
	uc := appcurrency.NewCreateRateUseCase(repo)

	req := appcurrency.CreateRateRequest{
		TenantID:     testTenantID,
		FromCurrency: "USD",
		ToCurrency:   "CNY",
		Rate:         decimal.NewFromFloat(7.25),
		EffectiveAt:  time.Now(),
	}
	_, err := uc.Execute(context.Background(), req)
	if err == nil {
		t.Fatal("expected repo error to be propagated, got nil")
	}
	if !errors.Is(err, repoErr) {
		t.Errorf("expected wrapped repoErr, got %v", err)
	}
}
