// Package currency_test exercises the PG currency repo via sqlmock.
// No external PostgreSQL is required; SQL contracts (column order, args, ON CONFLICT) are
// validated against literal expectations to catch unintended drift.
package currency_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	repocurrency "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/currency"
	appcurrency "github.com/hanmahong5-arch/lurus-tally/internal/app/currency"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/currency"
)

// newMockRepo returns a Repo bound to a sqlmock-backed *sql.DB and the mock controller.
// QueryMatcherEqual is used so we assert on the exact SQL text the repo emits.
func newMockRepo(t *testing.T) (*repocurrency.Repo, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	cleanup := func() {
		_ = db.Close()
	}
	return repocurrency.New(db), mock, cleanup
}

// TestCurrencyRepo_New_ReturnsNonNil is a sanity check that constructor wires the *sql.DB.
func TestCurrencyRepo_New_ReturnsNonNil(t *testing.T) {
	if repocurrency.New((*sql.DB)(nil)) == nil {
		t.Fatal("New(nil) returned nil")
	}
}

// TestCurrencyRepo_InterfaceSatisfied is a compile-time guard that *Repo satisfies CurrencyRepo.
func TestCurrencyRepo_InterfaceSatisfied(t *testing.T) {
	var _ appcurrency.CurrencyRepo = (*repocurrency.Repo)(nil)
}

const listCurrenciesSQL = `
		SELECT code, name, symbol, enabled
		FROM tally.currency
		WHERE enabled = true
		ORDER BY code`

func TestCurrencyRepo_ListCurrencies_ReturnsAllRows(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"code", "name", "symbol", "enabled"}).
		AddRow("CNY", "Chinese Yuan", "¥", true).
		AddRow("EUR", "Euro", "€", true).
		AddRow("USD", "US Dollar", "$", true)
	mock.ExpectQuery(listCurrenciesSQL).WillReturnRows(rows)

	result, err := repo.ListCurrencies(context.Background())
	if err != nil {
		t.Fatalf("ListCurrencies: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("len(result) = %d, want 3", len(result))
	}
	if result[0].Code != "CNY" || result[1].Code != "EUR" || result[2].Code != "USD" {
		t.Errorf("currencies out of order: %+v", result)
	}
	if !result[0].Enabled {
		t.Errorf("expected first row enabled, got %v", result[0].Enabled)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestCurrencyRepo_ListCurrencies_EmptyResult_ReturnsNilSlice(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	mock.ExpectQuery(listCurrenciesSQL).
		WillReturnRows(sqlmock.NewRows([]string{"code", "name", "symbol", "enabled"}))

	result, err := repo.ListCurrencies(context.Background())
	if err != nil {
		t.Fatalf("ListCurrencies: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty slice, got %d rows", len(result))
	}
}

func TestCurrencyRepo_ListCurrencies_QueryError_PropagatesWrapped(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	boom := errors.New("conn closed")
	mock.ExpectQuery(listCurrenciesSQL).WillReturnError(boom)

	_, err := repo.ListCurrencies(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error not wrapped: %v", err)
	}
}

func TestCurrencyRepo_ListCurrencies_ScanError_Propagates(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	// Wrong type for `enabled` column → scan failure.
	rows := sqlmock.NewRows([]string{"code", "name", "symbol", "enabled"}).
		AddRow("USD", "US Dollar", "$", "not-a-bool")
	mock.ExpectQuery(listCurrenciesSQL).WillReturnRows(rows)

	_, err := repo.ListCurrencies(context.Background())
	if err == nil {
		t.Fatal("expected scan error, got nil")
	}
}

const getRateOnSQL = `
		SELECT id, tenant_id, from_currency, to_currency, rate, source, effective_at, created_at
		FROM tally.exchange_rate
		WHERE tenant_id = $1
		  AND from_currency = $2
		  AND to_currency = $3
		  AND effective_at <= $4
		ORDER BY effective_at DESC
		LIMIT 1`

func TestCurrencyRepo_GetRateOn_Found_ReturnsRate(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	rateID := uuid.New()
	at := time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC)
	created := time.Date(2026, 4, 23, 1, 0, 0, 0, time.UTC)
	queryAt := time.Date(2026, 4, 24, 0, 0, 0, 0, time.UTC)

	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "from_currency", "to_currency", "rate", "source", "effective_at", "created_at",
	}).AddRow(rateID, tenantID, "USD", "CNY", "7.2500", domain.SourceManual, at, created)

	mock.ExpectQuery(getRateOnSQL).
		WithArgs(tenantID, "USD", "CNY", queryAt).
		WillReturnRows(rows)

	er, err := repo.GetRateOn(context.Background(), tenantID, "USD", "CNY", queryAt)
	if err != nil {
		t.Fatalf("GetRateOn: %v", err)
	}
	if er == nil {
		t.Fatal("expected non-nil rate")
	}
	if !er.Rate.Equal(decimal.RequireFromString("7.25")) {
		t.Errorf("rate = %s, want 7.25", er.Rate)
	}
	if er.Source != domain.SourceManual {
		t.Errorf("source = %s, want manual", er.Source)
	}
	if !er.EffectiveAt.Equal(at) {
		t.Errorf("effective_at = %v, want %v", er.EffectiveAt, at)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestCurrencyRepo_GetRateOn_NotFound_ReturnsNilNil(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	at := time.Now().UTC()

	mock.ExpectQuery(getRateOnSQL).
		WithArgs(tenantID, "USD", "CNY", at).
		WillReturnError(sql.ErrNoRows)

	er, err := repo.GetRateOn(context.Background(), tenantID, "USD", "CNY", at)
	if err != nil {
		t.Fatalf("GetRateOn: %v", err)
	}
	if er != nil {
		t.Errorf("expected nil rate on not-found, got %+v", er)
	}
}

func TestCurrencyRepo_GetRateOn_QueryError_PropagatesWrapped(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	at := time.Now().UTC()
	boom := errors.New("network down")

	mock.ExpectQuery(getRateOnSQL).
		WithArgs(tenantID, "USD", "CNY", at).
		WillReturnError(boom)

	_, err := repo.GetRateOn(context.Background(), tenantID, "USD", "CNY", at)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error not wrapped: %v", err)
	}
}

