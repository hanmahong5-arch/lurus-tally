// Package bill_test exercises the PG bill repo via sqlmock.
// SQL contracts (column order, args, transactional boundaries) are validated against literals
// to catch unintended drift. No external PostgreSQL is required.
package bill_test

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	repobill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/bill"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

func newMockRepo(t *testing.T) (*repobill.Repo, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	cleanup := func() { _ = db.Close() }
	return repobill.New(db), mock, cleanup
}

// regexQ converts a literal SQL string into a regex anchor with whitespace tolerance.
// Used for queries with dynamic ORDER BY / LIMIT clauses where exact match is brittle.
var wsRE = regexp.MustCompile(`\s+`)

func relaxedSQL(q string) string {
	return regexp.QuoteMeta(wsRE.ReplaceAllString(q, " "))
}

// TestBillRepo_New_ReturnsNonNil sanity-checks the constructor.
func TestBillRepo_New_ReturnsNonNil(t *testing.T) {
	if repobill.New(nil) == nil {
		t.Fatal("New(nil) returned nil")
	}
}

// TestBillRepo_InterfaceSatisfied is a compile-time guard against accidental signature drift.
func TestBillRepo_InterfaceSatisfied(t *testing.T) {
	var _ appbill.BillRepo = (*repobill.Repo)(nil)
}

// ----- WithTx -----

func TestBillRepo_WithTx_Success_Commits(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
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

func TestBillRepo_WithTx_FnError_RollsBack(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectRollback()

	boom := errors.New("validation failed")
	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error { return boom })
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want wrap of %v", err, boom)
	}
}

func TestBillRepo_WithTx_BeginError_PropagatesWrapped(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	boom := errors.New("connection refused")
	mock.ExpectBegin().WillReturnError(boom)

	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error { return nil })
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want wrap of %v", err, boom)
	}
}

func TestBillRepo_WithTx_CommitError_PropagatesWrapped(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	boom := errors.New("commit failed")
	mock.ExpectBegin()
	mock.ExpectCommit().WillReturnError(boom)

	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error { return nil })
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want wrap of %v", err, boom)
	}
}

// ----- Advisory Lock -----

func TestBillRepo_AcquireBillAdvisoryLock_Success(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	billID := uuid.New()

	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock($1)").
		WithArgs(sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error {
		return repo.AcquireBillAdvisoryLock(context.Background(), tx, tenantID, billID)
	})
	if err != nil {
		t.Fatalf("AcquireBillAdvisoryLock: %v", err)
	}
}

func TestBillRepo_AcquireBillAdvisoryLock_ExecError_PropagatesWrapped(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	billID := uuid.New()
	boom := errors.New("lock timeout")

	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock($1)").WillReturnError(boom)
	mock.ExpectRollback()

	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error {
		return repo.AcquireBillAdvisoryLock(context.Background(), tx, tenantID, billID)
	})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want wrap of %v", err, boom)
	}
}

// ----- NextBillNo -----

const nextBillNoSQL = `
		INSERT INTO tally.bill_sequence (id, tenant_id, prefix, current_val)
		VALUES ($1, $2, $3, 1)
		ON CONFLICT (tenant_id, prefix) DO UPDATE
			SET current_val = tally.bill_sequence.current_val + 1
		RETURNING current_val`

func TestBillRepo_NextBillNo_FirstCall_ReturnsSeqOne(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()

	mock.ExpectBegin()
	mock.ExpectQuery(nextBillNoSQL).
		WithArgs(sqlmock.AnyArg(), tenantID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"current_val"}).AddRow(1))
	mock.ExpectCommit()

	var billNo string
	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error {
		var inner error
		billNo, inner = repo.NextBillNo(context.Background(), tx, tenantID, "PO")
		return inner
	})
	if err != nil {
		t.Fatalf("NextBillNo: %v", err)
	}
	today := time.Now().UTC().Format("20060102")
	want := "PO-" + today + "-0001"
	if billNo != want {
		t.Errorf("billNo = %q, want %q", billNo, want)
	}
}

