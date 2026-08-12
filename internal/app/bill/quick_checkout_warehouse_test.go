package bill_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
)

// TestQuickCheckout_MixedWarehouses_AllLinesDeductedFromFirstWarehouse
//
// 防的业务事故：多仓租户在收银台一单里扫了两个仓的货（门店仓 + 后仓，或
// 总仓 + 分仓）。Tally 把**所有行**都从第一行那个仓扣掉，第二个仓的账一动不动。
// 后果：仓 A 被多扣、仓 B 虚增，两个仓同时账实不符；而且下单成功、无任何提示，
// 只有下次盘点才会发现，届时已无法追溯是哪几单造成的。
//
// 根因：QuickCheckoutRequest 的每一行都带 SaleItem.WarehouseID（接口上看是支持分仓的），
// 但 assembleSaleItems (quick_checkout.go:147-176) 在拼 BillItem 时把这个字段丢了，
// 单据只保留一个头上的仓（取自 Items[0]），审核时 approve_sale.go:147
// 对每一行都用这个头上的仓。也就是说：**入参接受、语义丢弃、静默生效**。
//
// 顺带暴露的第二个口子：validateRefs 只校验 Items[0].WarehouseID 属不属于本租户，
// 后续行的仓 id 完全不过校验。本用例把 Items[1] 的仓设成"本租户不存在的仓"，
// 它照样一路绿灯（因为根本没人看它）。目前不构成越权写入（该 id 从未被使用），
// 但"接受了一个没校验过的租户外 id 且不报错"本身就是下一个缺陷的温床。
//
// ⚠️ characterization（现状钉桩）用例：断言的是缺陷现状。一旦改成"按行分仓记账"
// 或"混仓直接拒绝"，本用例会失败 —— 届时按新语义改写，不要删断言。
func TestQuickCheckout_MixedWarehouses_AllLinesDeductedFromFirstWarehouse(t *testing.T) {
	repo := newMockBillRepo()
	stockUC := newMockStockUC()
	unitRepo := newMockProductUnitRepo()
	payRepo := newMockPaymentRepo()

	warehouseA := uuid.New() // 本租户的门店仓
	warehouseB := uuid.New() // 第二行声明的仓；同时标记成"本租户查不到"
	repo.missingRefs = map[uuid.UUID]struct{}{warehouseB: {}}

	productA := uuid.New()
	productB := uuid.New()

	req := appbill.QuickCheckoutRequest{
		TenantID:      testTenantID,
		CreatorID:     testCreatorID,
		CustomerName:  "散客",
		PaymentMethod: "cash",
		PaidAmount:    decimal.NewFromInt(300),
		Items: []appbill.SaleItem{
			{ProductID: productA, WarehouseID: warehouseA, Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(100), LineNo: 1},
			{ProductID: productB, WarehouseID: warehouseB, Qty: decimal.NewFromInt(2), UnitPrice: decimal.NewFromInt(100), LineNo: 2},
		},
	}

	uc := newQuickCheckoutUC(repo, stockUC, unitRepo, payRepo)
	res, err := uc.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("混仓单被拒绝了 —— 缺陷已修复，请把本 characterization 用例"+
			"改成正向断言（混仓应拒绝，或应按行分仓扣减）：%v", err)
	}
	if res.BillID == uuid.Nil {
		t.Fatal("单据没建出来，结论需要重判")
	}

	if len(stockUC.calls) != 2 {
		t.Fatalf("出库流水条数 = %d, want 2（每行一条）", len(stockUC.calls))
	}

	// 核心断言：两行都记在仓 A 头上，仓 B 一件都没动。
	for i, call := range stockUC.calls {
		if call.WarehouseID != warehouseA {
			t.Fatalf("第 %d 行的出库仓 = %s，本用例记录的缺陷现状是全部记在首行仓 %s",
				i+1, call.WarehouseID, warehouseA)
		}
	}

	// 第二行声明的仓从未出现在任何一条流水里 —— 用户以为扣了 B 仓，实际没有。
	for _, call := range stockUC.calls {
		if call.WarehouseID == warehouseB {
			t.Fatal("仓 B 出现在流水里，结论需要重判")
		}
	}

	// 而且这个"本租户查不到"的仓 id 一路没被拦下来。
	t.Logf("两行分别声明仓 A/仓 B，实际两条出库流水都落在仓 A（%s）；"+
		"仓 B（%s，本租户不存在）既没被使用也没被校验拒绝", warehouseA, warehouseB)
}

// TestQuickCheckout_FirstLineWarehouseOutsideTenant_IsRejected
//
// 对照组：把**第一行**的仓换成本租户不存在的仓，validateRefs 就会拦下来。
// 它证明 quick_checkout 的跨租户校验确实存在且有效，只是覆盖面只有 Items[0] ——
// 也就是说上一条用例里的漏检是"覆盖不全"而不是"根本没做"。
// 这条是正向不变量，守的是 refs.go 的 WarehouseExists 前置校验。
func TestQuickCheckout_FirstLineWarehouseOutsideTenant_IsRejected(t *testing.T) {
	repo := newMockBillRepo()
	stockUC := newMockStockUC()
	unitRepo := newMockProductUnitRepo()
	payRepo := newMockPaymentRepo()

	foreignWarehouse := uuid.New()
	repo.missingRefs = map[uuid.UUID]struct{}{foreignWarehouse: {}}

	req := appbill.QuickCheckoutRequest{
		TenantID:      testTenantID,
		CreatorID:     testCreatorID,
		PaymentMethod: "cash",
		PaidAmount:    decimal.NewFromInt(100),
		Items: []appbill.SaleItem{
			{ProductID: uuid.New(), WarehouseID: foreignWarehouse, Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(100), LineNo: 1},
		},
	}

	uc := newQuickCheckoutUC(repo, stockUC, unitRepo, payRepo)
	if _, err := uc.Execute(context.Background(), req); err == nil {
		t.Fatal("首行引用了本租户不存在的仓却下单成功 —— 跨租户前置校验失效")
	}

	if len(stockUC.calls) != 0 {
		t.Fatalf("被拒绝时不应产生出库流水，got %d", len(stockUC.calls))
	}
	if len(payRepo.recorded) != 0 {
		t.Fatalf("被拒绝时不应产生收款记录，got %d", len(payRepo.recorded))
	}
}
