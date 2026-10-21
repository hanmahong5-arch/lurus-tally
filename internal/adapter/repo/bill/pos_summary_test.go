package bill

import (
	"context"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
)

func TestPOSSummaryQueries(t *testing.T) {
	for name, q := range map[string]string{"totals": posSalesTotalsQuery, "pay": posPayTypeQuery, "returns": posReturnsQuery, "refunds": posRefundCountQuery, "credit": posCreditQuery} {
		if !strings.Contains(q, "status = 2") {
			t.Errorf("%s: must count approved bills only", name)
		}
		if !strings.Contains(q, ">= $2") || !strings.Contains(q, "< $3") {
			t.Errorf("%s: must use a half-open day window", name)
		}
		if !strings.Contains(q, "$4::uuid IS NULL OR") || !strings.Contains(q, "creator_id = $4") {
			t.Errorf("%s: must narrow to one person when $4 is set", name)
		}
		if !strings.Contains(q, "deleted_at IS NULL") {
			t.Errorf("%s: must skip deleted rows", name)
		}
	}
	if !strings.Contains(posCreditQuery, "GREATEST(b.total_amount - COALESCE(r.amt, 0), 0)") {
		t.Error("credit is net of returns")
	}
	if !strings.Contains(posPayTypeQuery, "GROUP BY p.pay_type") {
		t.Error("payments group by pay_type")
	}
	if !strings.Contains(posReturnsQuery, "'销售退货'") {
		t.Error("returns are 销售退货 bills")
	}
}

// 收款按钱动的那天（pay_date）统计：隔天的退款（负数收款）计入退款当天，不倒改昨天。
func TestPOSSummaryPayTypeCountsByPayDate(t *testing.T) {
	if !strings.Contains(posPayTypeQuery, "p.pay_date >= $2") || !strings.Contains(posPayTypeQuery, "p.pay_date < $3") {
		t.Error("payments must be windowed on p.pay_date")
	}
	if strings.Contains(posPayTypeQuery, "bill_date") {
		t.Error("payments must not be windowed on the bill date")
	}
}

// 现金笔数只数 amount>0 的行：隔天退款（负数收款）不让笔数多一笔。
func TestPOSSummaryPayTypeCountIgnoresRefundRows(t *testing.T) {
	if !strings.Contains(posPayTypeQuery, "FILTER (WHERE p.amount > 0)") {
		t.Error("pay type count must only take amount > 0 rows")
	}
}

