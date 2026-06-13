//go:build integration

// Export CSV real-SQL tests: the row-cap truncation boundary (the off-by-one in
// "fetch cap+1, emit trailer at row == cap") and the SQL-side projection
// (status label, date formatting, COALESCE NULL fallbacks) that the pure-function
// unit tests in internal/app/export can't reach. The cap is injected via
// export.WithRowLimit so the truncation path is exercised with 3 rows, not 50k.
//
//	go test -v -tags integration -timeout 180s ./tests/integration/ -run TestExport
package integration

import (
	"bytes"
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/hanmahong5-arch/lurus-tally/internal/app/export"
)

// insertBillWithRemark inserts an approved purchase bill carrying an explicit
// remark (NULL when remark == ""), used to exercise the COALESCE(remark,”)
// projection in the bills export.
func insertBillWithRemark(t *testing.T, db *sql.DB, ctx context.Context, tenantID uuid.UUID, partnerID *uuid.UUID, billDate time.Time, remark string) {
	t.Helper()
	id := uuid.New()
	creatorID := uuid.New()
	billNo := "EX-" + id.String()[:8]
	var remarkArg any
	if remark != "" {
		remarkArg = remark
	} // else nil → NULL
	_, err := db.ExecContext(ctx, `
		INSERT INTO tally.bill_head
		    (id, tenant_id, bill_no, bill_type, sub_type, status, partner_id, creator_id, bill_date, remark)
		VALUES ($1, $2, $3, '入库', '采购', 2, $4, $5, $6, $7)
	`, id, tenantID, billNo, partnerID, creatorID, billDate, remarkArg)
	if err != nil {
		t.Fatalf("insertBillWithRemark: %v", err)
	}
}

func TestExportBills_TruncationBoundary(t *testing.T) {
	db, cleanup := sqlRealDB(t)
	defer cleanup()
	ctx := context.Background()

	tenantID := insertTenant(t, db, ctx)
	partner := insertPartner(t, db, ctx, tenantID, "Export Supplier", "supplier")
	now := time.Now().UTC()

	// Two bills, cap == 2: exactly at the cap → NO truncation trailer.
	for i := 0; i < 2; i++ {
		insertBillHead(t, db, ctx, tenantID, "入库", "采购", 2, &partner, now.Add(-time.Duration(i)*time.Hour))
	}

	uc := export.NewBillsExportUseCase(db, nil, export.WithRowLimit(2))

	t.Run("at_cap_no_trailer", func(t *testing.T) {
		var buf bytes.Buffer
		n, err := uc.Execute(ctx, tenantID, &buf)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if n != 2 {
			t.Errorf("data rows = %d, want 2", n)
		}
		if strings.Contains(buf.String(), "[截断]") {
			t.Errorf("trailer present at exactly cap rows; should not truncate:\n%s", buf.String())
		}
	})

	// Add a third bill → cap+1 rows exist → truncation trailer emitted.
	insertBillHead(t, db, ctx, tenantID, "入库", "采购", 2, &partner, now.Add(-3*time.Hour))

	t.Run("over_cap_emits_trailer", func(t *testing.T) {
		var buf bytes.Buffer
		n, err := uc.Execute(ctx, tenantID, &buf)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if n != 2 {
			t.Errorf("data rows = %d, want 2 (cap), trailer is not counted", n)
		}
		if !strings.Contains(buf.String(), "[截断]") {
			t.Errorf("trailer missing despite cap+1 rows:\n%s", buf.String())
		}
		if !strings.Contains(buf.String(), "2") { // "数据超过 2 行限制"
			t.Errorf("trailer should report the cap (2):\n%s", buf.String())
		}
	})
}

func TestExportBills_ProjectionLabelsAndNulls(t *testing.T) {
	db, cleanup := sqlRealDB(t)
	defer cleanup()
	ctx := context.Background()

	tenantID := insertTenant(t, db, ctx)
	partner := insertPartner(t, db, ctx, tenantID, "Export Supplier", "supplier")
	billDate := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)

	// One bill with a partner + date but NULL remark (COALESCE → "").
	insertBillWithRemark(t, db, ctx, tenantID, &partner, billDate, "")

	// Round-trip the stored bill_date through the same driver to learn its
	// canonical Go formatting (independent of the session timezone), so the
	// assertion checks the export's formatting, not the DB's tz handling.
	var stored time.Time
	if err := db.QueryRowContext(ctx, `SELECT bill_date FROM tally.bill_head WHERE tenant_id = $1 LIMIT 1`, tenantID).Scan(&stored); err != nil {
		t.Fatalf("read back bill_date: %v", err)
	}
	wantDate := stored.Format("2006-01-02")

	uc := export.NewBillsExportUseCase(db, nil)
	var buf bytes.Buffer
	n, err := uc.Execute(ctx, tenantID, &buf)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if n != 1 {
		t.Fatalf("data rows = %d, want 1", n)
	}
	out := buf.String()
	// Header present (Chinese columns).
	if !strings.Contains(out, "单号") || !strings.Contains(out, "状态") {
		t.Errorf("CSV header missing expected columns:\n%s", out)
	}
	// status=2 → "已审核" label.
	if !strings.Contains(out, "已审核") {
		t.Errorf("status label 已审核 missing (statusLabel(2)):\n%s", out)
	}
	// bill_date formatted as YYYY-MM-DD (matching the canonical round-trip).
	if !strings.Contains(out, wantDate) {
		t.Errorf("formatted bill_date %s missing:\n%s", wantDate, out)
	}
	// NULL remark must COALESCE to empty — never the literal NULL/<nil>.
	if strings.Contains(out, "NULL") || strings.Contains(out, "<nil>") {
		t.Errorf("NULL remark leaked as literal:\n%s", out)
	}
}
