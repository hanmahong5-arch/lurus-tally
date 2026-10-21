package bill_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domainpayment "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// poRepo gives the mock repo the link and returned-quantity capabilities the production
// repo has for purchase returns, derived from the approved linked returns in the store.
type poRepo struct {
	*mockBillRepo
	linked    map[uuid.UUID]uuid.UUID
	itemLinks map[uuid.UUID]uuid.UUID
}

func (r poRepo) SetLinkBill(_ context.Context, _ *sql.Tx, _, billID, linked uuid.UUID) error {
	r.linked[billID] = linked
	return nil
}

func (r poRepo) SetLinkItems(_ context.Context, _ *sql.Tx, _, _ uuid.UUID, links map[uuid.UUID]uuid.UUID) error {
	for k, v := range links {
		r.itemLinks[k] = v
	}
	return nil
}

func (r poRepo) PurchaseReturnedQtyByLine(_ context.Context, _, poID uuid.UUID) (map[uuid.UUID]decimal.Decimal, error) {
	out := map[uuid.UUID]decimal.Decimal{}
	for id, h := range r.billsByID {
		if h.SubType != domain.BillSubTypePurchaseReturn || h.Status != domain.StatusApproved || r.linked[id] != poID {
			continue
		}
		for _, it := range r.itemsByBillID[id] {
			if l, ok := r.itemLinks[it.ID]; ok {
				out[l] = out[l].Add(it.Qty)
			}
		}
	}
	return out, nil
}

type poFixture struct {
	repo    poRepo
	stockUC *mockStockUC
	pay     *mockPaymentRepo
	uc      *appbill.PurchaseReturnUseCase
	poID    uuid.UUID
	lineID  uuid.UUID
	product uuid.UUID
}

// newPOFixture seeds an approved purchase of 10 units at 8.00 (total 80.00) with the
// given amount already paid.
func newPOFixture(paid string) *poFixture {
	repo := poRepo{mockBillRepo: newMockBillRepo(), linked: map[uuid.UUID]uuid.UUID{}, itemLinks: map[uuid.UUID]uuid.UUID{}}
	pid, poID, lineID, wh, partner := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	repo.billsByID[poID] = &domain.BillHead{
		ID: poID, TenantID: testTenantID, BillNo: "PO-20261010-0001",
		BillType: domain.BillTypePurchase, SubType: domain.BillSubTypePurchase, Status: domain.StatusApproved,
		WarehouseID: &wh, PartnerID: &partner,
		TotalAmount: decimal.RequireFromString("80.00"), PaidAmount: decimal.RequireFromString(paid),
	}
	repo.itemsByBillID[poID] = []*domain.BillItem{{
		ID: lineID, HeadID: poID, ProductID: pid, LineNo: 1,
		Qty: decimal.NewFromInt(10), UnitPrice: decimal.RequireFromString("8.00"),
	}}
	stockUC, pay := newMockStockUC(), newMockPaymentRepo()
	return &poFixture{
		repo: repo, stockUC: stockUC, pay: pay, poID: poID, lineID: lineID, product: pid,
		uc: appbill.NewPurchaseReturnUseCase(repo, stockUC).WithPayments(pay),
	}
}

func (f *poFixture) ret(qty int64) (*appbill.PurchaseReturnOutput, error) {
	return f.uc.Execute(context.Background(), appbill.PurchaseReturnRequest{
		TenantID: testTenantID, CreatorID: testCreatorID, PurchaseBillID: f.poID,
		Items: []appbill.PurchaseReturnLine{{BillItemID: f.lineID, Qty: decimal.NewFromInt(qty)}},
	})
}

