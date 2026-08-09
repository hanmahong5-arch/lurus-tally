package export

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
)

// sqlmockNew builds a sqlmock-backed *sql.DB plus its expectation recorder.
// Execute always resolves its DB handle via dbscope.From(ctx, uc.db); with a
// plain (non-pinned) context that falls back to uc.db directly, so the mock DB
// is exercised exactly like production when no request-scoped conn is pinned.
func sqlmockNew(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	return db, mock
}

func mustTime(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func mustDatetime(year int, month time.Month, day, hour, min, sec int) time.Time {
	return time.Date(year, month, day, hour, min, sec, 0, time.UTC)
}

// ---------------------------------------------------------------------------
// Stock export
// ---------------------------------------------------------------------------

func TestStockExport_HappyPath_NumericAsString(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	cols := []string{"code", "name", "warehouse", "qty", "cost"}
	rows := mock.NewRows(cols).
		AddRow("SKU001", "苗木A", "wh-1", "123.4500", "9.990000").
		AddRow("SKU002", "苗木B", "wh-2", "0.0000", "0.000001")
	mock.ExpectQuery(`SELECT`).
		WithArgs(tenantID, stockRowLimit+1).
		WillReturnRows(rows)

	uc := NewStockExportUseCase(db, nil)
	var buf bytes.Buffer
	count, err := uc.Execute(context.Background(), tenantID, &buf)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}

	records := readCSV(t, buf.Bytes())
	if len(records) != 3 { // header + 2 rows
		t.Fatalf("records = %d, want 3", len(records))
	}
	if got := records[0]; !equalSlice(got, stockHeader) {
		t.Errorf("header = %v, want %v", got, stockHeader)
	}
	// Business invariant: NUMERIC values pass through byte-for-byte, no float
	// rounding/formatting (e.g. "9.990000" must not become "9.99").
	if records[1][3] != "123.4500" || records[1][4] != "9.990000" {
		t.Errorf("row1 numeric mismatch: %v", records[1])
	}
	if records[2][3] != "0.0000" || records[2][4] != "0.000001" {
		t.Errorf("row2 numeric mismatch: %v", records[2])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestStockExport_EmptyResultSet(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	mock.ExpectQuery(`SELECT`).
		WithArgs(tenantID, stockRowLimit+1).
		WillReturnRows(mock.NewRows([]string{"code", "name", "warehouse", "qty", "cost"}))

	uc := NewStockExportUseCase(db, nil)
	var buf bytes.Buffer
	count, err := uc.Execute(context.Background(), tenantID, &buf)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0", count)
	}
	records := readCSV(t, buf.Bytes())
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1 (header only)", len(records))
	}
}

func TestStockExport_Truncation(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	// rowLimit=2, DB returns 3 rows (limit+1) → 2 data rows + trailer, count==2.
	rows := mock.NewRows([]string{"code", "name", "warehouse", "qty", "cost"}).
		AddRow("A", "a", "w1", "1", "1").
		AddRow("B", "b", "w2", "2", "2").
		AddRow("C", "c", "w3", "3", "3")
	mock.ExpectQuery(`SELECT`).
		WithArgs(tenantID, 3).
		WillReturnRows(rows)

	uc := NewStockExportUseCase(db, nil, WithRowLimit(2))
	var buf bytes.Buffer
	count, err := uc.Execute(context.Background(), tenantID, &buf)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2 (truncated at rowLimit, not rowLimit+1)", count)
	}
	records := readCSV(t, buf.Bytes())
	// header + 2 data rows + 1 trailer = 4
	if len(records) != 4 {
		t.Fatalf("records = %d, want 4", len(records))
	}
	if records[3][0] != "[截断]" {
		t.Errorf("trailer marker = %q, want [截断]", records[3][0])
	}
	// The 3rd real DB row ("C") must never appear as data.
	for _, r := range records[1:3] {
		if r[0] == "C" {
			t.Errorf("row beyond rowLimit leaked into output: %v", r)
		}
	}
}

