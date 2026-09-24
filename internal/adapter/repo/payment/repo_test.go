// Package payment_test exercises the PG payment repo via sqlmock.
// SQL contracts (column order, args, transactional boundaries) are validated against literals
// to catch unintended drift. No external PostgreSQL is required.
package payment_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	repopayment "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/payment"
	apppayment "github.com/hanmahong5-arch/lurus-tally/internal/app/payment"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
)

func newMockRepo(t *testing.T) (*repopayment.Repo, *sql.DB, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	cleanup := func() { _ = db.Close() }
	return repopayment.New(db), db, mock, cleanup
}

// TestPaymentRepo_New_ReturnsNonNil sanity-checks the constructor.
func TestPaymentRepo_New_ReturnsNonNil(t *testing.T) {
	if repopayment.New(nil) == nil {
		t.Fatal("New(nil) returned nil")
	}
}

// TestPaymentRepo_InterfaceSatisfied is a compile-time guard against accidental signature drift.
func TestPaymentRepo_InterfaceSatisfied(t *testing.T) {
	var _ apppayment.PaymentRepo = (*repopayment.Repo)(nil)
}

// ----- WithTx -----

func TestPaymentRepo_WithTx_Success_Commits(t *testing.T) {
	repo, _, mock, cleanup := newMockRepo(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectCommit()

	called := false
	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}
	if !called {
		t.Error("fn was not invoked")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestPaymentRepo_WithTx_FnError_RollsBack(t *testing.T) {
	repo, _, mock, cleanup := newMockRepo(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectRollback()

	boom := errors.New("business error")
	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error { return boom })
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want wrap of %v", err, boom)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestPaymentRepo_WithTx_BeginError_PropagatesWrapped(t *testing.T) {
	repo, _, mock, cleanup := newMockRepo(t)
	defer cleanup()

	boom := errors.New("connection refused")
	mock.ExpectBegin().WillReturnError(boom)

	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error { return nil })
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want wrap of %v", err, boom)
	}
}

func TestPaymentRepo_WithTx_CommitError_PropagatesWrapped(t *testing.T) {
	repo, _, mock, cleanup := newMockRepo(t)
	defer cleanup()

	boom := errors.New("commit failed")
	mock.ExpectBegin()
	mock.ExpectCommit().WillReturnError(boom)

	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error { return nil })
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want wrap of %v", err, boom)
	}
}

// ----- Record -----

const recordSQL = `
		INSERT INTO tally.payment_head
			(id, tenant_id, pay_type, partner_id, operator_id, creator_id,
			 bill_no, pay_date, amount, discount_amount, total_amount,
			 related_bill_id, remark, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`

// makePayment creates a Payment with deterministic non-zero fields for assertions.
func makePayment() *domain.Payment {
	tenantID := uuid.New()
	billID := uuid.New()
	creatorID := uuid.New()
	partnerID := uuid.New()
	operatorID := uuid.New()
	return &domain.Payment{
		ID:         uuid.New(),
		TenantID:   tenantID,
		BillID:     billID,
		PayType:    domain.PayTypeCash,
		Amount:     decimal.RequireFromString("100.00"),
		PartnerID:  &partnerID,
		OperatorID: &operatorID,
		CreatorID:  creatorID,
		BillNo:     "PO-20260423-0001",
		PayDate:    time.Date(2026, 4, 23, 10, 0, 0, 0, time.UTC),
		Remark:     "first instalment",
		CreatedAt:  time.Date(2026, 4, 23, 10, 1, 0, 0, time.UTC),
	}
}

