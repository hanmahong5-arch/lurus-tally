//go:build integration

// rls_withtx_test.go proves hazard H8: a transaction opened through a repo's
// WithTx must run on the tenant-pinned connection so it inherits the session
// app.tenant_id and the RLS policies bind its WRITES — not just reads.
//
// bill.Repo.WithTx routes through dbscope.BeginTx, which begins on the pinned
// *sql.Conn when the request pinned one (middleware.TenantDB) and on the shared
// pool otherwise. The whole purchase/sale approval flow shares that one tx
// (stock_movement/snapshot/lot, bill_head/item, outbox), so this single routing
// decision is what activates the backstop for the entire money-mutation path.
//
// Run with:
//
//	go test -v -tags integration -timeout 180s ./tests/integration/ -run TestRLS_WithTx
package integration

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	billrepo "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/bill"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
	paymentrepo "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/payment"
	stockrepo "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/stock"
)

// insertProductTx writes a product row inside the given tx (the unit of work a
// WithTx body performs). Same minimal columns the production CreateBill path and
// insertProduct helper use.
func insertProductTx(tx *sql.Tx, ctx context.Context, tenantID uuid.UUID, code string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO tally.product (id, tenant_id, code, name, enabled, lead_time_days)
		VALUES ($1, $2, $3, $4, true, 7)
	`, uuid.New(), tenantID, code, code)
	return err
}

// TestRLS_WithTxInheritsPin: with a connection pinned to tenant A, bill.WithTx's
// transaction refuses a tenant-B write (so the tx inherited the GUC and FORCE
// bound it) but accepts a tenant-A write; with NO pin, the same WithTx writes a
// tenant-B row unchecked (proving the pin — not something always-on — is what
// activates the database backstop inside a transaction).
func TestRLS_WithTxInheritsPin(t *testing.T) {
	dsn, db, cleanup := startRLSTestDB(t)
	defer cleanup()
	ctx := context.Background()

	tenantA := insertTenant(t, db, ctx)
	tenantB := insertTenant(t, db, ctx)

	// Reassign product ownership to the NOSUPERUSER role so FORCE is operative
	// for the tx (which runs as that role).
	provisionAppRole(t, db)

	appDB, err := sql.Open("pgx", appDSNFrom(t, dsn))
	if err != nil {
		t.Fatalf("open app db: %v", err)
	}
	defer appDB.Close() //nolint:errcheck

	repo := billrepo.New(appDB)

	// Pin one connection scoped to tenant A (as middleware.TenantDB does).
	conn, err := appDB.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire conn: %v", err)
	}
	defer conn.Close() //nolint:errcheck
	if _, err := conn.ExecContext(ctx,
		"SELECT set_config('app.tenant_id', $1, false)", tenantA.String()); err != nil {
		t.Fatalf("set app.tenant_id: %v", err)
	}
	ctxA := dbscope.With(ctx, conn)

	// (a) pinned WithTx: cross-tenant write is rejected by RLS inside the tx.
	errReject := repo.WithTx(ctxA, func(tx *sql.Tx) error {
		return insertProductTx(tx, ctx, tenantB, "TX-REJECT-B")
	})
	if errReject == nil {
		t.Error("FAIL: WithTx on a tenant-A-pinned conn wrote a tenant-B row; tx did not inherit the GUC / RLS not enforced")
	} else if !strings.Contains(errReject.Error(), "row-level security") {
		t.Errorf("FAIL: cross-tenant write in tx rejected, but not by RLS: %v", errReject)
	} else {
		t.Logf("PASS: pinned WithTx rejected cross-tenant write via RLS: %v", errReject)
	}

	// (b) pinned WithTx: same-tenant write succeeds (the rejection is RLS, not a
	// blanket failure, and the rollback in (a) left the conn usable).
	if err := repo.WithTx(ctxA, func(tx *sql.Tx) error {
		return insertProductTx(tx, ctx, tenantA, "TX-OK-A")
	}); err != nil {
		t.Errorf("FAIL: pinned same-tenant WithTx write rejected: %v", err)
	} else {
		t.Logf("PASS: pinned WithTx accepted same-tenant write")
	}

	// (c) no pin: WithTx falls back to the pool (no GUC); the CASE short-circuit
	// makes the write pass, so isolation here depends on application code, not the
	// DB. This is the contrast that proves (a)'s enforcement comes from the pin.
	if err := repo.WithTx(ctx, func(tx *sql.Tx) error {
		return insertProductTx(tx, ctx, tenantB, "TX-NOPIN-B")
	}); err != nil {
		t.Errorf("FAIL: un-pinned WithTx write errored unexpectedly: %v", err)
	} else {
		t.Logf("PASS: un-pinned WithTx wrote without DB-level enforcement (expected; relies on WHERE)")
	}

	// Confirm the writes that should have landed did, via the superuser view:
	// TX-OK-A (tenant A) + TX-NOPIN-B (tenant B) present, TX-REJECT-B absent.
	assertProductCode(t, db, ctx, "TX-OK-A", true)
	assertProductCode(t, db, ctx, "TX-NOPIN-B", true)
	assertProductCode(t, db, ctx, "TX-REJECT-B", false)
}

// TestRLS_WithTxStockPayment guards against a missed conversion: stock.Repo and
// payment.Repo each route WithTx through dbscope.BeginTx, so a transaction pinned
// to tenant A must reject a tenant-B write to stock_snapshot / payment_head. The
// unpinned money-path tests cannot catch a repo whose WithTx was left on
// r.db.BeginTx — this can.
func TestRLS_WithTxStockPayment(t *testing.T) {
	dsn, db, cleanup := startRLSTestDB(t)
	defer cleanup()
	ctx := context.Background()

	tenantA := insertTenant(t, db, ctx)
	tenantB := insertTenant(t, db, ctx)
	// stock_snapshot has FKs to product + warehouse (tenant-agnostic at the FK
	// level); seed a tenant-B product/warehouse so only RLS — not an FK — can
	// reject the cross-tenant write.
	whB := insertWarehouse(t, db, ctx, tenantB)
	prodB := insertProduct(t, db, ctx, tenantB, "B Product", "B-WT-1")

	provisionAppRole(t, db)

	appDB, err := sql.Open("pgx", appDSNFrom(t, dsn))
	if err != nil {
		t.Fatalf("open app db: %v", err)
	}
	defer appDB.Close() //nolint:errcheck

	conn, err := appDB.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire conn: %v", err)
	}
	defer conn.Close() //nolint:errcheck
	if _, err := conn.ExecContext(ctx,
		"SELECT set_config('app.tenant_id', $1, false)", tenantA.String()); err != nil {
		t.Fatalf("set app.tenant_id: %v", err)
	}
	ctxA := dbscope.With(ctx, conn)

	// stock.WithTx pinned to A → cross-tenant stock_snapshot write rejected.
	errStock := stockrepo.New(appDB).WithTx(ctxA, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `
			INSERT INTO tally.stock_snapshot (tenant_id, product_id, warehouse_id, on_hand_qty, available_qty, unit_cost)
			VALUES ($1, $2, $3, 1, 1, 1)
		`, tenantB, prodB, whB)
		return e
	})
	if errStock == nil || !strings.Contains(errStock.Error(), "row-level security") {
		t.Errorf("FAIL: stock.WithTx cross-tenant write not rejected by RLS: %v", errStock)
	} else {
		t.Logf("PASS: stock.WithTx rejected cross-tenant write: %v", errStock)
	}

	// payment.WithTx pinned to A → cross-tenant payment_head write rejected.
	errPay := paymentrepo.New(appDB).WithTx(ctxA, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `
			INSERT INTO tally.payment_head (id, tenant_id, pay_type, creator_id, pay_date, amount, total_amount)
			VALUES ($1, $2, 'income', $3, now(), 1, 1)
		`, uuid.New(), tenantB, uuid.New())
		return e
	})
	if errPay == nil || !strings.Contains(errPay.Error(), "row-level security") {
		t.Errorf("FAIL: payment.WithTx cross-tenant write not rejected by RLS: %v", errPay)
	} else {
		t.Logf("PASS: payment.WithTx rejected cross-tenant write: %v", errPay)
	}
}

func assertProductCode(t *testing.T, db *sql.DB, ctx context.Context, code string, want bool) {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx,
		"SELECT count(*) FROM tally.product WHERE code = $1", code).Scan(&n); err != nil {
		t.Fatalf("count product code %s: %v", code, err)
	}
	if want && n == 0 {
		t.Errorf("FAIL: product code %q expected present, found none", code)
	}
	if !want && n != 0 {
		t.Errorf("FAIL: product code %q expected absent, found %d", code, n)
	}
}
