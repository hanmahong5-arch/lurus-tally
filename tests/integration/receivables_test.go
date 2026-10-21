//go:build integration

// Package integration — receivables aging and list/summary parity against real Postgres.
//
// Locks the aging bucket edges (30/31, 60/61, 90/91 days and the Beijing midnight cut),
// which bills count (approved, unpaid), sales-return netting (link, as-of cut, malformed
// cancellation remark), and that the aging total equals the list summary the payments
// page shows next to it.
//
// Run: go test -tags integration -run TestReceivables ./tests/integration/ -timeout 360s -v
// Set TALLY_TEST_DSN to reuse an already-migrated database instead of starting a container;
// the test only writes under a fresh tenant and removes it afterwards.
package integration

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	repobill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/bill"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

var cst = time.FixedZone("CST", 8*3600)

type recvBill struct {
	billType, subType string
	status            int
	partner           *uuid.UUID
	date              time.Time
	total, paid       string
	link              *uuid.UUID
	remark            string
}

func recvDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	dsn := os.Getenv("TALLY_TEST_DSN")
	if dsn == "" {
		return sqlRealDB(t)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	return db, func() { db.Close() } //nolint:errcheck
}

func insertRecvBill(t *testing.T, db *sql.DB, tenant uuid.UUID, b recvBill) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO tally.bill_head
		    (id, tenant_id, bill_no, bill_type, sub_type, status, partner_id, creator_id, bill_date,
		     total_amount, paid_amount, link_bill_id, remark)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		id, tenant, "R-"+id.String()[:8], b.billType, b.subType, b.status, b.partner, uuid.New(), b.date,
		b.total, b.paid, b.link, b.remark)
	if err != nil {
		t.Fatalf("insert bill: %v", err)
	}
	return id
}

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestReceivables_AgingNettingAndListParity(t *testing.T) {
	db, cleanup := recvDB(t)
	defer cleanup()
	ctx := context.Background()
	tenant := insertTenant(t, db, ctx)
	defer func() {
		for _, q := range []string{
			`DELETE FROM tally.bill_head WHERE tenant_id = $1`,
			`DELETE FROM tally.partner WHERE tenant_id = $1`,
			`DELETE FROM tally.tenant WHERE id = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, tenant)
		}
	}()
	repo := repobill.New(db)

	asOf := time.Date(2026, 10, 10, 0, 0, 0, 0, cst)
	day := func(m time.Month, d, h, mi, s int) time.Time { return time.Date(2026, m, d, h, mi, s, 0, cst) }
	sale := func(p *uuid.UUID, date time.Time, total, paid string) recvBill {
		return recvBill{billType: "出库", subType: "销售", status: 2, partner: p, date: date, total: total, paid: paid}
	}

	a := insertPartner(t, db, ctx, tenant, "客户甲", "customer")
	b := insertPartner(t, db, ctx, tenant, "客户乙", "customer")
	c := insertPartner(t, db, ctx, tenant, "客户丙", "customer")
	d := insertPartner(t, db, ctx, tenant, "客户丁", "customer")

	// Customer A: one bill on each side of every bucket edge.
	insertRecvBill(t, db, tenant, sale(&a, day(9, 10, 0, 0, 0), "100", "0"))   // age 30
	insertRecvBill(t, db, tenant, sale(&a, day(9, 9, 23, 59, 59), "200", "0")) // age 31 (before CST midnight)
	insertRecvBill(t, db, tenant, sale(&a, day(8, 11, 12, 0, 0), "300", "0"))  // age 60
	insertRecvBill(t, db, tenant, sale(&a, day(8, 10, 12, 0, 0), "400", "0"))  // age 61
	insertRecvBill(t, db, tenant, sale(&a, day(7, 12, 12, 0, 0), "500", "0"))  // age 90
	insertRecvBill(t, db, tenant, sale(&a, day(7, 11, 12, 0, 0), "600", "0"))  // age 91
	// Not receivable: draft, cancelled, settled, and one after the as-of day.
	for _, st := range []int{0, 3} {
		x := sale(&a, day(9, 20, 12, 0, 0), "999", "0")
		x.status = st
		insertRecvBill(t, db, tenant, x)
	}
	insertRecvBill(t, db, tenant, sale(&a, day(9, 20, 12, 0, 0), "50", "50"))
	insertRecvBill(t, db, tenant, sale(&a, day(10, 11, 0, 0, 0), "77", "0"))

	// Customer B: sale 1000 with a linked return 300; sale 400 paid 300 with return 200 (settled).
	s1 := insertRecvBill(t, db, tenant, sale(&b, day(9, 20, 12, 0, 0), "1000", "0"))
	s2 := insertRecvBill(t, db, tenant, sale(&b, day(9, 20, 12, 0, 0), "400", "300"))
	ret := func(p *uuid.UUID, date time.Time, total string, link *uuid.UUID, remark string) {
		insertRecvBill(t, db, tenant, recvBill{billType: "入库", subType: "销售退货", status: 2, partner: p, date: date, total: total, paid: "0", link: link, remark: remark})
	}
	ret(&b, day(9, 25, 12, 0, 0), "300", &s1, "")
	ret(&b, day(9, 25, 12, 0, 0), "200", &s2, "")

	// Customer C: the return happens after the as-of day, so it must not reduce the balance.
	s3 := insertRecvBill(t, db, tenant, sale(&c, day(9, 20, 12, 0, 0), "500", "0"))
	ret(&c, day(10, 11, 12, 0, 0), "500", &s3, "")

	// Customer D: a webhook-style cancellation remark with a malformed tail must not break the query.
	insertRecvBill(t, db, tenant, sale(&d, day(9, 20, 12, 0, 0), "60", "0"))
	ret(&d, day(9, 21, 12, 0, 0), "10", nil, "cancel:x:original_bill_id=------------------------------------")

	out, err := repo.PartnerReceivables(ctx, appbill.ReceivablesQuery{TenantID: tenant, Direction: appbill.ReceivableAR, AsOf: asOf})
	if err != nil {
		t.Fatalf("PartnerReceivables: %v", err)
	}
	byName := map[string]appbill.PartnerReceivable{}
	for _, r := range out {
		byName[r.PartnerName] = r
	}
	ra := byName["客户甲"]
	for name, got := range map[string]decimal.Decimal{"unpaid": ra.Unpaid, "b0_30": ra.B0to30, "b31_60": ra.B31to60, "b61_90": ra.B61to90, "b90p": ra.B90Plus} {
		want := map[string]string{"unpaid": "2100", "b0_30": "100", "b31_60": "500", "b61_90": "900", "b90p": "600"}[name]
		if !got.Equal(dec(want)) {
			t.Errorf("A %s = %s, want %s", name, got, want)
		}
	}
	if got := byName["客户乙"].Unpaid; !got.Equal(dec("700")) {
		t.Errorf("B unpaid = %s, want 700 (1000 - 300 returned; settled sale drops out)", got)
	}
	if got := byName["客户丙"].Unpaid; !got.Equal(dec("500")) {
		t.Errorf("C unpaid = %s, want 500 (return is after the as-of day)", got)
	}
	if got := byName["客户丁"].Unpaid; !got.Equal(dec("60")) {
		t.Errorf("D unpaid = %s, want 60", got)
	}

	// Parity: with every bill in the past, the aging total equals the sale list's open summary.
	later := time.Date(2026, 12, 31, 0, 0, 0, 0, cst)
	all, err := repo.PartnerReceivables(ctx, appbill.ReceivablesQuery{TenantID: tenant, Direction: appbill.ReceivableAR, AsOf: later})
	if err != nil {
		t.Fatalf("PartnerReceivables later: %v", err)
	}
	sum := decimal.Zero
	for _, r := range all {
		sum = sum.Add(r.Unpaid)
	}
	f := appbill.BillListFilter{TenantID: tenant, BillType: domain.BillTypeSale, PaymentStatus: "open", Page: 1, Size: 100}
	s, err := repo.SummarizeBills(ctx, f)
	if err != nil {
		t.Fatalf("SummarizeBills: %v", err)
	}
	if !s.UnpaidAmount.Equal(sum) {
		t.Errorf("sale list unpaid %s != aging total %s", s.UnpaidAmount, sum)
	}
	bills, _, err := repo.ListBills(ctx, f)
	if err != nil {
		t.Fatalf("ListBills: %v", err)
	}
	for _, h := range bills {
		if h.TotalAmount.Equal(dec("400")) && h.PaidAmount.Equal(dec("300")) {
			t.Errorf("settled-after-return sale must not be in the open list")
		}
		if h.TotalAmount.Equal(dec("1000")) && !h.ReturnedAmount.Equal(dec("300")) {
			t.Errorf("returned = %s, want 300", h.ReturnedAmount)
		}
	}

	// AP: a sales return is an inbound bill but not a payable.
	sup := insertPartner(t, db, ctx, tenant, "供应商一", "supplier")
	insertRecvBill(t, db, tenant, recvBill{billType: "入库", subType: "采购", status: 2, partner: &sup, date: day(9, 1, 12, 0, 0), total: "230", paid: "0"})
	ap, err := repo.PartnerReceivables(ctx, appbill.ReceivablesQuery{TenantID: tenant, Direction: appbill.ReceivableAP, AsOf: later})
	if err != nil || len(ap) != 1 || !ap[0].Unpaid.Equal(dec("230")) {
		t.Fatalf("AP = %+v err=%v", ap, err)
	}
	pf := appbill.BillListFilter{TenantID: tenant, BillType: domain.BillTypePurchase, SubType: domain.BillSubTypePurchase, PaymentStatus: "open", Page: 1, Size: 100}
	ps, err := repo.SummarizeBills(ctx, pf)
	if err != nil || !ps.UnpaidAmount.Equal(dec("230")) || ps.Count != 1 {
		t.Fatalf("purchase summary = %+v err=%v", ps, err)
	}
}
