package bill_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// seedReturnDraftBill seeds a draft return-stock bill (入库/销售退货) and returns its ID.
func seedReturnDraftBill(repo *mockBillRepo, n int, warehouseID uuid.UUID) uuid.UUID {
	billID := uuid.New()
	now := time.Now()
	head := &domain.BillHead{
		ID:          billID,
		TenantID:    testTenantID,
		BillNo:      "RT-20260528-0001",
		BillType:    domain.BillTypePurchase,
		SubType:     domain.BillSubTypeSaleReturn,
		Status:      domain.StatusDraft,
		WarehouseID: &warehouseID,
		CreatorID:   testCreatorID,
		BillDate:    now,
		Subtotal:    decimal.NewFromFloat(300),
		TotalAmount: decimal.NewFromFloat(300),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	items := make([]*domain.BillItem, n)
	for i := 0; i < n; i++ {
		items[i] = &domain.BillItem{
			ID:         uuid.New(),
			TenantID:   testTenantID,
			HeadID:     billID,
			ProductID:  uuid.New(),
			LineNo:     i + 1,
			Qty:        decimal.NewFromFloat(5),
			UnitPrice:  decimal.NewFromFloat(60),
			LineAmount: decimal.NewFromFloat(300),
		}
	}
	repo.billsByID[billID] = head
	repo.itemsByBillID[billID] = items
	return billID
}

// TestApproveReturnBill_HappyPath_StockMovementsIn verifies that approving a 2-item return
// creates 2 stock-in movements with direction=in and reference_type=return.
func TestApproveReturnBill_HappyPath_StockMovementsIn(t *testing.T) {
	repo := newMockBillRepo()
	stockUC := newMockStockUC()
	warehouseID := uuid.New()
	billID := seedReturnDraftBill(repo, 2, warehouseID)

	uc := appbill.NewApproveReturnBillUseCase(repo, stockUC)
	if err := uc.Execute(context.Background(), appbill.ApproveReturnRequest{
		TenantID:  testTenantID,
		BillID:    billID,
		CreatorID: testCreatorID,
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(stockUC.calls) != 2 {
		t.Errorf("stock movements = %d, want 2", len(stockUC.calls))
	}
	for _, c := range stockUC.calls {
		if c.Direction != domainstock.DirectionIn {
			t.Errorf("direction = %s, want in", c.Direction)
		}
		if c.ReferenceType != domainstock.RefReturn {
			t.Errorf("reference_type = %s, want return", c.ReferenceType)
		}
		if c.WarehouseID != warehouseID {
			t.Errorf("warehouse_id mismatch: got %s", c.WarehouseID)
		}
	}

	head := repo.billsByID[billID]
	if head.Status != domain.StatusApproved {
		t.Errorf("status = %d, want %d (Approved)", head.Status, domain.StatusApproved)
	}
	if head.ApprovedAt == nil {
		t.Error("approved_at is nil")
	}
}

// TestApproveReturnBill_AlreadyApproved_IdempotentNoOp verifies that re-approving a bill
// that is already in Approved status returns nil without emitting any stock movements.
func TestApproveReturnBill_AlreadyApproved_IdempotentNoOp(t *testing.T) {
	repo := newMockBillRepo()
	warehouseID := uuid.New()
	billID := seedReturnDraftBill(repo, 1, warehouseID)
	// Force to approved state.
	repo.billsByID[billID].Status = domain.StatusApproved

	stockUC := newMockStockUC()
	uc := appbill.NewApproveReturnBillUseCase(repo, stockUC)
	if err := uc.Execute(context.Background(), appbill.ApproveReturnRequest{
		TenantID:  testTenantID,
		BillID:    billID,
		CreatorID: testCreatorID,
	}); err != nil {
		t.Fatalf("expected nil for already-approved bill (idempotent), got %v", err)
	}
	if len(stockUC.calls) != 0 {
		t.Errorf("expected 0 stock movements for idempotent call, got %d", len(stockUC.calls))
	}
}

// 退货单没选仓库：用默认仓并回写；没有默认仓则返回 ErrWarehouseRequired，库存零调用。
func TestApproveReturnBill_NoWarehouse_DefaultUsedOrRequired(t *testing.T) {
	req := func(id uuid.UUID) appbill.ApproveReturnRequest {
		return appbill.ApproveReturnRequest{TenantID: testTenantID, BillID: id, CreatorID: testCreatorID}
	}

	repo := newMockBillRepo()
	stockUC := newMockStockUC()
	billID := seedReturnDraftBill(repo, 1, uuid.New())
	repo.billsByID[billID].WarehouseID = nil
	def := uuid.New()
	repo.defaultWH = &def
	if err := appbill.NewApproveReturnBillUseCase(repo, stockUC).Execute(context.Background(), req(billID)); err != nil {
		t.Fatalf("Execute with default: %v", err)
	}
	if len(stockUC.calls) != 1 || stockUC.calls[0].WarehouseID != def || repo.setWHCalls != 1 {
		t.Errorf("calls=%d setWH=%d, want 1 movement at default warehouse and 1 write-back", len(stockUC.calls), repo.setWHCalls)
	}

	repo2 := newMockBillRepo()
	stockUC2 := newMockStockUC()
	billID2 := seedReturnDraftBill(repo2, 1, uuid.New())
	repo2.billsByID[billID2].WarehouseID = nil
	err := appbill.NewApproveReturnBillUseCase(repo2, stockUC2).Execute(context.Background(), req(billID2))
	if !errors.Is(err, appbill.ErrWarehouseRequired) {
		t.Fatalf("err = %v, want ErrWarehouseRequired", err)
	}
	if len(stockUC2.calls) != 0 {
		t.Errorf("stock calls = %d, want 0", len(stockUC2.calls))
	}
}

// costedRepo gives the mock repo the optional cost-lookup capability the production
// repo has: sale-out costs keyed by (sale bill, product) and a per-product snapshot cost.
type costedRepo struct {
	*mockBillRepo
	linked   map[uuid.UUID]uuid.UUID
	imported map[string]uuid.UUID
	saleCost map[[2]uuid.UUID]decimal.Decimal
	snapCost map[uuid.UUID]decimal.Decimal
}

func newCostedRepo() *costedRepo {
	return &costedRepo{
		mockBillRepo: newMockBillRepo(),
		linked:       map[uuid.UUID]uuid.UUID{},
		imported:     map[string]uuid.UUID{},
		saleCost:     map[[2]uuid.UUID]decimal.Decimal{},
		snapCost:     map[uuid.UUID]decimal.Decimal{},
	}
}

func (r *costedRepo) LinkedBillID(_ context.Context, _, billID uuid.UUID) (uuid.UUID, bool, error) {
	id, ok := r.linked[billID]
	return id, ok, nil
}

func (r *costedRepo) ImportedOrderBillID(_ context.Context, _ uuid.UUID, platform, orderNo string) (uuid.UUID, bool, error) {
	id, ok := r.imported[platform+"|"+orderNo]
	return id, ok, nil
}

func (r *costedRepo) SaleOutUnitCost(_ context.Context, _, saleBillID, productID uuid.UUID) (decimal.Decimal, bool, error) {
	c, ok := r.saleCost[[2]uuid.UUID{saleBillID, productID}]
	return c, ok, nil
}

func (r *costedRepo) SnapshotUnitCost(_ context.Context, _, productID, _ uuid.UUID) (decimal.Decimal, bool, error) {
	c, ok := r.snapCost[productID]
	return c, ok, nil
}

// 防的事故：退货按退款单价（20.00）入库，把已入库货的平均成本（12.3456）冲高，
// 毛利报表整体失真且无任何报错。退货必须按原销售出库时的成本入库，退款价绝不参与。
func TestApproveReturnBill_RestoresAtOriginalSaleCost_NotRefundPrice(t *testing.T) {
	repo := newCostedRepo()
	stockUC := newMockStockUC()
	billID := seedReturnDraftBill(repo.mockBillRepo, 1, uuid.New())
	item := repo.itemsByBillID[billID][0]
	item.UnitPrice = decimal.RequireFromString("20.00")
	saleID := uuid.New()
	repo.billsByID[billID].Remark = "cancel:shopify:#1001:original_bill_id=" + saleID.String()
	repo.saleCost[[2]uuid.UUID{saleID, item.ProductID}] = decimal.RequireFromString("12.3456")
	repo.snapCost[item.ProductID] = decimal.RequireFromString("99")

	err := appbill.NewApproveReturnBillUseCase(repo, stockUC).Execute(context.Background(),
		appbill.ApproveReturnRequest{TenantID: testTenantID, BillID: billID, CreatorID: testCreatorID})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := stockUC.calls[0].UnitCost; !got.Equal(decimal.RequireFromString("12.3456")) {
		t.Errorf("unit_cost = %s, want 12.3456 (original sale cost)", got)
	}
}

// 原销售成本找不到（手工退货单、历史单据）时取当前快照平均成本，仍不用退款价。
func TestApproveReturnBill_NoOriginalCost_FallsBackToSnapshotAverage(t *testing.T) {
	repo := newCostedRepo()
	stockUC := newMockStockUC()
	billID := seedReturnDraftBill(repo.mockBillRepo, 1, uuid.New())
	item := repo.itemsByBillID[billID][0]
	repo.snapCost[item.ProductID] = decimal.RequireFromString("7.5")

	if err := appbill.NewApproveReturnBillUseCase(repo, stockUC).Execute(context.Background(),
		appbill.ApproveReturnRequest{TenantID: testTenantID, BillID: billID, CreatorID: testCreatorID}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := stockUC.calls[0].UnitCost; !got.Equal(decimal.RequireFromString("7.5")) {
		t.Errorf("unit_cost = %s, want 7.5 (snapshot average)", got)
	}
}

// 部分退款走 "refund:<平台>:<订单号>:refund_id=" 备注：经导入记录找回原销售单，取其成本。
// link_bill_id 优先于备注。
func TestApproveReturnBill_RefundRemarkAndLinkResolveOriginalSale(t *testing.T) {
	repo := newCostedRepo()
	stockUC := newMockStockUC()
	billID := seedReturnDraftBill(repo.mockBillRepo, 1, uuid.New())
	pid := repo.itemsByBillID[billID][0].ProductID
	saleID := uuid.New()
	repo.billsByID[billID].Remark = "refund:shopify:#1001:refund_id=R9"
	repo.imported["shopify|#1001"] = saleID
	repo.saleCost[[2]uuid.UUID{saleID, pid}] = decimal.RequireFromString("3.21")

	req := appbill.ApproveReturnRequest{TenantID: testTenantID, BillID: billID, CreatorID: testCreatorID}
	if err := appbill.NewApproveReturnBillUseCase(repo, stockUC).Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := stockUC.calls[0].UnitCost; !got.Equal(decimal.RequireFromString("3.21")) {
		t.Errorf("refund remark: unit_cost = %s, want 3.21", got)
	}

	// link_bill_id wins over the remark.
	repo2 := newCostedRepo()
	stockUC2 := newMockStockUC()
	billID2 := seedReturnDraftBill(repo2.mockBillRepo, 1, uuid.New())
	pid2 := repo2.itemsByBillID[billID2][0].ProductID
	linkedSale, remarkSale := uuid.New(), uuid.New()
	repo2.billsByID[billID2].Remark = "cancel:shopify:#1:original_bill_id=" + remarkSale.String()
	repo2.linked[billID2] = linkedSale
	repo2.saleCost[[2]uuid.UUID{linkedSale, pid2}] = decimal.RequireFromString("5")
	repo2.saleCost[[2]uuid.UUID{remarkSale, pid2}] = decimal.RequireFromString("6")
	req2 := appbill.ApproveReturnRequest{TenantID: testTenantID, BillID: billID2, CreatorID: testCreatorID}
	if err := appbill.NewApproveReturnBillUseCase(repo2, stockUC2).Execute(context.Background(), req2); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := stockUC2.calls[0].UnitCost; !got.Equal(decimal.NewFromInt(5)) {
		t.Errorf("linked: unit_cost = %s, want 5", got)
	}
}

// 选了辅助单位（如 箱）的退货行必须带换算系数入库，否则 2 箱会被当成 2 个。
func TestApproveReturnBill_AuxiliaryUnit_UsesConversionFactor(t *testing.T) {
	repo := newMockBillRepo()
	stockUC := newMockStockUC()
	billID := seedReturnDraftBill(repo, 1, uuid.New())
	item := repo.itemsByBillID[billID][0]
	unitID := uuid.New()
	item.UnitID = &unitID
	units := &mockProductUnitRepo{factors: map[string]decimal.Decimal{
		item.ProductID.String() + ":" + unitID.String(): decimal.NewFromInt(12),
	}}

	uc := appbill.NewApproveReturnBillUseCase(repo, stockUC).WithUnitRepo(units)
	if err := uc.Execute(context.Background(),
		appbill.ApproveReturnRequest{TenantID: testTenantID, BillID: billID, CreatorID: testCreatorID}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := stockUC.calls[0].ConvFactor; got != "12" {
		t.Errorf("conv_factor = %q, want 12", got)
	}
}

func TestParseOriginRef(t *testing.T) {
	id := uuid.New()
	cases := []struct {
		remark string
		want   appbill.OriginRef
		ok     bool
	}{
		{"cancel:shopify:#1001:original_bill_id=" + id.String(), appbill.OriginRef{BillID: id}, true},
		{"refund:shopify:#1001:refund_id=R9", appbill.OriginRef{Platform: "shopify", PlatformOrderNo: "#1001"}, true},
		{"refund:shopify:A:B:refund_id=R9", appbill.OriginRef{Platform: "shopify", PlatformOrderNo: "A:B"}, true},
		{"cancel:shopify:#1:original_bill_id=not-a-uuid", appbill.OriginRef{}, false},
		{"手工退货", appbill.OriginRef{}, false},
		{"", appbill.OriginRef{}, false},
	}
	for _, c := range cases {
		got, ok := appbill.ParseOriginRef(c.remark)
		if ok != c.ok || got != c.want {
			t.Errorf("ParseOriginRef(%q) = %+v,%v want %+v,%v", c.remark, got, ok, c.want, c.ok)
		}
	}
}

// cappedRepo adds ReturnedQty on top of costedRepo, derived from the approved return
// bills in the mock store (as the production query does), so two drafts racing for the
// same sale see each other once one of them is approved.
type cappedRepo struct{ *costedRepo }

func (r cappedRepo) ReturnedQty(_ context.Context, _, saleBillID uuid.UUID) ([]appbill.ReturnedLine, error) {
	var out []appbill.ReturnedLine
	for id, h := range r.billsByID {
		if h.SubType != domain.BillSubTypeSaleReturn || h.Status != domain.StatusApproved {
			continue
		}
		if ref, ok := appbill.ParseOriginRef(h.Remark); !ok || ref.BillID != saleBillID {
			continue
		}
		for _, it := range r.itemsByBillID[id] {
			out = append(out, appbill.ReturnedLine{ProductID: it.ProductID, UnitID: it.UnitID, Qty: it.Qty})
		}
	}
	return out, nil
}

func seedApprovedSaleFor(repo *costedRepo, productID uuid.UUID, qty int64) uuid.UUID {
	id := uuid.New()
	repo.billsByID[id] = &domain.BillHead{ID: id, TenantID: testTenantID, BillType: domain.BillTypeSale, Status: domain.StatusApproved}
	repo.itemsByBillID[id] = []*domain.BillItem{{ID: uuid.New(), HeadID: id, ProductID: productID, LineNo: 1, Qty: decimal.NewFromInt(qty)}}
	return id
}

func seedCancelDraft(repo *costedRepo, saleID, productID uuid.UUID, qty int64) uuid.UUID {
	billID := seedReturnDraftBill(repo.mockBillRepo, 1, uuid.New())
	repo.billsByID[billID].Remark = "cancel:shopify:#1:original_bill_id=" + saleID.String()
	repo.itemsByBillID[billID][0].ProductID = productID
	repo.itemsByBillID[billID][0].Qty = decimal.NewFromInt(qty)
	return billID
}

// 防的事故：Shopify 5 秒超时重投，同一条 orders/cancelled 并发到达，两张完整的冲销草稿都建出来了，
// 各自审核就把同一批货入库两次。审核时必须按原销售单封顶：第二张已无货可退，作废草稿并报 ErrNothingToReverse。
func TestApproveReturnBill_ConcurrentCancelDrafts_SecondOneGivesBackNothing(t *testing.T) {
	repo := newCostedRepo()
	stockUC := newMockStockUC()
	pid := uuid.New()
	saleID := seedApprovedSaleFor(repo, pid, 5)
	first := seedCancelDraft(repo, saleID, pid, 5)
	second := seedCancelDraft(repo, saleID, pid, 5)
	uc := appbill.NewApproveReturnBillUseCase(cappedRepo{repo}, stockUC)

	if err := uc.Execute(context.Background(), appbill.ApproveReturnRequest{TenantID: testTenantID, BillID: first, CreatorID: testCreatorID}); err != nil {
		t.Fatalf("first approve: %v", err)
	}
	err := uc.Execute(context.Background(), appbill.ApproveReturnRequest{TenantID: testTenantID, BillID: second, CreatorID: testCreatorID})
	if !errors.Is(err, appbill.ErrNothingToReverse) {
		t.Fatalf("second approve err = %v, want ErrNothingToReverse", err)
	}
	if len(stockUC.calls) != 1 {
		t.Fatalf("stock movements = %d, want 1 (stock must come back once)", len(stockUC.calls))
	}
	if got := repo.billsByID[second].Status; got != domain.StatusCancelled {
		t.Errorf("second draft status = %d, want cancelled", got)
	}
}

// 部分已退（先退款 2 件）后整单取消草稿还写着 5 件：审核只回补剩下的 3 件。
func TestApproveReturnBill_CapsToWhatTheSaleHasLeft(t *testing.T) {
	repo := newCostedRepo()
	stockUC := newMockStockUC()
	pid := uuid.New()
	saleID := seedApprovedSaleFor(repo, pid, 5)
	refund := seedCancelDraft(repo, saleID, pid, 2)
	cancelDraft := seedCancelDraft(repo, saleID, pid, 5)
	uc := appbill.NewApproveReturnBillUseCase(cappedRepo{repo}, stockUC)

	for _, id := range []uuid.UUID{refund, cancelDraft} {
		if err := uc.Execute(context.Background(), appbill.ApproveReturnRequest{TenantID: testTenantID, BillID: id, CreatorID: testCreatorID}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	}
	if len(stockUC.calls) != 2 || !stockUC.calls[1].Qty.Equal(decimal.NewFromInt(3)) {
		t.Fatalf("movements = %+v, want second one capped to 3", stockUC.calls)
	}
}
