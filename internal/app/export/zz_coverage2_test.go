package export

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// This file closes the remaining coverage gap left by zz_coverage_test.go: the
// genuine rows.Scan error branch (as opposed to a rows.Err() failure, which is
// what RowError/CloseError actually trigger — see zz_coverage_test.go's
// "ScanError" tests, which in fact exercise the rows-error path) and the
// cw.Write error branches on the data-row and truncation-trailer writes.
//
// Reused from zz_coverage_test.go (same package, no re-declaration needed):
// sqlmockNew, readCSV, equalSlice.

// failWriter always fails when bufio.Writer actually flushes to it. Because
// csv.Writer wraps its io.Writer in a 4096-byte bufio.Writer, small writes
// (e.g. a short CSV header) never reach failWriter.Write at all — they just
// sit in the buffer — so failWriter only surfaces once accumulated buffered
// bytes cross the 4096-byte threshold. Tests below size their payloads
// deliberately to land the crossover on a specific cw.Write call.
type failWriter struct {
	err   error
	calls int
}

func (f *failWriter) Write(p []byte) (int, error) {
	f.calls++
	return 0, f.err
}

// csvBytesLen returns the exact encoded byte length of writing records through
// a real csv.Writer. Used only to size padding for the buffer-overflow tests
// below; it reflects encoding/csv's own well-known quoting/delimiter rules, not
// the business logic under test, so using it to size a stress payload is not
// self-validating.
func csvBytesLen(t *testing.T, records ...[]string) int {
	t.Helper()
	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)
	for _, r := range records {
		if err := cw.Write(r); err != nil {
			t.Fatalf("csvBytesLen: write: %v", err)
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		t.Fatalf("csvBytesLen: flush: %v", err)
	}
	return buf.Len()
}

// ---------------------------------------------------------------------------
// Genuine rows.Scan() errors (NUMERIC column unexpectedly NULL into a plain
// string destination) — distinct from the rows.Err()/CloseError-driven tests
// already in zz_coverage_test.go.
// ---------------------------------------------------------------------------

