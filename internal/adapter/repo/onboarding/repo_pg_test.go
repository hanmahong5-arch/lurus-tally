package onboarding

// PG integration tests for the clear-demo path. Skipped when DATABASE_DSN is not
// set; they need the tally schema with migrations applied.

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type demoFixture struct {
	db        *sql.DB
	tenant    uuid.UUID
	warehouse uuid.UUID
	demoProd  uuid.UUID
}

func newDemoFixture(t *testing.T) *demoFixture {
	t.Helper()
	dsn := os.Getenv("DATABASE_DSN")
	if dsn == "" {
		t.Skip("DATABASE_DSN not set — skipping integration test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	f := &demoFixture{db: db, tenant: uuid.New(), warehouse: uuid.New(), demoProd: uuid.New()}
	t.Cleanup(func() {
		ctx := context.Background()
		for _, q := range []string{
			`DELETE FROM tally.stock_movement WHERE tenant_id=$1`,
			`DELETE FROM tally.stock_snapshot WHERE tenant_id=$1`,
			`UPDATE tally.bill_head SET link_bill_id=NULL, amendment_of_id=NULL WHERE tenant_id=$1`,
			`DELETE FROM tally.bill_head WHERE tenant_id=$1`,
			`DELETE FROM tally.product WHERE tenant_id=$1`,
			`DELETE FROM tally.warehouse WHERE tenant_id=$1`,
		} {
			_, _ = db.ExecContext(ctx, q, f.tenant)
		}
		_ = db.Close()
	})
	f.exec(t, `INSERT INTO tally.warehouse (id, tenant_id, name) VALUES ($1,$2,'demo-wh')`, f.warehouse, f.tenant)
	f.exec(t, `INSERT INTO tally.product (id, tenant_id, code, name, remark) VALUES ($1,$2,'DEMO-1','demo','DEMO')`, f.demoProd, f.tenant)
	f.exec(t, `INSERT INTO tally.stock_snapshot (tenant_id, product_id, warehouse_id, on_hand_qty, available_qty) VALUES ($1,$2,$3,10,10)`,
		f.tenant, f.demoProd, f.warehouse)
	f.exec(t, `INSERT INTO tally.stock_movement (tenant_id, product_id, warehouse_id, direction, qty_base, reference_type, reference_id) VALUES ($1,$2,$3,'in',10,'init',gen_random_uuid())`,
		f.tenant, f.demoProd, f.warehouse)
	return f
}

func (f *demoFixture) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, err := f.db.ExecContext(context.Background(), q, args...); err != nil {
		t.Fatalf("exec %.40s: %v", q, err)
	}
}

// bill inserts an approved bill with one line on the demo product.
func (f *demoFixture) bill(t *testing.T, no, remark string, link, amend *uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	f.exec(t, `INSERT INTO tally.bill_head (id, tenant_id, bill_no, bill_type, sub_type, status, creator_id, bill_date, remark, link_bill_id, amendment_of_id)
		VALUES ($1,$2,$3,'出库','销售',2,$2,$4,$5,$6,$7)`, id, f.tenant, no, time.Now(), remark, link, amend)
	f.exec(t, `INSERT INTO tally.bill_item (tenant_id, head_id, product_id, warehouse_id, qty, line_no) VALUES ($1,$2,$3,$4,1,1)`,
		f.tenant, id, f.demoProd, f.warehouse)
	return id
}

func (f *demoFixture) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(`SELECT count(*) FROM tally.`+table+` WHERE tenant_id=$1`, f.tenant).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Seed demo sale -> return it -> clear: product, stock and both bills are gone.
func TestDeleteDemoProductsPG_ReturnOfDemoSaleIsCleared(t *testing.T) {
	f := newDemoFixture(t)
	sale := f.bill(t, "DEMO-S-pg1", "DEMO", nil, nil)
	f.bill(t, "TH-pg1", "", &sale, nil)

	if err := New(f.db).DeleteDemoProducts(context.Background(), f.tenant); err != nil {
		t.Fatalf("clear: %v", err)
	}
	for _, tb := range []string{"product", "stock_snapshot", "stock_movement", "bill_head", "bill_item"} {
		if n := f.count(t, tb); n != 0 {
			t.Errorf("%s rows = %d, want 0", tb, n)
		}
	}
}

// A real sale on a demo product no longer blocks: the product is kept as a real
// one (marker dropped, stock kept) while the demo bill goes.
func TestClearDemoPG_RealSaleConvertsProduct(t *testing.T) {
	f := newDemoFixture(t)
	f.bill(t, "DEMO-S-pg2", "DEMO", nil, nil)
	f.bill(t, "XS-REAL-1", "", nil, nil)

	res, err := New(f.db).ClearDemo(context.Background(), f.tenant)
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if len(res.Converted) != 1 || res.Converted[0].ID != f.demoProd || res.Converted[0].Name != "demo" {
		t.Errorf("Converted = %+v, want the demo product", res.Converted)
	}
	if res.ClearedProducts != 0 {
		t.Errorf("ClearedProducts = %d, want 0", res.ClearedProducts)
	}
	for tb, want := range map[string]int{"product": 1, "stock_snapshot": 1, "stock_movement": 1, "bill_head": 1, "bill_item": 1} {
		if n := f.count(t, tb); n != want {
			t.Errorf("%s rows = %d, want %d", tb, n, want)
		}
	}
	if n := f.countWhere(t, "product", "remark = 'DEMO'"); n != 0 {
		t.Errorf("DEMO products left = %d, want 0", n)
	}
	// The product repo scans remark into a string, so it must not be NULL.
	if n := f.countWhere(t, "product", "remark IS NULL"); n != 0 {
		t.Errorf("converted products with NULL remark = %d, want 0", n)
	}
	// Idempotent: a second run has nothing to do.
	res, err = New(f.db).ClearDemo(context.Background(), f.tenant)
	if err != nil || len(res.Converted) != 0 || res.ClearedProducts != 0 {
		t.Errorf("second clear = %+v, %v; want empty", res, err)
	}
}

// The owner deleted the demo product (soft delete) before clearing, and an
// approved bill still points at it: the clear succeeds, the row stays soft-deleted
// as history and is not offered back as a converted product.
func TestClearDemoPG_DeleteThenClear(t *testing.T) {
	f := newDemoFixture(t)
	f.bill(t, "XS-REAL-2", "", nil, nil)
	f.exec(t, `UPDATE tally.product SET deleted_at=now() WHERE id=$1`, f.demoProd)

	res, err := New(f.db).ClearDemo(context.Background(), f.tenant)
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if len(res.Converted) != 0 {
		t.Errorf("Converted = %+v, want none for a deleted product", res.Converted)
	}
	if n := f.countWhere(t, "product", "remark = 'DEMO'"); n != 0 {
		t.Errorf("DEMO products left = %d, want 0", n)
	}
	if n := f.countWhere(t, "product", "deleted_at IS NOT NULL"); n != 1 {
		t.Errorf("soft-deleted products = %d, want 1 (kept as history)", n)
	}
	if n := f.count(t, "bill_head"); n != 1 {
		t.Errorf("bill_head rows = %d, want 1", n)
	}
}

// The owner stocktook the demo product (an adjust movement, no bill) and then
// cleared: nothing references it, so everything goes.
func TestClearDemoPG_StocktakeThenClear(t *testing.T) {
	f := newDemoFixture(t)
	f.exec(t, `UPDATE tally.stock_snapshot SET on_hand_qty=7, available_qty=7 WHERE product_id=$1`, f.demoProd)
	f.exec(t, `INSERT INTO tally.stock_movement (tenant_id, product_id, warehouse_id, direction, qty_base, reference_type, reference_id) VALUES ($1,$2,$3,'out',3,'adjust',gen_random_uuid())`,
		f.tenant, f.demoProd, f.warehouse)

	res, err := New(f.db).ClearDemo(context.Background(), f.tenant)
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if res.ClearedProducts != 1 || len(res.Converted) != 0 {
		t.Errorf("res = %+v, want 1 cleared and none converted", res)
	}
	for _, tb := range []string{"product", "stock_snapshot", "stock_movement"} {
		if n := f.count(t, tb); n != 0 {
			t.Errorf("%s rows = %d, want 0", tb, n)
		}
	}
}

// Only the demo product a real bill uses is converted; the other one is deleted.
func TestClearDemoPG_PartialClear(t *testing.T) {
	f := newDemoFixture(t)
	other := uuid.New()
	f.exec(t, `INSERT INTO tally.product (id, tenant_id, code, name, remark) VALUES ($1,$2,'DEMO-2','demo2','DEMO')`, other, f.tenant)
	f.exec(t, `INSERT INTO tally.stock_snapshot (tenant_id, product_id, warehouse_id, on_hand_qty, available_qty) VALUES ($1,$2,$3,5,5)`,
		f.tenant, other, f.warehouse)
	f.bill(t, "XS-REAL-3", "", nil, nil)

	res, err := New(f.db).ClearDemo(context.Background(), f.tenant)
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if res.ClearedProducts != 1 || len(res.Converted) != 1 || res.Converted[0].ID != f.demoProd {
		t.Errorf("res = %+v, want demo2 cleared and demo converted", res)
	}
	if n := f.count(t, "product"); n != 1 {
		t.Errorf("product rows = %d, want 1", n)
	}
	if n := f.count(t, "stock_snapshot"); n != 1 {
		t.Errorf("stock_snapshot rows = %d, want 1", n)
	}
}

func (f *demoFixture) countWhere(t *testing.T, table, where string) int {
	t.Helper()
	var n int
	if err := f.db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM tally.`+table+` WHERE tenant_id=$1 AND `+where, f.tenant).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// billWith inserts a bill of the given status holding one line per product.
func (f *demoFixture) billWith(t *testing.T, no string, status int, total, local, discount string, products ...uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	f.exec(t, `INSERT INTO tally.bill_head (id, tenant_id, bill_no, bill_type, sub_type, status, creator_id, bill_date, total_amount, amount_local, discount_amount)
		VALUES ($1,$2,$3,'入库','采购',$4,$2,$5,$6,$7,$8)`, id, f.tenant, no, status, time.Now(), total, local, discount)
	for i, p := range products {
		f.exec(t, `INSERT INTO tally.bill_item (tenant_id, head_id, product_id, warehouse_id, qty, line_no, line_amount) VALUES ($1,$2,$3,$4,1,$5,10)`,
			f.tenant, id, p, f.warehouse, i+1)
	}
	return id
}

func (f *demoFixture) realProduct(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.New()
	f.exec(t, `INSERT INTO tally.product (id, tenant_id, code, name) VALUES ($1,$2,$3,'real')`, id, f.tenant, "R-"+id.String()[:8])
	return id
}

// A voided pure-demo draft never moved stock and cannot be edited: it must not
// block the clear, and it is removed with the demo.
func TestDeleteDemoProductsPG_VoidedDemoDraftDoesNotBlock(t *testing.T) {
	f := newDemoFixture(t)
	f.billWith(t, "CG-VOID-1", 9, "10", "10", "0", f.demoProd)

	if err := New(f.db).DeleteDemoProducts(context.Background(), f.tenant); err != nil {
		t.Fatalf("clear: %v", err)
	}
	for _, tb := range []string{"product", "bill_head", "bill_item"} {
		if n := f.count(t, tb); n != 0 {
			t.Errorf("%s rows = %d, want 0", tb, n)
		}
	}
}

// A voided bill mixing demo and real lines also must not block; the demo line
// goes, the real line stays, and the head totals are recomputed.
func TestDeleteDemoProductsPG_VoidedMixedDoesNotBlock(t *testing.T) {
	f := newDemoFixture(t)
	real := f.realProduct(t)
	id := f.billWith(t, "CG-VOID-2", 9, "20", "20", "0", f.demoProd, real)

	if err := New(f.db).DeleteDemoProducts(context.Background(), f.tenant); err != nil {
		t.Fatalf("clear: %v", err)
	}
	var n int
	if err := f.db.QueryRow(`SELECT count(*) FROM tally.bill_item WHERE head_id=$1 AND product_id=$2`, id, real).Scan(&n); err != nil || n != 1 {
		t.Errorf("real line count = %d (err %v), want 1", n, err)
	}
}

// Removing a demo line from a mixed CNY draft recomputes subtotal, local amount
// and total, caps the discount at the new total, and renumbers the lines.
func TestDeleteDemoProductsPG_MixedDraftRecomputed(t *testing.T) {
	f := newDemoFixture(t)
	real := f.realProduct(t)
	id := f.billWith(t, "CG-MIX-1", 0, "20", "20", "15", f.demoProd, real)
	f.exec(t, `UPDATE tally.bill_head SET subtotal=20, exchange_rate=1 WHERE id=$1`, id)

	if err := New(f.db).DeleteDemoProducts(context.Background(), f.tenant); err != nil {
		t.Fatalf("clear: %v", err)
	}
	sub, local, total, disc, lineNo := f.amounts(t, id)
	if sub != "10.0000" || local != "10.0000" || total != "10.0000" {
		t.Errorf("subtotal/local/total = %s/%s/%s, want 10/10/10", sub, local, total)
	}
	if disc != "10.0000" {
		t.Errorf("discount = %s, want 10.0000 (capped at total)", disc)
	}
	if lineNo != 1 {
		t.Errorf("line_no = %d, want 1", lineNo)
	}
}

// A USD draft: lines 10 (demo) + 10 (own) and freight 2 -> amount_local 22,
// rate 7 -> total 154. Dropping the demo line leaves 10 / 12 / 84.
func TestDeleteDemoProductsPG_MixedForeignCurrencyDraft(t *testing.T) {
	f := newDemoFixture(t)
	real := f.realProduct(t)
	id := f.billWith(t, "CG-MIX-USD", 0, "154", "22", "0", f.demoProd, real)
	f.exec(t, `UPDATE tally.bill_head SET subtotal=20, exchange_rate=7 WHERE id=$1`, id)

	if err := New(f.db).DeleteDemoProducts(context.Background(), f.tenant); err != nil {
		t.Fatalf("clear: %v", err)
	}
	sub, local, total, _, _ := f.amounts(t, id)
	if sub != "10.0000" || local != "12.0000" || total != "84.0000" {
		t.Errorf("subtotal/local/total = %s/%s/%s, want 10/12/84", sub, local, total)
	}
}

// A draft the owner built without any demo line is left exactly as it was,
// including line_no values.
func TestDeleteDemoProductsPG_UntouchedDraftKept(t *testing.T) {
	f := newDemoFixture(t)
	real := f.realProduct(t)
	id := f.billWith(t, "CG-OWN-1", 0, "10", "10", "0", real)
	f.exec(t, `UPDATE tally.bill_item SET line_no=0 WHERE head_id=$1`, id)

	if err := New(f.db).DeleteDemoProducts(context.Background(), f.tenant); err != nil {
		t.Fatalf("clear: %v", err)
	}
	_, _, total, _, lineNo := f.amounts(t, id)
	if total != "10.0000" || lineNo != 0 {
		t.Errorf("total=%s line_no=%d, want 10.0000 / 0", total, lineNo)
	}
}

func (f *demoFixture) amounts(t *testing.T, id uuid.UUID) (sub, local, total, disc string, lineNo int) {
	t.Helper()
	if err := f.db.QueryRow(`SELECT subtotal::text, amount_local::text, total_amount::text, COALESCE(discount_amount,0)::text FROM tally.bill_head WHERE id=$1`, id).
		Scan(&sub, &local, &total, &disc); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`SELECT line_no FROM tally.bill_item WHERE head_id=$1 ORDER BY line_no LIMIT 1`, id).Scan(&lineNo); err != nil {
		t.Fatal(err)
	}
	return
}

// A product the owner already soft-deleted is still demo: stock and the row go.
func TestDeleteDemoProductsPG_SoftDeletedDemoProductCleared(t *testing.T) {
	f := newDemoFixture(t)
	f.exec(t, `UPDATE tally.product SET deleted_at=now() WHERE id=$1`, f.demoProd)

	if err := New(f.db).DeleteDemoProducts(context.Background(), f.tenant); err != nil {
		t.Fatalf("clear: %v", err)
	}
	for _, tb := range []string{"product", "stock_snapshot", "stock_movement"} {
		if n := f.count(t, tb); n != 0 {
			t.Errorf("%s rows = %d, want 0", tb, n)
		}
	}
}

// A kept sample product drops its DEMO- code prefix when the bare code is free,
// and carries the converted marker so the checklist does not count it as the
// owner's own product.
func TestClearDemoPG_ConvertedCodeLosesPrefix(t *testing.T) {
	f := newDemoFixture(t)
	f.bill(t, "XS-REAL-4", "", nil, nil)

	res, err := New(f.db).ClearDemo(context.Background(), f.tenant)
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if len(res.Converted) != 1 || res.Converted[0].Code != "1" {
		t.Fatalf("Converted = %+v, want code 1", res.Converted)
	}
	if n := f.countWhere(t, "product", "code LIKE 'DEMO-%'"); n != 0 {
		t.Errorf("DEMO- codes left = %d, want 0", n)
	}
	if n := f.countWhere(t, "product", "remark = '"+ConvertedRemark+"'"); n != 1 {
		t.Errorf("marked products = %d, want 1", n)
	}
}

// A live product already owns the bare code: the converted one keeps DEMO-.
func TestClearDemoPG_ConvertedCodeConflictKeepsPrefix(t *testing.T) {
	f := newDemoFixture(t)
	f.exec(t, `INSERT INTO tally.product (id, tenant_id, code, name, remark) VALUES ($1,$2,'1','mine','')`, uuid.New(), f.tenant)
	f.bill(t, "XS-REAL-5", "", nil, nil)

	res, err := New(f.db).ClearDemo(context.Background(), f.tenant)
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if len(res.Converted) != 1 || res.Converted[0].Code != "DEMO-1" {
		t.Fatalf("Converted = %+v, want code DEMO-1", res.Converted)
	}
}
