package importing_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appimporting "github.com/hanmahong5-arch/lurus-tally/internal/app/importing"
)

// ----- mock repo --------------------------------------------------------

type mockRepo struct {
	mappings  map[string]*appimporting.SKUMapping // key: platform+":"+platformSKU
	seen      map[string]uuid.UUID                // key: platform+":"+orderNo → billID
	cancelled map[string][2]uuid.UUID             // key: "cancel:platform:orderNo" → [origBillID, revBillID]
	refunds   map[string]uuid.UUID                // key: "refund:platform:refundID" → billID
	upserted  []appimporting.SKUMapping
	marked    []string // orderNos marked seen
	markErr   error    // when set, MarkOrderSeen returns this error
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		mappings: make(map[string]*appimporting.SKUMapping),
		seen:     make(map[string]uuid.UUID),
	}
}

func (m *mockRepo) addMapping(platform, sku string, productID uuid.UUID) {
	key := platform + ":" + sku
	m.mappings[key] = &appimporting.SKUMapping{
		ID:          uuid.New(),
		TenantID:    uuid.New(),
		Platform:    platform,
		PlatformSKU: sku,
		ProductID:   productID,
	}
}

func (m *mockRepo) GetMapping(_ context.Context, _ uuid.UUID, platform, sku string) (*appimporting.SKUMapping, error) {
	return m.mappings[platform+":"+sku], nil
}

func (m *mockRepo) UpsertMapping(_ context.Context, mapping *appimporting.SKUMapping) error {
	key := mapping.Platform + ":" + mapping.PlatformSKU
	m.mappings[key] = mapping
	m.upserted = append(m.upserted, *mapping)
	return nil
}

func (m *mockRepo) ListMappings(_ context.Context, _ uuid.UUID, _ string) ([]appimporting.SKUMapping, error) {
	var out []appimporting.SKUMapping
	for _, v := range m.mappings {
		out = append(out, *v)
	}
	return out, nil
}

func (m *mockRepo) IsOrderSeen(_ context.Context, _ uuid.UUID, platform, orderNo string) (bool, uuid.UUID, error) {
	key := platform + ":" + orderNo
	if billID, ok := m.seen[key]; ok {
		return true, billID, nil
	}
	return false, uuid.Nil, nil
}

func (m *mockRepo) MarkOrderSeen(_ context.Context, _ uuid.UUID, platform, orderNo string, billID uuid.UUID) error {
	if m.markErr != nil {
		return m.markErr
	}
	m.seen[platform+":"+orderNo] = billID
	m.marked = append(m.marked, orderNo)
	return nil
}

func (m *mockRepo) IsCancelSeen(_ context.Context, _ uuid.UUID, platform, orderNo string) (bool, uuid.UUID, uuid.UUID, error) {
	key := "cancel:" + platform + ":" + orderNo
	if ids, ok := m.cancelled[key]; ok {
		return true, ids[0], ids[1], nil
	}
	return false, uuid.Nil, uuid.Nil, nil
}

func (m *mockRepo) MarkCancelSeen(_ context.Context, _ uuid.UUID, platform, orderNo string, origBillID, revBillID uuid.UUID) error {
	if m.cancelled == nil {
		m.cancelled = make(map[string][2]uuid.UUID)
	}
	m.cancelled["cancel:"+platform+":"+orderNo] = [2]uuid.UUID{origBillID, revBillID}
	return nil
}

func (m *mockRepo) IsRefundSeen(_ context.Context, _ uuid.UUID, platform, refundID string) (bool, uuid.UUID, error) {
	key := "refund:" + platform + ":" + refundID
	if billID, ok := m.refunds[key]; ok {
		return true, billID, nil
	}
	return false, uuid.Nil, nil
}

func (m *mockRepo) MarkRefundSeen(_ context.Context, _ uuid.UUID, platform, _, refundID string, billID uuid.UUID) error {
	if m.refunds == nil {
		m.refunds = make(map[string]uuid.UUID)
	}
	m.refunds["refund:"+platform+":"+refundID] = billID
	return nil
}

// ----- mock SaleCreator -------------------------------------------------

type mockCreator struct {
	created []appimporting.SaleCreatorInput
}

func (c *mockCreator) Create(_ context.Context, req appimporting.SaleCreatorInput) (*appimporting.SaleCreatorOutput, error) {
	c.created = append(c.created, req)
	return &appimporting.SaleCreatorOutput{
		BillID: uuid.New(),
		BillNo: "SL-20260101-0001",
	}, nil
}

