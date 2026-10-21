package importing_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appimporting "github.com/hanmahong5-arch/lurus-tally/internal/app/importing"
)

// nothingToReverseCreator mimics the production ReturnCreator for a sale that never
// shipped or was already refunded in full.
type nothingToReverseCreator struct{ calls int }

func (c *nothingToReverseCreator) Create(context.Context, appimporting.ReturnCreatorInput) (*appimporting.ReturnCreatorOutput, error) {
	c.calls++
	return nil, appimporting.ErrNothingToReverse
}

// repoWithBillNo adds the optional bill-number lookup the production repo provides.
type repoWithBillNo struct {
	*mockRepo
	nos map[uuid.UUID]string
}

func (r repoWithBillNo) ReversalBillNo(_ context.Context, _, id uuid.UUID) (string, error) {
	return r.nos[id], nil
}

// 防的事故：超卖订单审核失败只留下草稿，随后到来的 orders/cancelled 若返回 5xx，
// Shopify 会无限重试并最终删掉 webhook 订阅。无货可回时必须成功收尾，并记下已处理。
func TestIngestCancelOrder_NothingToReverse_MarksSeenAndSucceeds(t *testing.T) {
	repo := newMockRepo()
	tenantID, creatorID := mustUUID(t), mustUUID(t)
	draftID := mustUUID(t)
	repo.seen["shopify:#OVERSOLD"] = draftID

	retCreator := &nothingToReverseCreator{}
	retApprover := &mockReturnApprover{}
	uc := appimporting.NewImportOrdersUseCase(repo, &mockCreator{}, &mockApprover{}, newMockStockChecker(), nil, newMockRater(), "CNY").
		WithReturnHandlers(retCreator, retApprover)

	req := appimporting.CancelRequest{TenantID: tenantID, CreatorID: creatorID, Platform: appimporting.PlatformShopify, PlatformOrderNo: "#OVERSOLD"}
	res, err := uc.IngestCancelOrder(context.Background(), req)
	if err != nil {
		t.Fatalf("IngestCancelOrder: %v", err)
	}
	if !res.NothingToReverse || res.OriginalBillID != draftID || res.ReversalBillID != uuid.Nil {
		t.Errorf("result = %+v, want NothingToReverse for the draft", res)
	}
	if len(retApprover.approved) != 0 {
		t.Error("nothing may be approved when there is nothing to reverse")
	}
	if seen, _, _, _ := repo.IsCancelSeen(context.Background(), tenantID, "shopify", "#OVERSOLD"); !seen {
		t.Error("cancellation not recorded; a retry would re-run the reversal")
	}

	// A retry short-circuits on the dedup row.
	res, err = uc.IngestCancelOrder(context.Background(), req)
	if err != nil || !res.NothingToReverse {
		t.Fatalf("replay = %+v, %v; want NothingToReverse", res, err)
	}
	if retCreator.calls != 1 {
		t.Errorf("creator called %d times, want 1", retCreator.calls)
	}
}

// 重放同一个取消事件，应返回与首次相同的冲销单号，而不是空串。
func TestIngestCancelOrder_Replay_ReturnsReversalBillNo(t *testing.T) {
	base := newMockRepo()
	revID := mustUUID(t)
	repo := repoWithBillNo{mockRepo: base, nos: map[uuid.UUID]string{revID: "RT-20260101-0007"}}
	base.cancelled = map[string][2]uuid.UUID{"cancel:shopify:#1": {mustUUID(t), revID}}

	uc := appimporting.NewImportOrdersUseCase(repo, &mockCreator{}, &mockApprover{}, newMockStockChecker(), nil, newMockRater(), "CNY").
		WithReturnHandlers(&mockReturnCreator{}, &mockReturnApprover{})
	res, err := uc.IngestCancelOrder(context.Background(), appimporting.CancelRequest{
		TenantID: mustUUID(t), CreatorID: mustUUID(t), Platform: appimporting.PlatformShopify, PlatformOrderNo: "#1",
	})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if res.ReversalBillNo != "RT-20260101-0007" || res.NothingToReverse {
		t.Errorf("replay result = %+v, want the original reversal bill no", res)
	}
}