func TestStockExport_ScanError_GenuineNullNumeric(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	cols := []string{"code", "name", "warehouse", "qty", "cost"}
	// unit_cost NULL: driver returns nil for a Go `string` destination, which
	// database/sql genuinely rejects ("converting NULL to string is
	// unsupported") — this is a real Scan() failure, not a rows-iteration one.
	rows := mock.NewRows(cols).AddRow("SKU1", "n", "w1", "1.0000", nil)
	mock.ExpectQuery(`SELECT`).
		WithArgs(tenantID, stockRowLimit+1).
		WillReturnRows(rows)

	uc := NewStockExportUseCase(db, nil)
	var buf bytes.Buffer
	count, err := uc.Execute(context.Background(), tenantID, &buf)
	if err == nil {
		t.Fatal("expected genuine scan error, got nil")
	}
	if !strings.Contains(err.Error(), "export stock: scan row") {
		t.Errorf("err = %v, want to mention 'export stock: scan row'", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0 (scan failed before increment)", count)
	}
}

func TestBillsExport_ScanError_GenuineNullNumeric(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	cols := []string{"bill_no", "bill_type", "status", "bill_date", "partner_id", "warehouse_id", "total_amount", "paid_amount", "remark"}
	// total_amount NULL: same genuine "converting NULL to string" Scan failure.
	rows := mock.NewRows(cols).AddRow("B1", "t", int16(0), nil, "", "", nil, "1", "")
	mock.ExpectQuery(`SELECT`).
		WithArgs(tenantID, billsRowLimit+1).
		WillReturnRows(rows)

	uc := NewBillsExportUseCase(db, nil)
	var buf bytes.Buffer
	count, err := uc.Execute(context.Background(), tenantID, &buf)
	if err == nil {
		t.Fatal("expected genuine scan error, got nil")
	}
	if !strings.Contains(err.Error(), "export bills: scan row") {
		t.Errorf("err = %v, want to mention 'export bills: scan row'", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

func TestPaymentsExport_ScanError_GenuineNullNumeric(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	cols := []string{"bill_no", "partner_id", "amount", "pay_type", "pay_date"}
	// amount NULL: same genuine "converting NULL to string" Scan failure.
	rows := mock.NewRows(cols).AddRow("P1", "p1", nil, "cash", nil)
	mock.ExpectQuery(`SELECT`).
		WithArgs(tenantID, paymentsRowLimit+1).
		WillReturnRows(rows)

	uc := NewPaymentsExportUseCase(db, nil)
	var buf bytes.Buffer
	count, err := uc.Execute(context.Background(), tenantID, &buf)
	if err == nil {
		t.Fatal("expected genuine scan error, got nil")
	}
	if !strings.Contains(err.Error(), "export payments: scan row") {
		t.Errorf("err = %v, want to mention 'export payments: scan row'", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

// ---------------------------------------------------------------------------
// cw.Write(dataRow) failures: a data row large enough (>4096 bytes) to force
// csv.Writer's internal bufio.Writer to flush mid-write, surfacing the
// underlying writer's error on this specific write call (not the header,
// which is always too small to trigger a flush on its own — see note).
// ---------------------------------------------------------------------------

func TestStockExport_WriteRowError(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	huge := strings.Repeat("x", 6000)
	rows := mock.NewRows([]string{"code", "name", "warehouse", "qty", "cost"}).
		AddRow(huge, "n", "w1", "1", "1")
	mock.ExpectQuery(`SELECT`).
		WithArgs(tenantID, stockRowLimit+1).
		WillReturnRows(rows)

	uc := NewStockExportUseCase(db, nil)
	fw := &failWriter{err: errors.New("disk full")}
	count, err := uc.Execute(context.Background(), tenantID, fw)
	if err == nil {
		t.Fatal("expected write-row error, got nil")
	}
	if !strings.Contains(err.Error(), "export stock: write row") {
		t.Errorf("err = %v, want to mention 'export stock: write row'", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0 (row failed before increment)", count)
	}
	if fw.calls == 0 {
		t.Error("expected the underlying writer to actually be invoked")
	}
}

func TestBillsExport_WriteRowError(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	huge := strings.Repeat("x", 6000)
	cols := []string{"bill_no", "bill_type", "status", "bill_date", "partner_id", "warehouse_id", "total_amount", "paid_amount", "remark"}
	rows := mock.NewRows(cols).
		AddRow("B1", "t", int16(0), nil, "", "", "1", "1", huge)
	mock.ExpectQuery(`SELECT`).
		WithArgs(tenantID, billsRowLimit+1).
		WillReturnRows(rows)

	uc := NewBillsExportUseCase(db, nil)
	fw := &failWriter{err: errors.New("disk full")}
	count, err := uc.Execute(context.Background(), tenantID, fw)
	if err == nil {
		t.Fatal("expected write-row error, got nil")
	}
	if !strings.Contains(err.Error(), "export bills: write row") {
		t.Errorf("err = %v, want to mention 'export bills: write row'", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

func TestPaymentsExport_WriteRowError(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	huge := strings.Repeat("x", 6000)
	cols := []string{"bill_no", "partner_id", "amount", "pay_type", "pay_date"}
	rows := mock.NewRows(cols).
		AddRow(huge, "p1", "1", "cash", nil)
	mock.ExpectQuery(`SELECT`).
		WithArgs(tenantID, paymentsRowLimit+1).
		WillReturnRows(rows)

	uc := NewPaymentsExportUseCase(db, nil)
	fw := &failWriter{err: errors.New("disk full")}
	count, err := uc.Execute(context.Background(), tenantID, fw)
	if err == nil {
		t.Fatal("expected write-row error, got nil")
	}
	if !strings.Contains(err.Error(), "export payments: write row") {
		t.Errorf("err = %v, want to mention 'export payments: write row'", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

// ---------------------------------------------------------------------------
// cw.Write(truncationTrailer) failures: rowLimit=1 with row 0's padding sized
// (via csvBytesLen) so header+row0 lands just under bufio's 4096-byte buffer
// — row0 itself must succeed — while adding the trailer content pushes the
// cumulative total over 4096, forcing the *trailer* write specifically to hit
// the failing writer.
// ---------------------------------------------------------------------------

func TestStockExport_WriteTruncationTrailerError(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	const rowLimit = 1
	trailerMsg := fmt.Sprintf("数据超过 %d 行限制", rowLimit)
	trailerRow := []string{"[截断]", trailerMsg, "", "", ""}
	baseRowNoPad := []string{"P1", "n", "w1", "1", ""} // last field (cost) holds the padding

	headerLen := csvBytesLen(t, stockHeader)
	baseLen := csvBytesLen(t, baseRowNoPad)
	trailerLen := csvBytesLen(t, trailerRow)
	const margin = 8
	if trailerLen <= margin {
		t.Fatalf("test assumption invalid: trailerLen=%d <= margin=%d, trailer wouldn't overflow the buffer", trailerLen, margin)
	}
	padLen := 4096 - headerLen - baseLen - margin
	if padLen < 0 {
		t.Fatalf("padLen = %d (negative); header/base row too large for this technique", padLen)
	}
	padded := strings.Repeat("p", padLen)

	cols := []string{"code", "name", "warehouse", "qty", "cost"}
	rows := mock.NewRows(cols).
		AddRow("P1", "n", "w1", "1", padded). // row 0: succeeds, fills buffer near-full
		AddRow("P2", "n", "w2", "1", "1")     // extra (limit+1) row: scanned, then discarded for trailer
	mock.ExpectQuery(`SELECT`).
		WithArgs(tenantID, rowLimit+1).
		WillReturnRows(rows)

	uc := NewStockExportUseCase(db, nil, WithRowLimit(rowLimit))
	fw := &failWriter{err: errors.New("disk full")}
	count, err := uc.Execute(context.Background(), tenantID, fw)
	if err == nil {
		t.Fatal("expected truncation-trailer write error, got nil")
	}
	if !strings.Contains(err.Error(), "export stock: write truncation note") {
		t.Errorf("err = %v, want to mention 'export stock: write truncation note'", err)
	}
	if count != rowLimit {
		t.Errorf("count = %d, want %d (row0 succeeded before trailer failed)", count, rowLimit)
	}
}

func TestBillsExport_WriteTruncationTrailerError(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	const rowLimit = 1
	trailerMsg := fmt.Sprintf("数据超过 %d 行限制，请联系管理员导出完整数据", rowLimit)
	trailerRow := []string{"[截断]", trailerMsg, "", "", "", "", "", "", ""}
	baseRowNoPad := []string{"B1", "t", "草稿", "", "", "", "1", "1", ""} // remark holds the padding

	headerLen := csvBytesLen(t, billsHeader)
	baseLen := csvBytesLen(t, baseRowNoPad)
	trailerLen := csvBytesLen(t, trailerRow)
	const margin = 8
	if trailerLen <= margin {
		t.Fatalf("test assumption invalid: trailerLen=%d <= margin=%d, trailer wouldn't overflow the buffer", trailerLen, margin)
	}
	padLen := 4096 - headerLen - baseLen - margin
	if padLen < 0 {
		t.Fatalf("padLen = %d (negative); header/base row too large for this technique", padLen)
	}
	padded := strings.Repeat("p", padLen)

	cols := []string{"bill_no", "bill_type", "status", "bill_date", "partner_id", "warehouse_id", "total_amount", "paid_amount", "remark"}
	rows := mock.NewRows(cols).
		AddRow("B1", "t", int16(0), nil, "", "", "1", "1", padded).
		AddRow("B2", "t", int16(0), nil, "", "", "1", "1", "")
	mock.ExpectQuery(`SELECT`).
		WithArgs(tenantID, rowLimit+1).
		WillReturnRows(rows)

	uc := NewBillsExportUseCase(db, nil, WithRowLimit(rowLimit))
	fw := &failWriter{err: errors.New("disk full")}
	count, err := uc.Execute(context.Background(), tenantID, fw)
	if err == nil {
		t.Fatal("expected truncation-trailer write error, got nil")
	}
	if !strings.Contains(err.Error(), "export bills: write truncation note") {
		t.Errorf("err = %v, want to mention 'export bills: write truncation note'", err)
	}
	if count != rowLimit {
		t.Errorf("count = %d, want %d", count, rowLimit)
	}
}

func TestPaymentsExport_WriteTruncationTrailerError(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	const rowLimit = 1
	trailerMsg := fmt.Sprintf("数据超过 %d 行限制", rowLimit)
	trailerRow := []string{"[截断]", trailerMsg, "", "", ""}
	baseRowNoPad := []string{"P1", "p1", "1", "cash", ""} // pay_type holds the padding

	headerLen := csvBytesLen(t, paymentsHeader)
	baseLen := csvBytesLen(t, baseRowNoPad)
	trailerLen := csvBytesLen(t, trailerRow)
	const margin = 8
	if trailerLen <= margin {
		t.Fatalf("test assumption invalid: trailerLen=%d <= margin=%d, trailer wouldn't overflow the buffer", trailerLen, margin)
	}
	padLen := 4096 - headerLen - baseLen - margin
	if padLen < 0 {
		t.Fatalf("padLen = %d (negative); header/base row too large for this technique", padLen)
	}
	padded := strings.Repeat("p", padLen)

	cols := []string{"bill_no", "partner_id", "amount", "pay_type", "pay_date"}
	rows := mock.NewRows(cols).
		AddRow("P1", "p1", "1", padded, nil).
		AddRow("P2", "p1", "1", "cash", nil)
	mock.ExpectQuery(`SELECT`).
		WithArgs(tenantID, rowLimit+1).
		WillReturnRows(rows)

	uc := NewPaymentsExportUseCase(db, nil, WithRowLimit(rowLimit))
	fw := &failWriter{err: errors.New("disk full")}
	count, err := uc.Execute(context.Background(), tenantID, fw)
	if err == nil {
		t.Fatal("expected truncation-trailer write error, got nil")
	}
	if !strings.Contains(err.Error(), "export payments: write truncation note") {
		t.Errorf("err = %v, want to mention 'export payments: write truncation note'", err)
	}
	if count != rowLimit {
		t.Errorf("count = %d, want %d", count, rowLimit)
	}
}
