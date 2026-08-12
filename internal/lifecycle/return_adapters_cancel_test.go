package lifecycle

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	appimporting "github.com/hanmahong5-arch/lurus-tally/internal/app/importing"
)

// TestImportReturnCreator_ShopifyCancelPath_ProductionWiringRejectsReversal
//
// 防的业务事故：电商租户在 Shopify 后台取消一张已经同步进 Tally 的订单后，
// 那批货永远回不到库存 —— 每一次 orders/cancelled 投递都以 HTTP 500 收场，
// Shopify 重试到放弃为止，而租户在 Tally 里看到的库存一直停在"已卖出"。
// 结果是系统里少货：补货建议偏高、盘点对不上、再售时才发现。
//
// 为什么现有单测看不见它：importing 包的用例测试用的是自带的
// mockReturnCreator，它忽略 Items 直接返回一张单号；而生产接线
// (lifecycle/app.go 的 importReturnCreator → bill.CreateReturnBillUseCase)
// 会在 len(Items)==0 时直接拒绝。假件和真件行为不一致，缺陷就落在缝里。
//
// 本用例把"生产接线"这一段单独拎出来跑：用 IngestCancelOrder 真实构造的入参
// (internal/app/importing/usecase.go:846 —— Items 恒为 nil，注释期望
// ReturnCreator 自己按 Remark 里的 original_bill_id 去克隆原单行) 调用
// 生产适配器，观察它到底接不接。
//
// ⚠️ 这是一条 characterization（现状钉桩）用例，记录的是缺陷现状而不是期望行为。
// 缺陷修好那天本用例会失败 —— 那时请把它改成正向断言（取消应当产出回库单），
// 而不是删掉断言了事。
func TestImportReturnCreator_ShopifyCancelPath_ProductionWiringRejectsReversal(t *testing.T) {
	// 与 lifecycle/app.go:708 完全一致的生产接线。
	// repo 传 nil 是安全的：CreateReturnBillUseCase 在碰任何 repo 方法之前
	// 就会先跑完 tenant/creator/items 三道入参校验（create_return.go:54-62）。
	creator := importReturnCreator{uc: appbill.NewCreateReturnBillUseCase(nil)}

	tenantID := uuid.New()
	creatorID := uuid.New()
	originalBillID := uuid.New()

	// 逐字复刻 IngestCancelOrder 构造的入参：Items 恒为 nil，
	// 原单行只以字符串形式藏在 Remark 里。
	in := appimporting.ReturnCreatorInput{
		TenantID:  tenantID,
		CreatorID: creatorID,
		BillDate:  time.Now().UTC(),
		Remark:    "cancel:shopify:#1001:original_bill_id=" + originalBillID.String(),
		Items:     nil,
	}

	out, err := creator.Create(context.Background(), in)

	if err == nil {
		t.Fatalf("缺陷已修复：生产接线现在能为 Shopify 取消单产出回库单 (bill_id=%v)。"+
			"请把本 characterization 用例改写成正向断言：取消应当克隆原单行并回补库存。", out)
	}
	if !errors.Is(err, appbill.ErrValidation) {
		t.Fatalf("拒绝原因变了：want ErrValidation, got %v", err)
	}
	if out != nil {
		t.Fatalf("被拒绝时不应返回单据，got %+v", out)
	}

	// 把因果链钉死：拒绝的理由就是"没有明细行"，而明细行永远不会有，
	// 因为 IngestCancelOrder 从不填 Items，生产适配器也从不按 Remark 去克隆原单。
	if got := err.Error(); got == "" {
		t.Fatal("错误信息为空，无法定位")
	}
	t.Logf("生产接线对 Shopify orders/cancelled 的实际返回：%v", err)
}

// TestImportReturnCreator_WithItems_Accepted 是上面那条的对照组：
// 一旦入参里真的带上了原单行，同一个生产适配器就会走到 repo 层。
// 它证明"被拒绝"的唯一原因是 Items 为空，而不是这个适配器本身不可用 ——
// 也就是说修复方向明确：让取消路径把原单行填进来。
func TestImportReturnCreator_WithItems_PassesValidationAndReachesRepo(t *testing.T) {
	creator := importReturnCreator{uc: appbill.NewCreateReturnBillUseCase(nil)}

	in := appimporting.ReturnCreatorInput{
		TenantID:  uuid.New(),
		CreatorID: uuid.New(),
		BillDate:  time.Now().UTC(),
		Remark:    "cancel:shopify:#1001",
		Items: []appimporting.SaleLineItem{
			{ProductID: uuid.New(), LineNo: 1, Qty: decimal.NewFromInt(3), UnitPrice: decimal.NewFromInt(20)},
		},
	}

	// repo 为 nil，所以一旦越过入参校验就会 panic —— 用 recover 把"越过了校验"
	// 这个事实变成可断言的信号，而不必为此引入一整套 BillRepo 假件。
	var passedValidation bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				passedValidation = true
			}
		}()
		if _, err := creator.Create(context.Background(), in); err != nil {
			if errors.Is(err, appbill.ErrValidation) {
				t.Fatalf("带明细行仍被入参校验拒绝，说明拒绝原因不止 Items 为空：%v", err)
			}
			passedValidation = true
		} else {
			passedValidation = true
		}
	}()

	if !passedValidation {
		t.Fatal("带明细行的取消回库单没有越过入参校验")
	}
}