// Shopify 退款只带数字订单 id，订单却以订单名（#1001）导入：别名要一并登记，退款才找得到原销售单。
func TestIngestSingleOrder_RecordsOrderAlias(t *testing.T) {
	repo := newMockRepo()
	productID := mustUUID(t)
	repo.addMapping("shopify", "SKU-1", productID)
	uc := buildUseCaseWithReturn(repo, &mockCreator{}, &mockApprover{}, &mockReturnCreator{}, &mockReturnApprover{}, newMockStockChecker(), newMockRater())

	imported, skipped, err := uc.IngestSingleOrder(context.Background(), appimporting.SingleOrderRequest{
		TenantID: mustUUID(t), CreatorID: mustUUID(t), WarehouseID: mustUUID(t),
		Platform: appimporting.PlatformShopify, PlatformOrderNo: "#1001", PlatformOrderAlias: "5550001",
		Lines: []appimporting.OrderRow{{
			PlatformOrderNo: "#1001", PlatformSKU: "SKU-1", Qty: decimal.NewFromInt(1),
			UnitPrice: decimal.NewFromInt(10), Currency: "CNY", OrderDate: time.Now().UTC(),
		}},
	})
	if err != nil || skipped != nil {
		t.Fatalf("ingest: %v / %+v", err, skipped)
	}
	if got := repo.seen["shopify:5550001"]; got != imported.BillID {
		t.Errorf("alias row bill = %v, want %v", got, imported.BillID)
	}
}

type nothingToReverseApprover struct{}

func (nothingToReverseApprover) Approve(context.Context, appimporting.ReturnApproverInput) error {
	return appimporting.ErrNothingToReverse
}

func newReturnUC(repo appimporting.ImportRepo, rc appimporting.ReturnCreator, ra appimporting.ReturnApprover) *appimporting.ImportOrdersUseCase {
	return appimporting.NewImportOrdersUseCase(repo, &mockCreator{}, &mockApprover{}, newMockStockChecker(), nil, newMockRater(), "CNY").
		WithReturnHandlers(rc, ra)
}

// 防的事故：orders/cancelled 先于 orders/create 到达（后者在 Shopify 重试队列里）。
// 取消被软跳过后 create 又成功导入，已取消的订单照样扣库存且永远没人冲销。
// 取消要留墓碑，随后的导入直接跳过。
func TestIngestCancelOrder_UnknownOrder_LeavesTombstoneThatBlocksLaterImport(t *testing.T) {
	repo := newMockRepo()
	tenantID, creatorID, warehouseID := mustUUID(t), mustUUID(t), mustUUID(t)
	repo.addMapping("shopify", "SKU-1", mustUUID(t))
	uc := newReturnUC(repo, &mockReturnCreator{}, &mockReturnApprover{})

	res, err := uc.IngestCancelOrder(context.Background(), appimporting.CancelRequest{
		TenantID: tenantID, CreatorID: creatorID, Platform: appimporting.PlatformShopify, PlatformOrderNo: "#LATE",
	})
	if err != nil || !res.NothingToReverse {
		t.Fatalf("cancel = %+v, %v; want NothingToReverse", res, err)
	}

	_, skipped, err := uc.IngestSingleOrder(context.Background(), appimporting.SingleOrderRequest{
		TenantID: tenantID, CreatorID: creatorID, WarehouseID: warehouseID,
		Platform: appimporting.PlatformShopify, PlatformOrderNo: "#LATE",
		Lines: []appimporting.OrderRow{{
			PlatformOrderNo: "#LATE", PlatformSKU: "SKU-1", Qty: decimal.NewFromInt(1),
			UnitPrice: decimal.NewFromInt(10), Currency: "CNY", OrderDate: time.Now().UTC(),
		}},
	})
	if err != nil {
		t.Fatalf("IngestSingleOrder: %v", err)
	}
	if skipped == nil || skipped.Reason != "cancelled" {
		t.Fatalf("skipped = %+v, want reason cancelled", skipped)
	}
	if _, imported := repo.seen["shopify:#LATE"]; imported {
		t.Error("a cancelled order must not be imported")
	}
}

