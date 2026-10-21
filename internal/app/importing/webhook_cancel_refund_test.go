package importing_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appimporting "github.com/hanmahong5-arch/lurus-tally/internal/app/importing"
)

// TestShopifyRefund_ExceedingOriginalSoldQty_IsAccepted
//
// 防的业务事故：Shopify 侧一张卖 2 件的订单，退款 webhook 报 5 件（人工改数、
// 平台数据错乱、或恶意构造的重放）。Tally 照单全收，直接给仓库加 5 件不存在的货。
// 后果：库存凭空多出 3 件 → 系统显示有货、实际发不出，客服和仓库对着系统吵架。
//
// 这是"负数铸币"在进销存侧的同族：退货入库是唯一一条"外部输入直接增加资产"的路径，
// 却没有任何"退货量 ≤ 原销量"的封顶。
//
// ⚠️ characterization 用例：记录现状。加上封顶校验后本用例会失败，届时改为断言拒绝。
func TestShopifyRefund_ExceedingOriginalSoldQty_IsAccepted(t *testing.T) {
	repo := newMockRepo()
	creator := &mockCreator{}
	approver := &mockApprover{}
	retCreator := &mockReturnCreator{}
	retApprover := &mockReturnApprover{}
	checker := newMockStockChecker()
	rater := newMockRater()

	tenantID := mustUUID(t)
	creatorID := mustUUID(t)
	warehouseID := mustUUID(t)
	productID := mustUUID(t)
	repo.addMapping("shopify", "SKU-1", productID)

	uc := buildUseCaseWithReturn(repo, creator, approver, retCreator, retApprover, checker, rater)
	ctx := context.Background()

	const orderNo = "#2002"

	// 原单只卖了 2 件。
	if _, skipped, err := uc.IngestSingleOrder(ctx, appimporting.SingleOrderRequest{
		TenantID:        tenantID,
		CreatorID:       creatorID,
		WarehouseID:     warehouseID,
		Platform:        appimporting.PlatformShopify,
		PlatformOrderNo: orderNo,
		Lines: []appimporting.OrderRow{{
			PlatformOrderNo: orderNo,
			PlatformSKU:     "SKU-1",
			Qty:             decimal.NewFromInt(2),
			UnitPrice:       decimal.NewFromInt(50),
			Currency:        "CNY",
			OrderDate:       time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		}},
	}); err != nil || skipped != nil {
		t.Fatalf("前置条件没建立：err=%v skipped=%v", err, skipped)
	}

	// 退款却报了 5 件。
	res, err := uc.IngestRefund(ctx, appimporting.RefundRequest{
		TenantID:         tenantID,
		CreatorID:        creatorID,
		WarehouseID:      warehouseID,
		Platform:         appimporting.PlatformShopify,
		PlatformOrderNo:  orderNo,
		PlatformRefundID: "refund-2002",
		Currency:         "CNY",
		RefundDate:       time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC),
		Lines: []appimporting.RefundLine{{
			PlatformSKU:  "SKU-1",
			Qty:          decimal.NewFromInt(5),
			RefundAmount: decimal.NewFromInt(50),
		}},
	})
	if err != nil {
		t.Fatalf("退货超量已被拒绝 —— 缺陷已修复，请把本 characterization 用例改成"+
			"正向断言（超过原销量必须拒绝）：%v", err)
	}
	if res.BillID == uuid.Nil {
		t.Fatal("退款单没建出来，结论需要重判")
	}

	if len(retCreator.created) != 1 {
		t.Fatalf("退货入库单数 = %d, want 1", len(retCreator.created))
	}
	gotQty := retCreator.created[0].Items[0].Qty
	if !gotQty.Equal(decimal.NewFromInt(5)) {
		t.Fatalf("入库数量 = %s，期望原样收下 5（记录现状）", gotQty)
	}
	t.Logf("原单卖出 2 件，退货 webhook 报 5 件被原样入库 —— 净多出 3 件不存在的货")
}