// ----- mock SaleApprover ------------------------------------------------

type mockApprover struct {
	approved []uuid.UUID
}

func (a *mockApprover) Approve(_ context.Context, req appimporting.SaleApproverInput) error {
	a.approved = append(a.approved, req.BillID)
	return nil
}

// ----- mock StockChecker ------------------------------------------------

type mockStockChecker struct {
	// qty keyed by productID string; returns zero when absent (= no stock).
	qty map[string]decimal.Decimal
}

func newMockStockChecker() *mockStockChecker {
	return &mockStockChecker{qty: make(map[string]decimal.Decimal)}
}

func (s *mockStockChecker) AvailableQty(_ context.Context, _, productID, _ uuid.UUID) (decimal.Decimal, error) {
	if q, ok := s.qty[productID.String()]; ok {
		return q, nil
	}
	return decimal.Zero, nil
}

// ----- mock CurrencyRater -----------------------------------------------

type mockRater struct {
	rates map[string]decimal.Decimal // key: from+":"+to
}

func newMockRater() *mockRater {
	return &mockRater{rates: make(map[string]decimal.Decimal)}
}

func (r *mockRater) GetRate(_ context.Context, _ uuid.UUID, from, to string, _ time.Time) (decimal.Decimal, error) {
	if v, ok := r.rates[from+":"+to]; ok {
		return v, nil
	}
	return decimal.NewFromInt(1), nil // identity fallback
}

// ----- mock ReturnCreator -----------------------------------------------

type mockReturnCreator struct {
	created []appimporting.ReturnCreatorInput
}

func (c *mockReturnCreator) Create(_ context.Context, req appimporting.ReturnCreatorInput) (*appimporting.ReturnCreatorOutput, error) {
	c.created = append(c.created, req)
	return &appimporting.ReturnCreatorOutput{
		BillID: uuid.New(),
		BillNo: "RT-20260101-0001",
	}, nil
}

// ----- mock ReturnApprover ----------------------------------------------

type mockReturnApprover struct {
	approved []uuid.UUID
}

func (a *mockReturnApprover) Approve(_ context.Context, req appimporting.ReturnApproverInput) error {
	a.approved = append(a.approved, req.BillID)
	return nil
}

// ----- helpers ----------------------------------------------------------

func buildUseCase(repo *mockRepo, creator *mockCreator, approver *mockApprover, checker *mockStockChecker, rater *mockRater) *appimporting.ImportOrdersUseCase {
	// whChecker nil: existing tests do not exercise cross-tenant warehouse rejection.
	// See usecase_warehouse_check_test.go for the dedicated coverage.
	return appimporting.NewImportOrdersUseCase(repo, creator, approver, checker, nil, rater, "CNY")
}

func buildUseCaseWithReturn(repo *mockRepo, creator *mockCreator, approver *mockApprover, retCreator *mockReturnCreator, retApprover *mockReturnApprover, checker *mockStockChecker, rater *mockRater) *appimporting.ImportOrdersUseCase {
	uc := appimporting.NewImportOrdersUseCase(repo, creator, approver, checker, nil, rater, "CNY")
	return uc.WithReturnHandlers(retCreator, retApprover)
}

func mustUUID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.NewRandom()
	if err != nil {
		t.Fatalf("uuid: %v", err)
	}
	return id
}

// ----- Amazon CSV fixture -----------------------------------------------

func amazonCSV(rows ...string) []byte {
	header := "order-id,sku,quantity-purchased,item-price,currency,purchase-date"
	return []byte(header + "\n" + strings.Join(rows, "\n"))
}

// ----- tests ------------------------------------------------------------