// 采购 10 件退 3 件：库存出 3，应付减 24（未付 80 -> 56），单据已审核并指回原采购单。
func TestPurchaseReturn_PartialReducesStockAndPayable(t *testing.T) {
	f := newPOFixture("0")
	out, err := f.ret(3)
	if err != nil {
		t.Fatalf("return 3: %v", err)
	}
	if !out.ReturnAmount.Equal(decimal.NewFromInt(24)) {
		t.Errorf("return amount = %s, want 24", out.ReturnAmount)
	}
	if len(f.stockUC.calls) != 1 {
		t.Fatalf("movements = %d, want 1", len(f.stockUC.calls))
	}
	m := f.stockUC.calls[0]
	if m.Direction != domainstock.DirectionOut || !m.Qty.Equal(decimal.NewFromInt(3)) || m.ReferenceType != domainstock.RefReturn {
		t.Errorf("movement = %+v, want stock-out of 3 with reference_type return", m)
	}
	head := f.repo.billsByID[out.BillID]
	if head.Status != domain.StatusApproved || head.SubType != domain.BillSubTypePurchaseReturn || head.BillType != domain.BillTypeSale {
		t.Errorf("return bill = %+v, want approved 出库/采购退货", head)
	}
	if f.repo.linked[out.BillID] != f.poID {
		t.Errorf("return bill not linked to the purchase")
	}
	po := f.repo.billsByID[f.poID]
	unpaid := po.TotalAmount.Sub(po.PaidAmount)
	if !unpaid.Equal(decimal.NewFromInt(56)) {
		t.Errorf("payable after return = %s, want 56", unpaid)
	}
	if !out.PayableReduced.Equal(decimal.NewFromInt(24)) || !out.SupplierOwes.IsZero() {
		t.Errorf("reduced=%s owes=%s, want 24 / 0", out.PayableReduced, out.SupplierOwes)
	}
	if len(f.pay.recorded) != 1 || f.pay.recorded[0].PayType != domainpayment.PayTypeCredit || f.pay.recorded[0].BillID != f.poID {
		t.Errorf("offset payment = %+v, want one credit payment on the purchase", f.pay.recorded)
	}
}

// 已付 70、未付 10：退 3 件（24）只能冲抵未付的 10，其余 14 是供应商该退给我们的钱，不能记成已付。
func TestPurchaseReturn_OffsetCappedAtUnpaid(t *testing.T) {
	f := newPOFixture("70.00")
	out, err := f.ret(3)
	if err != nil {
		t.Fatalf("return: %v", err)
	}
	if !out.PayableReduced.Equal(decimal.NewFromInt(10)) || !out.SupplierOwes.Equal(decimal.NewFromInt(14)) {
		t.Errorf("reduced=%s owes=%s, want 10 / 14", out.PayableReduced, out.SupplierOwes)
	}
	po := f.repo.billsByID[f.poID]
	if !po.PaidAmount.Equal(decimal.NewFromInt(80)) {
		t.Errorf("paid = %s, want 80 (never above the total)", po.PaidAmount)
	}
}

// 防的事故：同一行分两次退，合计超过原采购数量；第二次必须按剩余数量拒绝，且不动库存。
func TestPurchaseReturn_CapsPerLineAcrossReturns(t *testing.T) {
	f := newPOFixture("0")
	if _, err := f.ret(7); err != nil {
		t.Fatalf("first return: %v", err)
	}
	_, err := f.ret(4)
	var over *appbill.ReturnExceededError
	if !errors.As(err, &over) || !over.Remaining.Equal(decimal.NewFromInt(3)) {
		t.Fatalf("err = %v, want ReturnExceededError with 3 left", err)
	}
	if len(f.stockUC.calls) != 1 {
		t.Errorf("movements = %d, want only the first return's", len(f.stockUC.calls))
	}
}

// 草稿或已作废的采购单没入过库，没有东西可退。
func TestPurchaseReturn_RejectsNonApprovedPurchase(t *testing.T) {
	f := newPOFixture("0")
	f.repo.billsByID[f.poID].Status = domain.StatusDraft
	if _, err := f.ret(1); !errors.Is(err, appbill.ErrPurchaseNotReturnable) {
		t.Fatalf("err = %v, want ErrPurchaseNotReturnable", err)
	}
}

// 防的事故：把销售单当采购单退，库存反而被扣。
func TestPurchaseReturn_RejectsSaleBill(t *testing.T) {
	f := newPOFixture("0")
	f.repo.billsByID[f.poID].BillType = domain.BillTypeSale
	f.repo.billsByID[f.poID].SubType = domain.BillSubTypeSale
	if _, err := f.ret(1); !errors.Is(err, appbill.ErrPurchaseNotReturnable) {
		t.Fatalf("err = %v, want ErrPurchaseNotReturnable", err)
	}
}

