package bill_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// linkedRepo gives the costed mock the link/returned-quantity capabilities the
// production repo has, derived from the approved linked returns in the store.
type linkedRepo struct {
	*costedRepo
	itemLinks map[uuid.UUID]uuid.UUID // return line id -> sale line id
}

func (r linkedRepo) SetLinkBill(_ context.Context, _ *sql.Tx, _, billID, linked uuid.UUID) error {
	r.linked[billID] = linked
	return nil
}

func (r linkedRepo) SetLinkItems(_ context.Context, _ *sql.Tx, _, _ uuid.UUID, links map[uuid.UUID]uuid.UUID) error {
	for k, v := range links {
		r.itemLinks[k] = v
	}
	return nil
}

func (r linkedRepo) ReturnLinks(_ context.Context, _, billID uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	out := map[uuid.UUID]uuid.UUID{}
	for _, it := range r.itemsByBillID[billID] {
		if l, ok := r.itemLinks[it.ID]; ok {
			out[it.ID] = l
		}
	}
	return out, nil
}

func (r linkedRepo) ReturnedQtyByLine(_ context.Context, _, saleID uuid.UUID) (map[uuid.UUID]decimal.Decimal, error) {
	out := map[uuid.UUID]decimal.Decimal{}
	for _, id := range r.approvedReturnsOf(saleID) {
		for _, it := range r.itemsByBillID[id] {
			if l, ok := r.itemLinks[it.ID]; ok {
				out[l] = out[l].Add(it.Qty)
			}
		}
	}
	return out, nil
}

func (r linkedRepo) approvedReturnsOf(saleID uuid.UUID) []uuid.UUID {
	var ids []uuid.UUID
	for id, h := range r.billsByID {
		if h.SubType == domain.BillSubTypeSaleReturn && h.Status == domain.StatusApproved && r.linked[id] == saleID {
			ids = append(ids, id)
		}
	}
	return ids
}

func (r linkedRepo) ReturnedQty(_ context.Context, _, saleID uuid.UUID) ([]appbill.ReturnedLine, error) {
	var out []appbill.ReturnedLine
	for _, id := range r.approvedReturnsOf(saleID) {
		for _, it := range r.itemsByBillID[id] {
			out = append(out, appbill.ReturnedLine{ProductID: it.ProductID, UnitID: it.UnitID, Qty: it.Qty})
		}
	}
	return out, nil
}

func (r linkedRepo) ReturnedAmount(_ context.Context, _, saleID uuid.UUID) (decimal.Decimal, error) {
	var sum decimal.Decimal
	for _, id := range r.approvedReturnsOf(saleID) {
		sum = sum.Add(r.billsByID[id].TotalAmount)
	}
	return sum, nil
}

type saleReturnFixture struct {
	repo    linkedRepo
	stockUC *mockStockUC
	uc      *appbill.ReturnSaleUseCase
	saleID  uuid.UUID
	lineID  uuid.UUID
	product uuid.UUID
}

// newSaleReturnFixture seeds an approved sale of 5 units at 20.00 shipped from a warehouse.
func newSaleReturnFixture() *saleReturnFixture {
	repo := linkedRepo{costedRepo: newCostedRepo(), itemLinks: map[uuid.UUID]uuid.UUID{}}
	stockUC := newMockStockUC()
	pid := uuid.New()
	saleID := seedApprovedSaleFor(repo.costedRepo, pid, 5)
	wh, partner := uuid.New(), uuid.New()
	sale := repo.billsByID[saleID]
	sale.BillNo, sale.WarehouseID, sale.PartnerID = "SO-20261010-0001", &wh, &partner
	line := repo.itemsByBillID[saleID][0]
	line.UnitPrice = decimal.RequireFromString("20.00")
	repo.saleCost[[2]uuid.UUID{saleID, pid}] = decimal.RequireFromString("12.3456")

	create := appbill.NewCreateReturnBillUseCase(repo)
	approve := appbill.NewApproveReturnBillUseCase(repo, stockUC)
	return &saleReturnFixture{
		repo: repo, stockUC: stockUC, saleID: saleID, lineID: line.ID, product: pid,
		uc: appbill.NewReturnSaleUseCase(repo, create, approve),
	}
}

func (f *saleReturnFixture) ret(qty int64) (*appbill.ReturnSaleOutput, error) {
	return f.uc.Execute(context.Background(), appbill.ReturnSaleRequest{
		TenantID: testTenantID, CreatorID: testCreatorID, SaleBillID: f.saleID,
		Items: []appbill.ReturnSaleLine{{BillItemID: f.lineID, Qty: decimal.NewFromInt(qty)}},
	})
}