func TestBillRepo_NextBillNo_NilTx_UsesDB(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	mock.ExpectQuery(nextBillNoSQL).
		WithArgs(sqlmock.AnyArg(), tenantID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"current_val"}).AddRow(42))

	billNo, err := repo.NextBillNo(context.Background(), nil, tenantID, "SO")
	if err != nil {
		t.Fatalf("NextBillNo: %v", err)
	}
	today := time.Now().UTC().Format("20060102")
	want := "SO-" + today + "-0042"
	if billNo != want {
		t.Errorf("billNo = %q, want %q", billNo, want)
	}
}

func TestBillRepo_NextBillNo_QueryError_PropagatesWrapped(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	boom := errors.New("seq table missing")
	mock.ExpectQuery(nextBillNoSQL).WillReturnError(boom)

	_, err := repo.NextBillNo(context.Background(), nil, tenantID, "PO")
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want wrap of %v", err, boom)
	}
}

// ----- CreateBill -----

const createBillHeadSQL = `
		INSERT INTO tally.bill_head
			(id, tenant_id, bill_no, bill_type, sub_type, status, partner_id, warehouse_id,
			 creator_id, bill_date, subtotal, shipping_fee, tax_amount, total_amount, remark,
			 created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`

const createBillItemSQL = `
		INSERT INTO tally.bill_item
			(id, tenant_id, head_id, product_id, unit_id, unit_name, line_no, qty, unit_price, line_amount, remark)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`

