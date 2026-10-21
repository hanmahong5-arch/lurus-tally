//go:build integration

// Invoice registry real-SQL tests: insert with links, cumulative invoiced amount
// under LockBills, duplicate number (unique violation -> ErrDuplicateNo), status
// transitions, and the bill list's derived invoiced_amount / invoice_status filter.
//
//	go test -v -tags integration -timeout 180s ./tests/integration/ -run TestInvoiceRepo
package integration

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	billrepo "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/bill"
	invoicerepo "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/invoice"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	appinv "github.com/hanmahong5-arch/lurus-tally/internal/app/invoice"
	domainbill "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domaininv "github.com/hanmahong5-arch/lurus-tally/internal/domain/invoice"
)

func TestInvoiceRepo_LinksDuplicateAndBillListStatus(t *testing.T) {
	db, cleanup := sqlRealDB(t)
	defer cleanup()
	ctx := context.Background()
	repo := invoicerepo.New(db)

	tenantID := insertTenant(t, db, ctx)
	partner := insertPartner(t, db, ctx, tenantID, "Alpha Trading", "customer")
	bill := insertBillHead(t, db, ctx, tenantID, "出库", "销售", 2, &partner, time.Now())
	if _, err := db.ExecContext(ctx, `UPDATE tally.bill_head SET total_amount = 113 WHERE id = $1`, bill); err != nil {
		t.Fatal(err)
	}

	newInv := func(no string, amt string) *domaininv.Invoice {
		inv, err := domaininv.NewInvoice(domaininv.CreateInput{
			TenantID: tenantID, Direction: domaininv.DirectionOut, InvoiceNo: no, InvoiceType: "数电普票",
			PartnerID: partner, IssueDate: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			AmountExclTax: decimal.RequireFromString("100"), TaxAmount: decimal.RequireFromString("13"),
			Links: []domaininv.Link{{BillID: bill, Amount: decimal.RequireFromString(amt)}},
		}, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		return inv
	}
	insert := func(inv *domaininv.Invoice) error {
		return repo.WithTx(ctx, func(tx *sql.Tx) error { return repo.Insert(ctx, tx, inv) })
	}

	first := newInv("INT-1", "60")
	if err := insert(first); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := insert(newInv("INT-1", "10")); !errors.Is(err, appinv.ErrDuplicateNo) {
		t.Fatalf("duplicate number: %v", err)
	}

	invoicedOf := func() (string, decimal.Decimal) {
		items, _, err := billrepo.New(db).ListBills(ctx, appbill.BillListFilter{TenantID: tenantID})
		if err != nil || len(items) != 1 {
			t.Fatalf("ListBills: %v n=%d", err, len(items))
		}
		return items[0].InvoiceStatus(), items[0].InvoicedAmount
	}
	if st, amt := invoicedOf(); st != domainbill.InvoicePartial || !amt.Equal(decimal.RequireFromString("60")) {
		t.Fatalf("after 60: %s %s", st, amt)
	}
	count := func(status string) int {
		_, total, err := billrepo.New(db).ListBills(ctx, appbill.BillListFilter{TenantID: tenantID, InvoiceStatus: status})
		if err != nil {
			t.Fatal(err)
		}
		return int(total)
	}
	if count("partial") != 1 || count("none") != 0 || count("full") != 0 {
		t.Fatal("invoice_status filter mismatch after partial")
	}

	// LockBills sees the already-invoiced amount.
	err := repo.WithTx(ctx, func(tx *sql.Tx) error {
		refs, err := repo.LockBills(ctx, tx, tenantID, []uuid.UUID{bill})
		if err != nil {
			return err
		}
		if !refs[bill].Invoiced.Equal(decimal.RequireFromString("60")) {
			t.Errorf("invoiced = %s", refs[bill].Invoiced)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Voiding releases the amount; striking with a red-letter invoice does too.
	err = repo.WithTx(ctx, func(tx *sql.Tx) error {
		ok, err := repo.SetStatus(ctx, tx, tenantID, first.ID, domaininv.StatusIssued, domaininv.StatusVoid)
		if err == nil && !ok {
			t.Error("SetStatus reported no change")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := invoicedOf(); st != domainbill.InvoiceNone {
		t.Fatalf("after void: %s", st)
	}

	got, err := repo.GetByID(ctx, tenantID, first.ID)
	if err != nil || got.Status != domaininv.StatusVoid || len(got.Links) != 1 || got.Links[0].BillNo == "" || got.PartnerName != "Alpha Trading" {
		t.Fatalf("GetByID: %v %+v", err, got)
	}
	if _, err := repo.GetByID(ctx, uuid.New(), first.ID); !errors.Is(err, appinv.ErrNotFound) {
		t.Fatalf("cross-tenant get: %v", err)
	}
	items, total, err := repo.List(ctx, domaininv.ListFilter{TenantID: tenantID, Direction: domaininv.DirectionOut, Keyword: "INT", Limit: 10})
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("List: %v total=%d", err, total)
	}
}
