package bill_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	repobill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/bill"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

func headWithRef(ref string) *domain.BillHead {
	now := time.Now()
	return &domain.BillHead{
		ID: uuid.New(), TenantID: uuid.New(), BillNo: "SL-1", BillType: domain.BillTypeSale, SubType: domain.BillSubTypeSale,
		CreatorID: uuid.New(), BillDate: now, CreatedAt: now, UpdatedAt: now,
		ClientRef: ref, Source: "pos_offline", OccurredAt: &now,
	}
}

func createWithErr(t *testing.T, insertErr error) error {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO tally.bill_head`).WillReturnError(insertErr)
	tx, _ := db.Begin()
	return repobill.New(db).CreateBill(context.Background(), tx, headWithRef("r-1"), nil)
}

// 唯一索引冲突（23505 且约束名是 client_ref 索引）→ ErrDuplicateClientRef；
// 其他唯一冲突（如 bill_no）不能被误当作重放。
func TestCreateBill_ClientRefUniqueViolation(t *testing.T) {
	err := createWithErr(t, &pgconn.PgError{Code: "23505", ConstraintName: "bill_head_tenant_client_ref_uidx"})
	if !errors.Is(err, appbill.ErrDuplicateClientRef) {
		t.Fatalf("want ErrDuplicateClientRef, got %v", err)
	}
	err = createWithErr(t, &pgconn.PgError{Code: "23505", ConstraintName: "idx_bill_head_no"})
	if err == nil || errors.Is(err, appbill.ErrDuplicateClientRef) {
		t.Fatalf("bill_no conflict must not map to ErrDuplicateClientRef, got %v", err)
	}
}

func TestCreateBill_WritesOfflineColumns(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()
	h := headWithRef("till-9")
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO tally.bill_head[\s\S]*client_ref, source, occurred_at`).
		WithArgs(h.ID, h.TenantID, "SL-1", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			"till-9", "pos_offline", h.OccurredAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	tx, _ := db.Begin()
	if err := repobill.New(db).CreateBill(context.Background(), tx, h, nil); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFindBillByClientRef(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()
	tenant, id := uuid.New(), uuid.New()
	mock.ExpectQuery(`FROM tally.bill_head WHERE tenant_id = \$1 AND client_ref = \$2`).
		WithArgs(tenant, "hit").
		WillReturnRows(sqlmock.NewRows([]string{"id", "bill_no"}).AddRow(id, "SL-7"))
	mock.ExpectQuery(`FROM tally.bill_head WHERE tenant_id = \$1 AND client_ref = \$2`).
		WithArgs(tenant, "miss").WillReturnError(sql.ErrNoRows)

	r := repobill.New(db)
	gotID, no, found, err := r.FindBillByClientRef(context.Background(), tenant, "hit")
	if err != nil || !found || gotID != id || no != "SL-7" {
		t.Fatalf("hit: %v %v %v %v", gotID, no, found, err)
	}
	if _, _, found, err := r.FindBillByClientRef(context.Background(), tenant, "miss"); err != nil || found {
		t.Fatalf("miss: found=%v err=%v", found, err)
	}
}

func TestRecordNegativeStock_WritesAuditNote(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()
	tenant, bill, user, product := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO tally.audit_log[\s\S]*stock.negative[\s\S]*FROM tally.product`).
		WithArgs(tenant, user, bill, product, "-3").
		WillReturnResult(sqlmock.NewResult(0, 1))
	tx, _ := db.Begin()
	if err := repobill.New(db).RecordNegativeStock(context.Background(), tx, tenant, bill, user, product, decimal.NewFromInt(-3)); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
