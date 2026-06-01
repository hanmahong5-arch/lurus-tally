//go:build integration

// Package integration — rls_isolation_test.go proves that migration 000042 turns
// Row-Level Security into a real database backstop, not a flag the application
// (which connects as the table OWNER) silently bypasses.
//
// The production hazard the audit found: tenant tables had only ENABLE ROW LEVEL
// SECURITY, which does not bind the table owner, and the app connects as owner —
// so isolation rested entirely on hand-written WHERE clauses. The fix is FORCE +
// short-circuit-safe CASE policies.
//
// To exercise the FIX rather than just RLS-for-non-owners, this test connects as
// a NON-SUPERUSER role that OWNS the tables under test (mirroring production,
// where the app is the owner). On the pre-042 schema (ENABLE only) such an owner
// would bypass RLS and these assertions would fail; they pass only because 000042
// added FORCE. The testcontainers default user is a SUPERUSER (bypasses RLS
// unconditionally), so it is used only to seed rows, never to assert isolation.
//
// Run with:
//
//	go test -v -tags integration -timeout 180s ./tests/integration/ -run TestRLS_
package integration

import (
	"context"
	"database/sql"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/hanmahong5-arch/lurus-tally/internal/lifecycle"
)

const (
	rlsAppRole     = "tally_app"
	rlsAppPassword = "tally_app_secret"
)

// rlsOwnedTables are reassigned to the non-superuser role so FORCE ROW LEVEL
// SECURITY is the operative mechanism (owner-bound RLS) when that role queries.
var rlsOwnedTables = []string{"product", "stock_snapshot", "bill_head", "payment_head"}

