package export

import (
	"bytes"
	"context"
	"testing"

	"github.com/google/uuid"
)

// The bills CSV must net approved sales returns off the sale total, the same balance the
// list and the receivables ageing use: the unpaid column and the payment state both read
// GREATEST(total - returns, 0), and the returns CTE is joined per sale.
func TestBillsExport_UnpaidAndStateAreNetOfReturns(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	mock.ExpectQuery(`(?is)^\s*WITH\s+rt AS .*rsum AS .*`+
		`THEN GREATEST\(GREATEST\(bh\.total_amount - COALESCE\(r\.amt, 0\), 0\) - bh\.paid_amount, 0\)::text.*`+
		`WHEN bh\.paid_amount < GREATEST\(bh\.total_amount - COALESCE\(r\.amt, 0\), 0\) THEN '部分'.*`+
		`LEFT JOIN rsum r ON r\.sale_id = bh\.id`).
		WithArgs(tenantID, billsRowLimit+1).
		WillReturnRows(mock.NewRows([]string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k"}).
			AddRow("S1", "出库", int16(2), nil, "老王", "一号仓", "300.00", "100.00", "", "0", "已付清"))

	var buf bytes.Buffer
	if _, err := NewBillsExportUseCase(db, nil).Execute(context.Background(), tenantID, &buf); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
	rec := readCSV(t, buf.Bytes())
	if len(rec) != 2 || rec[1][9] != "0" || rec[1][10] != "已付清" {
		t.Errorf("rows = %v", rec)
	}
}

// The stock CSV warehouse column is the warehouse name, never its UUID.
func TestStockExport_WarehouseColumnIsName(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	mock.ExpectQuery(`(?is)COALESCE\(wh\.name, ''\).*LEFT JOIN tally\.warehouse wh ON wh\.id = ss\.warehouse_id AND wh\.tenant_id = ss\.tenant_id`).
		WithArgs(tenantID, stockRowLimit+1).
		WillReturnRows(mock.NewRows([]string{"a", "b", "c", "d", "e", "f"}).AddRow("A1", "螺丝", "一号仓", "10", "2.5", "25.00"))

	var buf bytes.Buffer
	if _, err := NewStockExportUseCase(db, nil).Execute(context.Background(), tenantID, &buf); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	rec := readCSV(t, buf.Bytes())
	if len(rec) != 2 || rec[1][2] != "一号仓" {
		t.Errorf("rows = %v", rec)
	}
}
