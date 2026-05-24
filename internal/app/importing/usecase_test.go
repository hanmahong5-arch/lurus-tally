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

// ----- mock repo --------------------------------------------------------

type mockRepo struct {
	mappings map[string]*appimporting.SKUMapping // key: platform+":"+platformSKU
	seen     map[string]uuid.UUID                // key: platform+":"+orderNo → billID
	upserted []appimporting.SKUMapping
	marked   []string // orderNos marked seen
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
	m.seen[platform+":"+orderNo] = billID
	m.marked = append(m.marked, orderNo)
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

// ----- helpers ----------------------------------------------------------

func buildUseCase(repo *mockRepo, creator *mockCreator, approver *mockApprover, checker *mockStockChecker, rater *mockRater) *appimporting.ImportOrdersUseCase {
	return appimporting.NewImportOrdersUseCase(repo, creator, approver, checker, rater, "CNY")
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