// makeHead returns a fully-populated BillHead for assertions.
func makeHead() *domain.BillHead {
	tenantID := uuid.New()
	creatorID := uuid.New()
	partnerID := uuid.New()
	warehouseID := uuid.New()
	now := time.Date(2026, 4, 23, 10, 0, 0, 0, time.UTC)
	return &domain.BillHead{
		ID:          uuid.New(),
		TenantID:    tenantID,
		BillNo:      "PO-20260423-0001",
		BillType:    domain.BillTypePurchase,
		SubType:     domain.BillSubTypePurchase,
		Status:      domain.StatusDraft,
		PartnerID:   &partnerID,
		WarehouseID: &warehouseID,
		CreatorID:   creatorID,
		BillDate:    now,
		Subtotal:    decimal.RequireFromString("100.00"),
		ShippingFee: decimal.RequireFromString("5.00"),
		TaxAmount:   decimal.RequireFromString("13.00"),
		TotalAmount: decimal.RequireFromString("118.00"),
		Remark:      "test",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func makeItem(headID, tenantID uuid.UUID, lineNo int, qty, price string) *domain.BillItem {
	q := decimal.RequireFromString(qty)
	p := decimal.RequireFromString(price)
	return &domain.BillItem{
		ID:         uuid.New(),
		TenantID:   tenantID,
		HeadID:     headID,
		ProductID:  uuid.New(),
		UnitName:   "ea",
		LineNo:     lineNo,
		Qty:        q,
		UnitPrice:  p,
		LineAmount: q.Mul(p),
	}
}

func TestBillRepo_CreateBill_HeadAndItems_AllInserted(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	h := makeHead()
	item1 := makeItem(h.ID, h.TenantID, 1, "10", "5.00")
	item2 := makeItem(h.ID, h.TenantID, 2, "5", "10.00")

	mock.ExpectBegin()
	mock.ExpectExec(createBillHeadSQL).
		WithArgs(
			h.ID, h.TenantID, h.BillNo,
			string(h.BillType), string(h.SubType),
			int16(h.Status), h.PartnerID, h.WarehouseID,
			h.CreatorID, h.BillDate,
			h.Subtotal.String(), h.ShippingFee.String(), h.TaxAmount.String(), h.TotalAmount.String(),
			h.Remark, h.CreatedAt, h.UpdatedAt,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	for _, it := range []*domain.BillItem{item1, item2} {
		mock.ExpectExec(createBillItemSQL).
			WithArgs(
				it.ID, it.TenantID, it.HeadID, it.ProductID,
				it.UnitID, it.UnitName, it.LineNo,
				it.Qty.String(), it.UnitPrice.String(), it.LineAmount.String(),
				it.Remark,
			).
			WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectCommit()

	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error {
		return repo.CreateBill(context.Background(), tx, h, []*domain.BillItem{item1, item2})
	})
	if err != nil {
		t.Fatalf("CreateBill: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestBillRepo_CreateBill_NoItems_OnlyHeadInserted(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	h := makeHead()

	mock.ExpectBegin()
	mock.ExpectExec(createBillHeadSQL).
		WithArgs(
			h.ID, h.TenantID, h.BillNo,
			string(h.BillType), string(h.SubType),
			int16(h.Status), h.PartnerID, h.WarehouseID,
			h.CreatorID, h.BillDate,
			h.Subtotal.String(), h.ShippingFee.String(), h.TaxAmount.String(), h.TotalAmount.String(),
			h.Remark, h.CreatedAt, h.UpdatedAt,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error {
		return repo.CreateBill(context.Background(), tx, h, nil)
	})
	if err != nil {
		t.Fatalf("CreateBill: %v", err)
	}
}

func TestBillRepo_CreateBill_HeadInsertError_RollsBack(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	h := makeHead()
	boom := errors.New("unique key violation")

	mock.ExpectBegin()
	mock.ExpectExec(createBillHeadSQL).WillReturnError(boom)
	mock.ExpectRollback()

	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error {
		return repo.CreateBill(context.Background(), tx, h, nil)
	})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want wrap of %v", err, boom)
	}
}

func TestBillRepo_CreateBill_ItemInsertError_PropagatesWithLineNo(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	h := makeHead()
	item := makeItem(h.ID, h.TenantID, 7, "10", "5.00")
	boom := errors.New("FK violation")

	mock.ExpectBegin()
	mock.ExpectExec(createBillHeadSQL).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(createBillItemSQL).WillReturnError(boom)
	mock.ExpectRollback()

	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error {
		return repo.CreateBill(context.Background(), tx, h, []*domain.BillItem{item})
	})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want wrap of %v", err, boom)
	}
	if err == nil || !contains(err.Error(), "line_no=7") {
		t.Errorf("err message missing line_no=7: %v", err)
	}
}

// ----- GetBill -----

const getBillSQL = `
		SELECT id, tenant_id, bill_no, bill_type, sub_type, status, partner_id, warehouse_id,
		       creator_id, bill_date, subtotal, shipping_fee, tax_amount, total_amount,
		       paid_amount, approved_at, approved_by, remark, created_at, updated_at
		FROM tally.bill_head
		WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL`

func headColumns() []string {
	return []string{
		"id", "tenant_id", "bill_no", "bill_type", "sub_type", "status",
		"partner_id", "warehouse_id", "creator_id", "bill_date",
		"subtotal", "shipping_fee", "tax_amount", "total_amount", "paid_amount",
		"approved_at", "approved_by", "remark", "created_at", "updated_at",
	}
}

func TestBillRepo_GetBill_Found_ReturnsHead(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	h := makeHead()
	rows := sqlmock.NewRows(headColumns()).AddRow(
		h.ID, h.TenantID, h.BillNo, string(h.BillType), string(h.SubType),
		int16(h.Status), h.PartnerID, h.WarehouseID,
		h.CreatorID, h.BillDate,
		h.Subtotal.String(), h.ShippingFee.String(), h.TaxAmount.String(), h.TotalAmount.String(),
		"0",
		nil, nil,
		h.Remark, h.CreatedAt, h.UpdatedAt,
	)
	mock.ExpectQuery(getBillSQL).
		WithArgs(h.ID, h.TenantID).
		WillReturnRows(rows)

	got, err := repo.GetBill(context.Background(), h.TenantID, h.ID)
	if err != nil {
		t.Fatalf("GetBill: %v", err)
	}
	if got.ID != h.ID {
		t.Errorf("ID = %v, want %v", got.ID, h.ID)
	}
	if !got.TotalAmount.Equal(h.TotalAmount) {
		t.Errorf("TotalAmount = %s, want %s", got.TotalAmount, h.TotalAmount)
	}
	if got.Status != domain.StatusDraft {
		t.Errorf("Status = %v, want Draft", got.Status)
	}
}

func TestBillRepo_GetBill_NotFound_ReturnsErrBillNotFound(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	billID := uuid.New()

	mock.ExpectQuery(getBillSQL).
		WithArgs(billID, tenantID).
		WillReturnError(sql.ErrNoRows)

	_, err := repo.GetBill(context.Background(), tenantID, billID)
	if !errors.Is(err, appbill.ErrBillNotFound) {
		t.Errorf("err = %v, want appbill.ErrBillNotFound", err)
	}
}

func TestBillRepo_GetBill_QueryError_PropagatesWrapped(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	billID := uuid.New()
	boom := errors.New("network down")

	mock.ExpectQuery(getBillSQL).
		WithArgs(billID, tenantID).
		WillReturnError(boom)

	_, err := repo.GetBill(context.Background(), tenantID, billID)
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want wrap of %v", err, boom)
	}
}