func TestStockExport_NoTruncationWhenUnderLimit(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	rows := mock.NewRows([]string{"code", "name", "warehouse", "qty", "cost"})
	for i := 0; i < 5; i++ {
		rows.AddRow("C", "n", "w", "1", "1")
	}
	mock.ExpectQuery(`SELECT`).
		WithArgs(tenantID, stockRowLimit+1).
		WillReturnRows(rows)

	uc := NewStockExportUseCase(db, nil) // default limit 50000
	var buf bytes.Buffer
	count, err := uc.Execute(context.Background(), tenantID, &buf)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if count != 5 {
		t.Fatalf("count = %d, want 5", count)
	}
	records := readCSV(t, buf.Bytes())
	if len(records) != 6 { // header + 5, no trailer
		t.Fatalf("records = %d, want 6 (no trailer)", len(records))
	}
}

func TestStockExport_QueryError(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	wantErr := errors.New("boom")
	mock.ExpectQuery(`SELECT`).
		WithArgs(tenantID, stockRowLimit+1).
		WillReturnError(wantErr)

	uc := NewStockExportUseCase(db, nil)
	var buf bytes.Buffer
	count, err := uc.Execute(context.Background(), tenantID, &buf)
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
	if err == nil || !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want wrapping %v", err, wantErr)
	}
}

func TestStockExport_ScanError(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	scanErr := errors.New("scan boom")
	rows := mock.NewRows([]string{"code", "name", "warehouse", "qty", "cost"}).
		AddRow("A", "a", "w1", "1", "1").
		RowError(0, scanErr)
	mock.ExpectQuery(`SELECT`).
		WithArgs(tenantID, stockRowLimit+1).
		WillReturnRows(rows)

	uc := NewStockExportUseCase(db, nil)
	var buf bytes.Buffer
	_, err := uc.Execute(context.Background(), tenantID, &buf)
	if err == nil {
		t.Fatal("expected scan error, got nil")
	}
}

func TestStockExport_RowsErr(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	closeErr := errors.New("rows iteration failed")
	rows := mock.NewRows([]string{"code", "name", "warehouse", "qty", "cost"}).
		AddRow("A", "a", "w1", "1", "1").
		CloseError(closeErr)
	mock.ExpectQuery(`SELECT`).
		WithArgs(tenantID, stockRowLimit+1).
		WillReturnRows(rows)

	uc := NewStockExportUseCase(db, nil)
	var buf bytes.Buffer
	count, err := uc.Execute(context.Background(), tenantID, &buf)
	if err == nil {
		t.Fatal("expected rows error, got nil")
	}
	if count != 1 {
		t.Errorf("count = %d, want 1 (row scanned before rows.Err surfaced)", count)
	}
}

func TestStockExport_NilLoggerDefaultsToSlogDefault(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	// Force the truncation branch (which logs) with a nil logger — must not panic.
	rows := mock.NewRows([]string{"code", "name", "warehouse", "qty", "cost"}).
		AddRow("A", "a", "w1", "1", "1").
		AddRow("B", "b", "w2", "2", "2")
	mock.ExpectQuery(`SELECT`).
		WithArgs(tenantID, 2).
		WillReturnRows(rows)

	uc := NewStockExportUseCase(db, nil, WithRowLimit(1))
	var buf bytes.Buffer
	count, err := uc.Execute(context.Background(), tenantID, &buf)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
}

// ---------------------------------------------------------------------------
// Bills export
// ---------------------------------------------------------------------------

