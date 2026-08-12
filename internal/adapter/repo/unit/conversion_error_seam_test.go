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

// TestGetConversionFactor_MissingPair_ErrorUnrecognisedByHandler
//
// 防的业务事故：用户在采购单/销售单某一行上选了一个该商品没有配置换算关系的单位
// （比如商品按"棵"建档，行上却选了"箱"）。审核时用户看到的是
// "服务器内部错误 500"，而不是本该出现的"该单位不适用于此商品，请检查单位配置"。
// 后果：用户不知道该改哪一行，只能反复重试；客服拿到的截图是一个 500，
// 定位要翻服务端日志；而且 500 会污染错误率告警，把一个用户输入问题伪装成故障。
//
// 根因是一条"假件比生产件更宽容"的缝：
//   - handler/bill/handler.go:211 靠 errors.Is(err, appbill.ErrInvalidUnitForProduct)
//     把这种情况映射成 422 + 可读文案；
//   - 单测里的 mockProductUnitRepo 直接返回 appbill.ErrInvalidUnitForProduct，所以永远走 422；
//   - 生产用的 repo/unit 却在 repo.go:141 自建了一个哨兵
//     `fmt.Errorf("unit: unit_id is not valid for this product")` —— 没有 %w，
//     不 wrap 任何东西，因此 errors.Is 恒为 false，真实请求一律落到 500 分支。
//     （repo.go:120 的文档注释写的是"Returns appbill.ErrInvalidUnitForProduct"，与实现不符。）
//
// 本用例绕开假件，直接驱动生产 repo：用 sqlmock 造出"product_unit 里查不到这一对"
// 的场景（空结果集 → sql.ErrNoRows），看它吐出来的错误能不能被 handler 认出来。
//
// ⚠️ characterization（现状钉桩）用例：断言的是缺陷现状。把 repo 的哨兵改成
// wrap 住 appbill.ErrInvalidUnitForProduct 之后本用例会失败，届时把断言反过来即可。
func TestGetConversionFactor_MissingPair_ErrorUnrecognisedByHandler(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	productID := uuid.New()
	unitID := uuid.New()

	// product_unit 里没有这一对 → 空结果集 → Scan 返回 sql.ErrNoRows，
	// 正是 repo.go:128 那条分支的触发条件。
	mock.ExpectQuery("SELECT conversion_factor FROM tally.product_unit").
		WithArgs(productID, unitID).
		WillReturnRows(sqlmock.NewRows([]string{"conversion_factor"}))

	repo := repounit.New(db)
	factor, err := repo.GetConversionFactor(context.Background(), productID, unitID)

	if err == nil {
		t.Fatalf("查不到换算关系却没有报错，返回 factor=%s —— 这会让审核按 0 或 1 结转", factor)
	}
	if !factor.IsZero() {
		t.Fatalf("出错时应返回零值，got %s", factor)
	}

	// 核心断言：handler 的分类判据在生产错误上失效。
	if errors.Is(err, appbill.ErrInvalidUnitForProduct) {
		t.Fatalf("缺陷已修复：生产 repo 的错误现在能被 handler 认出来了。"+
			"请把本 characterization 用例改成正向断言（必须 errors.Is 成立）。err=%v", err)
	}

	t.Logf("生产 repo 返回：%q；handler 的 errors.Is(err, ErrInvalidUnitForProduct) = false"+
		" ⇒ 走 httperr.WriteInternal ⇒ 用户看到 500 而不是 422", err.Error())

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock 期望未满足（说明查询没按预期发出）：%v", err)
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
