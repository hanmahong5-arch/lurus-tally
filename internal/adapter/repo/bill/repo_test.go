package bill_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	repobill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/bill"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
)

// TestBillRepoPG_InterfaceSatisfied is a compile-time proof that *Repo satisfies BillRepo.
// The actual DB integration tests require a live PG instance (CI environment).
func TestBillRepoPG_InterfaceSatisfied(t *testing.T) {
	var _ appbill.BillRepo = repobill.New((*sql.DB)(nil))
	t.Log("*repobill.Repo satisfies appbill.BillRepo — compile-time check passed")
}

var billHeadCols = []string{
	"id", "tenant_id", "bill_no", "bill_type", "sub_type", "status", "partner_id", "warehouse_id",
	"creator_id", "bill_date", "subtotal", "shipping_fee", "tax_amount", "total_amount",
	"paid_amount", "approved_at", "approved_by", "revision", "remark", "created_at", "updated_at",
	"currency", "exchange_rate", "amount_local", "occurred_at",
}

// TestGetBillForUpdate_ReadsCurrency guards the draft-update path: a foreign-currency
// draft must come back with its stored currency so the update keeps converting at
// the stored rate instead of falling back to 1:1.
func TestGetBillForUpdate_ReadsCurrency(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	tenantID, billID := uuid.New(), uuid.New()
	now := time.Now()
	occ := now.Add(-4 * time.Hour)
	mock.ExpectBegin()
	mock.ExpectQuery(`COALESCE\(currency, 'CNY'\), exchange_rate, COALESCE\(amount_local, total_amount\),\s+occurred_at\s+FROM tally.bill_head`).
		WithArgs(billID, tenantID).
		WillReturnRows(sqlmock.NewRows(billHeadCols).AddRow(
			billID, tenantID, "PO-1", "入库", "采购", int16(0), nil, nil,
			uuid.New(), now, "100", "0", "0", "720",
			"0", nil, nil, 1, "", now, now,
			"USD", "7.20000000", "100", occ,
		))

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	h, err := repobill.New(db).GetBillForUpdate(context.Background(), tx, tenantID, billID)
	if err != nil {
		t.Fatalf("GetBillForUpdate: %v", err)
	}
	if h.Currency != "USD" {
		t.Errorf("Currency = %q, want USD", h.Currency)
	}
	if h.ExchangeRateVal.String() != "7.2" {
		t.Errorf("ExchangeRateVal = %s, want 7.2", h.ExchangeRateVal)
	}
	if h.OccurredAt == nil || !h.OccurredAt.Equal(occ) {
		t.Errorf("OccurredAt = %v, want %v（离线单真实成交时间）", h.OccurredAt, occ)
	}
	if h.AmountLocal.String() != "100" {
		t.Errorf("AmountLocal = %s, want 100 (original-currency total)", h.AmountLocal)
	}
}

// TestListBills_KeywordEscapesLikeMetacharacters checks that backslash, % and _ in
// the search keyword are matched literally.
func TestListBills_KeywordEscapesLikeMetacharacters(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	tenantID := uuid.New()
	want := `%a\\b\%c\_d%`
	mock.ExpectQuery(`SELECT COUNT\(\*\)`).
		WithArgs(tenantID, want).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`ORDER BY b.bill_date DESC`).
		WithArgs(tenantID, want, 20, 0).
		WillReturnRows(sqlmock.NewRows(append(billHeadCols[:21:21], "exchange_rate", "partner_name")))

	if _, _, err := repobill.New(db).ListBills(context.Background(), appbill.BillListFilter{
		TenantID: tenantID,
		Keyword:  `a\b%c_d`,
	}); err != nil {
		t.Fatalf("ListBills: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expectations: %v", err)
	}
}

// NULL 的 is_default / enabled 不能让默认仓查询炸掉，停用仓不参与选择。
func TestDefaultWarehouseID_NullSafeAndEnabledOnly(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	tenantID, whID := uuid.New(), uuid.New()
	mock.ExpectQuery(`COALESCE\(is_default, false\).*COALESCE\(enabled, true\).*ORDER BY COALESCE\(is_default, false\) DESC`).
		WithArgs(tenantID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "is_default"}).AddRow(whID, false))

	id, ok, err := repobill.New(db).DefaultWarehouseID(context.Background(), tenantID)
	if err != nil || !ok || id != whID {
		t.Fatalf("got (%v,%v,%v), want (%v,true,nil)", id, ok, err, whID)
	}
}

// 回写 0 行必须报错，不能静默成功。
func TestSetBillWarehouse_ZeroRowsErrors(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE tally.bill_head SET warehouse_id`).WillReturnResult(sqlmock.NewResult(0, 0))
	tx, _ := db.Begin()
	err = repobill.New(db).SetBillWarehouse(context.Background(), tx, uuid.New(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("want error on 0 rows affected")
	}
}

// TestBillNoDay_UsesShanghaiCalendarDay: 00:30 Beijing time is still the previous day
// in UTC, but the bill number must carry the Beijing date.
func TestBillNoDay_UsesShanghaiCalendarDay(t *testing.T) {
	utc := time.Date(2026, 10, 9, 16, 30, 0, 0, time.UTC) // = 2026-10-10 00:30 CST
	if got := repobill.BillNoDay(utc); got != "20261010" {
		t.Errorf("BillNoDay = %s, want 20261010", got)
	}
}
