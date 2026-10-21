//go:build integration

// Package integration — month-end report cost of goods is cut by the owning bill's date.
//
// Run: go test -tags integration -run TestMonthlyCost ./tests/integration/ -timeout 360s -v
// Set TALLY_TEST_DSN to reuse an already-migrated database.
package integration

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	repreports "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/reports"
)

func TestMonthlyCost_FollowsBillDateNotApprovalTime(t *testing.T) {
	db, cleanup := recvDB(t)
	defer cleanup()
	ctx := context.Background()
	tenant := insertTenant(t, db, ctx)
	defer func() {
		for _, q := range []string{
			`DELETE FROM tally.stock_movement WHERE tenant_id = $1`,
			`DELETE FROM tally.bill_item WHERE tenant_id = $1`,
			`DELETE FROM tally.bill_head WHERE tenant_id = $1`,
			`DELETE FROM tally.product WHERE tenant_id = $1`,
			`DELETE FROM tally.warehouse WHERE tenant_id = $1`,
			`DELETE FROM tally.tenant WHERE id = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, tenant)
		}
	}()
	wh := insertWarehouse(t, db, ctx, tenant)
	prod := insertProduct(t, db, ctx, tenant, "成本测试品", "COST-1")

	sep := time.Date(2026, 9, 20, 12, 0, 0, 0, cst)
	oct := time.Date(2026, 10, 5, 12, 0, 0, 0, cst)
	sale := func(date time.Time, status int) uuid.UUID {
		return insertRecvBill(t, db, tenant, recvBill{billType: "出库", subType: "销售", status: status, date: date, total: "100", paid: "0"})
	}
	move := func(ref uuid.UUID, typ, dir, cost string, at time.Time) {
		t.Helper()
		_, err := db.ExecContext(ctx, `
			INSERT INTO tally.stock_movement
			    (tenant_id, product_id, warehouse_id, direction, qty_base, unit_cost, total_cost, reference_type, reference_id, occurred_at)
			VALUES ($1,$2,$3,$4,1,$5,$5,$6,$7,$8)`, tenant, prod, wh, dir, cost, typ, ref, at)
		if err != nil {
			t.Fatalf("insert movement: %v", err)
		}
	}

	// Dated in September, approved only in October: the cost belongs to September.
	late := sale(sep, 2)
	move(late, "sale", "out", "40", oct)
	// A movement pointing at one of the bill's lines resolves to the same bill.
	item := uuid.New()
	if _, err := db.ExecContext(ctx, `INSERT INTO tally.bill_item (id, tenant_id, head_id, product_id, qty) VALUES ($1,$2,$3,$4,1)`,
		item, tenant, late, prod); err != nil {
		t.Fatalf("insert item: %v", err)
	}
	move(item, "sale", "out", "5", oct)
	// October bill, cancelled bill, deleted bill and an orphan movement count nowhere.
	move(sale(oct, 2), "sale", "out", "7", oct)
	move(sale(sep, 3), "sale", "out", "900", sep)
	gone := sale(sep, 2)
	if _, err := db.ExecContext(ctx, `UPDATE tally.bill_head SET deleted_at = now() WHERE id = $1`, gone); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	move(gone, "sale", "out", "800", sep)
	move(uuid.New(), "sale", "out", "700", sep)

	repo := repreports.New(db)
	cost := func(from, to time.Time) string {
		f, err := repo.MonthlyFigures(ctx, tenant, from, to)
		if err != nil {
			t.Fatalf("MonthlyFigures: %v", err)
		}
		return f.CostOfGoods.StringFixed(2)
	}
	if got := cost(time.Date(2026, 9, 1, 0, 0, 0, 0, cst), time.Date(2026, 10, 1, 0, 0, 0, 0, cst)); got != "45.00" {
		t.Errorf("september cost = %s, want 45.00", got)
	}
	if got := cost(time.Date(2026, 10, 1, 0, 0, 0, 0, cst), time.Date(2026, 11, 1, 0, 0, 0, 0, cst)); got != "7.00" {
		t.Errorf("october cost = %s, want 7.00", got)
	}
}
