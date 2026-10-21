package bill_test

import (
	"context"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	repobill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/bill"
)

// 防的事故：采购退货的已退数量把销售退货算进去（或反过来），封顶就会错。
// 只认 采购退货、已审核、指向该采购单的行，按原采购行汇总。
func TestPurchaseReturnedQtyByLine_SQLScopesToPurchaseReturns(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	tenantID, poID, lineID := uuid.New(), uuid.New(), uuid.New()
	mock.ExpectQuery(`(?s)h\.sub_type = '采购退货' AND h\.status = 2 AND h\.deleted_at IS NULL`+
		`.*h\.link_bill_id = \$2 AND i\.link_item_id IS NOT NULL`+
		`.*GROUP BY i\.link_item_id`).
		WithArgs(tenantID, poID).
		WillReturnRows(sqlmock.NewRows([]string{"link_item_id", "sum"}).AddRow(lineID, "3.000"))

	got, err := repobill.New(db).PurchaseReturnedQtyByLine(context.Background(), tenantID, poID)
	if err != nil {
		t.Fatalf("PurchaseReturnedQtyByLine: %v", err)
	}
	if len(got) != 1 || got[lineID].String() != "3" {
		t.Fatalf("returned = %v, want 3 on the line", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