func TestBillsExport_HappyPath_TenantScopedAndSoftDeleteFiltered(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	cols := []string{"bill_no", "bill_type", "status", "bill_date", "partner_id", "warehouse_id", "total_amount", "paid_amount", "remark"}
	rows := mock.NewRows(cols).
		AddRow("B0001", "purchase", int16(0), nil, "p1", "w1", "1000.00", "500.00", "note1").
		AddRow("B0002", "sale", int16(2), mustTime(2026, 6, 13), "p2", "w2", "2500.5000", "2500.5000", "")
	// Assert tenant isolation: WHERE tenant_id=$1 AND deleted_at IS NULL — the
	// query text match plus WithArgs(tenantID, ...) pins arg #1 to tenantID.
	mock.ExpectQuery(`(?i)FROM tally\.bill_head\s+WHERE tenant_id = \$1 AND deleted_at IS NULL`).
		WithArgs(tenantID, billsRowLimit+1).
		WillReturnRows(rows)

	uc := NewBillsExportUseCase(db, nil)
	var buf bytes.Buffer
	count, err := uc.Execute(context.Background(), tenantID, &buf)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}

	records := readCSV(t, buf.Bytes())
	if len(records) != 3 {
		t.Fatalf("records = %d, want 3", len(records))
	}
	if !equalSlice(records[0], billsHeader) {
		t.Errorf("header = %v, want %v", records[0], billsHeader)
	}
	// row1: status 0 -> 草稿, NULL date -> "".
	if records[1][2] != "草稿" || records[1][3] != "" {
		t.Errorf("row1 status/date = %q/%q", records[1][2], records[1][3])
	}
	// Business invariant: amounts pass through as exact NUMERIC text.
	if records[1][6] != "1000.00" || records[1][7] != "500.00" {
		t.Errorf("row1 amounts mismatch: %v", records[1])
	}
	// row2: status 2 -> 已审核, valid date formatted with dateLayout.
	if records[2][2] != "已审核" || records[2][3] != "2026-06-13" {
		t.Errorf("row2 status/date = %q/%q", records[2][2], records[2][3])
	}
	if records[2][6] != "2500.5000" || records[2][7] != "2500.5000" {
		t.Errorf("row2 amounts mismatch: %v", records[2])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestBillsExport_StatusCancelledAndUnknownFallback(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	cols := []string{"bill_no", "bill_type", "status", "bill_date", "partner_id", "warehouse_id", "total_amount", "paid_amount", "remark"}
	rows := mock.NewRows(cols).
		AddRow("B0003", "purchase", int16(9), nil, "", "", "0.00", "0.00", "").
		AddRow("B0004", "purchase", int16(4), nil, "", "", "0.00", "0.00", "")
	mock.ExpectQuery(`SELECT`).
		WithArgs(tenantID, billsRowLimit+1).
		WillReturnRows(rows)

	uc := NewBillsExportUseCase(db, nil)
	var buf bytes.Buffer
	if _, err := uc.Execute(context.Background(), tenantID, &buf); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	records := readCSV(t, buf.Bytes())
	if records[1][2] != "已取消" {
		t.Errorf("status 9 = %q, want 已取消", records[1][2])
	}
	if records[2][2] != "4" {
		t.Errorf("status 4 (unknown) = %q, want numeric fallback \"4\"", records[2][2])
	}
}

func TestBillsExport_Truncation(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	cols := []string{"bill_no", "bill_type", "status", "bill_date", "partner_id", "warehouse_id", "total_amount", "paid_amount", "remark"}
	rows := mock.NewRows(cols).
		AddRow("B1", "t", int16(0), nil, "", "", "1", "1", "").
		AddRow("B2", "t", int16(0), nil, "", "", "1", "1", "").
		AddRow("B3", "t", int16(0), nil, "", "", "1", "1", "")
	mock.ExpectQuery(`SELECT`).
		WithArgs(tenantID, 3).
		WillReturnRows(rows)

	uc := NewBillsExportUseCase(db, nil, WithRowLimit(2))
	var buf bytes.Buffer
	count, err := uc.Execute(context.Background(), tenantID, &buf)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	records := readCSV(t, buf.Bytes())
	if len(records) != 4 { // header + 2 + trailer
		t.Fatalf("records = %d, want 4", len(records))
	}
	if records[3][0] != "[截断]" {
		t.Errorf("trailer = %v", records[3])
	}
}

func TestBillsExport_QueryError(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	wantErr := errors.New("db down")
	mock.ExpectQuery(`SELECT`).
		WithArgs(tenantID, billsRowLimit+1).
		WillReturnError(wantErr)

	uc := NewBillsExportUseCase(db, nil)
	var buf bytes.Buffer
	count, err := uc.Execute(context.Background(), tenantID, &buf)
	if count != 0 || err == nil || !errors.Is(err, wantErr) {
		t.Fatalf("count=%d err=%v, want 0 + wrapped %v", count, err, wantErr)
	}
}

func TestBillsExport_ScanError(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	cols := []string{"bill_no", "bill_type", "status", "bill_date", "partner_id", "warehouse_id", "total_amount", "paid_amount", "remark"}
	rows := mock.NewRows(cols).
		AddRow("B1", "t", int16(0), nil, "", "", "1", "1", "").
		RowError(0, errors.New("scan fail"))
	mock.ExpectQuery(`SELECT`).
		WithArgs(tenantID, billsRowLimit+1).
		WillReturnRows(rows)

	uc := NewBillsExportUseCase(db, nil)
	var buf bytes.Buffer
	if _, err := uc.Execute(context.Background(), tenantID, &buf); err == nil {
		t.Fatal("expected scan error, got nil")
	}
}

func TestBillsExport_RowsErr(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	cols := []string{"bill_no", "bill_type", "status", "bill_date", "partner_id", "warehouse_id", "total_amount", "paid_amount", "remark"}
	rows := mock.NewRows(cols).
		AddRow("B1", "t", int16(0), nil, "", "", "1", "1", "").
		CloseError(errors.New("iter fail"))
	mock.ExpectQuery(`SELECT`).
		WithArgs(tenantID, billsRowLimit+1).
		WillReturnRows(rows)

	uc := NewBillsExportUseCase(db, nil)
	var buf bytes.Buffer
	if _, err := uc.Execute(context.Background(), tenantID, &buf); err == nil {
		t.Fatal("expected rows error, got nil")
	}
}

// ---------------------------------------------------------------------------
// Payments export
// ---------------------------------------------------------------------------

func TestPaymentsExport_HappyPath_TenantScopedAndSoftDeleteFiltered(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	cols := []string{"bill_no", "partner_id", "amount", "pay_type", "pay_date"}
	rows := mock.NewRows(cols).
		AddRow("B0001", "p1", "999.990000", "cash", mustDatetime(2026, 6, 13, 14, 30, 5)).
		AddRow("", "", "0.00", "wire", nil)
	mock.ExpectQuery(`(?i)FROM tally\.payment_head\s+WHERE tenant_id = \$1 AND deleted_at IS NULL`).
		WithArgs(tenantID, paymentsRowLimit+1).
		WillReturnRows(rows)

	uc := NewPaymentsExportUseCase(db, nil)
	var buf bytes.Buffer
	count, err := uc.Execute(context.Background(), tenantID, &buf)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	records := readCSV(t, buf.Bytes())
	if len(records) != 3 {
		t.Fatalf("records = %d, want 3", len(records))
	}
	if !equalSlice(records[0], paymentsHeader) {
		t.Errorf("header = %v, want %v", records[0], paymentsHeader)
	}
	// Business invariant: amount is exact NUMERIC text, not float-rounded.
	if records[1][2] != "999.990000" {
		t.Errorf("amount = %q, want 999.990000", records[1][2])
	}
	if records[1][4] != "2026-06-13 14:30:05" {
		t.Errorf("pay_date = %q, want 2026-06-13 14:30:05", records[1][4])
	}
	if records[2][4] != "" {
		t.Errorf("NULL pay_date = %q, want empty", records[2][4])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestPaymentsExport_Truncation(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	cols := []string{"bill_no", "partner_id", "amount", "pay_type", "pay_date"}
	rows := mock.NewRows(cols).
		AddRow("P1", "p", "1", "cash", nil).
		AddRow("P2", "p", "1", "cash", nil).
		AddRow("P3", "p", "1", "cash", nil)
	mock.ExpectQuery(`SELECT`).
		WithArgs(tenantID, 3).
		WillReturnRows(rows)

	uc := NewPaymentsExportUseCase(db, nil, WithRowLimit(2))
	var buf bytes.Buffer
	count, err := uc.Execute(context.Background(), tenantID, &buf)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	records := readCSV(t, buf.Bytes())
	if len(records) != 4 {
		t.Fatalf("records = %d, want 4", len(records))
	}
	if records[3][0] != "[截断]" {
		t.Errorf("trailer = %v", records[3])
	}
}

func TestPaymentsExport_NoTruncationWhenUnderLimit(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	cols := []string{"bill_no", "partner_id", "amount", "pay_type", "pay_date"}
	rows := mock.NewRows(cols).AddRow("P1", "p", "1", "cash", nil)
	mock.ExpectQuery(`SELECT`).
		WithArgs(tenantID, paymentsRowLimit+1).
		WillReturnRows(rows)

	uc := NewPaymentsExportUseCase(db, nil)
	var buf bytes.Buffer
	count, err := uc.Execute(context.Background(), tenantID, &buf)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	records := readCSV(t, buf.Bytes())
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2 (no trailer)", len(records))
	}
}

func TestPaymentsExport_EmptyResultSet(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	mock.ExpectQuery(`SELECT`).
		WithArgs(tenantID, paymentsRowLimit+1).
		WillReturnRows(mock.NewRows([]string{"bill_no", "partner_id", "amount", "pay_type", "pay_date"}))

	uc := NewPaymentsExportUseCase(db, nil)
	var buf bytes.Buffer
	count, err := uc.Execute(context.Background(), tenantID, &buf)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0", count)
	}
	records := readCSV(t, buf.Bytes())
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1 (header only)", len(records))
	}
}

