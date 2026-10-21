package lifecycle

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	appimporting "github.com/hanmahong5-arch/lurus-tally/internal/app/importing"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// cancelFakeRepo is the slice of BillRepo the return-creation path touches. The embedded
// interface stays nil: any other call panics, which flags an unexpected dependency.
type cancelFakeRepo struct {
	appbill.BillRepo
	bills     map[uuid.UUID]*domain.BillHead
	items     map[uuid.UUID][]*domain.BillItem
	defaultWH *uuid.UUID

	created      *domain.BillHead
	createdItems []*domain.BillItem

	returned []appbill.ReturnedLine
}

func (r *cancelFakeRepo) ReturnedQty(context.Context, uuid.UUID, uuid.UUID) ([]appbill.ReturnedLine, error) {
	return r.returned, nil
}

func (r *cancelFakeRepo) GetBill(_ context.Context, _, id uuid.UUID) (*domain.BillHead, error) {
	h, ok := r.bills[id]
	if !ok {
		return nil, appbill.ErrBillNotFound
	}
	return h, nil
}

func (r *cancelFakeRepo) GetBillItems(_ context.Context, _, id uuid.UUID) ([]*domain.BillItem, error) {
	return r.items[id], nil
}

func (r *cancelFakeRepo) DefaultWarehouseID(context.Context, uuid.UUID) (uuid.UUID, bool, error) {
	if r.defaultWH == nil {
		return uuid.Nil, false, nil
	}
	return *r.defaultWH, true, nil
}

func (r *cancelFakeRepo) ProductExists(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return true, nil
}

func (r *cancelFakeRepo) WarehouseExists(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return true, nil
}

func (r *cancelFakeRepo) WithTx(_ context.Context, fn func(tx *sql.Tx) error) error { return fn(nil) }

func (r *cancelFakeRepo) NextBillNo(context.Context, *sql.Tx, uuid.UUID, string) (string, error) {
	return "RT-20260101-0001", nil
}

func (r *cancelFakeRepo) CreateBill(_ context.Context, _ *sql.Tx, h *domain.BillHead, items []*domain.BillItem) error {
	r.created, r.createdItems = h, items
	return nil
}

func seedApprovedSale(r *cancelFakeRepo, tenantID uuid.UUID, warehouse *uuid.UUID) (uuid.UUID, []*domain.BillItem) {
	id := uuid.New()
	unit := uuid.New()
	lines := []*domain.BillItem{
		{ProductID: uuid.New(), LineNo: 1, Qty: decimal.NewFromInt(3), UnitPrice: decimal.RequireFromString("20.5")},
		{ProductID: uuid.New(), UnitID: &unit, UnitName: "箱", LineNo: 2, Qty: decimal.NewFromInt(2), UnitPrice: decimal.NewFromInt(100)},
	}
	r.bills[id] = &domain.BillHead{ID: id, TenantID: tenantID, BillType: domain.BillTypeSale, Status: domain.StatusApproved, WarehouseID: warehouse}
	r.items[id] = lines
	return id, lines
}

func newCancelFakeRepo() *cancelFakeRepo {
	return &cancelFakeRepo{bills: map[uuid.UUID]*domain.BillHead{}, items: map[uuid.UUID][]*domain.BillItem{}}
}

func cancelInput(tenantID, originalBillID uuid.UUID) appimporting.ReturnCreatorInput {
	// Exactly what IngestCancelOrder builds: no lines, the original bill only in the remark.
	return appimporting.ReturnCreatorInput{
		TenantID:  tenantID,
		CreatorID: uuid.New(),
		BillDate:  time.Now().UTC(),
		Remark:    "cancel:shopify:#1001:original_bill_id=" + originalBillID.String(),
	}
}