func TestImportOrdersUseCase_AmazonCSV_HappyPath(t *testing.T) {
	repo := newMockRepo()
	creator := &mockCreator{}
	approver := &mockApprover{}
	checker := newMockStockChecker()
	rater := newMockRater()

	productID := mustUUID(t)
	repo.addMapping("amazon", "SKU-001", productID)

	tenantID := mustUUID(t)
	warehouseID := mustUUID(t)
	creatorID := mustUUID(t)

	uc := buildUseCase(repo, creator, approver, checker, rater)

	csv := amazonCSV("ORD-001,SKU-001,2,50.00,CNY,2026-01-15")

	result, err := uc.Execute(context.Background(), appimporting.ImportRequest{
		TenantID:    tenantID,
		CreatorID:   creatorID,
		WarehouseID: warehouseID,
		Platform:    appimporting.PlatformAmazon,
		CSVData:     csv,
	})

	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.Imported) != 1 {
		t.Fatalf("expected 1 imported, got %d", len(result.Imported))
	}
	if result.Imported[0].PlatformOrderNo != "ORD-001" {
		t.Errorf("order no: got %s, want ORD-001", result.Imported[0].PlatformOrderNo)
	}
	if len(creator.created) != 1 {
		t.Fatalf("expected 1 sale created, got %d", len(creator.created))
	}
	if len(approver.approved) != 1 {
		t.Fatalf("expected 1 sale approved, got %d", len(approver.approved))
	}
	if len(repo.marked) != 1 || repo.marked[0] != "ORD-001" {
		t.Errorf("expected ORD-001 marked seen, got %v", repo.marked)
	}
}