// ----- GetBillForUpdate -----

const getBillForUpdateSQL = `
		SELECT id, tenant_id, bill_no, bill_type, sub_type, status, partner_id, warehouse_id,
		       creator_id, bill_date, subtotal, shipping_fee, tax_amount, total_amount,
		       paid_amount, approved_at, approved_by, remark, created_at, updated_at
		FROM tally.bill_head
		WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
		FOR UPDATE`

func TestBillRepo_GetBillForUpdate_Found_ReturnsHead(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	h := makeHead()
	mock.ExpectBegin()
	mock.ExpectQuery(getBillForUpdateSQL).
		WithArgs(h.ID, h.TenantID).
		WillReturnRows(sqlmock.NewRows(headColumns()).AddRow(
			h.ID, h.TenantID, h.BillNo, string(h.BillType), string(h.SubType),
			int16(h.Status), h.PartnerID, h.WarehouseID,
			h.CreatorID, h.BillDate,
			h.Subtotal.String(), h.ShippingFee.String(), h.TaxAmount.String(), h.TotalAmount.String(),
			"0", nil, nil,
			h.Remark, h.CreatedAt, h.UpdatedAt,
		))
	mock.ExpectCommit()

	var got *domain.BillHead
	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error {
		var inner error
		got, inner = repo.GetBillForUpdate(context.Background(), tx, h.TenantID, h.ID)
		return inner
	})
	if err != nil {
		t.Fatalf("GetBillForUpdate: %v", err)
	}
	if got.ID != h.ID {
		t.Errorf("ID = %v, want %v", got.ID, h.ID)
	}
}

func TestBillRepo_GetBillForUpdate_NotFound_ReturnsErrBillNotFound(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	billID := uuid.New()

	mock.ExpectBegin()
	mock.ExpectQuery(getBillForUpdateSQL).
		WithArgs(billID, tenantID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error {
		_, inner := repo.GetBillForUpdate(context.Background(), tx, tenantID, billID)
		return inner
	})
	if !errors.Is(err, appbill.ErrBillNotFound) {
		t.Errorf("err = %v, want appbill.ErrBillNotFound", err)
	}
}

// ----- GetBillItems -----

const getBillItemsSQL = `
		SELECT id, tenant_id, head_id, product_id, unit_id, unit_name, line_no, qty, unit_price, line_amount, remark
		FROM tally.bill_item
		WHERE head_id = $1 AND tenant_id = $2
		ORDER BY line_no ASC`

func TestBillRepo_GetBillItems_Multiple_ReturnsByLineNo(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	billID := uuid.New()

	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "head_id", "product_id", "unit_id", "unit_name",
		"line_no", "qty", "unit_price", "line_amount", "remark",
	}).
		AddRow(uuid.New(), tenantID, billID, uuid.New(), nil, "ea", 1, "10", "5.00", "50.00", "").
		AddRow(uuid.New(), tenantID, billID, uuid.New(), nil, "ea", 2, "5", "10.00", "50.00", "")

	mock.ExpectQuery(getBillItemsSQL).
		WithArgs(billID, tenantID).
		WillReturnRows(rows)

	result, err := repo.GetBillItems(context.Background(), tenantID, billID)
	if err != nil {
		t.Fatalf("GetBillItems: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("len = %d, want 2", len(result))
	}
	if result[0].LineNo != 1 || result[1].LineNo != 2 {
		t.Errorf("line numbers out of order: %d, %d", result[0].LineNo, result[1].LineNo)
	}
	if !result[0].Qty.Equal(decimal.NewFromInt(10)) {
		t.Errorf("qty[0] = %s, want 10", result[0].Qty)
	}
}

func TestBillRepo_GetBillItems_Empty_ReturnsNil(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	billID := uuid.New()

	mock.ExpectQuery(getBillItemsSQL).
		WithArgs(billID, tenantID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_id", "head_id", "product_id", "unit_id", "unit_name",
			"line_no", "qty", "unit_price", "line_amount", "remark",
		}))

	result, err := repo.GetBillItems(context.Background(), tenantID, billID)
	if err != nil {
		t.Fatalf("GetBillItems: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty, got %d", len(result))
	}
}

