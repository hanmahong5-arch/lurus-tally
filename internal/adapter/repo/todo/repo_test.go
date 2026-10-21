package todo

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	appob "github.com/hanmahong5-arch/lurus-tally/internal/app/onboarding"
)

func newMock(t *testing.T) (*Repo, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return New(db), mock
}

// The query is the heart of the dashboard checklist: sample rows never count,
// only approved bills, real (non-credit) money, and opening stock counts as the
// purchase step. Scan order is product, purchase, sale, payment, demo.
func TestOnboarding_SQLShapeAndScanOrder(t *testing.T) {
	r, mock := newMock(t)
	tid := uuid.New()
	mock.ExpectQuery(`(?s)tally\.product.*remark IS DISTINCT FROM 'DEMO'.*remark IS DISTINCT FROM \$2.*code NOT LIKE 'DEMO-%'.*sub_type = '采购' AND status = 2.*reference_type = 'init'.*sub_type = '销售' AND status = 2.*pay_type <> 'credit'.*remark = 'DEMO'`).
		WithArgs(tid, appob.ConvertedRemark).
		WillReturnRows(sqlmock.NewRows([]string{"a", "b", "c", "d", "e"}).AddRow(true, false, true, false, true))
	o, err := r.Onboarding(context.Background(), tid)
	if err != nil {
		t.Fatal(err)
	}
	if !o.HasProduct || o.HasPurchase || !o.HasSale || o.HasPayment || !o.HasDemo {
		t.Fatalf("scan order wrong: %+v", o)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOnboarding_QueryErrorIsWrapped(t *testing.T) {
	r, mock := newMock(t)
	mock.ExpectQuery(`tally\.product`).WillReturnError(context.DeadlineExceeded)
	if _, err := r.Onboarding(context.Background(), uuid.New()); err == nil {
		t.Fatal("want error")
	}
}

func TestNegativeStock_SQLShapeAndTotal(t *testing.T) {
	r, mock := newMock(t)
	tid, pid := uuid.New(), uuid.New()
	mock.ExpectQuery(`(?s)tally\.stock_snapshot.*on_hand_qty < 0.*p\.deleted_at IS NULL.*w\.deleted_at IS NULL.*LIMIT \$2`).
		WithArgs(tid, 50).
		WillReturnRows(sqlmock.NewRows([]string{"a", "b", "c", "d", "e"}).
			AddRow(pid.String(), "矿泉水", "主仓", "-3.0000", 9))
	items, n, err := r.NegativeStock(context.Background(), tid, 50)
	if err != nil {
		t.Fatal(err)
	}
	if n != 9 || len(items) != 1 || items[0].OnHand != "-3" || items[0].WarehouseName != "主仓" || items[0].ProductID != pid {
		t.Fatalf("got %+v n=%d", items, n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestNegativeStock_QueryErrorIsWrapped(t *testing.T) {
	r, mock := newMock(t)
	mock.ExpectQuery(`tally\.stock_snapshot`).WillReturnError(context.DeadlineExceeded)
	if _, _, err := r.NegativeStock(context.Background(), uuid.New(), 50); err == nil {
		t.Fatal("want error")
	}
}
