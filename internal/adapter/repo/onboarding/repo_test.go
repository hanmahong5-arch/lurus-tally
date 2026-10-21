package onboarding

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
)

// The clear path must remove everything that hangs off a demo bill (payments,
// invoice links, return / amendment / adoption pointers) before the bill, and
// keep demo products a real bill still uses out of the deletes and convert them last.
func TestDeleteDemoProducts_OrderAndScope(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	tid, kept := uuid.New(), uuid.New()

	mock.ExpectBegin()
	mock.ExpectExec(`CREATE TEMP TABLE demo_keep`).WithArgs(tid).WillReturnResult(sqlmock.NewResult(0, 0))
	steps := []string{
		`UPDATE tally\.bill_item SET link_item_id = NULL .*head_id IN`,
		`DELETE FROM tally\.payment_item .*related_bill_id IN`,
		`DELETE FROM tally\.payment_head .*related_bill_id IN`,
		`DELETE FROM tally\.invoice_bill .*bill_id IN`,
		`UPDATE tally\.bill_head SET link_bill_id = NULL`,
		`UPDATE tally\.bill_head SET amendment_of_id = NULL`,
		`UPDATE tally\.replenish_suggestion_log SET adopted_bill_id = NULL`,
		`DELETE FROM tally\.bill_head .*WITH RECURSIVE`,
		`CREATE TEMP TABLE demo_touched_heads`,
		`UPDATE tally\.bill_item SET link_item_id = NULL .*demo_touched_heads`,
		`WITH del AS \(DELETE FROM tally\.bill_item .*UPDATE tally\.bill_head h SET subtotal`,
		`UPDATE tally\.bill_item bi SET line_no = r\.rn`,
		`DELETE FROM tally\.stock_movement`,
		`DELETE FROM tally\.stock_lot`,
		`DELETE FROM tally\.stock_snapshot`,
		`DELETE FROM tally\.stock_initial`,
		`DELETE FROM tally\.stock_serial`,
		`DELETE FROM tally\.replenish_suggestion_log WHERE`,
		`DELETE FROM tally\.product_sku`,
		`DELETE FROM tally\.product WHERE`,
	}
	for _, s := range steps {
		mock.ExpectExec(s).WithArgs(tid).WillReturnResult(sqlmock.NewResult(0, 0))
	}
	mock.ExpectExec(`UPDATE tally\.product p SET code = substr\(p\.code, \$2\).*demo_keep`).WithArgs(tid, 6).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE tally\.product SET remark = \$2 .*demo_keep`).WithArgs(tid, ConvertedRemark).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT k\.id, p\.code, k\.name FROM demo_keep k JOIN tally\.product p`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "code", "name"}).AddRow(kept, "A1", "示例商品"))
	mock.ExpectCommit()

	res, err := New(db).ClearDemo(context.Background(), tid)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Converted) != 1 || res.Converted[0].ID != kept || res.Hint == "" {
		t.Errorf("res = %+v, want the kept product converted with a hint", res)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
