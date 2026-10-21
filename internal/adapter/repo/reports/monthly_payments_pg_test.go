package reports

// PG integration test: the payments CSV filtered to counted rows adds up to the monthly
// receipts and payments. Skipped when DATABASE_DSN is not set; needs the tally schema.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"

	appexport "github.com/hanmahong5-arch/lurus-tally/internal/app/export"
)

func TestMonthlyPayments_AddUpToCountedCSV(t *testing.T) {
	dsn := os.Getenv("DATABASE_DSN")
	if dsn == "" {
		t.Skip("DATABASE_DSN not set — skipping integration test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	tenant := uuid.New()
	ctx := context.Background()
	t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM tally.payment_head WHERE tenant_id=$1`,
			`DELETE FROM tally.bill_head WHERE tenant_id=$1`,
		} {
			_, _ = db.ExecContext(ctx, q, tenant)
		}
		_ = db.Close()
	})
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("exec %.40s: %v", q, err)
		}
	}
	day := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	bill := func(no, billType, sub string) uuid.UUID {
		id := uuid.New()
		exec(`INSERT INTO tally.bill_head (id, tenant_id, bill_no, bill_type, sub_type, status, creator_id, bill_date, total_amount)
			VALUES ($1,$2,$3,$4,$5,2,$2,$6,100)`, id, tenant, no, billType, sub, day)
		return id
	}
	pay := func(billID uuid.UUID, payType, amount string) {
		exec(`INSERT INTO tally.payment_head (tenant_id, pay_type, creator_id, pay_date, amount, total_amount, related_bill_id)
			VALUES ($1,$2,$1,$3,$4,$4,$5)`, tenant, payType, day, amount, billID)
	}
	so, sr := bill("SO-1", "出库", "销售"), bill("SR-1", "入库", "销售退货")
	po, pr := bill("PO-1", "入库", "采购"), bill("PR-1", "出库", "采购退货")
	pay(so, "cash", "30")
	pay(so, "wechat", "20")
	pay(so, "cash", "-5") // refund booked on the original sale
	pay(so, "credit", "50")
	pay(sr, "cash", "4") // handed back on the sales return: receipts go down
	pay(po, "cash", "40")
	pay(po, "credit", "10")
	pay(pr, "cash", "5") // cash back from the supplier on the purchase return: payments go down
	// Legacy sub-cent amounts: the card and the CSV both round each payment to the cent.
	pay(so, "cash", "41.6250")
	pay(so, "cash", "0.0050")
	pay(so, "cash", "0.0050")
	pay(so, "cash", "0.0050")
	pay(so, "cash", "9.9999")
	pay(po, "cash", "0.1235")

	from, to := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	fig, err := New(db).MonthlyFigures(ctx, tenant, from, to)
	if err != nil {
		t.Fatalf("MonthlyFigures: %v", err)
	}

	sum := func(direction string) decimal.Decimal {
		var buf bytes.Buffer
		_, err := appexport.NewPaymentsExportUseCase(db, nil).ExecuteFiltered(ctx, tenant,
			appexport.PaymentsFilter{From: &from, To: &to, Direction: direction, CountedOnly: true}, &buf)
		if err != nil {
			t.Fatalf("export %s: %v", direction, err)
		}
		recs, err := csv.NewReader(&buf).ReadAll()
		if err != nil {
			t.Fatalf("csv: %v", err)
		}
		total := decimal.Zero
		for _, r := range recs[1:] {
			if r[6] != "是" {
				t.Errorf("row %v is not counted but was exported with counted_only", r)
			}
			total = total.Add(decimal.RequireFromString(r[2]))
		}
		return total
	}
	if got := sum("in"); !got.Equal(fig.Receipts) || !fig.Receipts.Equal(decimal.RequireFromString("92.66")) {
		t.Errorf("in rows = %s, receipts = %s, want both 92.66", got, fig.Receipts)
	}
	if got := sum("out"); !got.Equal(fig.Payments) || !fig.Payments.Equal(decimal.RequireFromString("35.12")) {
		t.Errorf("out rows = %s, payments = %s, want both 35.12", got, fig.Payments)
	}
}
