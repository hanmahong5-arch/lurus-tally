package product

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/product"
)

// 字母数字条码查询:SQL 含 barcode 精确条件,参数为原始串与带 % 的模糊串。
func TestList_QueryIncludesBarcodeExactMatch(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()
	tid := uuid.New()

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM tally.product WHERE .*attributes->>'barcode' = \$3`).
		WithArgs(tid, "%ABC123456%", "ABC123456").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`attributes->>'barcode' = \$3.*LIMIT \$4 OFFSET \$5`).
		WithArgs(tid, "%ABC123456%", "ABC123456", 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	_, total, err := New(db).List(context.Background(), domain.ListFilter{
		TenantID: tid, Query: "ABC123456", Limit: 20,
	})
	if err != nil || total != 0 {
		t.Fatalf("total=%d err=%v", total, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