func TestCurrencyRepo_GetRateOn_BadRateString_ReturnsParseError(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	at := time.Now().UTC()

	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "from_currency", "to_currency", "rate", "source", "effective_at", "created_at",
	}).AddRow(uuid.New(), tenantID, "USD", "CNY", "not-a-decimal", "manual", at, at)

	mock.ExpectQuery(getRateOnSQL).
		WithArgs(tenantID, "USD", "CNY", at).
		WillReturnRows(rows)

	_, err := repo.GetRateOn(context.Background(), tenantID, "USD", "CNY", at)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
}

const saveRateSQL = `
		INSERT INTO tally.exchange_rate
			(id, tenant_id, from_currency, to_currency, rate, source, effective_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (tenant_id, from_currency, to_currency, effective_at)
		DO UPDATE SET rate = EXCLUDED.rate, source = EXCLUDED.source`

func TestCurrencyRepo_SaveRate_Insert_ExecutesUpsert(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	er := &domain.ExchangeRate{
		ID:           uuid.New(),
		TenantID:     uuid.New(),
		FromCurrency: "USD",
		ToCurrency:   "CNY",
		Rate:         decimal.RequireFromString("7.2500"),
		Source:       domain.SourceManual,
		EffectiveAt:  time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC),
		CreatedAt:    time.Date(2026, 4, 23, 1, 0, 0, 0, time.UTC),
	}

	mock.ExpectExec(saveRateSQL).
		WithArgs(er.ID, er.TenantID, er.FromCurrency, er.ToCurrency,
			er.Rate.String(), er.Source, er.EffectiveAt, er.CreatedAt).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.SaveRate(context.Background(), er); err != nil {
		t.Fatalf("SaveRate: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestCurrencyRepo_SaveRate_DBError_PropagatesWrapped(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	er := &domain.ExchangeRate{
		ID:           uuid.New(),
		TenantID:     uuid.New(),
		FromCurrency: "USD",
		ToCurrency:   "CNY",
		Rate:         decimal.NewFromInt(7),
		Source:       domain.SourceManual,
		EffectiveAt:  time.Now().UTC(),
		CreatedAt:    time.Now().UTC(),
	}

	boom := errors.New("constraint violation")
	mock.ExpectExec(saveRateSQL).WillReturnError(boom)

	err := repo.SaveRate(context.Background(), er)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error not wrapped: %v", err)
	}
}

// listRateHistorySQL uses a regex-friendly variant because sqlmock QueryMatcherEqual is whitespace-sensitive.
const listRateHistorySQL = `
		SELECT id, tenant_id, from_currency, to_currency, rate, source, effective_at, created_at
		FROM tally.exchange_rate
		WHERE tenant_id = $1
		  AND from_currency = $2
		  AND to_currency = $3
		  AND effective_at >= now() - ($4::int * interval '1 day')
		ORDER BY effective_at ASC`

func TestCurrencyRepo_ListRateHistory_Multiple_ReturnsAscOrder(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	t1 := time.Date(2026, 4, 22, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC)

	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "from_currency", "to_currency", "rate", "source", "effective_at", "created_at",
	}).
		AddRow(uuid.New(), tenantID, "USD", "CNY", "7.20", "manual", t1, t1).
		AddRow(uuid.New(), tenantID, "USD", "CNY", "7.25", "manual", t2, t2)

	mock.ExpectQuery(listRateHistorySQL).
		WithArgs(tenantID, "USD", "CNY", 30).
		WillReturnRows(rows)

	result, err := repo.ListRateHistory(context.Background(), tenantID, "USD", "CNY", 30)
	if err != nil {
		t.Fatalf("ListRateHistory: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("len = %d, want 2", len(result))
	}
	if !result[0].Rate.Equal(decimal.RequireFromString("7.20")) {
		t.Errorf("first rate = %s, want 7.20", result[0].Rate)
	}
	if !result[1].EffectiveAt.Equal(t2) {
		t.Errorf("second effective_at = %v, want %v", result[1].EffectiveAt, t2)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestCurrencyRepo_ListRateHistory_QueryError_PropagatesWrapped(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	boom := errors.New("read timeout")
	mock.ExpectQuery(listRateHistorySQL).WillReturnError(boom)

	_, err := repo.ListRateHistory(context.Background(), tenantID, "USD", "CNY", 30)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error not wrapped: %v", err)
	}
}

func TestCurrencyRepo_ListRateHistory_BadRateString_ReturnsScanError(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	t1 := time.Now().UTC()
	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "from_currency", "to_currency", "rate", "source", "effective_at", "created_at",
	}).AddRow(uuid.New(), tenantID, "USD", "CNY", "garbage", "manual", t1, t1)

	mock.ExpectQuery(listRateHistorySQL).
		WithArgs(tenantID, "USD", "CNY", 7).
		WillReturnRows(rows)

	_, err := repo.ListRateHistory(context.Background(), tenantID, "USD", "CNY", 7)
	if err == nil {
		t.Fatal("expected scan error, got nil")
	}
}

