package bill_test

import (
	"context"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	repobill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/bill"
)

// 防的事故：ReturnedQty 少认一种退货来源，取消就会把已退过的货再回一遍（双倍回库）。
// 这条 SQL 没有真库覆盖时，用正则锁住三个来源分支和两个过滤条件，任何一处被改掉都会红。
func TestReturnedQty_SQLCoversEveryReturnSourceAndFilter(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	tenantID, saleID, productID := uuid.New(), uuid.New(), uuid.New()
	mock.ExpectQuery(`(?s)h\.sub_type = '销售退货' AND h\.status = 2 AND h\.deleted_at IS NULL`+
		`.*h\.link_bill_id = \$2`+
		`.*h\.remark LIKE 'cancel:%:original_bill_id=' \|\| \$2::text`+
		`.*starts_with\(h\.remark, 'refund:' \|\| s\.platform \|\| ':' \|\| s\.platform_order_no \|\| ':refund_id='\)`+
		`.*GROUP BY i\.product_id, i\.unit_id`).
		WithArgs(tenantID, saleID).
		WillReturnRows(sqlmock.NewRows([]string{"product_id", "unit_id", "sum"}).AddRow(productID, nil, "2.500"))

	got, err := repobill.New(db).ReturnedQty(context.Background(), tenantID, saleID)
	if err != nil {
		t.Fatalf("ReturnedQty: %v", err)
	}
	if len(got) != 1 || got[0].ProductID != productID || got[0].UnitID != nil || got[0].Qty.String() != "2.5" {
		t.Fatalf("lines = %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 退货成本取原销售出库成本：只认该销售单、该商品、方向为出库的流水，按数量加权。
func TestSaleOutUnitCost_SQLScopesToTheSaleOutMovements(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	tenantID, saleID, productID := uuid.New(), uuid.New(), uuid.New()
	mock.ExpectQuery(`(?s)SUM\(total_cost\)::text, SUM\(qty_base\)::text.*FROM tally\.stock_movement`+
		`.*tenant_id = \$1 AND reference_type = 'sale' AND reference_id = \$2`+
		`.*product_id = \$3 AND direction = 'out'`).
		WithArgs(tenantID, saleID, productID).
		WillReturnRows(sqlmock.NewRows([]string{"c", "q"}).AddRow("30.00", "4"))

	cost, ok, err := repobill.New(db).SaleOutUnitCost(context.Background(), tenantID, saleID, productID)
	if err != nil || !ok {
		t.Fatalf("SaleOutUnitCost = %v, %v, %v", cost, ok, err)
	}
	if cost.String() != "7.5" {
		t.Errorf("unit cost = %s, want 7.5", cost)
	}

	// A sale that booked no stock-out yields ok=false (aggregate over zero rows is NULL).
	mock.ExpectQuery(`FROM tally\.stock_movement`).
		WillReturnRows(sqlmock.NewRows([]string{"c", "q"}).AddRow(nil, nil))
	if _, ok, err := repobill.New(db).SaleOutUnitCost(context.Background(), tenantID, saleID, productID); err != nil || ok {
		t.Errorf("empty aggregate: ok=%v err=%v, want ok=false", ok, err)
	}
}
