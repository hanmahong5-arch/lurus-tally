package stock_test

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// newSnapshotWith 建一个"仓里有 10 件、单位成本 5 元"的起始账。
func newSnapshotWith(qty, cost int64) *domain.Snapshot {
	return &domain.Snapshot{
		TenantID:     testTenantID,
		ProductID:    testProductID,
		WarehouseID:  testWarehouseID,
		OnHandQty:    decimal.NewFromInt(qty),
		AvailableQty: decimal.NewFromInt(qty),
		UnitCost:     decimal.NewFromInt(cost),
		CostStrategy: domain.CostStrategyWAC,
	}
}

// TestRecordMovement_AdjustCannotDriveStockNegative
//
// 防的业务事故：盘点录错数（比如仓里 10 件却按 -50 件报盘亏），库存被打成负数。
// 负库存会顺着链路一路污染下去：可售数量为负、补货建议算出天文数字、
// 毛利报表的成本项变成负数。这是盘点场景里最常见的一次性误操作。
//
// 这条是正向不变量，当前成立；它守的是 calc_wac.go:98 / calc_fifo.go:157 的
// `newQty.IsNegative()` 闸门 —— 任何"简化 adjust 分支"的重构都必须先弄红它。
func TestRecordMovement_AdjustCannotDriveStockNegative(t *testing.T) {
	for _, strategy := range []string{"wac", "fifo"} {
		t.Run(strategy, func(t *testing.T) {
			repo := newMockRepo(newSnapshotWith(10, 5))
			calc := appstock.NewCalculator(stubProfile{strategy}, repo)
			uc := appstock.NewRecordMovementUseCase(repo, calc, nil, nil)

			_, err := uc.Execute(context.Background(), appstock.RecordMovementRequest{
				TenantID:      testTenantID,
				ProductID:     testProductID,
				WarehouseID:   testWarehouseID,
				Direction:     domain.DirectionAdjust,
				Qty:           decimal.NewFromInt(-50), // 盘亏 50，但仓里只有 10
				ReferenceType: domain.RefAdjust,
			})
			if err == nil {
				t.Fatal("盘亏超过在手数量却通过了 —— 库存会被打成负数")
			}
			if !appstock.IsInsufficientStock(err) {
				t.Fatalf("want InsufficientStockError, got %T: %v", err, err)
			}
			if got := repo.snapshot.OnHandQty; !got.Equal(decimal.NewFromInt(10)) {
				t.Fatalf("被拒绝后账面数量不应改变：got %s, want 10", got)
			}
		})
	}
}

// TestRecordMovement_NegativeOutQty_MintsStockAndNegativeCOGS
//
// 防的业务事故：一张出库单的数量被填成负数（前端漏校验、导入的 CSV 带负号、
// 或调用方把"退货"错写成"出库 -N"），系统不但不报错，反而**凭空造出库存**：
// 仓里 10 件，出库 -50 件之后账面变成 60 件。同时销售成本记成负数
// （-50 × 5 = -250），毛利被虚高 250 元。
//
// 这就是"负数铸币"在进销存侧的对应物：出库方向收到负数量时，
// `newQty = oldQty - qty` 变成了加法，而 ValidateMovement 只比较
// `qty > available`（-50 > 10 为假）所以一路放行。
//
// 目前的实际暴露面：HTTP 端点 POST /api/v1/stock/movements 没有对外注册
// （router 只挂了 GET，且有用例守着 POST 不暴露），所以还不能被租户直接触发；
// 但 RecordMovementUseCase 是所有出入库的公共入口，任何新调用方（导入、
// 开放 API、批量工具）只要传进一个负数就会中招，而且全链路零报错。
//
// ⚠️ characterization（现状钉桩）用例：断言的是缺陷现状。补上
// "qty 必须为正" 的入参校验后本用例会失败，届时改为断言拒绝。
func TestRecordMovement_NegativeOutQty_MintsStockAndNegativeCOGS(t *testing.T) {
	repo := newMockRepo(newSnapshotWith(10, 5))
	calc := appstock.NewCalculator(stubProfile{"wac"}, repo)
	uc := appstock.NewRecordMovementUseCase(repo, calc, nil, nil)

	snap, err := uc.Execute(context.Background(), appstock.RecordMovementRequest{
		TenantID:      testTenantID,
		ProductID:     testProductID,
		WarehouseID:   testWarehouseID,
		Direction:     domain.DirectionOut,
		Qty:           decimal.NewFromInt(-50),
		ReferenceType: domain.RefSale,
	})
	if err != nil {
		t.Fatalf("负数出库已被拒绝 —— 缺陷已修复，请把本 characterization 用例"+
			"改成正向断言（出库数量必须为正）：%v", err)
	}

	// 手算期望：oldQty=10, QtyBase=-50 → newQty = 10 - (-50) = 60。
	if want := decimal.NewFromInt(60); !snap.OnHandQty.Equal(want) {
		t.Fatalf("在手数量 = %s，本用例记录的缺陷现状是 %s（凭空多出 50 件）",
			snap.OnHandQty, want)
	}
	if len(repo.movements) != 1 {
		t.Fatalf("流水条数 = %d, want 1", len(repo.movements))
	}
	// 手算期望：TotalCost = QtyBase × oldCost = -50 × 5 = -250 → 负成本。
	gotCost := repo.movements[0].TotalCost
	if want := decimal.NewFromInt(-250); !gotCost.Equal(want) {
		t.Fatalf("销售成本 = %s，本用例记录的缺陷现状是 %s（负成本 → 毛利虚高）",
			gotCost, want)
	}
	t.Logf("出库 -50 件后：在手 10→%s 件，本笔销售成本 %s 元", snap.OnHandQty, gotCost)
}

