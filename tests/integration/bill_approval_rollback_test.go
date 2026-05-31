//go:build integration

// Package integration — bill-approval transaction rollback invariant.
//
// WHY THIS EXISTS
// ---------------
// The existing unit test (internal/app/bill/approve_stock_honesty_test.go) asserts
// rollback against a MOCK WithTx that runs fn(nil): it never opens a real Postgres
// transaction, so a partial-commit bug — e.g. an item-1 stock movement written and
// COMMITTED outside the transaction while item-2 fails — would pass every unit test
// today while silently corrupting inventory in production.
//
// This test drives the REAL ApprovePurchaseUseCase against a REAL Postgres container,
// wired with the REAL adapters (bill repo + RecordMovementUseCase + unit repo), and
// forces a mid-approval failure AFTER the first line item has already produced a stock
// movement inside the transaction. It then proves — via raw SQL independent of the repo
// under test — that the whole transaction rolled back: the bill is still Draft and there
// are ZERO stock_movement rows for the bill.
//
// Run:
//
//	go test -tags integration -run TestBillApprovalRollback ./tests/integration/ -timeout 360s -v
package integration

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"

	repobill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/bill"
	repostock "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/stock"
	repounit "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/unit"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domainbill "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// barFixture bundles every ID seeded for the rollback test so the assertions can
// reference them by name. The "bar" prefix (Bill Approval Rollback) keeps all
// symbols in this file unique versus the other integration test files that compile
// into the same package.
type barFixture struct {
	tenantID    uuid.UUID
	warehouseID uuid.UUID
	supplierID  uuid.UUID
	productOne  uuid.UUID // line 1 — unit_id NULL → conversion succeeds → movement is recorded inside the tx
	productTwo  uuid.UUID // line 2 — unit_id set but NO product_unit row → GetConversionFactor fails mid-tx
	systemUnit  uuid.UUID // a real unit_def row (FK target) that has no product_unit conversion for productTwo
	billID      uuid.UUID
	creatorID   uuid.UUID
}

// barExec runs a statement and fails the test on error (no swallowed errors).
func barExec(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		t.Fatalf("barExec: %v\nquery: %s", err, query)
	}
}

// barSeedBase inserts the tenant, warehouse, supplier, two products, and resolves a
// system unit_def row id to use as the poison unit for line 2.
func barSeedBase(t *testing.T, ctx context.Context, db *sql.DB) barFixture {
	t.Helper()
	f := barFixture{
		tenantID:    uuid.New(),
		warehouseID: uuid.New(),
		supplierID:  uuid.New(),
		productOne:  uuid.New(),
		productTwo:  uuid.New(),
		billID:      uuid.New(),
		creatorID:   uuid.New(),
	}

	barExec(t, ctx, db, `INSERT INTO tally.tenant (id, name, status) VALUES ($1, $2, 1)`,
		f.tenantID, "BAR Tenant "+f.tenantID.String()[:8])

	barExec(t, ctx, db, `
		INSERT INTO tally.warehouse (id, tenant_id, name, enabled, is_default)
		VALUES ($1, $2, 'BAR WH', true, true)`,
		f.warehouseID, f.tenantID)

	barExec(t, ctx, db, `
		INSERT INTO tally.partner (id, tenant_id, name, partner_type, enabled)
		VALUES ($1, $2, 'BAR Supplier', 'supplier', true)`,
		f.supplierID, f.tenantID)

	barExec(t, ctx, db, `
		INSERT INTO tally.product (id, tenant_id, code, name, enabled, lead_time_days)
		VALUES ($1, $2, $3, 'BAR Product One', true, 7)`,
		f.productOne, f.tenantID, "BAR-P1-"+f.productOne.String()[:6])

	barExec(t, ctx, db, `
		INSERT INTO tally.product (id, tenant_id, code, name, enabled, lead_time_days)
		VALUES ($1, $2, $3, 'BAR Product Two', true, 7)`,
		f.productTwo, f.tenantID, "BAR-P2-"+f.productTwo.String()[:6])

	// Resolve a real system unit_def row to satisfy bill_item.unit_id's FK
	// (bill_item_unit_id_fkey → unit_def, ON DELETE RESTRICT). System units are
	// seeded by migration 000014 and visible to every tenant via RLS / the is_system
	// flag. We deliberately do NOT create a product_unit row for (productTwo, systemUnit),
	// so GetConversionFactor returns ErrInvalidUnitForProduct mid-approval.
	if err := db.QueryRowContext(ctx,
		`SELECT id FROM tally.unit_def WHERE is_system = true AND code = 'pcs' LIMIT 1`,
	).Scan(&f.systemUnit); err != nil {
		t.Fatalf("barSeedBase: resolve system unit 'pcs': %v", err)
	}

	return f
}