// 原单 5 件退 2 件：库存加 2，按原销售成本入库；再退 4 件超出剩余 3 件，报可退数量。
func TestReturnSale_PartialThenOverReturn(t *testing.T) {
	f := newSaleReturnFixture()

	out, err := f.ret(2)
	if err != nil {
		t.Fatalf("return 2: %v", err)
	}
	if !out.ReturnAmount.Equal(decimal.NewFromInt(40)) {
		t.Errorf("return amount = %s, want 40", out.ReturnAmount)
	}
	if len(f.stockUC.calls) != 1 || !f.stockUC.calls[0].Qty.Equal(decimal.NewFromInt(2)) {
		t.Fatalf("movements = %+v, want one stock-in of 2", f.stockUC.calls)
	}
	if got := f.stockUC.calls[0].UnitCost; !got.Equal(decimal.RequireFromString("12.3456")) {
		t.Errorf("unit_cost = %s, want the sale cost 12.3456", got)
	}
	head := f.repo.billsByID[out.BillID]
	if head.Status != domain.StatusApproved || f.repo.linked[out.BillID] != f.saleID {
		t.Errorf("return bill status=%d link=%s, want approved and linked to the sale", head.Status, f.repo.linked[out.BillID])
	}
	if head.PartnerID == nil || *head.PartnerID != *f.repo.billsByID[f.saleID].PartnerID {
		t.Errorf("return bill partner = %v, want the sale's customer", head.PartnerID)
	}
	if head.WarehouseID == nil || *head.WarehouseID != *f.repo.billsByID[f.saleID].WarehouseID {
		t.Errorf("return bill warehouse = %v, want the sale's warehouse", head.WarehouseID)
	}

	_, err = f.ret(4)
	var over *appbill.ReturnExceededError
	if !errors.As(err, &over) || !errors.Is(err, appbill.ErrReturnExceedsSold) {
		t.Fatalf("return 4 err = %v, want ReturnExceededError", err)
	}
	if !over.Remaining.Equal(decimal.NewFromInt(3)) {
		t.Errorf("remaining = %s, want 3", over.Remaining)
	}
	if len(f.stockUC.calls) != 1 {
		t.Errorf("movements = %d after a rejected return, want still 1", len(f.stockUC.calls))
	}

	// the remaining 3 can still go back
	if _, err := f.ret(3); err != nil {
		t.Fatalf("return remaining 3: %v", err)
	}
}

func TestReturnSale_DraftSaleIsNotReturnable(t *testing.T) {
	f := newSaleReturnFixture()
	f.repo.billsByID[f.saleID].Status = domain.StatusDraft
	if _, err := f.ret(1); !errors.Is(err, appbill.ErrSaleNotReturnable) {
		t.Fatalf("err = %v, want ErrSaleNotReturnable", err)
	}
	if len(f.stockUC.calls) != 0 {
		t.Errorf("stock moved for a draft sale")
	}
}

func TestReturnSale_ForeignLineIsRejected(t *testing.T) {
	f := newSaleReturnFixture()
	_, err := f.uc.Execute(context.Background(), appbill.ReturnSaleRequest{
		TenantID: testTenantID, CreatorID: testCreatorID, SaleBillID: f.saleID,
		Items: []appbill.ReturnSaleLine{{BillItemID: uuid.New(), Qty: decimal.NewFromInt(1)}},
	})
	if !errors.Is(err, appbill.ErrReturnItemNotFound) {
		t.Fatalf("err = %v, want ErrReturnItemNotFound", err)
	}
}

func TestReturnSale_RepeatedLineAddsUp_AndBadQtyRejected(t *testing.T) {
	f := newSaleReturnFixture()
	_, err := f.uc.Execute(context.Background(), appbill.ReturnSaleRequest{
		TenantID: testTenantID, CreatorID: testCreatorID, SaleBillID: f.saleID,
		Items: []appbill.ReturnSaleLine{
			{BillItemID: f.lineID, Qty: decimal.NewFromInt(2)},
			{BillItemID: f.lineID, Qty: decimal.NewFromInt(4)},
		},
	})
	if !errors.Is(err, appbill.ErrReturnExceedsSold) {
		t.Fatalf("2+4 of 5: err = %v, want ErrReturnExceedsSold", err)
	}
	for _, q := range []int64{0, -1} {
		if _, err := f.ret(q); !errors.Is(err, appbill.ErrValidation) {
			t.Errorf("qty %d: err = %v, want ErrValidation", q, err)
		}
	}
	if _, err := f.uc.Execute(context.Background(), appbill.ReturnSaleRequest{
		TenantID: testTenantID, CreatorID: testCreatorID, SaleBillID: f.saleID,
	}); !errors.Is(err, appbill.ErrValidation) {
		t.Errorf("no items: err = %v, want ErrValidation", err)
	}
}