func TestPaymentsExport_QueryError(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	wantErr := errors.New("conn refused")
	mock.ExpectQuery(`SELECT`).
		WithArgs(tenantID, paymentsRowLimit+1).
		WillReturnError(wantErr)

	uc := NewPaymentsExportUseCase(db, nil)
	var buf bytes.Buffer
	count, err := uc.Execute(context.Background(), tenantID, &buf)
	if count != 0 || err == nil || !errors.Is(err, wantErr) {
		t.Fatalf("count=%d err=%v, want 0 + wrapped %v", count, err, wantErr)
	}
}

func TestPaymentsExport_ScanError(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	cols := []string{"bill_no", "partner_id", "amount", "pay_type", "pay_date"}
	rows := mock.NewRows(cols).
		AddRow("P1", "p", "1", "cash", nil).
		RowError(0, errors.New("scan fail"))
	mock.ExpectQuery(`SELECT`).
		WithArgs(tenantID, paymentsRowLimit+1).
		WillReturnRows(rows)

	uc := NewPaymentsExportUseCase(db, nil)
	var buf bytes.Buffer
	if _, err := uc.Execute(context.Background(), tenantID, &buf); err == nil {
		t.Fatal("expected scan error, got nil")
	}
}

func TestPaymentsExport_RowsErr(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	cols := []string{"bill_no", "partner_id", "amount", "pay_type", "pay_date"}
	rows := mock.NewRows(cols).
		AddRow("P1", "p", "1", "cash", nil).
		CloseError(errors.New("iter fail"))
	mock.ExpectQuery(`SELECT`).
		WithArgs(tenantID, paymentsRowLimit+1).
		WillReturnRows(rows)

	uc := NewPaymentsExportUseCase(db, nil)
	var buf bytes.Buffer
	if _, err := uc.Execute(context.Background(), tenantID, &buf); err == nil {
		t.Fatal("expected rows error, got nil")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func readCSV(t *testing.T, b []byte) [][]string {
	t.Helper()
	r := csv.NewReader(bytes.NewReader(b))
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("csv parse: %v (raw: %q)", err, string(b))
	}
	return records
}

func equalSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
