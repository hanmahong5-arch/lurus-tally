package export

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The stock CSV closes with the stock value (quantity x unit cost) per row.
func TestStockExport_ValueColumn(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	mock.ExpectQuery(`(?is)ROUND\(ss\.on_hand_qty \* ss\.unit_cost, 2\)`).
		WithArgs(tenantID, stockRowLimit+1).
		WillReturnRows(mock.NewRows([]string{"a", "b", "c", "d", "e", "f"}).AddRow("A1", "螺丝", "一号仓", "10", "2.5", "25.00"))

	var buf bytes.Buffer
	if _, err := NewStockExportUseCase(db, nil).Execute(context.Background(), tenantID, &buf); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	rec := readCSV(t, buf.Bytes())
	if rec[0][5] != "库存金额" || rec[1][5] != "25.00" {
		t.Errorf("rows = %v", rec)
	}
}

// The bills CSV type column is the sub type, and a return carries no unpaid amount or state.
func TestBillsExport_TypeIsSubTypeAndReturnsHaveNoUnpaid(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()

	mock.ExpectQuery(`(?is)COALESCE\(NULLIF\(bh\.sub_type, ''\), bh\.bill_type\).*`+
		`bh\.status = 2 AND bh\.sub_type NOT IN \('销售退货', '采购退货'\) THEN GREATEST.*`+
		`WHEN bh\.sub_type IN \('销售退货', '采购退货'\) THEN '退货单'`).
		WithArgs(tenantID, billsRowLimit+1).
		WillReturnRows(mock.NewRows([]string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k"}).
			AddRow("R1", "销售退货", int16(2), nil, "老王", "一号仓", "30.00", "0", "", "", "退货单"))

	var buf bytes.Buffer
	if _, err := NewBillsExportUseCase(db, nil).Execute(context.Background(), tenantID, &buf); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
	rec := readCSV(t, buf.Bytes())
	if rec[1][1] != "销售退货" || rec[1][9] != "" || rec[1][10] != "退货单" {
		t.Errorf("rows = %v", rec)
	}
}

// A date range narrows the payments CSV to pay_date in [from, to).
func TestPaymentsExport_DateRange(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, BusinessTZ)
	to := time.Date(2026, 10, 1, 0, 0, 0, 0, BusinessTZ)

	mock.ExpectQuery(`(?is)ph\.pay_date >= \$2 AND ph\.pay_date < \$3.*LIMIT \$4`).
		WithArgs(tenantID, from, to, paymentsRowLimit+1).
		WillReturnRows(mock.NewRows([]string{"a", "b", "c", "d", "e", "f", "g", "h"}).AddRow("PO-1", "老王", "1.0000", "cash", nil, "out", true, "采购"))

	var buf bytes.Buffer
	n, err := NewPaymentsExportUseCase(db, nil).ExecuteRange(context.Background(), tenantID, &from, &to, &buf)
	if err != nil || n != 1 {
		t.Fatalf("ExecuteRange n=%d err=%v", n, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// Direction follows the side of the shop the bill sits on: sales and sales returns are money
// coming in, purchases and purchase returns money going out. A payment hung on a return bill
// is the opposite flow, so its amount is negated and nets off against the same side. Credit
// and bill-less rows are not counted, matching the monthly figures.
func TestPaymentsExport_DirectionColumnsAndFilters(t *testing.T) {
	db, mock := sqlmockNew(t)
	defer db.Close()
	tenantID := uuid.New()
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, BusinessTZ)

	mock.ExpectQuery(`(?is)WHERE.*ph\.pay_date >= \$2.*AND bh\.sub_type IN \('销售', '销售退货'\).*AND ph\.pay_type <> 'credit'.*LIMIT \$3`).
		WithArgs(tenantID, from, paymentsRowLimit+1).
		WillReturnRows(mock.NewRows([]string{"a", "b", "c", "d", "e", "f", "g", "h"}).
			AddRow("SO-1", "老王", "10.0000", "cash", nil, "in", true, "销售").
			AddRow("RT-1", "老王", "-5.0000", "cash", nil, "in", true, "销售退货").
			AddRow("SO-2", "老李", "8.0000", "credit", nil, "in", false, "销售").
			AddRow("", "", "1.0000", "cash", nil, "", false, ""))

	var buf bytes.Buffer
	n, err := NewPaymentsExportUseCase(db, nil).ExecuteFiltered(context.Background(), tenantID,
		PaymentsFilter{From: &from, Direction: "in", CountedOnly: true}, &buf)
	if err != nil || n != 4 {
		t.Fatalf("ExecuteFiltered n=%d err=%v", n, err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if lines[0] != "单号,收付款方,金额,方式,时间,收/付,是否计入实收,关联单据类型" {
		t.Errorf("header = %q", lines[0])
	}
	want := []string{
		"SO-1,老王,10.00,现金,,收,是,销售",
		"RT-1,老王,-5.00,现金,,收,是,销售退货",
		"SO-2,老李,8.00,赊账,,收,否,销售",
	}
	for i, w := range want {
		if lines[i+1] != w {
			t.Errorf("row %d = %q, want %q", i, lines[i+1], w)
		}
	}
	if !strings.HasSuffix(lines[4], ",现金,,,否,") {
		t.Errorf("bill-less row = %q", lines[4])
	}
}

func TestPaymentsExport_RejectsUnknownDirection(t *testing.T) {
	db, _ := sqlmockNew(t)
	defer db.Close()
	_, err := NewPaymentsExportUseCase(db, nil).ExecuteFiltered(context.Background(), uuid.New(),
		PaymentsFilter{Direction: "sideways"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("want error for unknown direction")
	}
}