// barSeedDraftBill seeds a DRAFT purchase bill with two line items via the REAL bill
// repo CreateBill, inside repo.WithTx (the production write path), so the fixture is
// not hand-rolled SQL but the same code the approve use case will later read back.
//
//   - line 1 (productOne): UnitID == nil → ApprovePurchase uses convFactor "1" and
//     records a real stock movement inside the approval transaction.
//   - line 2 (productTwo): UnitID == systemUnit but no product_unit conversion row →
//     GetConversionFactor fails, aborting the approval AFTER line 1's movement was
//     written inside the same transaction. This is the canonical partial-commit probe.
func barSeedDraftBill(t *testing.T, ctx context.Context, db *sql.DB, f barFixture) {
	t.Helper()

	billRepo := repobill.New(db)

	now := time.Now().UTC()
	whID := f.warehouseID
	partnerID := f.supplierID

	head := &domainbill.BillHead{
		ID:          f.billID,
		TenantID:    f.tenantID,
		BillNo:      "BAR-" + f.billID.String()[:8],
		BillType:    domainbill.BillTypePurchase,
		SubType:     domainbill.BillSubTypePurchase,
		Status:      domainbill.StatusDraft,
		PartnerID:   &partnerID,
		WarehouseID: &whID,
		CreatorID:   f.creatorID,
		BillDate:    now,
		Subtotal:    decimal.RequireFromString("300.00"),
		ShippingFee: decimal.Zero,
		TaxAmount:   decimal.Zero,
		TotalAmount: decimal.RequireFromString("300.00"),
		Remark:      "BAR rollback fixture",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	poisonUnit := f.systemUnit
	items := []*domainbill.BillItem{
		{
			// Line 1 — conversion succeeds (no unit) → movement recorded inside tx.
			ID:         uuid.New(),
			TenantID:   f.tenantID,
			HeadID:     f.billID,
			ProductID:  f.productOne,
			UnitID:     nil,
			LineNo:     1,
			Qty:        decimal.RequireFromString("10"),
			UnitPrice:  decimal.RequireFromString("10.00"),
			LineAmount: decimal.RequireFromString("100.00"),
		},
		{
			// Line 2 — unit present but no product_unit conversion → fails mid-tx.
			ID:         uuid.New(),
			TenantID:   f.tenantID,
			HeadID:     f.billID,
			ProductID:  f.productTwo,
			UnitID:     &poisonUnit,
			LineNo:     2,
			Qty:        decimal.RequireFromString("20"),
			UnitPrice:  decimal.RequireFromString("10.00"),
			LineAmount: decimal.RequireFromString("200.00"),
		},
	}

	if err := billRepo.WithTx(ctx, func(tx *sql.Tx) error {
		return billRepo.CreateBill(ctx, tx, head, items)
	}); err != nil {
		t.Fatalf("barSeedDraftBill: CreateBill: %v", err)
	}
}

// barCountMovements returns how many stock_movement rows reference the bill.
func barCountMovements(t *testing.T, ctx context.Context, db *sql.DB, tenantID, billID uuid.UUID) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tally.stock_movement WHERE tenant_id = $1 AND reference_id = $2`,
		tenantID, billID,
	).Scan(&n); err != nil {
		t.Fatalf("barCountMovements: %v", err)
	}
	return n
}

// barBillStatus returns the raw SMALLINT status of the bill, read directly from the DB.
func barBillStatus(t *testing.T, ctx context.Context, db *sql.DB, tenantID, billID uuid.UUID) int16 {
	t.Helper()
	var status int16
	if err := db.QueryRowContext(ctx,
		`SELECT status FROM tally.bill_head WHERE id = $1 AND tenant_id = $2`,
		billID, tenantID,
	).Scan(&status); err != nil {
		t.Fatalf("barBillStatus: %v", err)
	}
	return status
}

// barSnapshotExists reports whether a stock_snapshot row exists for the product.
func barSnapshotExists(t *testing.T, ctx context.Context, db *sql.DB, tenantID, productID uuid.UUID) bool {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tally.stock_snapshot WHERE tenant_id = $1 AND product_id = $2`,
		tenantID, productID,
	).Scan(&n); err != nil {
		t.Fatalf("barSnapshotExists: %v", err)
	}
	return n > 0
}