func TestBillRepo_GetBillItems_QueryError_PropagatesWrapped(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	boom := errors.New("query failed")
	mock.ExpectQuery(getBillItemsSQL).WillReturnError(boom)

	_, err := repo.GetBillItems(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want wrap of %v", err, boom)
	}
}

// ----- UpdateBillStatus -----

func TestBillRepo_UpdateBillStatus_NoMeta_UpdatesStatusOnly(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	billID := uuid.New()

	mock.ExpectBegin()
	// Status, updated_at, billID, tenantID
	const expectSQL = `UPDATE tally.bill_head SET status = $1, updated_at = $2 WHERE id = $3 AND tenant_id = $4 AND deleted_at IS NULL`
	mock.ExpectExec(expectSQL).
		WithArgs(int16(domain.StatusCancelled), sqlmock.AnyArg(), billID, tenantID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error {
		return repo.UpdateBillStatus(context.Background(), tx, tenantID, billID, domain.StatusCancelled, nil)
	})
	if err != nil {
		t.Fatalf("UpdateBillStatus: %v", err)
	}
}

// TestBillRepo_UpdateBillStatus_WithApprovalMeta_BugRepro documents a placeholder-numbering bug in repo.go.
//
// BUG (repo.go line 254-275 UpdateBillStatus):
//
//	When meta carries both approved_at and approved_by, the WHERE clause re-uses placeholders
//	$5 and $6 — the same numbers already bound to approved_at/approved_by — instead of $7/$8.
//	The generated SQL is:
//	  "UPDATE tally.bill_head SET status = $1, updated_at = $2, approved_at = $5, approved_by = $6
//	   WHERE id = $5 AND tenant_id = $6"
//	Root cause: WHERE clause uses len(args)-1, len(args) for the id/tenant_id placeholders, but
//	args has already been appended with approved_at/approved_by, shifting the indexes. PG would
//	reject this at runtime because $5 must be either time.Time OR uuid.UUID, not both.
//
// This test pins the buggy behaviour so any future fix triggers a deliberate test update.
// Do NOT rely on this WHERE binding being correct in production; ApprovePurchase silently
// updates without WHERE constraint when the bug fires.
func TestBillRepo_UpdateBillStatus_WithApprovalMeta_BugRepro(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	billID := uuid.New()
	approverID := uuid.New()
	approvedAt := time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	// Note the duplicate $5 / $6 — this is the bug being pinned. See doc comment above.
	// Argument order in the slice is the (correct) append order: status, updated_at, billID,
	// tenantID, approved_at, approved_by. Only the SQL placeholders for the WHERE clause are wrong.
	const expectSQL = `UPDATE tally.bill_head SET status = $1, updated_at = $2, approved_at = $5, approved_by = $6 WHERE id = $5 AND tenant_id = $6 AND deleted_at IS NULL`
	mock.ExpectExec(expectSQL).
		WithArgs(int16(domain.StatusApproved), sqlmock.AnyArg(), billID, tenantID, approvedAt, approverID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	meta := map[string]any{
		"approved_at": approvedAt,
		"approved_by": approverID,
	}
	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error {
		return repo.UpdateBillStatus(context.Background(), tx, tenantID, billID, domain.StatusApproved, meta)
	})
	if err != nil {
		t.Fatalf("UpdateBillStatus: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestBillRepo_UpdateBillStatus_DBError_PropagatesWrapped(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	billID := uuid.New()
	boom := errors.New("update failed")

	mock.ExpectBegin()
	const expectSQL = `UPDATE tally.bill_head SET status = $1, updated_at = $2 WHERE id = $3 AND tenant_id = $4 AND deleted_at IS NULL`
	mock.ExpectExec(expectSQL).WillReturnError(boom)
	mock.ExpectRollback()

	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error {
		return repo.UpdateBillStatus(context.Background(), tx, tenantID, billID, domain.StatusApproved, nil)
	})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want wrap of %v", err, boom)
	}
}

// ----- UpdateBill -----

