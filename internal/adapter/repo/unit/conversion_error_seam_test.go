package unit_test

import (
	"context"
	"errors"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	repounit "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/unit"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
)

// TestGetConversionFactor_NonBaseUnitWithoutRow_MatchesHandlerSentinel
//
// 防的业务事故：用户在采购单/销售单某一行上选了一个该商品没有配置换算关系的单位
// （比如商品按"棵"建档，行上却选了"箱"）。审核时必须得到 422 invalid_unit_for_product
// 和可读文案，而不是"服务器内部错误 500"。handler 靠
// errors.Is(err, appbill.ErrInvalidUnitForProduct) 分类，所以生产 repo 返回的错误必须能被它命中。
func TestGetConversionFactor_NonBaseUnitWithoutRow_MatchesHandlerSentinel(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	productID := uuid.New()
	unitID := uuid.New()

	mock.ExpectQuery("SELECT conversion_factor FROM tally.product_unit").
		WithArgs(productID, unitID).
		WillReturnRows(sqlmock.NewRows([]string{"conversion_factor"}))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(productID, unitID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	repo := repounit.New(db)
	factor, err := repo.GetConversionFactor(context.Background(), productID, unitID)

	if !errors.Is(err, appbill.ErrInvalidUnitForProduct) {
		t.Fatalf("err = %v, want errors.Is ErrInvalidUnitForProduct", err)
	}
	if !factor.IsZero() {
		t.Fatalf("出错时应返回零值，got %s", factor)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock 期望未满足：%v", err)
	}
}

// TestGetConversionFactor_BaseUnitWithoutRow_ReturnsOne
//
// 防的业务事故：商品的基本单位（建档时选的单位）没有 product_unit 记录，
// 网页端建单行上选了它，审核却失败。基本单位换算系数恒为 1，不需要记录。
func TestGetConversionFactor_BaseUnitWithoutRow_ReturnsOne(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	productID := uuid.New()
	unitID := uuid.New()

	mock.ExpectQuery("SELECT conversion_factor FROM tally.product_unit").
		WithArgs(productID, unitID).
		WillReturnRows(sqlmock.NewRows([]string{"conversion_factor"}))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(productID, unitID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	repo := repounit.New(db)
	factor, err := repo.GetConversionFactor(context.Background(), productID, unitID)

	if err != nil {
		t.Fatalf("GetConversionFactor: %v", err)
	}
	if factor.String() != "1" {
		t.Fatalf("factor = %s, want 1", factor)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock 期望未满足：%v", err)
	}
}

// TestGetConversionFactor_MalformedFactor_SwallowsParseErrorAndReturnsZero
//
// 防的业务事故：product_unit.conversion_factor 这一列被写脏了（历史导入、
// 人工改库、或将来某次 migration 留下的空串）。repo.go:135 明确忽略了解析错误
// （`f, _ := decimalutil.Parse(...)`），于是返回 0 且不报错。
//
// 好消息是下游有兜底：stock.convertToBase 见到 0 会返回 ErrInvalidUnitFactor，
// 审核失败而不是按 0 件入库 —— 这条链路目前是安全的。本用例把这个"看似危险
// 实则被兜住"的组合钉住：如果哪天有人放宽 convertToBase 的零值校验，
// 或者让 repo 的零值走到别的调用方，脏数据就会变成"入库 0 件、静默成功"。
func TestGetConversionFactor_MalformedFactor_SwallowsParseErrorAndReturnsZero(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	productID := uuid.New()
	unitID := uuid.New()

	mock.ExpectQuery("SELECT conversion_factor FROM tally.product_unit").
		WithArgs(productID, unitID).
		WillReturnRows(sqlmock.NewRows([]string{"conversion_factor"}).AddRow("not-a-number"))

	repo := repounit.New(db)
	factor, err := repo.GetConversionFactor(context.Background(), productID, unitID)

	if err != nil {
		t.Fatalf("解析错误已被上报 —— 缺陷已修复，请把本 characterization 用例"+
			"改成正向断言（脏数据必须报错）：%v", err)
	}
	if !factor.IsZero() {
		t.Fatalf("脏数据的解析结果 = %s, 现状应为 0", factor)
	}
	t.Log(`conversion_factor = "not-a-number" 被静默吞成 0；` +
		`当前靠 stock.convertToBase 的零值校验兜住（审核会失败而非按 0 件结转）`)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock 期望未满足：%v", err)
	}
}
