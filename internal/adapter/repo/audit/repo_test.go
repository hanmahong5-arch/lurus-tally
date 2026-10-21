package audit

import (
	"context"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	appaudit "github.com/hanmahong5-arch/lurus-tally/internal/app/audit"
)

var cols = []string{"id", "user_id", "action", "resource", "resource_id", "bill_no", "changes", "created_at"}

func TestList_TenantAndFilters(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	tenant, rid, uid := uuid.New(), uuid.New(), uuid.New()
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 10)

	mock.ExpectQuery(`SELECT count\(\*\) FROM tally.audit_log a WHERE a.tenant_id = \$1 AND a.resource = \$2 AND a.resource_id = \$3 AND a.created_at >= \$4 AND a.created_at < \$5`).
		WithArgs(tenant, "purchase_bill", rid, from, to).
		WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(7))
	mock.ExpectQuery(`LEFT JOIN tally.bill_head b ON b.id = a.resource_id AND b.tenant_id = a.tenant_id(.|\n)*ORDER BY a.created_at DESC(.|\n)*LIMIT \$6 OFFSET \$7`).
		WithArgs(tenant, "purchase_bill", rid, from, to, 20, 40).
		WillReturnRows(sqlmock.NewRows(cols).
			AddRow(uuid.New(), uid, "bill.approved", "purchase_bill", rid, "PO-1", []byte(`{"reason":"x"}`), from).
			AddRow(uuid.New(), nil, "bill.voided", "purchase_bill", nil, "", nil, from))

	rows, total, err := New(db).List(context.Background(), tenant, appaudit.Filter{
		Resource: "purchase_bill", ResourceID: rid, From: &from, To: &to, Limit: 20, Offset: 40})
	if err != nil {
		t.Fatal(err)
	}
	if total != 7 || len(rows) != 2 || rows[0].ResourceNo != "PO-1" || rows[0].UserID != uid || rows[1].UserID != uuid.Nil {
		t.Fatalf("total=%d rows=%+v", total, rows)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestList_DefaultsAndClamp(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()
	tenant := uuid.New()
	mock.ExpectQuery(`SELECT count`).WithArgs(tenant).WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(0))
	mock.ExpectQuery(`LIMIT \$2 OFFSET \$3`).WithArgs(tenant, 50, 0).WillReturnRows(sqlmock.NewRows(cols))
	if _, _, err := New(db).List(context.Background(), tenant, appaudit.Filter{Limit: 9999, Offset: -3}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAppend(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()
	tenant, uid, rid := uuid.New(), uuid.New(), uuid.New()
	mock.ExpectExec(`INSERT INTO tally.audit_log`).
		WithArgs(tenant, uid, "bill.voided", "sale_bill", rid, nil, "1.2.3.4").
		WillReturnResult(sqlmock.NewResult(0, 1))
	err := New(db).Append(context.Background(), appaudit.Entry{TenantID: tenant, UserID: uid, Action: "bill.voided", Resource: "sale_bill", ResourceID: rid, ClientIP: "1.2.3.4"})
	if err != nil {
		t.Fatal(err)
	}
}