func TestPOSSummary_ScansThreeQueriesAndParsesDecimals(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	tenant := uuid.New()
	from := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 1)
	mock.ExpectQuery(`COUNT\(\*\), COALESCE\(SUM\(total_amount\)`).WithArgs(tenant, from, to, nil).
		WillReturnRows(sqlmock.NewRows([]string{"c", "t", "p"}).AddRow(3, "300.50", "250.25"))
	mock.ExpectQuery(`GREATEST\(GREATEST`).WithArgs(tenant, from, to, nil).
		WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow("50.25"))
	mock.ExpectQuery(`GROUP BY p\.pay_type`).WithArgs(tenant, from, to, nil).
		WillReturnRows(sqlmock.NewRows([]string{"pay_type", "c", "s"}).AddRow("cash", 2, "200.00").AddRow("wechat", 1, "50.25"))
	mock.ExpectQuery(`p\.amount < 0`).WithArgs(tenant, from, to, nil).
		WillReturnRows(sqlmock.NewRows([]string{"c", "a"}).AddRow(2, "12.50"))
	mock.ExpectQuery(`销售退货`).WithArgs(tenant, from, to, nil).
		WillReturnRows(sqlmock.NewRows([]string{"c", "t"}).AddRow(1, "10.10"))

	got, err := New(db).POSSummary(context.Background(), appbill.POSSummaryRange{TenantID: tenant, From: from, To: to})
	if err != nil {
		t.Fatalf("POSSummary: %v", err)
	}
	if got.Count != 3 || got.TotalAmount.String() != "300.5" || got.PaidAmount.String() != "250.25" {
		t.Fatalf("totals = %+v", got)
	}
	if got.CreditAmount.String() != "50.25" {
		t.Fatalf("credit = %s", got.CreditAmount)
	}
	if len(got.ByPayType) != 2 || got.ByPayType[0].PayType != "cash" || got.ByPayType[0].Count != 2 || got.ByPayType[1].Amount.String() != "50.25" {
		t.Fatalf("by pay type = %+v", got.ByPayType)
	}
	if got.RefundCount != 2 || got.RefundAmount.String() != "12.5" {
		t.Fatalf("refunds = %d / %s, want 2 / 12.5", got.RefundCount, got.RefundAmount)
	}
	if got.ReturnCount != 1 || got.ReturnAmount.String() != "10.1" {
		t.Fatalf("returns = %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 按人结班：第 4 个参数带上本人，五条查询都收窄；不带时是 NULL（全店）。
func TestPOSSummary_MineBindsCreatorOnEveryQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	tenant, me := uuid.New(), uuid.New()
	from := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 1)
	mock.ExpectQuery(`COUNT\(\*\), COALESCE\(SUM\(total_amount\)`).WithArgs(tenant, from, to, me).
		WillReturnRows(sqlmock.NewRows([]string{"c", "t", "p"}).AddRow(1, "10", "10"))
	mock.ExpectQuery(`GREATEST\(GREATEST`).WithArgs(tenant, from, to, me).
		WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow("0"))
	mock.ExpectQuery(`GROUP BY p\.pay_type`).WithArgs(tenant, from, to, me).
		WillReturnRows(sqlmock.NewRows([]string{"pay_type", "c", "s"}))
	mock.ExpectQuery(`p\.amount < 0`).WithArgs(tenant, from, to, me).
		WillReturnRows(sqlmock.NewRows([]string{"c", "a"}).AddRow(0, "0"))
	mock.ExpectQuery(`销售退货`).WithArgs(tenant, from, to, me).
		WillReturnRows(sqlmock.NewRows([]string{"c", "t"}).AddRow(0, "0"))

	got, err := New(db).POSSummary(context.Background(), appbill.POSSummaryRange{TenantID: tenant, From: from, To: to, CreatorID: &me})
	if err != nil {
		t.Fatalf("POSSummary: %v", err)
	}
	if got.Count != 1 {
		t.Fatalf("count = %d", got.Count)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestPOSCashierQueries(t *testing.T) {
	if !strings.Contains(posCashierSalesQuery, "GROUP BY creator_id") || !strings.Contains(posCashierRefundsQuery, "GROUP BY p.creator_id") {
		t.Error("cashier split must group by creator")
	}
	for name, q := range map[string]string{"sales": posCashierSalesQuery, "refunds": posCashierRefundsQuery} {
		if !strings.Contains(q, "status = 2") || !strings.Contains(q, "deleted_at IS NULL") || !strings.Contains(q, "< $3") {
			t.Errorf("%s: approved, undeleted, half-open window only", name)
		}
	}
	if !strings.Contains(posCashierRefundsQuery, "p.pay_date >= $2") || !strings.Contains(posCashierRefundsQuery, "p.amount < 0") {
		t.Error("refunds are negative payments windowed on pay_date")
	}
}

func TestPOSCashiers_MergesSalesAndRefundsPerPerson(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	tenant, a, b := uuid.New(), uuid.New(), uuid.New()
	from := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 1)
	mock.ExpectQuery(`GROUP BY creator_id`).WithArgs(tenant, from, to).
		WillReturnRows(sqlmock.NewRows([]string{"c", "n", "t", "p"}).AddRow(a, 2, "300.50", "250.25").AddRow(nil, 1, "10", "10"))
	mock.ExpectQuery(`GROUP BY p\.creator_id`).WithArgs(tenant, from, to).
		WillReturnRows(sqlmock.NewRows([]string{"c", "a"}).AddRow(a, "5.00").AddRow(b, "7.50"))

	got, err := New(db).POSCashiers(context.Background(), appbill.POSSummaryRange{TenantID: tenant, From: from, To: to})
	if err != nil {
		t.Fatalf("POSCashiers: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("rows = %+v", got)
	}
	by := map[uuid.UUID]appbill.POSCashierTotal{}
	for _, r := range got {
		by[r.CreatorID] = r
	}
	if r := by[a]; r.Count != 2 || r.TotalAmount.String() != "300.5" || r.PaidAmount.String() != "250.25" || r.RefundAmount.String() != "5" {
		t.Errorf("a = %+v", r)
	}
	if r := by[b]; r.Count != 0 || r.RefundAmount.String() != "7.5" {
		t.Errorf("refund-only person = %+v", r)
	}
	if r := by[uuid.Nil]; r.Count != 1 {
		t.Errorf("no-creator row = %+v", r)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