// 防的事故：Shopify 取消已同步订单，货却回不了库存 —— 取消回库单没有明细行会被拒，
// 每次 orders/cancelled 都以 500 收场。生产适配器必须按原销售单克隆明细和仓库。
func TestImportReturnCreator_ShopifyCancel_ClonesOriginalLinesAndWarehouse(t *testing.T) {
	repo := newCancelFakeRepo()
	tenantID := uuid.New()
	wh := uuid.New()
	saleID, lines := seedApprovedSale(repo, tenantID, &wh)

	creator := importReturnCreator{uc: appbill.NewCreateReturnBillUseCase(repo)}
	out, err := creator.Create(context.Background(), cancelInput(tenantID, saleID))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if out == nil || out.BillNo == "" {
		t.Fatalf("no reversal bill returned: %+v", out)
	}
	if repo.created.WarehouseID == nil || *repo.created.WarehouseID != wh {
		t.Errorf("warehouse = %v, want the original sale's %s", repo.created.WarehouseID, wh)
	}
	if len(repo.createdItems) != len(lines) {
		t.Fatalf("lines = %d, want %d", len(repo.createdItems), len(lines))
	}
	for i, it := range repo.createdItems {
		if it.ProductID != lines[i].ProductID || !it.Qty.Equal(lines[i].Qty) || !it.UnitPrice.Equal(lines[i].UnitPrice) {
			t.Errorf("line %d not cloned: %+v vs %+v", i, it, lines[i])
		}
	}
	if repo.createdItems[1].UnitID == nil || repo.createdItems[1].UnitName != "箱" {
		t.Errorf("auxiliary unit lost on clone: %+v", repo.createdItems[1])
	}
}

// 原销售单没记仓库时回落到租户默认仓；两者都没有则在建单阶段就返回 ErrWarehouseRequired，
// 不留下一张注定审核失败的草稿（Shopify 每次重试都会再造一张）。
func TestImportReturnCreator_ShopifyCancel_WarehouseFallbackAndRequired(t *testing.T) {
	repo := newCancelFakeRepo()
	tenantID := uuid.New()
	saleID, _ := seedApprovedSale(repo, tenantID, nil)
	creator := importReturnCreator{uc: appbill.NewCreateReturnBillUseCase(repo)}

	_, err := creator.Create(context.Background(), cancelInput(tenantID, saleID))
	if !errors.Is(err, appbill.ErrWarehouseRequired) {
		t.Fatalf("err = %v, want ErrWarehouseRequired", err)
	}
	if repo.created != nil {
		t.Error("a draft was created although no warehouse could be resolved")
	}

	def := uuid.New()
	repo.defaultWH = &def
	if _, err := creator.Create(context.Background(), cancelInput(tenantID, saleID)); err != nil {
		t.Fatalf("Create with default warehouse: %v", err)
	}
	if repo.created.WarehouseID == nil || *repo.created.WarehouseID != def {
		t.Errorf("warehouse = %v, want default %s", repo.created.WarehouseID, def)
	}
}

// 草稿销售单没有出过库，不能被"取消回库"。
func TestImportReturnCreator_ShopifyCancel_RejectsUnapprovedOriginal(t *testing.T) {
	repo := newCancelFakeRepo()
	tenantID := uuid.New()
	wh := uuid.New()
	saleID, _ := seedApprovedSale(repo, tenantID, &wh)
	repo.bills[saleID].Status = domain.StatusDraft

	creator := importReturnCreator{uc: appbill.NewCreateReturnBillUseCase(repo)}
	_, err := creator.Create(context.Background(), cancelInput(tenantID, saleID))
	if !errors.Is(err, appimporting.ErrNothingToReverse) {
		t.Fatalf("err = %v, want ErrNothingToReverse", err)
	}
	if repo.created != nil {
		t.Error("a reversal draft was created for a sale that never shipped")
	}
}