func TestReturnSale_UnknownSale(t *testing.T) {
	f := newSaleReturnFixture()
	f.saleID = uuid.New()
	if _, err := f.ret(1); !errors.Is(err, appbill.ErrBillNotFound) {
		t.Fatalf("err = %v, want ErrBillNotFound", err)
	}
}

// 箱与件共用一个池：卖 2 箱（每箱 12 件），退 3 箱超出。
func TestReturnSale_AuxiliaryUnitSharesOnePool(t *testing.T) {
	f := newSaleReturnFixture()
	unitID := uuid.New()
	line := f.repo.itemsByBillID[f.saleID][0]
	line.UnitID = &unitID
	line.Qty = decimal.NewFromInt(2)
	units := &mockProductUnitRepo{factors: map[string]decimal.Decimal{
		f.product.String() + ":" + unitID.String(): decimal.NewFromInt(12),
	}}
	f.uc = appbill.NewReturnSaleUseCase(f.repo,
		appbill.NewCreateReturnBillUseCase(f.repo).WithUnitRepo(units),
		appbill.NewApproveReturnBillUseCase(f.repo, f.stockUC).WithUnitRepo(units)).WithUnitRepo(units)

	if _, err := f.ret(3); !errors.Is(err, appbill.ErrReturnExceedsSold) {
		t.Fatalf("3 boxes of 2: err = %v, want ErrReturnExceedsSold", err)
	}
	if _, err := f.ret(1); err != nil {
		t.Fatalf("1 box: %v", err)
	}
	if got := f.stockUC.calls[0].ConvFactor; got != "12" {
		t.Errorf("conv_factor = %q, want 12", got)
	}
}

func TestReturnSale_SummaryReportsReturnedQtyAndAmount(t *testing.T) {
	f := newSaleReturnFixture()
	if _, err := f.ret(2); err != nil {
		t.Fatalf("return: %v", err)
	}
	sale := f.repo.billsByID[f.saleID]
	sum, err := f.uc.Summary(context.Background(), testTenantID, sale, f.repo.itemsByBillID[f.saleID])
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got := sum.ReturnedQty[f.lineID]; !got.Equal(decimal.NewFromInt(2)) {
		t.Errorf("returned qty = %s, want 2", got)
	}
	if !sum.ReturnedAmount.Equal(decimal.NewFromInt(40)) {
		t.Errorf("returned amount = %s, want 40", sum.ReturnedAmount)
	}
}

// 防的事故：同一商品在原单里分两行（5 件正价 + 1 件赠品）。第 1 行退满 5 件后，再退第 1 行不能成功，
// 否则赠品会被按正价退款；赠品行自己仍可退 1 件。
func TestReturnSale_SameProductOnTwoLines_CapsPerLine(t *testing.T) {
	f := newSaleReturnFixture()
	gift := &domain.BillItem{ID: uuid.New(), HeadID: f.saleID, ProductID: f.product, LineNo: 2, Qty: decimal.NewFromInt(1)}
	f.repo.itemsByBillID[f.saleID] = append(f.repo.itemsByBillID[f.saleID], gift)

	if _, err := f.ret(5); err != nil {
		t.Fatalf("return 5 of line 1: %v", err)
	}
	_, err := f.ret(1)
	var over *appbill.ReturnExceededError
	if !errors.As(err, &over) || over.LineNo != 1 || !over.Remaining.IsZero() {
		t.Fatalf("return 1 more of line 1: err = %v, want ReturnExceededError line 1 with 0 left", err)
	}
	if _, err := f.uc.Execute(context.Background(), appbill.ReturnSaleRequest{
		TenantID: testTenantID, CreatorID: testCreatorID, SaleBillID: f.saleID,
		Items: []appbill.ReturnSaleLine{{BillItemID: gift.ID, Qty: decimal.NewFromInt(1)}},
	}); err != nil {
		t.Fatalf("return the gift line: %v", err)
	}

	sale := f.repo.billsByID[f.saleID]
	sum, err := f.uc.Summary(context.Background(), testTenantID, sale, f.repo.itemsByBillID[f.saleID])
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got := sum.ReturnedQty[f.lineID]; !got.Equal(decimal.NewFromInt(5)) {
		t.Errorf("line 1 returned = %s, want 5", got)
	}
	if got := sum.ReturnedQty[gift.ID]; !got.Equal(decimal.NewFromInt(1)) {
		t.Errorf("gift line returned = %s, want 1", got)
	}
}

