package importing_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appimporting "github.com/hanmahong5-arch/lurus-tally/internal/app/importing"
)

// ----- helpers reused from usecase_test.go (same package _test) ---------------

func singleOrderReq(tenantID, creatorID, warehouseID uuid.UUID, platform appimporting.Platform, orderNo string, lines []appimporting.OrderRow) appimporting.SingleOrderRequest {
	return appimporting.SingleOrderRequest{
		TenantID:        tenantID,
		CreatorID:       creatorID,
		WarehouseID:     warehouseID,
		Platform:        platform,
		PlatformOrderNo: orderNo,
		Lines:           lines,
	}
}

func singleLine(orderNo, sku string, qty float64, price, currency string) appimporting.OrderRow {
	return appimporting.OrderRow{
		PlatformOrderNo: orderNo,
		PlatformSKU:     sku,
		Qty:             decimal.NewFromFloat(qty),
		UnitPrice:       decimal.RequireFromString(price),
		Currency:        currency,
		OrderDate:       time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC),
	}
}

// ----- tests ----------------------------------------------------------------

func TestIngestSingleOrder_Success(t *testing.T) {
	repo := newMockRepo()
	creator := &mockCreator{}
	approver := &mockApprover{}
	checker := newMockStockChecker()
	rater := newMockRater()

	productID := mustUUID(t)
	repo.addMapping("shopify", "WEBHOOK-SKU", productID)

	tenantID, creatorID, warehouseID := mustUUID(t), mustUUID(t), mustUUID(t)
	uc := buildUseCase(repo, creator, approver, checker, rater)

	req := singleOrderReq(tenantID, creatorID, warehouseID,
		appimporting.PlatformShopify, "#WH-001",
		[]appimporting.OrderRow{singleLine("#WH-001", "WEBHOOK-SKU", 2, "100.00", "CNY")},
	)

	imported, skipped, err := uc.IngestSingleOrder(context.Background(), req)
	if err != nil {
		t.Fatalf("IngestSingleOrder: %v", err)
	}
	if skipped != nil {
		t.Fatalf("expected no skip, got %+v", skipped)
	}
	if imported.PlatformOrderNo != "#WH-001" {
		t.Errorf("platform_order_no: got %s", imported.PlatformOrderNo)
	}
	if imported.BillID == uuid.Nil {
		t.Error("expected non-nil BillID")
	}
	if len(creator.created) != 1 {
		t.Errorf("expected 1 bill created, got %d", len(creator.created))
	}
	if len(approver.approved) != 1 {
		t.Errorf("expected 1 bill approved, got %d", len(approver.approved))
	}
	if len(repo.marked) != 1 || repo.marked[0] != "#WH-001" {
		t.Errorf("expected #WH-001 marked seen, got %v", repo.marked)
	}
}

func TestIngestSingleOrder_Dedup_ReturnsSkipped(t *testing.T) {
	repo := newMockRepo()
	creator := &mockCreator{}
	approver := &mockApprover{}
	checker := newMockStockChecker()
	rater := newMockRater()

	productID := mustUUID(t)
	repo.addMapping("shopify", "WEBHOOK-SKU", productID)
	existingBillID := mustUUID(t)
	repo.seen["shopify:#WH-DUP"] = existingBillID

	tenantID, creatorID, warehouseID := mustUUID(t), mustUUID(t), mustUUID(t)
	uc := buildUseCase(repo, creator, approver, checker, rater)

	req := singleOrderReq(tenantID, creatorID, warehouseID,
		appimporting.PlatformShopify, "#WH-DUP",
		[]appimporting.OrderRow{singleLine("#WH-DUP", "WEBHOOK-SKU", 1, "50.00", "CNY")},
	)

	imported, skipped, err := uc.IngestSingleOrder(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skipped == nil {
		t.Fatal("expected skipped, got nil")
	}
	if !strings.Contains(skipped.Reason, "duplicate") {
		t.Errorf("reason: got %s", skipped.Reason)
	}
	if imported.BillID != uuid.Nil {
		t.Error("expected zero ImportedOrder on dedup")
	}
	if len(creator.created) != 0 {
		t.Errorf("no bill should be created on dedup, got %d", len(creator.created))
	}
}

func TestIngestSingleOrder_UnknownSKU_ReturnsSkipped(t *testing.T) {
	repo := newMockRepo()
	creator := &mockCreator{}
	approver := &mockApprover{}
	checker := newMockStockChecker()
	rater := newMockRater()

	tenantID, creatorID, warehouseID := mustUUID(t), mustUUID(t), mustUUID(t)
	uc := buildUseCase(repo, creator, approver, checker, rater)

	req := singleOrderReq(tenantID, creatorID, warehouseID,
		appimporting.PlatformShopify, "#WH-SKU",
		[]appimporting.OrderRow{singleLine("#WH-SKU", "NO-SUCH-SKU", 1, "30.00", "CNY")},
	)

	_, skipped, err := uc.IngestSingleOrder(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skipped == nil {
		t.Fatal("expected skipped for unknown sku")
	}
	if !strings.Contains(skipped.Reason, "unknown_sku") {
		t.Errorf("reason: got %s, want unknown_sku:...", skipped.Reason)
	}
	if len(creator.created) != 0 {
		t.Errorf("no bill should be created for unknown SKU, got %d", len(creator.created))
	}
}

func TestIngestSingleOrder_FXConversion(t *testing.T) {
	repo := newMockRepo()
	creator := &mockCreator{}
	approver := &mockApprover{}
	checker := newMockStockChecker()
	rater := newMockRater()
	rater.rates["USD:CNY"] = decimal.NewFromFloat(7.3)

	productID := mustUUID(t)
	repo.addMapping("shopify", "FX-SKU", productID)

	tenantID, creatorID, warehouseID := mustUUID(t), mustUUID(t), mustUUID(t)
	uc := buildUseCase(repo, creator, approver, checker, rater)

	req := singleOrderReq(tenantID, creatorID, warehouseID,
		appimporting.PlatformShopify, "#WH-FX",
		[]appimporting.OrderRow{singleLine("#WH-FX", "FX-SKU", 1, "10.00", "USD")},
	)

	imported, skipped, err := uc.IngestSingleOrder(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skipped != nil {
		t.Fatalf("unexpected skip: %+v", skipped)
	}
	if imported.BillID == uuid.Nil {
		t.Error("expected non-nil BillID")
	}
	if len(creator.created) != 1 {
		t.Fatalf("expected 1 bill created, got %d", len(creator.created))
	}
	expected := decimal.NewFromFloat(73.0) // 10.00 * 7.3 = 73.0000
	got := creator.created[0].Items[0].UnitPrice
	if !got.Equal(expected) {
		t.Errorf("FX price: got %s, want %s", got, expected)
	}
}