func TestImportOrdersUseCase_Dedup_SkipsAlreadySeen(t *testing.T) {
	repo := newMockRepo()
	creator := &mockCreator{}
	approver := &mockApprover{}
	checker := newMockStockChecker()
	rater := newMockRater()

	productID := mustUUID(t)
	repo.addMapping("amazon", "SKU-001", productID)
	existingBillID := mustUUID(t)
	// Pre-populate the seen table.
	repo.seen["amazon:ORD-001"] = existingBillID

	tenantID := mustUUID(t)
	warehouseID := mustUUID(t)
	creatorID := mustUUID(t)

	uc := buildUseCase(repo, creator, approver, checker, rater)
	csv := amazonCSV("ORD-001,SKU-001,2,50.00,CNY,2026-01-15")

	result, err := uc.Execute(context.Background(), appimporting.ImportRequest{
		TenantID:    tenantID,
		CreatorID:   creatorID,
		WarehouseID: warehouseID,
		Platform:    appimporting.PlatformAmazon,
		CSVData:     csv,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.Imported) != 0 {
		t.Errorf("expected 0 imported (dedup), got %d", len(result.Imported))
	}
	if len(result.Skipped) != 1 {
		t.Fatalf("expected 1 skipped, got %d", len(result.Skipped))
	}
	if !strings.Contains(result.Skipped[0].Reason, "duplicate") {
		t.Errorf("expected duplicate reason, got %s", result.Skipped[0].Reason)
	}
	if len(creator.created) != 0 {
		t.Errorf("expected no bills created on duplicate, got %d", len(creator.created))
	}
}

func TestImportOrdersUseCase_UnknownSKU_Flagged(t *testing.T) {
	repo := newMockRepo()
	creator := &mockCreator{}
	approver := &mockApprover{}
	checker := newMockStockChecker()
	rater := newMockRater()

	tenantID := mustUUID(t)
	warehouseID := mustUUID(t)
	creatorID := mustUUID(t)

	uc := buildUseCase(repo, creator, approver, checker, rater)
	// "SKU-UNKNOWN" has no mapping in the repo and no hint.
	csv := amazonCSV("ORD-002,SKU-UNKNOWN,1,30.00,CNY,2026-01-16")

	result, err := uc.Execute(context.Background(), appimporting.ImportRequest{
		TenantID:    tenantID,
		CreatorID:   creatorID,
		WarehouseID: warehouseID,
		Platform:    appimporting.PlatformAmazon,
		CSVData:     csv,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.UnknownSKUs) != 1 {
		t.Fatalf("expected 1 unknown sku, got %d", len(result.UnknownSKUs))
	}
	if result.UnknownSKUs[0].PlatformSKU != "SKU-UNKNOWN" {
		t.Errorf("unknown sku: got %s", result.UnknownSKUs[0].PlatformSKU)
	}
	if len(creator.created) != 0 {
		t.Errorf("expected no bill for unknown sku, got %d", len(creator.created))
	}
}

func TestImportOrdersUseCase_Hint_PersistsMapping(t *testing.T) {
	repo := newMockRepo()
	creator := &mockCreator{}
	approver := &mockApprover{}
	checker := newMockStockChecker()
	rater := newMockRater()

	tenantID := mustUUID(t)
	warehouseID := mustUUID(t)
	creatorID := mustUUID(t)
	productID := mustUUID(t)

	uc := buildUseCase(repo, creator, approver, checker, rater)
	csv := amazonCSV("ORD-003,SKU-NEW,1,100.00,CNY,2026-01-17")

	result, err := uc.Execute(context.Background(), appimporting.ImportRequest{
		TenantID:    tenantID,
		CreatorID:   creatorID,
		WarehouseID: warehouseID,
		Platform:    appimporting.PlatformAmazon,
		CSVData:     csv,
		SKUHints: []appimporting.SKUHint{
			{PlatformSKU: "SKU-NEW", ProductID: productID},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.Imported) != 1 {
		t.Fatalf("expected 1 imported via hint, got %d", len(result.Imported))
	}
	// The mapping should have been persisted.
	if len(repo.upserted) == 0 {
		t.Errorf("expected mapping upserted, got none")
	}
	if repo.upserted[0].PlatformSKU != "SKU-NEW" || repo.upserted[0].ProductID != productID {
		t.Errorf("unexpected upserted mapping: %+v", repo.upserted[0])
	}
}

func TestImportOrdersUseCase_DryRun_OversellFlagged(t *testing.T) {
	repo := newMockRepo()
	creator := &mockCreator{}
	approver := &mockApprover{}
	checker := newMockStockChecker()
	rater := newMockRater()

	productID := mustUUID(t)
	repo.addMapping("amazon", "SKU-LOW", productID)
	checker.qty[productID.String()] = decimal.NewFromInt(1) // only 1 in stock

	tenantID := mustUUID(t)
	warehouseID := mustUUID(t)
	creatorID := mustUUID(t)

	uc := buildUseCase(repo, creator, approver, checker, rater)
	// Request 5, have 1 → oversell
	csv := amazonCSV("ORD-004,SKU-LOW,5,20.00,CNY,2026-01-18")

	result, err := uc.Execute(context.Background(), appimporting.ImportRequest{
		TenantID:    tenantID,
		CreatorID:   creatorID,
		WarehouseID: warehouseID,
		Platform:    appimporting.PlatformAmazon,
		CSVData:     csv,
		DryRun:      true,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.Oversells) != 1 {
		t.Fatalf("expected 1 oversell row, got %d", len(result.Oversells))
	}
	if result.Oversells[0].ProductID != productID {
		t.Errorf("oversell product_id mismatch")
	}
	if !result.Oversells[0].Requested.Equal(decimal.NewFromInt(5)) {
		t.Errorf("oversell requested: got %s", result.Oversells[0].Requested)
	}
	// No bills created in dry-run.
	if len(creator.created) != 0 {
		t.Errorf("expected no bills in dry-run, got %d", len(creator.created))
	}
}

func TestImportOrdersUseCase_FXConversion(t *testing.T) {
	repo := newMockRepo()
	creator := &mockCreator{}
	approver := &mockApprover{}
	checker := newMockStockChecker()
	rater := newMockRater()
	rater.rates["USD:CNY"] = decimal.NewFromFloat(7.25)

	productID := mustUUID(t)
	repo.addMapping("shopify", "SHOPIFY-SKU", productID)

	tenantID := mustUUID(t)
	warehouseID := mustUUID(t)
	creatorID := mustUUID(t)

	uc := buildUseCase(repo, creator, approver, checker, rater)

	// Shopify CSV header: Name,Lineitem sku,Lineitem quantity,Lineitem price,Currency,Created at
	csv := []byte("Name,Lineitem sku,Lineitem quantity,Lineitem price,Currency,Created at\n" +
		"#1001,SHOPIFY-SKU,3,10.00,USD,2026-01-20\n")

	result, err := uc.Execute(context.Background(), appimporting.ImportRequest{
		TenantID:    tenantID,
		CreatorID:   creatorID,
		WarehouseID: warehouseID,
		Platform:    appimporting.PlatformShopify,
		CSVData:     csv,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.Imported) != 1 {
		t.Fatalf("expected 1 imported, got %d", len(result.Imported))
	}
	// Creator should receive price converted: 10.00 USD × 7.25 = 72.5000 CNY
	if len(creator.created) != 1 {
		t.Fatalf("expected 1 sale created, got %d", len(creator.created))
	}
	items := creator.created[0].Items
	if len(items) == 0 {
		t.Fatalf("no line items")
	}
	expected := decimal.NewFromFloat(72.5)
	if !items[0].UnitPrice.Equal(expected) {
		t.Errorf("unit_price after FX: got %s, want %s", items[0].UnitPrice, expected)
	}
}

// UAT-3 Bug 1: when MarkOrderSeen fails after the bill committed, the use case
// must NOT halt the batch and NOT roll back. The bill is reported as Imported
// with MarkSeenError set, so the next import attempt's stage-2 fallback in
// Repo.IsOrderSeen can self-heal the dedup row.
func TestImportOrdersUseCase_MarkOrderSeenFailure_StillReportsImported(t *testing.T) {
	repo := newMockRepo()
	repo.markErr = errors.New("transient DB error")
	creator := &mockCreator{}
	approver := &mockApprover{}
	checker := newMockStockChecker()
	rater := newMockRater()

	productID := mustUUID(t)
	repo.addMapping("amazon", "SKU-001", productID)

	uc := buildUseCase(repo, creator, approver, checker, rater)
	csv := amazonCSV("ORD-001,SKU-001,1,50.00,CNY,2026-01-15")

	result, err := uc.Execute(context.Background(), appimporting.ImportRequest{
		TenantID:    mustUUID(t),
		CreatorID:   mustUUID(t),
		WarehouseID: mustUUID(t),
		Platform:    appimporting.PlatformAmazon,
		CSVData:     csv,
	})
	if err != nil {
		t.Fatalf("Execute should not return error when only MarkOrderSeen fails: %v", err)
	}
	if len(result.Imported) != 1 {
		t.Fatalf("expected 1 imported (bill committed), got %d", len(result.Imported))
	}
	if result.Imported[0].MarkSeenError == "" {
		t.Error("expected MarkSeenError to be set on the imported order")
	}
	if len(creator.created) != 1 {
		t.Errorf("bill should have been created, got %d", len(creator.created))
	}
	if len(approver.approved) != 1 {
		t.Errorf("bill should have been approved, got %d", len(approver.approved))
	}
}

// ----- IngestCancelOrder tests -----------------------------------------------

func TestIngestCancelOrder_Happy(t *testing.T) {
	repo := newMockRepo()
	creator := &mockCreator{}
	approver := &mockApprover{}
	retCreator := &mockReturnCreator{}
	retApprover := &mockReturnApprover{}
	checker := newMockStockChecker()
	rater := newMockRater()

	tenantID, creatorID := mustUUID(t), mustUUID(t)
	origBillID := mustUUID(t)
	// Pre-populate: order was previously imported.
	repo.seen["shopify:#CANCEL-001"] = origBillID

	uc := buildUseCaseWithReturn(repo, creator, approver, retCreator, retApprover, checker, rater)

	result, err := uc.IngestCancelOrder(context.Background(), appimporting.CancelRequest{
		TenantID:        tenantID,
		CreatorID:       creatorID,
		Platform:        appimporting.PlatformShopify,
		PlatformOrderNo: "#CANCEL-001",
	})
	if err != nil {
		t.Fatalf("IngestCancelOrder: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.OriginalBillID != origBillID {
		t.Errorf("original_bill_id: got %s, want %s", result.OriginalBillID, origBillID)
	}
	if result.ReversalBillID == uuid.Nil {
		t.Error("expected non-nil reversal_bill_id")
	}
	if len(retCreator.created) != 1 {
		t.Errorf("expected 1 reversal bill created, got %d", len(retCreator.created))
	}
	if len(retApprover.approved) != 1 {
		t.Errorf("expected 1 reversal bill approved, got %d", len(retApprover.approved))
	}
	// Verify cancel dedup row was written.
	seen, _, _, sErr := repo.IsCancelSeen(context.Background(), tenantID, "shopify", "#CANCEL-001")
	if sErr != nil || !seen {
		t.Error("expected cancel seen row written")
	}
}

func TestIngestCancelOrder_Dedup_ReturnsExisting(t *testing.T) {
	repo := newMockRepo()
	retCreator := &mockReturnCreator{}
	retApprover := &mockReturnApprover{}

	tenantID, creatorID := mustUUID(t), mustUUID(t)
	origBillID, revBillID := mustUUID(t), mustUUID(t)

	// Pre-populate both seen rows.
	repo.seen["shopify:#CANCEL-DUP"] = origBillID
	if repo.cancelled == nil {
		repo.cancelled = make(map[string][2]uuid.UUID)
	}
	repo.cancelled["cancel:shopify:#CANCEL-DUP"] = [2]uuid.UUID{origBillID, revBillID}

	uc := buildUseCaseWithReturn(repo, &mockCreator{}, &mockApprover{}, retCreator, retApprover, newMockStockChecker(), newMockRater())

	result, err := uc.IngestCancelOrder(context.Background(), appimporting.CancelRequest{
		TenantID:        tenantID,
		CreatorID:       creatorID,
		Platform:        appimporting.PlatformShopify,
		PlatformOrderNo: "#CANCEL-DUP",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ReversalBillID != revBillID {
		t.Errorf("expected existing reversal_bill_id on dup, got %s", result.ReversalBillID)
	}
	// No new bill should be created.
	if len(retCreator.created) != 0 {
		t.Errorf("expected no new reversal bill on dup, got %d", len(retCreator.created))
	}
}

func TestIngestCancelOrder_MissingOriginal_ReturnsError(t *testing.T) {
	repo := newMockRepo()
	retCreator := &mockReturnCreator{}
	retApprover := &mockReturnApprover{}

	uc := buildUseCaseWithReturn(repo, &mockCreator{}, &mockApprover{}, retCreator, retApprover, newMockStockChecker(), newMockRater())

	_, err := uc.IngestCancelOrder(context.Background(), appimporting.CancelRequest{
		TenantID:        mustUUID(t),
		CreatorID:       mustUUID(t),
		Platform:        appimporting.PlatformShopify,
		PlatformOrderNo: "#NEVER-IMPORTED",
	})
	if err == nil {
		t.Fatal("expected error for unimported order, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

// ----- IngestRefund tests ----------------------------------------------------

func TestIngestRefund_Happy(t *testing.T) {
	repo := newMockRepo()
	retCreator := &mockReturnCreator{}
	retApprover := &mockReturnApprover{}
	rater := newMockRater()

	tenantID, creatorID, warehouseID := mustUUID(t), mustUUID(t), mustUUID(t)
	productID := mustUUID(t)
	repo.addMapping("shopify", "REF-SKU", productID)

	uc := buildUseCaseWithReturn(repo, &mockCreator{}, &mockApprover{}, retCreator, retApprover, newMockStockChecker(), rater)

	result, err := uc.IngestRefund(context.Background(), appimporting.RefundRequest{
		TenantID:         tenantID,
		CreatorID:        creatorID,
		WarehouseID:      warehouseID,
		Platform:         appimporting.PlatformShopify,
		PlatformOrderNo:  "#3001",
		PlatformRefundID: "REFUND-001",
		Currency:         "CNY",
		RefundDate:       time.Now().UTC(),
		Lines: []appimporting.RefundLine{
			{PlatformSKU: "REF-SKU", Qty: decimal.NewFromInt(2), RefundAmount: decimal.NewFromFloat(50.00)},
		},
	})
	if err != nil {
		t.Fatalf("IngestRefund: %v", err)
	}
	if result == nil || result.BillID == uuid.Nil {
		t.Fatal("expected non-nil result with bill_id")
	}
	if result.PlatformRefundID != "REFUND-001" {
		t.Errorf("platform_refund_id: got %s", result.PlatformRefundID)
	}
	if len(retCreator.created) != 1 {
		t.Errorf("expected 1 refund bill created, got %d", len(retCreator.created))
	}
	if len(retApprover.approved) != 1 {
		t.Errorf("expected 1 refund bill approved, got %d", len(retApprover.approved))
	}
}

func TestIngestRefund_Dedup_ReturnsExistingBillID(t *testing.T) {
	repo := newMockRepo()
	retCreator := &mockReturnCreator{}
	retApprover := &mockReturnApprover{}

	tenantID, creatorID, warehouseID := mustUUID(t), mustUUID(t), mustUUID(t)
	existingBillID := mustUUID(t)
	if repo.refunds == nil {
		repo.refunds = make(map[string]uuid.UUID)
	}
	repo.refunds["refund:shopify:REFUND-DUP"] = existingBillID

	uc := buildUseCaseWithReturn(repo, &mockCreator{}, &mockApprover{}, retCreator, retApprover, newMockStockChecker(), newMockRater())

	result, err := uc.IngestRefund(context.Background(), appimporting.RefundRequest{
		TenantID:         tenantID,
		CreatorID:        creatorID,
		WarehouseID:      warehouseID,
		Platform:         appimporting.PlatformShopify,
		PlatformOrderNo:  "#3002",
		PlatformRefundID: "REFUND-DUP",
		Currency:         "CNY",
		RefundDate:       time.Now().UTC(),
		Lines: []appimporting.RefundLine{
			{PlatformSKU: "ANY-SKU", Qty: decimal.NewFromInt(1), RefundAmount: decimal.NewFromFloat(10.00)},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BillID != existingBillID {
		t.Errorf("expected existing bill_id on dup, got %s", result.BillID)
	}
	if len(retCreator.created) != 0 {
		t.Errorf("no new bill should be created on refund dup, got %d", len(retCreator.created))
	}
}

func TestIngestRefund_MissingOriginal_ReturnsError(t *testing.T) {
	repo := newMockRepo()
	retCreator := &mockReturnCreator{}
	retApprover := &mockReturnApprover{}

	uc := buildUseCaseWithReturn(repo, &mockCreator{}, &mockApprover{}, retCreator, retApprover, newMockStockChecker(), newMockRater())

	// "NO-SUCH-SKU" is not in import_sku_map.
	_, err := uc.IngestRefund(context.Background(), appimporting.RefundRequest{
		TenantID:         mustUUID(t),
		CreatorID:        mustUUID(t),
		WarehouseID:      mustUUID(t),
		Platform:         appimporting.PlatformShopify,
		PlatformOrderNo:  "#3003",
		PlatformRefundID: "REFUND-MISSING-SKU",
		Currency:         "CNY",
		RefundDate:       time.Now().UTC(),
		Lines: []appimporting.RefundLine{
			{PlatformSKU: "NO-SUCH-SKU", Qty: decimal.NewFromInt(1), RefundAmount: decimal.NewFromFloat(10.00)},
		},
	})
	if err == nil {
		t.Fatal("expected error for unknown SKU, got nil")
	}
	if !strings.Contains(err.Error(), "not in import_sku_map") {
		t.Errorf("expected 'not in import_sku_map' error, got: %v", err)
	}
}

// ----- F06 fix tests --------------------------------------------------------

// TestDryRun_OversellRow_PlatformSKUNonEmpty verifies the F06 fix:
// OversellRow.PlatformSKU must not be empty in preview mode so the UI can
// display "which SKU is out of stock" without requiring a separate lookup.
func TestDryRun_OversellRow_PlatformSKUNonEmpty(t *testing.T) {
	repo := newMockRepo()
	creator := &mockCreator{}
	approver := &mockApprover{}
	checker := newMockStockChecker()
	rater := newMockRater()

	productID := mustUUID(t)
	const wantSKU = "OVERSELL-SKU"
	repo.addMapping("amazon", wantSKU, productID)
	checker.qty[productID.String()] = decimal.NewFromInt(1) // only 1 in stock

	uc := buildUseCase(repo, creator, approver, checker, rater)
	csv := amazonCSV("ORD-OVERSELL," + wantSKU + ",5,20.00,CNY,2026-01-18")

	result, err := uc.Execute(context.Background(), appimporting.ImportRequest{
		TenantID:    uuid.New(),
		CreatorID:   uuid.New(),
		WarehouseID: uuid.New(),
		Platform:    appimporting.PlatformAmazon,
		CSVData:     csv,
		DryRun:      true,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.Oversells) != 1 {
		t.Fatalf("expected 1 oversell row, got %d", len(result.Oversells))
	}
	got := result.Oversells[0].PlatformSKU
	if got == "" {
		t.Error("F06: OversellRow.PlatformSKU must not be empty in DryRun mode")
	}
	if got != wantSKU {
		t.Errorf("F06: OversellRow.PlatformSKU got %q, want %q", got, wantSKU)
	}
}

func TestParseCSV_Amazon_InvalidQty(t *testing.T) {
	csv := amazonCSV("ORD-X,SKU-X,zero,50.00,CNY,2026-01-01")
	repo := newMockRepo()
	productID := mustUUID(t)
	repo.addMapping("amazon", "SKU-X", productID)
	uc := buildUseCase(repo, &mockCreator{}, &mockApprover{}, newMockStockChecker(), newMockRater())

	_, err := uc.Execute(context.Background(), appimporting.ImportRequest{
		TenantID:    uuid.New(),
		CreatorID:   uuid.New(),
		WarehouseID: uuid.New(),
		Platform:    appimporting.PlatformAmazon,
		CSVData:     csv,
	})
	if err == nil {
		t.Fatal("expected error for invalid qty, got nil")
	}
	if !strings.Contains(err.Error(), "invalid qty") {
		t.Errorf("error should mention invalid qty: %v", err)
	}
}