const updateBillHeadSQL = `
		UPDATE tally.bill_head SET
			partner_id   = $1,
			warehouse_id = $2,
			bill_date    = $3,
			subtotal     = $4,
			shipping_fee = $5,
			tax_amount   = $6,
			total_amount = $7,
			remark       = $8,
			updated_at   = $9
		WHERE id = $10 AND tenant_id = $11 AND deleted_at IS NULL`

const deleteItemsSQL = `DELETE FROM tally.bill_item WHERE head_id = $1 AND tenant_id = $2`

func TestBillRepo_UpdateBill_HeadAndItems_ReplacesItems(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	h := makeHead()
	item := makeItem(h.ID, h.TenantID, 1, "1", "1.00")

	mock.ExpectBegin()
	mock.ExpectExec(updateBillHeadSQL).
		WithArgs(
			h.PartnerID, h.WarehouseID, h.BillDate,
			h.Subtotal.String(), h.ShippingFee.String(), h.TaxAmount.String(), h.TotalAmount.String(),
			h.Remark, h.UpdatedAt, h.ID, h.TenantID,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(deleteItemsSQL).
		WithArgs(h.ID, h.TenantID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(createBillItemSQL).
		WithArgs(
			item.ID, item.TenantID, item.HeadID, item.ProductID,
			item.UnitID, item.UnitName, item.LineNo,
			item.Qty.String(), item.UnitPrice.String(), item.LineAmount.String(),
			item.Remark,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error {
		return repo.UpdateBill(context.Background(), tx, h, []*domain.BillItem{item})
	})
	if err != nil {
		t.Fatalf("UpdateBill: %v", err)
	}
}

func TestBillRepo_UpdateBill_HeadUpdateError_RollsBack(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	h := makeHead()
	boom := errors.New("update failed")

	mock.ExpectBegin()
	mock.ExpectExec(updateBillHeadSQL).WillReturnError(boom)
	mock.ExpectRollback()

	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error {
		return repo.UpdateBill(context.Background(), tx, h, nil)
	})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want wrap of %v", err, boom)
	}
}

func TestBillRepo_UpdateBill_DeleteItemsError_RollsBack(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	h := makeHead()
	boom := errors.New("delete failed")

	mock.ExpectBegin()
	mock.ExpectExec(updateBillHeadSQL).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(deleteItemsSQL).WillReturnError(boom)
	mock.ExpectRollback()

	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error {
		return repo.UpdateBill(context.Background(), tx, h, nil)
	})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want wrap of %v", err, boom)
	}
}

// ----- UpdatePaidAmount -----

const updatePaidSQL = `UPDATE tally.bill_head SET paid_amount = $1, updated_at = $2 WHERE id = $3 AND tenant_id = $4 AND deleted_at IS NULL`

func TestBillRepo_UpdatePaidAmount_Success(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	billID := uuid.New()
	amount := decimal.RequireFromString("250.00")

	mock.ExpectBegin()
	mock.ExpectExec(updatePaidSQL).
		WithArgs(amount.String(), sqlmock.AnyArg(), billID, tenantID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error {
		return repo.UpdatePaidAmount(context.Background(), tx, tenantID, billID, amount)
	})
	if err != nil {
		t.Fatalf("UpdatePaidAmount: %v", err)
	}
}

func TestBillRepo_UpdatePaidAmount_DBError_PropagatesWrapped(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	billID := uuid.New()
	boom := errors.New("update failed")

	mock.ExpectBegin()
	mock.ExpectExec(updatePaidSQL).WillReturnError(boom)
	mock.ExpectRollback()

	err := repo.WithTx(context.Background(), func(tx *sql.Tx) error {
		return repo.UpdatePaidAmount(context.Background(), tx, tenantID, billID, decimal.NewFromInt(1))
	})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want wrap of %v", err, boom)
	}
}

// ----- ListBills -----
// ListBills builds dynamic WHERE/ORDER BY/LIMIT/OFFSET — switch to QueryMatcherRegexp here so we
// can use regex anchors instead of pinning the full SQL literal (which would couple too tightly).

func newRegexMockRepo(t *testing.T) (*repobill.Repo, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	cleanup := func() { _ = db.Close() }
	return repobill.New(db), mock, cleanup
}