// 并发重复投递：审核阶段发现货已被另一条投递回库，应按"无货可回"收尾，不报 500。
func TestIngestCancelOrder_ApproveFindsNothingLeft_Succeeds(t *testing.T) {
	repo := newMockRepo()
	tenantID, creatorID := mustUUID(t), mustUUID(t)
	repo.seen["shopify:#RACE"] = mustUUID(t)
	uc := newReturnUC(repo, &mockReturnCreator{}, nothingToReverseApprover{})

	res, err := uc.IngestCancelOrder(context.Background(), appimporting.CancelRequest{
		TenantID: tenantID, CreatorID: creatorID, Platform: appimporting.PlatformShopify, PlatformOrderNo: "#RACE",
	})
	if err != nil || !res.NothingToReverse {
		t.Fatalf("cancel = %+v, %v; want NothingToReverse", res, err)
	}
}

func refundReq(tenantID uuid.UUID, orderNo, refundID string) appimporting.RefundRequest {
	return appimporting.RefundRequest{
		TenantID: tenantID, CreatorID: uuid.New(), WarehouseID: uuid.New(),
		Platform: appimporting.PlatformShopify, PlatformOrderNo: orderNo, PlatformRefundID: refundID,
		Currency: "CNY", RefundDate: time.Now().UTC(),
		Lines: []appimporting.RefundLine{{PlatformSKU: "SKU-1", Qty: decimal.NewFromInt(2), RefundAmount: decimal.NewFromInt(10)}},
	}
}

// 防的事故：订单因库存不足从未导入，之后来了退款，却照样以成本 0 入库，凭空多出库存。
// 原单不认识就软跳过，且不记已处理（订单补导入后重发仍可生效）。
func TestIngestRefund_UnknownOrder_SoftSkipsWithoutRestock(t *testing.T) {
	repo := newMockRepo()
	repo.addMapping("shopify", "SKU-1", mustUUID(t))
	rc := &mockReturnCreator{}
	uc := newReturnUC(repo, rc, &mockReturnApprover{})

	res, err := uc.IngestRefund(context.Background(), refundReq(mustUUID(t), "#NEVER", "R1"))
	if err != nil || !res.NothingToReverse {
		t.Fatalf("refund = %+v, %v; want NothingToReverse", res, err)
	}
	if len(rc.created) != 0 {
		t.Error("stock was restored for an order Tally never booked out")
	}
	if len(repo.refunds) != 0 {
		t.Error("refund must not be recorded as handled")
	}
}

// 先取消后退款：退款被封顶到 0 → 200 无货可回，记已处理，重放不再建单。
func TestIngestRefund_NothingLeft_MarksSeenAndReplays(t *testing.T) {
	repo := newMockRepo()
	repo.addMapping("shopify", "SKU-1", mustUUID(t))
	repo.seen["shopify:#1001"] = mustUUID(t)
	rc := &nothingToReverseCreator{}
	uc := newReturnUC(repo, rc, &mockReturnApprover{})
	tenantID := mustUUID(t)

	res, err := uc.IngestRefund(context.Background(), refundReq(tenantID, "#1001", "R2"))
	if err != nil || !res.NothingToReverse {
		t.Fatalf("refund = %+v, %v; want NothingToReverse", res, err)
	}
	res, err = uc.IngestRefund(context.Background(), refundReq(tenantID, "#1001", "R2"))
	if err != nil || !res.NothingToReverse {
		t.Fatalf("replay = %+v, %v; want NothingToReverse", res, err)
	}
	if rc.calls != 1 {
		t.Errorf("creator called %d times, want 1", rc.calls)
	}
}

// 退款重放应带回与首次相同的单号，而不是空串。
func TestIngestRefund_Replay_ReturnsBillNo(t *testing.T) {
	base := newMockRepo()
	billID := mustUUID(t)
	tenantID := mustUUID(t)
	base.refunds = map[string]uuid.UUID{"refund:shopify:R3": billID}
	repo := repoWithBillNo{mockRepo: base, nos: map[uuid.UUID]string{billID: "RT-20260101-0009"}}
	uc := newReturnUC(repo, &mockReturnCreator{}, &mockReturnApprover{})

	res, err := uc.IngestRefund(context.Background(), refundReq(tenantID, "#1", "R3"))
	if err != nil {
		t.Fatalf("IngestRefund: %v", err)
	}
	if res.BillID != billID || res.BillNo != "RT-20260101-0009" {
		t.Errorf("replay = %+v, want bill %s with number", res, billID)
	}
}