func TestPurchaseReturn_RejectsForeignLine(t *testing.T) {
	f := newPOFixture("0")
	_, err := f.uc.Execute(context.Background(), appbill.PurchaseReturnRequest{
		TenantID: testTenantID, CreatorID: testCreatorID, PurchaseBillID: f.poID,
		Items: []appbill.PurchaseReturnLine{{BillItemID: uuid.New(), Qty: decimal.NewFromInt(1)}},
	})
	if !errors.Is(err, appbill.ErrReturnItemNotFound) {
		t.Fatalf("err = %v, want ErrReturnItemNotFound", err)
	}
}

// 货已经卖出去一部分，库存不够退：错误原样交给上层，不记冲抵付款。
func TestPurchaseReturn_InsufficientStockFailsWholeReturn(t *testing.T) {
	f := newPOFixture("0")
	f.stockUC.failOnIdx = 0
	f.stockUC.failErr = &appstock.InsufficientStockError{ProductID: f.product, Available: decimal.NewFromInt(1), Requested: decimal.NewFromInt(3)}
	_, err := f.ret(3)
	var short *appstock.InsufficientStockError
	if !errors.As(err, &short) {
		t.Fatalf("err = %v, want insufficient stock", err)
	}
	if len(f.pay.recorded) != 0 {
		t.Errorf("payments = %d, want none after a failed return", len(f.pay.recorded))
	}
}

// 辅助单位（箱）的退货行要带换算系数出库，否则 2 箱会只扣 2 个。
func TestPurchaseReturn_AuxiliaryUnitUsesConversionFactor(t *testing.T) {
	f := newPOFixture("0")
	unitID := uuid.New()
	f.repo.itemsByBillID[f.poID][0].UnitID = &unitID
	units := newMockProductUnitRepo()
	units.set(f.product, unitID, decimal.NewFromInt(12))
	f.uc = appbill.NewPurchaseReturnUseCase(f.repo, f.stockUC).WithPayments(f.pay).WithUnitRepo(units)
	if _, err := f.ret(2); err != nil {
		t.Fatalf("return: %v", err)
	}
	if got := f.stockUC.calls[0].ConvFactor; got != "12" {
		t.Errorf("conv_factor = %q, want 12", got)
	}
}

// 详情页要显示每行已退多少，才能给退货对话框封顶。
func TestPurchaseReturn_SummaryReportsReturnedQty(t *testing.T) {
	f := newPOFixture("0")
	if _, err := f.ret(4); err != nil {
		t.Fatalf("return: %v", err)
	}
	got, err := f.uc.Summary(context.Background(), testTenantID, f.repo.billsByID[f.poID])
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if !got[f.lineID].Equal(decimal.NewFromInt(4)) {
		t.Errorf("returned = %v, want 4", got[f.lineID])
	}
}

// 退货按原采购行的进价出库（不是当前平均价），出库请求要标明按指定单价。
func TestPurchaseReturn_LeavesAtOriginalPurchasePrice(t *testing.T) {
	f := newPOFixture("0")
	if _, err := f.ret(3); err != nil {
		t.Fatalf("return: %v", err)
	}
	m := f.stockUC.calls[0]
	if !m.ExplicitCost || !m.UnitCost.Equal(decimal.RequireFromString("8")) {
		t.Errorf("movement explicit=%v unit_cost=%s, want true / 8", m.ExplicitCost, m.UnitCost)
	}
}

// 外币采购、辅助单位：出库单价 = 单价 × 汇率 ÷ 换算系数，与入库时的成本口径一致。
func TestPurchaseReturn_PriceConvertedByRateAndFactor(t *testing.T) {
	f := newPOFixture("0")
	unitID := uuid.New()
	f.repo.itemsByBillID[f.poID][0].UnitID = &unitID
	f.repo.billsByID[f.poID].ExchangeRateVal = decimal.RequireFromString("7.2")
	units := newMockProductUnitRepo()
	units.set(f.product, unitID, decimal.NewFromInt(12))
	f.uc = appbill.NewPurchaseReturnUseCase(f.repo, f.stockUC).WithPayments(f.pay).WithUnitRepo(units)
	if _, err := f.ret(1); err != nil {
		t.Fatalf("return: %v", err)
	}
	// 8 * 7.2 / 12 = 4.8
	if got := f.stockUC.calls[0].UnitCost; !got.Equal(decimal.RequireFromString("4.8")) {
		t.Errorf("unit_cost = %s, want 4.8", got)
	}
}