// 防的事故：Shopify 后台"取消+退款+补货"会先发 refunds/create 再发 orders/cancelled，
// 整单取消若再克隆全部数量，库存会被双倍回库。只回还没退过的部分。
func TestImportReturnCreator_ShopifyCancel_SkipsAlreadyRefundedQty(t *testing.T) {
	repo := newCancelFakeRepo()
	tenantID := uuid.New()
	wh := uuid.New()
	saleID, lines := seedApprovedSale(repo, tenantID, &wh)
	repo.returned = []appbill.ReturnedLine{
		{ProductID: lines[0].ProductID, Qty: decimal.NewFromInt(2)},
		{ProductID: lines[1].ProductID, UnitID: lines[1].UnitID, Qty: decimal.NewFromInt(2)},
	}

	creator := importReturnCreator{uc: appbill.NewCreateReturnBillUseCase(repo)}
	if _, err := creator.Create(context.Background(), cancelInput(tenantID, saleID)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(repo.createdItems) != 1 {
		t.Fatalf("lines = %d, want only the line with quantity left", len(repo.createdItems))
	}
	if got := repo.createdItems[0]; got.ProductID != lines[0].ProductID || !got.Qty.Equal(decimal.NewFromInt(1)) {
		t.Errorf("line = %+v, want product 0 with the remaining 1", got)
	}
}

// 全部已退完时不建单，返回 ErrNothingToReverse 让 webhook 以 200 收尾。
func TestImportReturnCreator_ShopifyCancel_FullyRefundedNothingToReverse(t *testing.T) {
	repo := newCancelFakeRepo()
	tenantID := uuid.New()
	wh := uuid.New()
	saleID, lines := seedApprovedSale(repo, tenantID, &wh)
	repo.returned = []appbill.ReturnedLine{
		{ProductID: lines[0].ProductID, Qty: decimal.NewFromInt(3)},
		{ProductID: lines[1].ProductID, UnitID: lines[1].UnitID, Qty: decimal.NewFromInt(5)},
	}
	creator := importReturnCreator{uc: appbill.NewCreateReturnBillUseCase(repo)}
	_, err := creator.Create(context.Background(), cancelInput(tenantID, saleID))
	if !errors.Is(err, appimporting.ErrNothingToReverse) {
		t.Fatalf("err = %v, want ErrNothingToReverse", err)
	}
	if repo.created != nil {
		t.Error("a reversal draft was created although everything was already returned")
	}
}

// 对照组：带明细行的部分退款不走克隆，行为不变。
func TestImportReturnCreator_RefundWithItems_NotCloned(t *testing.T) {
	repo := newCancelFakeRepo()
	wh := uuid.New()
	in := appimporting.ReturnCreatorInput{
		TenantID:    uuid.New(),
		CreatorID:   uuid.New(),
		WarehouseID: &wh,
		BillDate:    time.Now().UTC(),
		Remark:      "refund:shopify:#1001:refund_id=R1",
		Items:       []appimporting.SaleLineItem{{ProductID: uuid.New(), LineNo: 1, Qty: decimal.NewFromInt(2), UnitPrice: decimal.NewFromInt(20)}},
	}
	creator := importReturnCreator{uc: appbill.NewCreateReturnBillUseCase(repo)}
	if _, err := creator.Create(context.Background(), in); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(repo.createdItems) != 1 || !repo.createdItems[0].Qty.Equal(decimal.NewFromInt(2)) {
		t.Errorf("items = %+v, want the single refund line", repo.createdItems)
	}
}

// importedFakeRepo adds the order-number lookup the production repo has, so a refund
// remark ("refund:<platform>:<order>:refund_id=") finds the sale it belongs to.
type importedFakeRepo struct {
	*cancelFakeRepo
	orders map[string]uuid.UUID
}

func (r importedFakeRepo) ImportedOrderBillID(_ context.Context, _ uuid.UUID, platform, orderNo string) (uuid.UUID, bool, error) {
	id, ok := r.orders[platform+"|"+orderNo]
	return id, ok, nil
}

func refundInput(tenantID uuid.UUID, wh *uuid.UUID, productID uuid.UUID, qty int64) appimporting.ReturnCreatorInput {
	return appimporting.ReturnCreatorInput{
		TenantID:    tenantID,
		CreatorID:   uuid.New(),
		WarehouseID: wh,
		BillDate:    time.Now().UTC(),
		Remark:      "refund:shopify:#1001:refund_id=R1",
		Items:       []appimporting.SaleLineItem{{ProductID: productID, LineNo: 1, Qty: decimal.NewFromInt(qty), UnitPrice: decimal.NewFromInt(20)}},
	}
}

// 防的事故：Shopify 后台"取消订单并退款、回库"同时发 orders/cancelled 与 refunds/create，
// 到达顺序不定。先取消后退款时，退款这条不能把已回库的货再回一遍（卖 3 取消回 3，再退 2 不得变成 +5）。
func TestImportReturnCreator_RefundAfterCancel_GivesBackNothing(t *testing.T) {
	base := newCancelFakeRepo()
	tenantID := uuid.New()
	wh := uuid.New()
	saleID, lines := seedApprovedSale(base, tenantID, &wh)
	base.returned = []appbill.ReturnedLine{ // the cancellation already put everything back
		{ProductID: lines[0].ProductID, Qty: decimal.NewFromInt(3)},
		{ProductID: lines[1].ProductID, UnitID: lines[1].UnitID, Qty: decimal.NewFromInt(2)},
	}
	repo := importedFakeRepo{cancelFakeRepo: base, orders: map[string]uuid.UUID{"shopify|#1001": saleID}}
	creator := importReturnCreator{uc: appbill.NewCreateReturnBillUseCase(repo)}

	_, err := creator.Create(context.Background(), refundInput(tenantID, &wh, lines[0].ProductID, 2))
	if !errors.Is(err, appimporting.ErrNothingToReverse) {
		t.Fatalf("err = %v, want ErrNothingToReverse", err)
	}
	if base.created != nil {
		t.Error("a refund draft was created although the cancellation already restocked the goods")
	}
}

// 退款量超过剩余可退量时封顶：卖 3 已退 1，退款报 5 只收 2。
func TestImportReturnCreator_Refund_CappedToRemaining(t *testing.T) {
	base := newCancelFakeRepo()
	tenantID := uuid.New()
	wh := uuid.New()
	saleID, lines := seedApprovedSale(base, tenantID, &wh)
	base.returned = []appbill.ReturnedLine{{ProductID: lines[0].ProductID, Qty: decimal.NewFromInt(1)}}
	repo := importedFakeRepo{cancelFakeRepo: base, orders: map[string]uuid.UUID{"shopify|#1001": saleID}}
	creator := importReturnCreator{uc: appbill.NewCreateReturnBillUseCase(repo)}

	if _, err := creator.Create(context.Background(), refundInput(tenantID, &wh, lines[0].ProductID, 5)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(base.createdItems) != 1 || !base.createdItems[0].Qty.Equal(decimal.NewFromInt(2)) {
		t.Fatalf("items = %+v, want one line capped to 2", base.createdItems)
	}
}

type fixedUnits map[uuid.UUID]decimal.Decimal

func (f fixedUnits) GetConversionFactor(_ context.Context, _, unitID uuid.UUID) (decimal.Decimal, error) {
	return f[unitID], nil
}

// 已退数量按基本单位折算：原单行用"箱"(1 箱 = 12 件)，已有退货行按"件"记 12 件 = 1 箱，
// 取消只该克隆剩下的 1 箱，不能因为单位不同而整单克隆造成双倍回库。
func TestImportReturnCreator_ShopifyCancel_DeductsReturnedAcrossUnits(t *testing.T) {
	repo := newCancelFakeRepo()
	tenantID := uuid.New()
	wh := uuid.New()
	saleID, lines := seedApprovedSale(repo, tenantID, &wh)
	box := *lines[1].UnitID // 2 boxes sold
	repo.returned = []appbill.ReturnedLine{
		{ProductID: lines[0].ProductID, Qty: decimal.NewFromInt(3)},
		{ProductID: lines[1].ProductID, Qty: decimal.NewFromInt(12)}, // base unit, 12 pieces
	}
	units := fixedUnits{box: decimal.NewFromInt(12)}
	creator := importReturnCreator{uc: appbill.NewCreateReturnBillUseCase(repo).WithUnitRepo(units)}

	if _, err := creator.Create(context.Background(), cancelInput(tenantID, saleID)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(repo.createdItems) != 1 {
		t.Fatalf("lines = %d, want 1", len(repo.createdItems))
	}
	if got := repo.createdItems[0]; got.ProductID != lines[1].ProductID || !got.Qty.Equal(decimal.NewFromInt(1)) {
		t.Errorf("line = %+v, want 1 box left", got)
	}
}