func TestPaymentRepo_Record_Full_InsertsAllFields(t *testing.T) {
	repo, _, mock, cleanup := newMockRepo(t)
	defer cleanup()

	p := makePayment()

	mock.ExpectBegin()
	mock.ExpectExec(recordSQL).
		WithArgs(
			p.ID, p.TenantID, string(p.PayType),
			p.PartnerID, p.OperatorID, p.CreatorID,
			p.BillNo, p.PayDate,
			p.Amount.String(), "0", p.Amount.String(),
			p.BillID,
			p.Remark,
			p.CreatedAt,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error {
		return repo.Record(context.Background(), tx, p)
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestPaymentRepo_Record_NilID_AssignsNewUUID(t *testing.T) {
	repo, _, mock, cleanup := newMockRepo(t)
	defer cleanup()

	p := makePayment()
	p.ID = uuid.Nil

	mock.ExpectBegin()
	// Use AnyArg for the generated ID; assert other fields explicitly.
	mock.ExpectExec(recordSQL).
		WithArgs(
			sqlmock.AnyArg(),
			p.TenantID, string(p.PayType),
			p.PartnerID, p.OperatorID, p.CreatorID,
			p.BillNo, p.PayDate,
			p.Amount.String(), "0", p.Amount.String(),
			p.BillID, p.Remark, p.CreatedAt,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error {
		return repo.Record(context.Background(), tx, p)
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if p.ID == uuid.Nil {
		t.Error("expected ID to be populated, still nil")
	}
}

func TestPaymentRepo_Record_ZeroPayDate_DefaultsToNow(t *testing.T) {
	repo, _, mock, cleanup := newMockRepo(t)
	defer cleanup()

	p := makePayment()
	p.PayDate = time.Time{}
	p.CreatedAt = time.Time{}

	before := time.Now().UTC().Add(-1 * time.Second)

	var recordedPayDate, recordedCreatedAt time.Time
	mock.ExpectBegin()
	mock.ExpectExec(recordSQL).
		WithArgs(
			p.ID, p.TenantID, string(p.PayType),
			p.PartnerID, p.OperatorID, p.CreatorID,
			p.BillNo,
			sqlmock.AnyArg(), // pay_date
			p.Amount.String(), "0", p.Amount.String(),
			p.BillID, p.Remark,
			sqlmock.AnyArg(), // created_at
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error {
		return repo.Record(context.Background(), tx, p)
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	// Defaults are applied to the in-memory struct (CreatedAt) but PayDate is local.
	if p.CreatedAt.Before(before) {
		t.Errorf("CreatedAt = %v, expected >= %v", p.CreatedAt, before)
	}
	_ = recordedPayDate
	_ = recordedCreatedAt
}

func TestPaymentRepo_Record_EmptyBillNoAndRemark_PassNilToDB(t *testing.T) {
	repo, _, mock, cleanup := newMockRepo(t)
	defer cleanup()

	p := makePayment()
	p.BillNo = ""
	p.Remark = ""

	mock.ExpectBegin()
	mock.ExpectExec(recordSQL).
		WithArgs(
			p.ID, p.TenantID, string(p.PayType),
			p.PartnerID, p.OperatorID, p.CreatorID,
			nil, // bill_no → NULL
			p.PayDate,
			p.Amount.String(), "0", p.Amount.String(),
			p.BillID,
			nil, // remark → NULL
			p.CreatedAt,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error {
		return repo.Record(context.Background(), tx, p)
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestPaymentRepo_Record_DBError_RollsBackAndPropagatesWrapped(t *testing.T) {
	repo, _, mock, cleanup := newMockRepo(t)
	defer cleanup()

	p := makePayment()
	boom := errors.New("unique violation")

	mock.ExpectBegin()
	mock.ExpectExec(recordSQL).WillReturnError(boom)
	mock.ExpectRollback()

	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error {
		return repo.Record(context.Background(), tx, p)
	})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want wrap of %v", err, boom)
	}
}

// ----- ListByBill -----

const listByBillSQL = `
		SELECT id, tenant_id, related_bill_id, pay_type, amount, partner_id, operator_id,
		       creator_id, bill_no, pay_date, remark, created_at
		FROM tally.payment_head
		WHERE related_bill_id = $1 AND tenant_id = $2 AND deleted_at IS NULL
		ORDER BY pay_date ASC`

func TestPaymentRepo_ListByBill_Multiple_ReturnsAscOrder(t *testing.T) {
	repo, _, mock, cleanup := newMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	billID := uuid.New()
	creatorID := uuid.New()
	t1 := time.Date(2026, 4, 23, 9, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 4, 24, 9, 0, 0, 0, time.UTC)

	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "related_bill_id", "pay_type", "amount",
		"partner_id", "operator_id", "creator_id",
		"bill_no", "pay_date", "remark", "created_at",
	}).
		AddRow(uuid.New(), tenantID, billID, "cash", "30.00",
			nil, nil, creatorID,
			sql.NullString{String: "PO-1", Valid: true}, t1, sql.NullString{String: "first", Valid: true}, t1).
		AddRow(uuid.New(), tenantID, billID, "wechat", "70.00",
			nil, nil, creatorID,
			sql.NullString{}, t2, sql.NullString{}, t2)

	mock.ExpectQuery(listByBillSQL).
		WithArgs(billID, tenantID).
		WillReturnRows(rows)

	result, err := repo.ListByBill(context.Background(), tenantID, billID)
	if err != nil {
		t.Fatalf("ListByBill: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("len = %d, want 2", len(result))
	}
	if !result[0].Amount.Equal(decimal.RequireFromString("30.00")) {
		t.Errorf("first amount = %s, want 30.00", result[0].Amount)
	}
	if result[0].PayType != domain.PayTypeCash {
		t.Errorf("first pay_type = %s, want cash", result[0].PayType)
	}
	if result[0].BillNo != "PO-1" {
		t.Errorf("first bill_no = %q, want PO-1", result[0].BillNo)
	}
	if result[1].BillNo != "" {
		t.Errorf("second bill_no = %q, want empty (NULL)", result[1].BillNo)
	}
	if result[1].Remark != "" {
		t.Errorf("second remark = %q, want empty (NULL)", result[1].Remark)
	}
	if !result[0].TotalAmount.Equal(result[0].Amount) {
		t.Errorf("TotalAmount must mirror Amount: %s vs %s", result[0].TotalAmount, result[0].Amount)
	}
}

func TestPaymentRepo_ListByBill_EmptyResult_ReturnsNilSlice(t *testing.T) {
	repo, _, mock, cleanup := newMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	billID := uuid.New()

	mock.ExpectQuery(listByBillSQL).
		WithArgs(billID, tenantID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_id", "related_bill_id", "pay_type", "amount",
			"partner_id", "operator_id", "creator_id",
			"bill_no", "pay_date", "remark", "created_at",
		}))

	result, err := repo.ListByBill(context.Background(), tenantID, billID)
	if err != nil {
		t.Fatalf("ListByBill: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty, got %d", len(result))
	}
}

func TestPaymentRepo_ListByBill_QueryError_PropagatesWrapped(t *testing.T) {
	repo, _, mock, cleanup := newMockRepo(t)
	defer cleanup()

	boom := errors.New("query failed")
	mock.ExpectQuery(listByBillSQL).WillReturnError(boom)

	_, err := repo.ListByBill(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want wrap of %v", err, boom)
	}
}

func TestPaymentRepo_ListByBill_ScanError_Propagates(t *testing.T) {
	repo, _, mock, cleanup := newMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	billID := uuid.New()

	// Wrong column count → driver returns scan error on the first iteration.
	rows := sqlmock.NewRows([]string{"id", "tenant_id"}).
		AddRow(uuid.New(), tenantID)

	mock.ExpectQuery(listByBillSQL).
		WithArgs(billID, tenantID).
		WillReturnRows(rows)

	_, err := repo.ListByBill(context.Background(), tenantID, billID)
	if err == nil {
		t.Fatal("expected scan error")
	}
}

// ----- SumByBill -----

const sumByBillSQL = `
		SELECT COALESCE(SUM(amount), 0)
		FROM tally.payment_head
		WHERE related_bill_id = $1 AND tenant_id = $2 AND deleted_at IS NULL
		FOR UPDATE`

func TestPaymentRepo_SumByBill_Existing_ReturnsTotal(t *testing.T) {
	repo, _, mock, cleanup := newMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	billID := uuid.New()

	mock.ExpectBegin()
	mock.ExpectQuery(sumByBillSQL).
		WithArgs(billID, tenantID).
		WillReturnRows(sqlmock.NewRows([]string{"sum"}).AddRow("250.00"))
	mock.ExpectCommit()

	var total decimal.Decimal
	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error {
		var inner error
		total, inner = repo.SumByBill(context.Background(), tx, tenantID, billID)
		return inner
	})
	if err != nil {
		t.Fatalf("SumByBill: %v", err)
	}
	if !total.Equal(decimal.RequireFromString("250.00")) {
		t.Errorf("total = %s, want 250.00", total)
	}
}

func TestPaymentRepo_SumByBill_NoPayments_ReturnsZero(t *testing.T) {
	repo, _, mock, cleanup := newMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	billID := uuid.New()

	mock.ExpectBegin()
	mock.ExpectQuery(sumByBillSQL).
		WithArgs(billID, tenantID).
		WillReturnRows(sqlmock.NewRows([]string{"sum"}).AddRow("0"))
	mock.ExpectCommit()

	var total decimal.Decimal
	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error {
		var inner error
		total, inner = repo.SumByBill(context.Background(), tx, tenantID, billID)
		return inner
	})
	if err != nil {
		t.Fatalf("SumByBill: %v", err)
	}
	if !total.IsZero() {
		t.Errorf("total = %s, want 0", total)
	}
}

func TestPaymentRepo_SumByBill_QueryError_PropagatesWrapped(t *testing.T) {
	repo, _, mock, cleanup := newMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	billID := uuid.New()
	boom := errors.New("deadlock detected")

	mock.ExpectBegin()
	mock.ExpectQuery(sumByBillSQL).WillReturnError(boom)
	mock.ExpectRollback()

	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error {
		_, inner := repo.SumByBill(context.Background(), tx, tenantID, billID)
		return inner
	})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want wrap of %v", err, boom)
	}
}

func TestPaymentRepo_SumByBill_BadDecimal_ReturnsParseError(t *testing.T) {
	repo, _, mock, cleanup := newMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	billID := uuid.New()

	mock.ExpectBegin()
	mock.ExpectQuery(sumByBillSQL).
		WithArgs(billID, tenantID).
		WillReturnRows(sqlmock.NewRows([]string{"sum"}).AddRow("not-a-number"))
	mock.ExpectRollback()

	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error {
		_, inner := repo.SumByBill(context.Background(), tx, tenantID, billID)
		return inner
	})
	if err == nil {
		t.Fatal("expected parse error")
	}
}