// 防的事故：两个并发请求都在建单时通过了校验（各退 2，原单剩 3），审核串行后第二张只剩 1 件可入库，
// 却仍按 2 件记金额。严格模式下第二张必须整单失败，库存只入第一张的 2 件。
func TestReturnSale_ConcurrentOverdraw_SecondFailsInsteadOfTruncating(t *testing.T) {
	f := newSaleReturnFixture()
	if _, err := f.ret(2); err != nil {
		t.Fatalf("first return: %v", err)
	}
	create := appbill.NewCreateReturnBillUseCase(f.repo)
	mk := func() uuid.UUID {
		out, err := create.Execute(context.Background(), appbill.CreateReturnRequest{
			TenantID: testTenantID, CreatorID: testCreatorID, LinkBillID: &f.saleID,
			WarehouseID: f.repo.billsByID[f.saleID].WarehouseID,
			Items: []appbill.ReturnItem{{ProductID: f.product, LineNo: 1, Qty: decimal.NewFromInt(2),
				UnitPrice: decimal.NewFromInt(20), LinkItemID: &f.lineID}},
		})
		if err != nil {
			t.Fatalf("create draft: %v", err)
		}
		return out.BillID
	}
	a, b := mk(), mk()
	approve := appbill.NewApproveReturnBillUseCase(f.repo, f.stockUC)
	if err := approve.Execute(context.Background(), appbill.ApproveReturnRequest{TenantID: testTenantID, BillID: a, CreatorID: testCreatorID, Strict: true}); err != nil {
		t.Fatalf("approve a: %v", err)
	}
	err := approve.Execute(context.Background(), appbill.ApproveReturnRequest{TenantID: testTenantID, BillID: b, CreatorID: testCreatorID, Strict: true})
	var over *appbill.ReturnExceededError
	if !errors.As(err, &over) || !over.Remaining.Equal(decimal.NewFromInt(1)) {
		t.Fatalf("approve b: err = %v, want ReturnExceededError with 1 left", err)
	}
	if len(f.stockUC.calls) != 2 {
		t.Errorf("movements = %d, want 2 (first return and a)", len(f.stockUC.calls))
	}
}

// 非严格路径（取消/退款回调）仍按剩余量截断，行为不变。
func TestApproveReturn_NonStrictStillTruncates(t *testing.T) {
	f := newSaleReturnFixture()
	if _, err := f.ret(4); err != nil {
		t.Fatalf("first return: %v", err)
	}
	out, err := appbill.NewCreateReturnBillUseCase(f.repo).Execute(context.Background(), appbill.CreateReturnRequest{
		TenantID: testTenantID, CreatorID: testCreatorID, LinkBillID: &f.saleID,
		WarehouseID: f.repo.billsByID[f.saleID].WarehouseID,
		Items:       []appbill.ReturnItem{{ProductID: f.product, LineNo: 1, Qty: decimal.NewFromInt(3), UnitPrice: decimal.NewFromInt(20)}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := appbill.NewApproveReturnBillUseCase(f.repo, f.stockUC).Execute(context.Background(),
		appbill.ApproveReturnRequest{TenantID: testTenantID, BillID: out.BillID, CreatorID: testCreatorID}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if last := f.stockUC.calls[len(f.stockUC.calls)-1]; !last.Qty.Equal(decimal.NewFromInt(1)) {
		t.Errorf("last movement qty = %s, want 1", last.Qty)
	}
}

func TestReturnSale_PurchaseReturnIsNotReturnable(t *testing.T) {
	f := newSaleReturnFixture()
	f.repo.billsByID[f.saleID].SubType = domain.BillSubTypePurchaseReturn
	if _, err := f.ret(1); !errors.Is(err, appbill.ErrSaleNotReturnable) {
		t.Fatalf("err = %v, want ErrSaleNotReturnable", err)
	}
	if len(f.stockUC.calls) != 0 {
		t.Errorf("stock moved for a return to a supplier")
	}
}
