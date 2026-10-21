package replenish

import (
	"context"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
)

// 锁定需求速度子查询的 SQL 形状：只算「单据仍存在」的销售出库，退货/盘点调整出库不算。
// 集成夹具里的销售流水必须指向真实 bill_head，否则会被当成已清除的样例单而跳过。
func TestListSuggestions_VelocityQueryShape(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	tenant := uuid.New()
	mock.ExpectQuery(`(?s)NOT IN \('return', 'adjust'\).*reference_type IS DISTINCT FROM 'sale' OR EXISTS.*FROM tally\.bill_head bh`).
		WithArgs(tenant).
		WillReturnRows(sqlmock.NewRows([]string{"product_id"}))
	if _, err := NewSQLSuggestionRepo(db).ListSuggestions(context.Background(), tenant); err != nil {
		t.Fatalf("ListSuggestions: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