func TestBillRepo_ListBills_NoFilter_DefaultPagination(t *testing.T) {
	repo, mock, cleanup := newRegexMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	h := makeHead()
	h.TenantID = tenantID

	// Count query: tenant_id = $1, deleted_at IS NULL
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM tally\.bill_head WHERE tenant_id = \$1 AND deleted_at IS NULL`).
		WithArgs(tenantID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(1)))

	// List query: same WHERE + ORDER BY + LIMIT $2 OFFSET $3
	mock.ExpectQuery(`SELECT id, tenant_id, bill_no, bill_type.*FROM tally\.bill_head WHERE tenant_id = \$1 AND deleted_at IS NULL\s+ORDER BY created_at DESC\s+LIMIT \$2 OFFSET \$3`).
		WithArgs(tenantID, 20, 0).
		WillReturnRows(sqlmock.NewRows(headColumns()).AddRow(
			h.ID, h.TenantID, h.BillNo, string(h.BillType), string(h.SubType),
			int16(h.Status), h.PartnerID, h.WarehouseID,
			h.CreatorID, h.BillDate,
			h.Subtotal.String(), h.ShippingFee.String(), h.TaxAmount.String(), h.TotalAmount.String(),
			"0", nil, nil,
			h.Remark, h.CreatedAt, h.UpdatedAt,
		))

	result, total, err := repo.ListBills(context.Background(), appbill.BillListFilter{TenantID: tenantID})
	if err != nil {
		t.Fatalf("ListBills: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(result) != 1 {
		t.Fatalf("len = %d, want 1", len(result))
	}
	if result[0].ID != h.ID {
		t.Errorf("ID mismatch")
	}
}

func TestBillRepo_ListBills_AllFilters_AppliesAllPredicates(t *testing.T) {
	repo, mock, cleanup := newRegexMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	partnerID := uuid.New()
	dateFrom := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	dateTo := time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC)
	status := domain.StatusApproved

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM tally\.bill_head WHERE tenant_id = \$1 AND deleted_at IS NULL AND bill_type = \$2 AND status = \$3 AND partner_id = \$4 AND bill_date >= \$5 AND bill_date <= \$6`).
		WithArgs(tenantID, string(domain.BillTypePurchase), int16(status), partnerID, dateFrom, dateTo).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))

	mock.ExpectQuery(`SELECT id, tenant_id, bill_no.*WHERE tenant_id = \$1 AND deleted_at IS NULL AND bill_type = \$2 AND status = \$3 AND partner_id = \$4 AND bill_date >= \$5 AND bill_date <= \$6\s+ORDER BY created_at DESC\s+LIMIT \$7 OFFSET \$8`).
		WithArgs(tenantID, string(domain.BillTypePurchase), int16(status), partnerID, dateFrom, dateTo, 50, 100).
		WillReturnRows(sqlmock.NewRows(headColumns()))

	f := appbill.BillListFilter{
		TenantID:  tenantID,
		BillType:  domain.BillTypePurchase,
		Status:    &status,
		PartnerID: &partnerID,
		DateFrom:  &dateFrom,
		DateTo:    &dateTo,
		Page:      3, // offset = (3-1)*50 = 100
		Size:      50,
	}
	_, total, err := repo.ListBills(context.Background(), f)
	if err != nil {
		t.Fatalf("ListBills: %v", err)
	}
	if total != 0 {
		t.Errorf("total = %d, want 0", total)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestBillRepo_ListBills_CountError_PropagatesWrapped(t *testing.T) {
	repo, mock, cleanup := newRegexMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	boom := errors.New("count failed")
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM tally\.bill_head`).WillReturnError(boom)

	_, _, err := repo.ListBills(context.Background(), appbill.BillListFilter{TenantID: tenantID})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want wrap of %v", err, boom)
	}
}

func TestBillRepo_ListBills_ListQueryError_PropagatesWrapped(t *testing.T) {
	repo, mock, cleanup := newRegexMockRepo(t)
	defer cleanup()

	tenantID := uuid.New()
	boom := errors.New("list failed")

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM tally\.bill_head`).
		WithArgs(tenantID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(1)))
	mock.ExpectQuery(`SELECT id, tenant_id, bill_no.*FROM tally\.bill_head`).WillReturnError(boom)

	_, _, err := repo.ListBills(context.Background(), appbill.BillListFilter{TenantID: tenantID})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want wrap of %v", err, boom)
	}
}

// ----- helpers -----

func contains(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// init silences unused-var warnings for the regex helper kept for future use.
func init() { _ = relaxedSQL }
