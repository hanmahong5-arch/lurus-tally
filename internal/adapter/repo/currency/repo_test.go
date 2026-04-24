// Package currency_test contains integration tests for the currency PG repo.
// These tests require a real PostgreSQL database (migration 000024 applied).
// They are skipped when DATABASE_DSN is not set.
package currency_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	_ "github.com/jackc/pgx/v5/stdlib"

	repocurrency "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/currency"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/currency"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_DSN")
	if dsn == "" {
		t.Skip("DATABASE_DSN not set — skipping integration test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestCurrencyRepoPG_ListCurrencies_Returns6Rows(t *testing.T) {
	db := openDB(t)
	repo := repocurrency.New(db)

	currencies, err := repo.ListCurrencies(context.Background())
	if err != nil {
		t.Fatalf("ListCurrencies: %v", err)
	}
	if len(currencies) != 6 {
		t.Errorf("expected 6 currencies, got %d", len(currencies))
	}
}

func TestCurrencyRepoPG_GetRateOn_FallbackToPriorDate(t *testing.T) {
	db := openDB(t)
	repo := repocurrency.New(db)
	tenantID := uuid.New()
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Truncate(24 * time.Hour)

	// Insert a rate for yesterday.
	r := &domain.ExchangeRate{
		ID:           uuid.New(),
		TenantID:     tenantID,
		FromCurrency: "USD",
		ToCurrency:   "CNY",
		Rate:         decimal.RequireFromString("7.25"),
		Source:       domain.SourceManual,
		EffectiveAt:  yesterday,
		CreatedAt:    time.Now().UTC(),
	}
	if err := repo.SaveRate(context.Background(), r); err != nil {
		t.Fatalf("SaveRate: %v", err)
	}

	// Query for today — should fall back to yesterday's rate.
	result, err := repo.GetRateOn(context.Background(), tenantID, "USD", "CNY", time.Now().UTC())
	if err != nil {
		t.Fatalf("GetRateOn: %v", err)
	}
	if result == nil {
		t.Fatal("expected fallback rate, got nil")
	}
	if !result.Rate.Equal(decimal.RequireFromString("7.25")) {
		t.Errorf("rate = %s, want 7.25", result.Rate)
	}
}