// startRLSTestDB starts a container, runs migrations, and returns the superuser
// DSN + an open superuser *sql.DB (for seeding) alongside cleanup.
func startRLSTestDB(t *testing.T) (string, *sql.DB, func()) {
	t.Helper()
	dsn, cleanup := startPostgres(t)

	ctx := context.Background()
	if err := lifecycle.RunMigrations(ctx, dsn, nil); err != nil {
		cleanup()
		t.Fatalf("RunMigrations: %v", err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		cleanup()
		t.Fatalf("open superuser db: %v", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close() //nolint:errcheck
		cleanup()
		t.Fatalf("ping superuser db: %v", err)
	}
	return dsn, db, func() {
		db.Close() //nolint:errcheck
		cleanup()
	}
}

// appDSNFrom rewrites the superuser DSN's userinfo to the non-superuser role.
func appDSNFrom(t *testing.T, superDSN string) string {
	t.Helper()
	u, err := url.Parse(superDSN)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	u.User = url.UserPassword(rlsAppRole, rlsAppPassword)
	return u.String()
}

// provisionAppRole creates the non-superuser role, grants it the schema/table
// privileges the production app role holds, and reassigns ownership of the
// tables under test so FORCE binds it.
func provisionAppRole(t *testing.T, db *sql.DB) {
	t.Helper()
	mustExec(t, db, `CREATE ROLE `+rlsAppRole+` LOGIN NOSUPERUSER PASSWORD '`+rlsAppPassword+`'`)
	mustExec(t, db, `GRANT USAGE ON SCHEMA tally TO `+rlsAppRole)
	mustExec(t, db, `GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA tally TO `+rlsAppRole)
	mustExec(t, db, `GRANT USAGE ON ALL SEQUENCES IN SCHEMA tally TO `+rlsAppRole)
	for _, tbl := range rlsOwnedTables {
		mustExec(t, db, `ALTER TABLE tally.`+tbl+` OWNER TO `+rlsAppRole)
	}
}

// seedPaymentHead inserts a minimal payment_head row for the given tenant.
func seedPaymentHead(t *testing.T, db *sql.DB, ctx context.Context, tenantID uuid.UUID) {
	t.Helper()
	_, err := db.ExecContext(ctx, `
		INSERT INTO tally.payment_head
		    (id, tenant_id, pay_type, creator_id, pay_date, amount, total_amount)
		VALUES ($1, $2, 'income', $3, now(), 100, 100)
	`, uuid.New(), tenantID, uuid.New())
	if err != nil {
		t.Fatalf("seedPaymentHead: %v", err)
	}
}

// TestRLS_ForceIsolation is the central proof: a non-superuser owner connection
// that has set app.tenant_id = A sees ONLY tenant A's rows on a WHERE-less query
// (so isolation is the database's doing, not a WHERE clause), is refused a
// cross-tenant write, and — once the GUC is cleared — sees every row again
// (proving the short-circuit CASE keeps un-pinned paths working).
func TestRLS_ForceIsolation(t *testing.T) {
	dsn, db, cleanup := startRLSTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// ── seed two tenants via the superuser connection (bypasses RLS) ──────────
	tenantA := insertTenant(t, db, ctx)
	tenantB := insertTenant(t, db, ctx)

	whA := insertWarehouse(t, db, ctx, tenantA)
	whB := insertWarehouse(t, db, ctx, tenantB)

	prodA1 := insertProduct(t, db, ctx, tenantA, "A Alpha", "A-CODE-1")
	_ = insertProduct(t, db, ctx, tenantA, "A Beta", "A-CODE-2")
	prodB1 := insertProduct(t, db, ctx, tenantB, "B Alpha", "B-CODE-1")

	insertStockSnapshot(t, db, ctx, tenantA, prodA1, whA, 10, 10, 5)
	insertStockSnapshot(t, db, ctx, tenantB, prodB1, whB, 20, 20, 7)

	insertBillHead(t, db, ctx, tenantA, "入库", "采购", 0, nil, time.Now())
	insertBillHead(t, db, ctx, tenantB, "入库", "采购", 0, nil, time.Now())

	seedPaymentHead(t, db, ctx, tenantA)
	seedPaymentHead(t, db, ctx, tenantB)

	// Sanity: the superuser sees BOTH tenants' rows (so the isolation asserted
	// below is meaningful — tenant B's rows really exist).
	if got := countTable(t, db, ctx, "product"); got != 3 {
		t.Fatalf("superuser product count = %d, want 3 (2 A + 1 B)", got)
	}

	// ── reassign ownership to a non-superuser role so FORCE is operative ──────
	provisionAppRole(t, db)

	appDB, err := sql.Open("pgx", appDSNFrom(t, dsn))
	if err != nil {
		t.Fatalf("open app db: %v", err)
	}
	defer appDB.Close() //nolint:errcheck

	// Pin one connection and scope it to tenant A, exactly as middleware.TenantDB does.
	conn, err := appDB.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire app conn: %v", err)
	}
	defer conn.Close() //nolint:errcheck
	if _, err := conn.ExecContext(ctx,
		"SELECT set_config('app.tenant_id', $1, false)", tenantA.String()); err != nil {
		t.Fatalf("set app.tenant_id: %v", err)
	}

	// (1) WHERE-less counts return ONLY tenant A's rows → RLS, not WHERE, isolates.
	isolationCases := []struct {
		table string
		wantA int
	}{
		{"product", 2},
		{"stock_snapshot", 1},
		{"bill_head", 1},
		{"payment_head", 1},
	}
	for _, tc := range isolationCases {
		got := countConnTable(t, conn, ctx, tc.table)
		if got != tc.wantA {
			t.Errorf("FAIL [%s]: WHERE-less count under app.tenant_id=A = %d, want %d (cross-tenant rows leaked or RLS not enforced)",
				tc.table, got, tc.wantA)
		} else {
			t.Logf("PASS [%s]: WHERE-less count = %d (tenant A only)", tc.table, got)
		}
	}

	// (2a) cross-tenant write is rejected by WITH CHECK.
	_, werr := conn.ExecContext(ctx, `
		INSERT INTO tally.product (id, tenant_id, code, name, enabled, lead_time_days)
		VALUES ($1, $2, 'XTENANT', 'cross-tenant write', true, 7)
	`, uuid.New(), tenantB)
	if werr == nil {
		t.Error("FAIL: insert of a tenant-B row under app.tenant_id=A was accepted; WITH CHECK did not fire")
	} else if !strings.Contains(werr.Error(), "row-level security") {
		t.Errorf("FAIL: cross-tenant insert rejected, but not by RLS: %v", werr)
	} else {
		t.Logf("PASS: cross-tenant write rejected by RLS: %v", werr)
	}

	// (2b) same-tenant write succeeds — proves (2a) is RLS, not a blanket denial.
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO tally.product (id, tenant_id, code, name, enabled, lead_time_days)
		VALUES ($1, $2, 'A-CTRL', 'same-tenant control', true, 7)
	`, uuid.New(), tenantA); err != nil {
		t.Errorf("FAIL: same-tenant write rejected (should pass WITH CHECK): %v", err)
	} else {
		t.Logf("PASS: same-tenant write accepted")
	}

	// (3) RESET clears the GUC.
	if _, err := conn.ExecContext(ctx, "RESET app.tenant_id"); err != nil {
		t.Fatalf("RESET app.tenant_id: %v", err)
	}
	var cur sql.NullString
	if err := conn.QueryRowContext(ctx,
		"SELECT current_setting('app.tenant_id', true)").Scan(&cur); err != nil {
		t.Fatalf("read app.tenant_id after RESET: %v", err)
	}
	if cur.Valid && cur.String != "" {
		t.Errorf("FAIL: app.tenant_id after RESET = %q, want empty/null", cur.String)
	} else {
		t.Logf("PASS: app.tenant_id cleared after RESET")
	}

	// (4) with the GUC cleared, the CASE short-circuit makes every row visible
	// again (non-breaking for un-pinned paths). 3 A-rows (2 seeded + 1 control)
	// + 1 B-row = 4.
	if got := countConnTable(t, conn, ctx, "product"); got != 4 {
		t.Errorf("FAIL: empty-GUC product count = %d, want 4 (CASE empty→true should show all rows)", got)
	} else {
		t.Logf("PASS: empty-GUC sees all rows (count=4)")
	}
}

// TestRLS_ForceFlagSet asserts the structural half of the fix: every covered
// tenant table actually carries relforcerowsecurity = true after 000042, so the
// policy binds the owner connection (the production identity).
func TestRLS_ForceFlagSet(t *testing.T) {
	_, db, cleanup := startRLSTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Every tenant-scoped table must be FORCE after 000042 (parents) + 000043
	// (children), so the policy binds the owner connection the app uses. This is
	// the structural backbone of the backstop; behavioural isolation is proven on
	// the core parents in TestRLS_ForceIsolation, and the children carry the
	// identical canonical policy (shape asserted in TestMigration_RLSEnabled).
	tables := []string{
		// 000012 parents
		"partner", "product", "warehouse", "stock_snapshot", "bill_head", "bill_item",
		"payment_head", "audit_log", "system_config", "bill_sequence", "org_department",
		// 000013/000031 relaxed-but-FORCE (pre-tenant auth)
		"tenant_profile", "user_identity_mapping", "personal_access_token",
		// 000014/000022/000024/000028/000029/000033
		"unit_def", "product_unit", "stock_lot", "stock_movement", "exchange_rate",
		"nursery_dict", "project", "supplier",
		// 000035 service-branch + 000036 account tables (H10 fix)
		"event_outbox", "user_session", "account_audit_log", "user_profile",
		// 000037/000040 import dedup
		"import_sku_map", "import_order_seen", "import_order_cancel_seen", "import_refund_seen",
		// 000043 children
		"org_user_rel", "partner_bank", "product_category", "product_sku",
		"product_attribute", "unit", "warehouse_bin", "stock_initial",
		"stock_serial", "finance_account", "finance_category", "payment_item",
		"shopify_shop_map",
	}
	for _, tbl := range tables {
		var forced bool
		err := db.QueryRowContext(ctx, `
			SELECT relforcerowsecurity FROM pg_class
			WHERE relnamespace = 'tally'::regnamespace AND relname = $1
		`, tbl).Scan(&forced)
		if err != nil {
			t.Fatalf("query relforcerowsecurity for %s: %v", tbl, err)
		}
		if !forced {
			t.Errorf("FAIL [%s]: relforcerowsecurity = false; FORCE ROW LEVEL SECURITY not applied", tbl)
		} else {
			t.Logf("PASS [%s]: FORCE ROW LEVEL SECURITY set", tbl)
		}
	}
}

// countTable counts all rows via a pool handle (superuser bypasses RLS).
func countTable(t *testing.T, db *sql.DB, ctx context.Context, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM tally."+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// countConnTable counts all rows on a pinned connection (RLS-scoped). The query
// carries NO WHERE clause, so any filtering is the policy's doing.
func countConnTable(t *testing.T, conn *sql.Conn, ctx context.Context, table string) int {
	t.Helper()
	var n int
	if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM tally."+table).Scan(&n); err != nil {
		t.Fatalf("count %s on pinned conn: %v", table, err)
	}
	return n
}
