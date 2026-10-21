package bill

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	apppayment "github.com/hanmahong5-arch/lurus-tally/internal/app/payment"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

func TestUnpaidExpr_FlooredAndRoundedPerBill(t *testing.T) {
	sale := unpaidExpr(appbill.BillListFilter{BillType: domain.BillTypeSale})
	if !strings.Contains(sale, "ROUND(GREATEST(") || !strings.Contains(sale, "r.amt") {
		t.Errorf("sale expr = %q, want floored net of returns", sale)
	}
	purchase := unpaidExpr(appbill.BillListFilter{BillType: domain.BillTypePurchase})
	if strings.Contains(purchase, "r.amt") || !strings.Contains(purchase, "GREATEST(b.total_amount - b.paid_amount, 0)") {
		t.Errorf("purchase expr = %q", purchase)
	}
}

func TestLedgerQueries_UseAgeingBalanceRule(t *testing.T) {
	ar := openBillsQuery(apppayment.DirectionReceivable)
	if !strings.Contains(ar, "GREATEST(b.total_amount - COALESCE(r.amt, 0), 0) - b.paid_amount") || !strings.Contains(ar, "ORDER BY bill_date ASC") {
		t.Errorf("ar open bills query does not follow the ageing rule:\n%s", ar)
	}
	ap := openBillsQuery(apppayment.DirectionPayable)
	if strings.Contains(ap, "rsum") {
		t.Errorf("ap open bills query must not net sales returns:\n%s", ap)
	}
	if !strings.Contains(balanceAtQuery(apppayment.DirectionReceivable), "$6::boolean") {
		t.Error("balance-at query must switch to paid_amount for a future cut-off")
	}
}

func TestStatement_AssemblesRunningBalanceAndPurchaseReturnOffset(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	tenant, partner := uuid.New(), uuid.New()
	billID := uuid.New()
	d := func(day int) time.Time { return time.Date(2026, 10, day, 0, 0, 0, 0, time.UTC) }

	mock.ExpectQuery("FROM tally.partner").WillReturnRows(sqlmock.NewRows([]string{"name"}).AddRow("老供应商"))
	mock.ExpectQuery("pays AS").WillReturnRows(sqlmock.NewRows([]string{"s"}).AddRow("100.00")) // opening
	mock.ExpectQuery("pays AS").WillReturnRows(sqlmock.NewRows([]string{"s"}).AddRow("230.00")) // closing
	cols := []string{"d", "id", "no", "m", "r", "amt"}
	mock.ExpectQuery("FROM tally.bill_head").WillReturnRows(sqlmock.NewRows(cols).AddRow(d(3), billID, "PO-1", nil, "", "200.00"))
	mock.ExpectQuery("FROM tally.payment_head").WillReturnRows(sqlmock.NewRows(cols).
		AddRow(d(9), billID, "PO-1", "cash", "", "50.00").
		AddRow(d(5), billID, "PO-1", "credit", "采购退货冲抵", "20.00"))

	st, err := New(db).Statement(context.Background(), apppayment.StatementQuery{
		TenantID: tenant, Direction: apppayment.DirectionPayable, PartnerID: partner, From: d(1), To: d(31)})
	if err != nil {
		t.Fatalf("Statement: %v", err)
	}
	want := []struct {
		kind, bal string
	}{{apppayment.LineBill, "300"}, {apppayment.LineReturn, "280"}, {apppayment.LinePayment, "230"}}
	if len(st.Lines) != len(want) {
		t.Fatalf("lines = %d, want %d", len(st.Lines), len(want))
	}
	for i, w := range want {
		if st.Lines[i].Kind != w.kind || !st.Lines[i].Balance.Equal(decimal.RequireFromString(w.bal)) {
			t.Errorf("line %d = %s %s, want %s %s", i, st.Lines[i].Kind, st.Lines[i].Balance, w.kind, w.bal)
		}
	}
	if st.Lines[1].Method != "" {
		t.Errorf("offset keeps method %q", st.Lines[1].Method)
	}
	for name, got := range map[string]decimal.Decimal{"billed": st.Billed, "returned": st.Returned, "paid": st.Paid} {
		wantV := map[string]string{"billed": "200", "returned": "20", "paid": "50"}[name]
		if !got.Equal(decimal.RequireFromString(wantV)) {
			t.Errorf("%s = %s, want %s", name, got, wantV)
		}
	}
	if !st.Opening.Equal(decimal.NewFromInt(100)) || !st.Closing.Equal(decimal.NewFromInt(230)) || !st.Adjustment.IsZero() {
		t.Errorf("opening %s closing %s adjustment %s", st.Opening, st.Closing, st.Adjustment)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 赊账方式的付款不一定是退货冲抵：只有备注以「采购退货冲抵」开头的才算退货。
func TestStatement_CreditPaymentWithoutOffsetRemarkStaysPayment(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	tenant, partner, billID := uuid.New(), uuid.New(), uuid.New()
	d := func(day int) time.Time { return time.Date(2026, 10, day, 0, 0, 0, 0, time.UTC) }

	mock.ExpectQuery("FROM tally.partner").WillReturnRows(sqlmock.NewRows([]string{"name"}).AddRow("老供应商"))
	mock.ExpectQuery("pays AS").WillReturnRows(sqlmock.NewRows([]string{"s"}).AddRow("0.00"))
	mock.ExpectQuery("pays AS").WillReturnRows(sqlmock.NewRows([]string{"s"}).AddRow("0.00"))
	cols := []string{"d", "id", "no", "m", "r", "amt"}
	mock.ExpectQuery("FROM tally.bill_head").WillReturnRows(sqlmock.NewRows(cols))
	mock.ExpectQuery("FROM tally.payment_head").WillReturnRows(sqlmock.NewRows(cols).
		AddRow(d(5), billID, "PO-1", "credit", "", "20.00"))

	st, err := New(db).Statement(context.Background(), apppayment.StatementQuery{
		TenantID: tenant, Direction: apppayment.DirectionPayable, PartnerID: partner, From: d(1), To: d(31)})
	if err != nil {
		t.Fatalf("Statement: %v", err)
	}
	if len(st.Lines) != 1 || st.Lines[0].Kind != apppayment.LinePayment {
		t.Fatalf("lines = %+v, want one payment", st.Lines)
	}
}