// TestBillApprovalRollback_PartialCommitIsRolledBack proves the approve-purchase
// transaction is atomic against a REAL Postgres backend: when line 2's unit
// conversion fails after line 1 has already produced a stock movement inside the
// transaction, the entire transaction rolls back — the bill stays Draft and no
// stock_movement / stock_snapshot rows survive.
func TestBillApprovalRollback_PartialCommitIsRolledBack(t *testing.T) {
	db, cleanup := sqlRealDB(t)
	defer cleanup()

	ctx := context.Background()

	// ── seed ────────────────────────────────────────────────────────────────
	f := barSeedBase(t, ctx, db)
	barSeedDraftBill(t, ctx, db, f)

	// Sanity: the draft must start as Draft (0) with no movements/snapshots, otherwise
	// the post-condition assertions would be meaningless (measuring nothing).
	if got := barBillStatus(t, ctx, db, f.tenantID, f.billID); got != int16(domainbill.StatusDraft) {
		t.Fatalf("pre-condition: bill status got %d, want %d (Draft)", got, domainbill.StatusDraft)
	}
	if got := barCountMovements(t, ctx, db, f.tenantID, f.billID); got != 0 {
		t.Fatalf("pre-condition: expected 0 movements before approval, got %d", got)
	}
	if barSnapshotExists(t, ctx, db, f.tenantID, f.productOne) {
		t.Fatalf("pre-condition: product one already has a stock snapshot before approval")
	}

	// ── wire the REAL use case + REAL adapters ───────────────────────────────
	billRepo := repobill.New(db)
	stockRepo := repostock.New(db)
	unitRepo := repounit.New(db)

	// WAC calculator (default, no profile). outbox nil → events skipped, movement still
	// commits/rolls-back with the outer tx; log nil → slog.Default.
	wacCalc := appstock.NewCalculator(nil, stockRepo)
	recordMvUC := appstock.NewRecordMovementUseCase(stockRepo, wacCalc, nil, nil)

	uc := appbill.NewApprovePurchaseUseCase(billRepo, recordMvUC, unitRepo)

	// ── act ──────────────────────────────────────────────────────────────────
	approver := uuid.New()
	err := uc.Execute(ctx, f.tenantID, f.billID, approver)

	// (1) Execute must return a non-nil error — line 2's missing conversion aborts it.
	if err == nil {
		t.Fatal("Execute: expected a non-nil error (line 2 has no unit conversion), got nil")
	}
	t.Logf("Execute returned expected error: %v", err)

	// ── assert the transaction rolled back, via raw SQL independent of the repo ─

	// (2) bill_head.status is still Draft (0).
	if got := barBillStatus(t, ctx, db, f.tenantID, f.billID); got != int16(domainbill.StatusDraft) {
		t.Errorf("ROLLBACK LEAK: bill status got %d, want %d (Draft) — approval was not rolled back", got, domainbill.StatusDraft)
	} else {
		t.Logf("PASS: bill status still Draft (%d)", got)
	}

	// (3) ZERO stock_movement rows for this bill — proving line 1's movement, which was
	//     written inside the transaction before the error, was rolled back. If the
	//     movement leaked (committed outside the tx or never rolled back), this fails red.
	if got := barCountMovements(t, ctx, db, f.tenantID, f.billID); got != 0 {
		t.Errorf("ROLLBACK LEAK: found %d stock_movement row(s) for bill %s, want 0 — a partial commit corrupted inventory",
			got, f.billID)
	} else {
		t.Logf("PASS: zero stock_movement rows reference the bill (line 1 movement rolled back)")
	}

	// (4) The stock snapshot for line 1's product is unchanged / absent. It had no row
	//     before approval, so a leaked movement would have upserted one — it must still
	//     be absent after rollback.
	if barSnapshotExists(t, ctx, db, f.tenantID, f.productOne) {
		t.Errorf("ROLLBACK LEAK: stock_snapshot for product %s exists after a failed approval, want absent", f.productOne)
	} else {
		t.Logf("PASS: no stock_snapshot row for product one (unchanged / absent after rollback)")
	}
}