// TestRecordMovement_NegativeInQty_DrivesSnapshotNegativeAndCorruptsWAC
//
// 防的业务事故：一张入库单的数量被填成负数（采购退货被错记成"入库 -N"）。
// 入库方向上 ValidateMovement 直接 return nil —— 一道校验都没有 ——
// 于是账面在手数量被打成负数（10 → -40），加权平均成本同时被算成负数
// （(10×5 + (-50)×8) / -40 = 8.75… 取决于数值，总之与真实成本无关）。
// 后果：该 SKU 的库存金额、毛利、补货建议全部失真，且没有任何报错或告警。
//
// 与上一条是同一个根因的另一半：Qty 没有正数校验，而 in 方向连
// "结果不得为负" 的兜底都没有（adjust 方向是有的）。
//
// ⚠️ characterization（现状钉桩）用例。
func TestRecordMovement_NegativeInQty_DrivesSnapshotNegativeAndCorruptsWAC(t *testing.T) {
	repo := newMockRepo(newSnapshotWith(10, 5))
	calc := appstock.NewCalculator(stubProfile{"wac"}, repo)
	uc := appstock.NewRecordMovementUseCase(repo, calc, nil, nil)

	snap, err := uc.Execute(context.Background(), appstock.RecordMovementRequest{
		TenantID:      testTenantID,
		ProductID:     testProductID,
		WarehouseID:   testWarehouseID,
		Direction:     domain.DirectionIn,
		Qty:           decimal.NewFromInt(-50),
		UnitCost:      decimal.NewFromInt(8),
		ReferenceType: domain.RefPurchase,
	})
	if err != nil {
		t.Fatalf("负数入库已被拒绝 —— 缺陷已修复，请把本 characterization 用例"+
			"改成正向断言（入库数量必须为正）：%v", err)
	}

	// 手算期望：newQty = 10 + (-50) = -40。
	if want := decimal.NewFromInt(-40); !snap.OnHandQty.Equal(want) {
		t.Fatalf("在手数量 = %s，本用例记录的缺陷现状是 %s（账面负库存）",
			snap.OnHandQty, want)
	}
	if !snap.OnHandQty.IsNegative() {
		t.Fatal("账面没有变成负数，结论需要重判")
	}
	// 手算期望：newCost = (10×5 + (-50)×8) / (-40) = (50 - 400) / -40 = 8.75。
	// 数字本身不重要 —— 重要的是它由一个负分母算出来，与真实成本毫无关系。
	if want := decimal.NewFromFloat(8.75); !snap.UnitCost.Equal(want) {
		t.Fatalf("加权平均成本 = %s，手算现状应为 %s", snap.UnitCost, want)
	}
	t.Logf("入库 -50 件后：在手 10→%s 件，加权平均成本 5→%s（由负分母算出）",
		snap.OnHandQty, snap.UnitCost)
}

// TestRecordMovement_NegativeOutQty_FIFO_MintsIntoOldestLot
//
// 防的业务事故：同一张负数出库单在 FIFO 租户身上后果更重 —— 它不只是把
// 快照数量加回去，还会把"凭空多出来的货"写进**最早那批**的剩余数量里。
// 于是这批不存在的货带着最早批次的成本价，之后每一次出库都会先按这个
// 错误成本结转，成本错误从此长期潜伏在批次台账里，盘点也看不出来。
//
// ⚠️ characterization（现状钉桩）用例。
func TestRecordMovement_NegativeOutQty_FIFO_MintsIntoOldestLot(t *testing.T) {
	repo := newMockRepo(newSnapshotWith(10, 5))
	calc := appstock.NewCalculator(stubProfile{"fifo"}, repo)
	uc := appstock.NewRecordMovementUseCase(repo, calc, nil, nil)
	ctx := context.Background()

	// 先正常入 10 件 @5 元，建出唯一一个批次。
	if _, err := uc.Execute(ctx, appstock.RecordMovementRequest{
		TenantID:      testTenantID,
		ProductID:     testProductID,
		WarehouseID:   testWarehouseID,
		Direction:     domain.DirectionIn,
		Qty:           decimal.NewFromInt(10),
		UnitCost:      decimal.NewFromInt(5),
		ReferenceType: domain.RefPurchase,
	}); err != nil {
		t.Fatalf("前置入库失败：%v", err)
	}
	if len(repo.lots) != 1 {
		t.Fatalf("批次数 = %d, want 1", len(repo.lots))
	}
	lotBefore := repo.lots[0].QtyRemaining

	// 再来一张 -4 件的出库单。
	if _, err := uc.Execute(ctx, appstock.RecordMovementRequest{
		TenantID:      testTenantID,
		ProductID:     testProductID,
		WarehouseID:   testWarehouseID,
		Direction:     domain.DirectionOut,
		Qty:           decimal.NewFromInt(-4),
		ReferenceType: domain.RefSale,
	}); err != nil {
		t.Fatalf("负数出库已被拒绝 —— 缺陷已修复，请改写本 characterization 用例：%v", err)
	}

	lotAfter := repo.lots[0].QtyRemaining
	if !lotAfter.GreaterThan(lotBefore) {
		t.Fatalf("批次剩余量 %s → %s：没有增加，结论需要重判", lotBefore, lotAfter)
	}
	// 手算期望：consume = min(QtyRemaining, -4) = -4，
	// newLotQty = QtyRemaining - consume = 10 - (-4) = 14 —— 减法变成了加法。
	if want := lotBefore.Add(decimal.NewFromInt(4)); !lotAfter.Equal(want) {
		t.Fatalf("批次剩余量 = %s, 手算现状应为 %s", lotAfter, want)
	}
	t.Logf("出库 -4 件后，最早批次剩余量 %s → %s（凭空多出 4 件、带最早批次成本）",
		lotBefore, lotAfter)
}
