package importing_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appimporting "github.com/hanmahong5-arch/lurus-tally/internal/app/importing"
)

// TestShopifyCancelThenRefund_SameOrder_RestocksTwice
//
// 防的业务事故：客人在 Shopify 上取消一张已付款的订单，商家在后台点"取消并退款"。
// Shopify 会为这一个动作先后投两条 webhook —— orders/cancelled 和 refunds/create。
// Tally 对这两条分别建了一张"销售退货入库单"并各自审核通过，同一批货于是回补了两次。
// 后果：库存虚增一倍 → 补货建议偏低、可售数量虚高 → 承诺发货但仓里没货（超卖）；
// 财务侧同一笔退货入了两次成本。整条链路没有任何报错，租户只有盘点时才会发现。
//
// 根因（不在本轮修改范围内，见报告）：两条路径各用各的去重命名空间 ——
// IngestCancelOrder 查 IsCancelSeen(orderNo)，IngestRefund 查 IsRefundSeen(refundID) ——
// 谁也不看对方，也没有"同一张原单累计退货量不得超过原销量"的封顶校验。
//
// ⚠️ characterization（现状钉桩）用例：断言的是缺陷现状。修好那天它会失败，
// 届时请把期望值从 2 改成 1，而不是删掉断言。
func TestShopifyCancelThenRefund_SameOrder_RestocksTwice(t *testing.T) {
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

	const orderNo = "#1001"
	const soldQty = 3

	// ---- 1. orders/create：一张卖出 3 件的订单进来，扣 3 件库存 -------------
	imported, skipped, err := uc.IngestSingleOrder(ctx, appimporting.SingleOrderRequest{
		TenantID:        tenantID,
		CreatorID:       creatorID,
		WarehouseID:     warehouseID,
		Platform:        appimporting.PlatformShopify,
		PlatformOrderNo: orderNo,
		Lines: []appimporting.OrderRow{{
			PlatformOrderNo: orderNo,
			PlatformSKU:     "SKU-1",
			Qty:             decimal.NewFromInt(soldQty),
			UnitPrice:       decimal.NewFromInt(50),
			Currency:        "CNY",
			OrderDate:       time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		}},
	})
	if err != nil {
		t.Fatalf("IngestSingleOrder: %v", err)
	}
	if skipped != nil {
		t.Fatalf("订单被跳过，前置条件没建立：%s", skipped.Reason)
	}
	if imported.BillID == uuid.Nil {
		t.Fatal("销售单没建出来，前置条件没建立")
	}
	if len(approver.approved) != 1 {
		t.Fatalf("销售出库应审核 1 次，got %d", len(approver.approved))
	}

	// ---- 2. orders/cancelled：整单取消 → 回库单 #1 -------------------------
	cancelRes, err := uc.IngestCancelOrder(ctx, appimporting.CancelRequest{
		TenantID:        tenantID,
		CreatorID:       creatorID,
		Platform:        appimporting.PlatformShopify,
		PlatformOrderNo: orderNo,
	})
	if err != nil {
		t.Fatalf("IngestCancelOrder: %v", err)
	}
	if cancelRes.ReversalBillID == uuid.Nil {
		t.Fatal("取消没有产出冲销单")
	}
	if len(retApprover.approved) != 1 {
		t.Fatalf("取消后回库单应为 1 张，got %d", len(retApprover.approved))
	}

	// ---- 3. refunds/create：同一张订单、同样 3 件的退款 → 回库单 #2 --------
	// 这条 webhook 由同一个"取消并退款"动作触发，退的是同一批货。
	refundRes, err := uc.IngestRefund(ctx, appimporting.RefundRequest{
		TenantID:         tenantID,
		CreatorID:        creatorID,
		WarehouseID:      warehouseID,
		Platform:         appimporting.PlatformShopify,
		PlatformOrderNo:  orderNo,
		PlatformRefundID: "refund-9001",
		Currency:         "CNY",
		RefundDate:       time.Date(2026, 1, 16, 0, 0, 0, 0, time.UTC),
		Lines: []appimporting.RefundLine{{
			PlatformSKU:  "SKU-1",
			Qty:          decimal.NewFromInt(soldQty),
			RefundAmount: decimal.NewFromInt(50),
		}},
	})
	if err != nil {
		t.Fatalf("IngestRefund 被拒绝了 —— 若这是新增的跨命名空间去重，请更新本用例：%v", err)
	}
	if refundRes.BillID == uuid.Nil {
		t.Fatal("退款没有产出回库单")
	}

	// ---- 断言：同一批货被回补了两次 ---------------------------------------
	const wantRestocksToday = 2 // 业务上正确的值是 1
	if got := len(retApprover.approved); got != wantRestocksToday {
		t.Fatalf("同一张订单的回库审核次数 = %d，本用例记录的缺陷现状是 %d。"+
			"若已修复（应为 1），请把期望值改成 1 并删掉这段说明。", got, wantRestocksToday)
	}

	// 两张回库单是不同的单据，说明确实是两笔独立的入库，而不是同一张被重放。
	if retApprover.approved[0] == retApprover.approved[1] {
		t.Fatal("两次审核指向同一张单，那就不是重复入库了 —— 结论需要重判")
	}

	// 把回补总量算出来，让事故规模可读：卖出 3 件，回补 3+3=6 件。
	var restocked decimal.Decimal
	for _, in := range retCreator.created {
		for _, it := range in.Items {
			restocked = restocked.Add(it.Qty)
		}
	}
	// 取消路径的 Items 恒为 nil（见 lifecycle 侧用例），所以这里只统计得到退款那 3 件；
	// 真实生产中取消路径若被修好，两条链路加总就是 6。
	t.Logf("卖出 %d 件；两条回库链路各建了一张单（取消单不带明细行、退款单带 %s 件）",
		soldQty, restocked)
}

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
