package importing

// Internal-package (whitebox) coverage tests. These complement the external
// *_test.go files (package importing_test) by exercising unexported helpers
// (parseCSV/buildRow/columnIndex/appendUniqueSKU/resolveSKU) directly and by
// hitting error/guard branches on the exported use-case methods that the
// external tests do not reach (nil-field guards, infra-error propagation from
// every collaborator, and the "soft failure" dedup-write paths).
//
// Naming: all fakes/helpers are prefixed zz to avoid any accidental symbol
// collision with production code or with fakes defined in the external test
// package (which lives in a different Go package namespace anyway).

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ----- fakes -----------------------------------------------------------

type zzFakeRepo struct {
	mappings      map[string]*SKUMapping
	getMappingErr error
	upsertErr     error
	listErr       error
	listOut       []SKUMapping
	upserted      []SKUMapping

	seen             map[string]uuid.UUID
	isOrderSeenErr   error
	markOrderSeenErr error
	marked           []string

	cancelled         map[string][2]uuid.UUID
	isCancelSeenErr   error
	markCancelSeenErr error

	refunds           map[string]uuid.UUID
	isRefundSeenErr   error
	markRefundSeenErr error
}

func newZZFakeRepo() *zzFakeRepo {
	return &zzFakeRepo{
		mappings:  make(map[string]*SKUMapping),
		seen:      make(map[string]uuid.UUID),
		cancelled: make(map[string][2]uuid.UUID),
		refunds:   make(map[string]uuid.UUID),
	}
}

func (r *zzFakeRepo) addMapping(platform, sku string, productID uuid.UUID) {
	r.mappings[platform+":"+sku] = &SKUMapping{Platform: platform, PlatformSKU: sku, ProductID: productID}
}

func (r *zzFakeRepo) GetMapping(_ context.Context, _ uuid.UUID, platform, sku string) (*SKUMapping, error) {
	if r.getMappingErr != nil {
		return nil, r.getMappingErr
	}
	return r.mappings[platform+":"+sku], nil
}

func (r *zzFakeRepo) UpsertMapping(_ context.Context, m *SKUMapping) error {
	if r.upsertErr != nil {
		return r.upsertErr
	}
	r.upserted = append(r.upserted, *m)
	return nil
}

func (r *zzFakeRepo) ListMappings(_ context.Context, _ uuid.UUID, _ string) ([]SKUMapping, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.listOut, nil
}

func (r *zzFakeRepo) IsOrderSeen(_ context.Context, _ uuid.UUID, platform, orderNo string) (bool, uuid.UUID, error) {
	if r.isOrderSeenErr != nil {
		return false, uuid.Nil, r.isOrderSeenErr
	}
	if id, ok := r.seen[platform+":"+orderNo]; ok {
		return true, id, nil
	}
	return false, uuid.Nil, nil
}

func (r *zzFakeRepo) MarkOrderSeen(_ context.Context, _ uuid.UUID, platform, orderNo string, billID uuid.UUID) error {
	if r.markOrderSeenErr != nil {
		return r.markOrderSeenErr
	}
	r.seen[platform+":"+orderNo] = billID
	r.marked = append(r.marked, orderNo)
	return nil
}

func (r *zzFakeRepo) IsCancelSeen(_ context.Context, _ uuid.UUID, platform, orderNo string) (bool, uuid.UUID, uuid.UUID, error) {
	if r.isCancelSeenErr != nil {
		return false, uuid.Nil, uuid.Nil, r.isCancelSeenErr
	}
	if ids, ok := r.cancelled["cancel:"+platform+":"+orderNo]; ok {
		return true, ids[0], ids[1], nil
	}
	return false, uuid.Nil, uuid.Nil, nil
}

func (r *zzFakeRepo) MarkCancelSeen(_ context.Context, _ uuid.UUID, platform, orderNo string, origBillID, revBillID uuid.UUID) error {
	if r.markCancelSeenErr != nil {
		return r.markCancelSeenErr
	}
	r.cancelled["cancel:"+platform+":"+orderNo] = [2]uuid.UUID{origBillID, revBillID}
	return nil
}

func (r *zzFakeRepo) IsRefundSeen(_ context.Context, _ uuid.UUID, platform, refundID string) (bool, uuid.UUID, error) {
	if r.isRefundSeenErr != nil {
		return false, uuid.Nil, r.isRefundSeenErr
	}
	if id, ok := r.refunds["refund:"+platform+":"+refundID]; ok {
		return true, id, nil
	}
	return false, uuid.Nil, nil
}

func (r *zzFakeRepo) MarkRefundSeen(_ context.Context, _ uuid.UUID, platform, _, refundID string, billID uuid.UUID) error {
	if r.markRefundSeenErr != nil {
		return r.markRefundSeenErr
	}
	r.refunds["refund:"+platform+":"+refundID] = billID
	return nil
}

type zzFakeCreator struct {
	err   error
	calls []SaleCreatorInput
}

func (c *zzFakeCreator) Create(_ context.Context, req SaleCreatorInput) (*SaleCreatorOutput, error) {
	c.calls = append(c.calls, req)
	if c.err != nil {
		return nil, c.err
	}
	return &SaleCreatorOutput{BillID: uuid.New(), BillNo: "SL-ZZ-0001"}, nil
}

type zzFakeApprover struct {
	err   error
	calls []uuid.UUID
}

func (a *zzFakeApprover) Approve(_ context.Context, req SaleApproverInput) error {
	a.calls = append(a.calls, req.BillID)
	return a.err
}

type zzFakeReturnCreator struct {
	err   error
	calls []ReturnCreatorInput
}

func (c *zzFakeReturnCreator) Create(_ context.Context, req ReturnCreatorInput) (*ReturnCreatorOutput, error) {
	c.calls = append(c.calls, req)
	if c.err != nil {
		return nil, c.err
	}
	return &ReturnCreatorOutput{BillID: uuid.New(), BillNo: "RT-ZZ-0001"}, nil
}

type zzFakeReturnApprover struct {
	err   error
	calls []uuid.UUID
}

func (a *zzFakeReturnApprover) Approve(_ context.Context, req ReturnApproverInput) error {
	a.calls = append(a.calls, req.BillID)
	return a.err
}

type zzFakeStockChecker struct {
	qty map[string]decimal.Decimal
	err error
}

func newZZFakeStockChecker() *zzFakeStockChecker {
	return &zzFakeStockChecker{qty: make(map[string]decimal.Decimal)}
}

func (s *zzFakeStockChecker) AvailableQty(_ context.Context, _, productID, _ uuid.UUID) (decimal.Decimal, error) {
	if s.err != nil {
		return decimal.Zero, s.err
	}
	if q, ok := s.qty[productID.String()]; ok {
		return q, nil
	}
	return decimal.Zero, nil
}

type zzFakeWarehouseChecker struct {
	err    error
	called bool
}

func (w *zzFakeWarehouseChecker) BelongsToTenant(_ context.Context, _, _ uuid.UUID) error {
	w.called = true
	return w.err
}

type zzFakeRater struct {
	rate  decimal.Decimal
	err   error
	calls int
}

func (r *zzFakeRater) GetRate(_ context.Context, _ uuid.UUID, _, _ string, _ time.Time) (decimal.Decimal, error) {
	r.calls++
	if r.err != nil {
		return decimal.Zero, r.err
	}
	if r.rate.IsZero() {
		return decimal.NewFromInt(1), nil
	}
	return r.rate, nil
}

// ----- helpers -----------------------------------------------------------

func zzAmazonCSV(rows ...string) []byte {
	header := "order-id,sku,quantity-purchased,item-price,currency,purchase-date"
	return []byte(header + "\n" + strings.Join(rows, "\n"))
}

func zzShopifyCSV(rows ...string) []byte {
	header := "Name,Lineitem sku,Lineitem quantity,Lineitem price,Currency,Created at"
	return []byte(header + "\n" + strings.Join(rows, "\n"))
}

func zzBuild(repo *zzFakeRepo, creator *zzFakeCreator, approver *zzFakeApprover, checker *zzFakeStockChecker, rater *zzFakeRater) *ImportOrdersUseCase {
	return NewImportOrdersUseCase(repo, creator, approver, checker, nil, rater, "CNY")
}

func zzBuildWithReturn(repo *zzFakeRepo, retCreator *zzFakeReturnCreator, retApprover *zzFakeReturnApprover) *ImportOrdersUseCase {
	uc := NewImportOrdersUseCase(repo, &zzFakeCreator{}, &zzFakeApprover{}, newZZFakeStockChecker(), nil, &zzFakeRater{}, "CNY")
	return uc.WithReturnHandlers(retCreator, retApprover)
}

// ----- Platform.Validate --------------------------------------------------

func TestZZ_Platform_Validate_Unknown(t *testing.T) {
	err := Platform("ebay").Validate()
	if err == nil {
		t.Fatal("expected error for unknown platform")
	}
	if !strings.Contains(err.Error(), `unknown platform "ebay"`) {
		t.Errorf("error: got %v", err)
	}
}

func TestZZ_Platform_Validate_Known(t *testing.T) {
	if err := PlatformAmazon.Validate(); err != nil {
		t.Errorf("amazon should validate, got %v", err)
	}
	if err := PlatformShopify.Validate(); err != nil {
		t.Errorf("shopify should validate, got %v", err)
	}
}

// ----- NewImportOrdersUseCase default targetCurrency ----------------------

// When targetCurrency is "" the constructor must default it to "CNY". We
// prove this indirectly: feeding a CNY-denominated line must NOT trigger any
// FX lookup (srcCur == targetCurrency skips GetRate) -- if the default were
// left as "" the CNY line would look like "CNY" != "" and a conversion would
// be attempted, calling the rater unexpectedly.
func TestZZ_NewImportOrdersUseCase_DefaultsEmptyTargetCurrencyToCNY(t *testing.T) {
	repo := newZZFakeRepo()
	productID := uuid.New()
	repo.addMapping("amazon", "SKU-DEF", productID)
	creator := &zzFakeCreator{}
	rater := &zzFakeRater{}

	uc := NewImportOrdersUseCase(repo, creator, &zzFakeApprover{}, newZZFakeStockChecker(), nil, rater, "")

	csv := zzAmazonCSV("ORD-DEF,SKU-DEF,1,10.00,CNY,2026-01-01")
	_, err := uc.Execute(context.Background(), ImportRequest{
		TenantID: uuid.New(), CreatorID: uuid.New(), WarehouseID: uuid.New(),
		Platform: PlatformAmazon, CSVData: csv,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if rater.calls != 0 {
		t.Errorf("expected no FX lookup when currency already matches default CNY target, rater called %d times", rater.calls)
	}
	if len(creator.calls) != 1 || !creator.calls[0].Items[0].UnitPrice.Equal(decimal.NewFromFloat(10.00)) {
		t.Errorf("expected unconverted 10.00 price, got %+v", creator.calls)
	}
}

// ----- Execute: nil-field guards & top-level errors ------------------------

func TestZZ_Execute_Guards(t *testing.T) {
	validCSV := zzAmazonCSV("ORD-1,SKU-1,1,10.00,CNY,2026-01-01")
	cases := []struct {
		name string
		req  ImportRequest
		want string
	}{
		{"nil tenant", ImportRequest{CreatorID: uuid.New(), WarehouseID: uuid.New(), Platform: PlatformAmazon, CSVData: validCSV}, "tenant_id is required"},
		{"nil creator", ImportRequest{TenantID: uuid.New(), WarehouseID: uuid.New(), Platform: PlatformAmazon, CSVData: validCSV}, "creator_id is required"},
		{"nil warehouse", ImportRequest{TenantID: uuid.New(), CreatorID: uuid.New(), Platform: PlatformAmazon, CSVData: validCSV}, "warehouse_id is required"},
		{"bad platform", ImportRequest{TenantID: uuid.New(), CreatorID: uuid.New(), WarehouseID: uuid.New(), Platform: Platform("bogus"), CSVData: validCSV}, "unknown platform"},
		{"empty csv", ImportRequest{TenantID: uuid.New(), CreatorID: uuid.New(), WarehouseID: uuid.New(), Platform: PlatformAmazon, CSVData: nil}, "csv_data is empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uc := zzBuild(newZZFakeRepo(), &zzFakeCreator{}, &zzFakeApprover{}, newZZFakeStockChecker(), &zzFakeRater{})
			_, err := uc.Execute(context.Background(), tc.req)
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%s: got %v, want substring %q", tc.name, err, tc.want)
			}
		})
	}
}

// ----- Execute: cross-tenant warehouse guard aborts before any bill --------

func TestZZ_Execute_CrossTenantWarehouse_NoBillCreated(t *testing.T) {
	repo := newZZFakeRepo()
	productID := uuid.New()
	repo.addMapping("amazon", "SKU-1", productID)
	creator := &zzFakeCreator{}
	wh := &zzFakeWarehouseChecker{err: errors.New("not in tenant")}

	uc := NewImportOrdersUseCase(repo, creator, &zzFakeApprover{}, newZZFakeStockChecker(), wh, &zzFakeRater{}, "CNY")

	csv := zzAmazonCSV("ORD-1,SKU-1,1,10.00,CNY,2026-01-01")
	_, err := uc.Execute(context.Background(), ImportRequest{
		TenantID: uuid.New(), CreatorID: uuid.New(), WarehouseID: uuid.New(),
		Platform: PlatformAmazon, CSVData: csv,
	})
	if err == nil {
		t.Fatal("expected error for cross-tenant warehouse")
	}
	if !wh.called {
		t.Error("expected BelongsToTenant to be called")
	}
	if len(creator.calls) != 0 {
		t.Errorf("saleCreator must never be called when warehouse guard rejects, got %d calls", len(creator.calls))
	}
}

// ----- Execute: dedup / resolve / fx / stock-check / create infra errors --

func TestZZ_Execute_DedupCheckError(t *testing.T) {
	repo := newZZFakeRepo()
	repo.isOrderSeenErr = errors.New("db down")
	uc := zzBuild(repo, &zzFakeCreator{}, &zzFakeApprover{}, newZZFakeStockChecker(), &zzFakeRater{})

	csv := zzAmazonCSV("ORD-1,SKU-1,1,10.00,CNY,2026-01-01")
	_, err := uc.Execute(context.Background(), ImportRequest{
		TenantID: uuid.New(), CreatorID: uuid.New(), WarehouseID: uuid.New(),
		Platform: PlatformAmazon, CSVData: csv,
	})
	if err == nil || !strings.Contains(err.Error(), "dedup check for order") {
		t.Errorf("expected dedup check error, got %v", err)
	}
}

func TestZZ_Execute_ResolveSKUError(t *testing.T) {
	repo := newZZFakeRepo()
	repo.getMappingErr = errors.New("mapping lookup failed")
	uc := zzBuild(repo, &zzFakeCreator{}, &zzFakeApprover{}, newZZFakeStockChecker(), &zzFakeRater{})

	csv := zzAmazonCSV("ORD-1,SKU-1,1,10.00,CNY,2026-01-01")
	_, err := uc.Execute(context.Background(), ImportRequest{
		TenantID: uuid.New(), CreatorID: uuid.New(), WarehouseID: uuid.New(),
		Platform: PlatformAmazon, CSVData: csv,
	})
	if err == nil || !strings.Contains(err.Error(), "resolve sku") {
		t.Errorf("expected resolve sku error, got %v", err)
	}
}

func TestZZ_Execute_FXError(t *testing.T) {
	repo := newZZFakeRepo()
	productID := uuid.New()
	repo.addMapping("amazon", "SKU-1", productID)
	rater := &zzFakeRater{err: errors.New("fx service down")}
	uc := zzBuild(repo, &zzFakeCreator{}, &zzFakeApprover{}, newZZFakeStockChecker(), rater)

	csv := zzAmazonCSV("ORD-1,SKU-1,1,10.00,USD,2026-01-01")
	_, err := uc.Execute(context.Background(), ImportRequest{
		TenantID: uuid.New(), CreatorID: uuid.New(), WarehouseID: uuid.New(),
		Platform: PlatformAmazon, CSVData: csv,
	})
	if err == nil || !strings.Contains(err.Error(), "fx for order") {
		t.Errorf("expected fx error, got %v", err)
	}
}

func TestZZ_Execute_DryRun_StockCheckError(t *testing.T) {
	repo := newZZFakeRepo()
	productID := uuid.New()
	repo.addMapping("amazon", "SKU-1", productID)
	checker := &zzFakeStockChecker{err: errors.New("stock service down")}
	uc := zzBuild(repo, &zzFakeCreator{}, &zzFakeApprover{}, checker, &zzFakeRater{})

	csv := zzAmazonCSV("ORD-1,SKU-1,1,10.00,CNY,2026-01-01")
	_, err := uc.Execute(context.Background(), ImportRequest{
		TenantID: uuid.New(), CreatorID: uuid.New(), WarehouseID: uuid.New(),
		Platform: PlatformAmazon, CSVData: csv, DryRun: true,
	})
	if err == nil || !strings.Contains(err.Error(), "stock check for order") {
		t.Errorf("expected stock check error, got %v", err)
	}
}

func TestZZ_Execute_SaleCreateError(t *testing.T) {
	repo := newZZFakeRepo()
	productID := uuid.New()
	repo.addMapping("amazon", "SKU-1", productID)
	creator := &zzFakeCreator{err: errors.New("insert failed")}
	uc := zzBuild(repo, creator, &zzFakeApprover{}, newZZFakeStockChecker(), &zzFakeRater{})

	csv := zzAmazonCSV("ORD-1,SKU-1,1,10.00,CNY,2026-01-01")
	_, err := uc.Execute(context.Background(), ImportRequest{
		TenantID: uuid.New(), CreatorID: uuid.New(), WarehouseID: uuid.New(),
		Platform: PlatformAmazon, CSVData: csv,
	})
	if err == nil || !strings.Contains(err.Error(), "create sale bill for order") {
		t.Errorf("expected create sale bill error, got %v", err)
	}
}

// Live-mode approve failure (e.g. oversell at approval time) is a soft skip,
// not a hard error, and the batch must continue to the next order.
func TestZZ_Execute_ApproveFailure_SkippedNotHardError_BatchContinues(t *testing.T) {
	repo := newZZFakeRepo()
	productID := uuid.New()
	repo.addMapping("amazon", "SKU-1", productID)
	approver := &zzFakeApprover{err: errors.New("insufficient stock")}
	uc := zzBuild(repo, &zzFakeCreator{}, approver, newZZFakeStockChecker(), &zzFakeRater{})

	csv := zzAmazonCSV(
		"ORD-1,SKU-1,1,10.00,CNY,2026-01-01",
		"ORD-2,SKU-1,1,10.00,CNY,2026-01-02",
	)
	result, err := uc.Execute(context.Background(), ImportRequest{
		TenantID: uuid.New(), CreatorID: uuid.New(), WarehouseID: uuid.New(),
		Platform: PlatformAmazon, CSVData: csv,
	})
	if err != nil {
		t.Fatalf("approve failure must not be a hard error: %v", err)
	}
	if len(result.Skipped) != 2 {
		t.Fatalf("expected both orders skipped (approver always fails), got %d", len(result.Skipped))
	}
	for _, s := range result.Skipped {
		if !strings.HasPrefix(s.Reason, "approve_failed:") {
			t.Errorf("expected approve_failed reason, got %q", s.Reason)
		}
	}
	if len(result.Imported) != 0 {
		t.Errorf("expected 0 imported, got %d", len(result.Imported))
	}
}

// Persisting a hint-derived mapping failing is a hard error.
func TestZZ_Execute_PersistMappingError(t *testing.T) {
	repo := newZZFakeRepo()
	repo.upsertErr = errors.New("upsert failed")
	productID := uuid.New()
	uc := zzBuild(repo, &zzFakeCreator{}, &zzFakeApprover{}, newZZFakeStockChecker(), &zzFakeRater{})

	csv := zzAmazonCSV("ORD-1,SKU-NEW,1,10.00,CNY,2026-01-01")
	_, err := uc.Execute(context.Background(), ImportRequest{
		TenantID: uuid.New(), CreatorID: uuid.New(), WarehouseID: uuid.New(),
		Platform: PlatformAmazon, CSVData: csv,
		SKUHints: []SKUHint{{PlatformSKU: "SKU-NEW", ProductID: productID}},
	})
	if err == nil || !strings.Contains(err.Error(), "persist mapping for sku") {
		t.Errorf("expected persist mapping error, got %v", err)
	}
}

// appendUniqueSKU dedup: two lines in the same order sharing the same unknown
// SKU must yield exactly one UnknownSKU entry.
func TestZZ_Execute_UnknownSKU_DedupedAcrossLines(t *testing.T) {
	uc := zzBuild(newZZFakeRepo(), &zzFakeCreator{}, &zzFakeApprover{}, newZZFakeStockChecker(), &zzFakeRater{})
	csv := zzAmazonCSV(
		"ORD-1,SKU-UNK,1,10.00,CNY,2026-01-01",
		"ORD-1,SKU-UNK,2,20.00,CNY,2026-01-01",
	)
	result, err := uc.Execute(context.Background(), ImportRequest{
		TenantID: uuid.New(), CreatorID: uuid.New(), WarehouseID: uuid.New(),
		Platform: PlatformAmazon, CSVData: csv,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.UnknownSKUs) != 1 {
		t.Fatalf("expected exactly 1 deduped UnknownSKU, got %d: %+v", len(result.UnknownSKUs), result.UnknownSKUs)
	}
}

// Multi-line same order_no grouped into ONE bill, preserving encounter order.
func TestZZ_Execute_MultiLineGroupedIntoOneBill_EncounterOrderPreserved(t *testing.T) {
	repo := newZZFakeRepo()
	pA, pB := uuid.New(), uuid.New()
	repo.addMapping("amazon", "SKU-A", pA)
	repo.addMapping("amazon", "SKU-B", pB)
	creator := &zzFakeCreator{}
	uc := zzBuild(repo, creator, &zzFakeApprover{}, newZZFakeStockChecker(), &zzFakeRater{})

	csv := zzAmazonCSV(
		"ORD-9,SKU-B,1,10.00,CNY,2026-01-01", // order 9, line 1: SKU-B first
		"ORD-1,SKU-A,1,5.00,CNY,2026-01-02",  // order 1 encountered second
		"ORD-9,SKU-A,2,15.00,CNY,2026-01-01", // order 9, line 2
	)
	result, err := uc.Execute(context.Background(), ImportRequest{
		TenantID: uuid.New(), CreatorID: uuid.New(), WarehouseID: uuid.New(),
		Platform: PlatformAmazon, CSVData: csv,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.Imported) != 2 {
		t.Fatalf("expected 2 imported orders (grouped), got %d", len(result.Imported))
	}
	// Encounter order: ORD-9 seen first, ORD-1 second.
	if result.Imported[0].PlatformOrderNo != "ORD-9" || result.Imported[1].PlatformOrderNo != "ORD-1" {
		t.Errorf("expected encounter order [ORD-9, ORD-1], got [%s, %s]", result.Imported[0].PlatformOrderNo, result.Imported[1].PlatformOrderNo)
	}
	if len(creator.calls) != 2 {
		t.Fatalf("expected 2 sale bills created, got %d", len(creator.calls))
	}
	// The first bill created corresponds to ORD-9 and must carry BOTH lines.
	ord9Items := creator.calls[0].Items
	if len(ord9Items) != 2 {
		t.Fatalf("expected ORD-9 bill to have 2 line items (grouped), got %d", len(ord9Items))
	}
	if ord9Items[0].ProductID != pB || ord9Items[1].ProductID != pA {
		t.Errorf("expected line order [B, A] preserved, got [%s, %s]", ord9Items[0].ProductID, ord9Items[1].ProductID)
	}
	if ord9Items[0].LineNo != 1 || ord9Items[1].LineNo != 2 {
		t.Errorf("expected LineNo 1,2 got %d,%d", ord9Items[0].LineNo, ord9Items[1].LineNo)
	}
}

// FX correctness, hand-computed: 10.00 USD * rate 6.8 = 68.0000 CNY.
func TestZZ_Execute_FXCorrectness_HandComputed(t *testing.T) {
	repo := newZZFakeRepo()
	productID := uuid.New()
	repo.addMapping("amazon", "SKU-FX", productID)
	rater := &zzFakeRater{rate: decimal.NewFromFloat(6.8)}
	creator := &zzFakeCreator{}
	uc := zzBuild(repo, creator, &zzFakeApprover{}, newZZFakeStockChecker(), rater)

	csv := zzAmazonCSV("ORD-FX,SKU-FX,1,10.00,USD,2026-01-01")
	_, err := uc.Execute(context.Background(), ImportRequest{
		TenantID: uuid.New(), CreatorID: uuid.New(), WarehouseID: uuid.New(),
		Platform: PlatformAmazon, CSVData: csv,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := decimal.NewFromFloat(10.00).Mul(decimal.NewFromFloat(6.8)).Round(4) // 68.0000
	got := creator.calls[0].Items[0].UnitPrice
	if !got.Equal(want) {
		t.Errorf("FX unit_price: got %s, want %s", got, want)
	}
}

// ----- ListMappings passthrough --------------------------------------------

func TestZZ_ListMappings_Passthrough(t *testing.T) {
	repo := newZZFakeRepo()
	repo.listOut = []SKUMapping{{PlatformSKU: "X"}}
	uc := zzBuild(repo, &zzFakeCreator{}, &zzFakeApprover{}, newZZFakeStockChecker(), &zzFakeRater{})
	out, err := uc.ListMappings(context.Background(), uuid.New(), "amazon")
	if err != nil {
		t.Fatalf("ListMappings: %v", err)
	}
	if len(out) != 1 || out[0].PlatformSKU != "X" {
		t.Errorf("expected passthrough result, got %+v", out)
	}

	repo2 := newZZFakeRepo()
	repo2.listErr = errors.New("list failed")
	uc2 := zzBuild(repo2, &zzFakeCreator{}, &zzFakeApprover{}, newZZFakeStockChecker(), &zzFakeRater{})
	if _, err := uc2.ListMappings(context.Background(), uuid.New(), "amazon"); err == nil {
		t.Error("expected error to propagate from repo.ListMappings")
	}
}

// ----- IngestSingleOrder guards --------------------------------------------

func TestZZ_IngestSingleOrder_Guards(t *testing.T) {
	validLines := []OrderRow{{PlatformOrderNo: "O1", PlatformSKU: "S1", Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(10), Currency: "CNY", OrderDate: time.Now()}}
	cases := []struct {
		name string
		req  SingleOrderRequest
		want string
	}{
		{"nil tenant", SingleOrderRequest{CreatorID: uuid.New(), WarehouseID: uuid.New(), Platform: PlatformAmazon, PlatformOrderNo: "O1", Lines: validLines}, "tenant_id is required"},
		{"nil creator", SingleOrderRequest{TenantID: uuid.New(), WarehouseID: uuid.New(), Platform: PlatformAmazon, PlatformOrderNo: "O1", Lines: validLines}, "creator_id is required"},
		{"nil warehouse", SingleOrderRequest{TenantID: uuid.New(), CreatorID: uuid.New(), Platform: PlatformAmazon, PlatformOrderNo: "O1", Lines: validLines}, "warehouse_id is required"},
		{"bad platform", SingleOrderRequest{TenantID: uuid.New(), CreatorID: uuid.New(), WarehouseID: uuid.New(), Platform: Platform("bogus"), PlatformOrderNo: "O1", Lines: validLines}, "unknown platform"},
		{"empty order no", SingleOrderRequest{TenantID: uuid.New(), CreatorID: uuid.New(), WarehouseID: uuid.New(), Platform: PlatformAmazon, PlatformOrderNo: "", Lines: validLines}, "platform_order_no is required"},
		{"zero lines", SingleOrderRequest{TenantID: uuid.New(), CreatorID: uuid.New(), WarehouseID: uuid.New(), Platform: PlatformAmazon, PlatformOrderNo: "O1", Lines: nil}, "at least one order line is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uc := zzBuild(newZZFakeRepo(), &zzFakeCreator{}, &zzFakeApprover{}, newZZFakeStockChecker(), &zzFakeRater{})
			_, _, err := uc.IngestSingleOrder(context.Background(), tc.req)
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%s: got %v, want substring %q", tc.name, err, tc.want)
			}
		})
	}
}

// ----- ingestOneOrder (via IngestSingleOrder) infra-error / soft-fail paths -

func zzLine(orderNo, sku string, qty float64, price, currency string) OrderRow {
	return OrderRow{
		PlatformOrderNo: orderNo,
		PlatformSKU:     sku,
		Qty:             decimal.NewFromFloat(qty),
		UnitPrice:       decimal.RequireFromString(price),
		Currency:        currency,
		OrderDate:       time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestZZ_IngestSingleOrder_DedupCheckError(t *testing.T) {
	repo := newZZFakeRepo()
	repo.isOrderSeenErr = errors.New("db down")
	uc := zzBuild(repo, &zzFakeCreator{}, &zzFakeApprover{}, newZZFakeStockChecker(), &zzFakeRater{})

	_, _, err := uc.IngestSingleOrder(context.Background(), SingleOrderRequest{
		TenantID: uuid.New(), CreatorID: uuid.New(), WarehouseID: uuid.New(),
		Platform: PlatformShopify, PlatformOrderNo: "#1",
		Lines: []OrderRow{zzLine("#1", "S1", 1, "10.00", "CNY")},
	})
	if err == nil || !strings.Contains(err.Error(), "dedup check for order") {
		t.Errorf("expected dedup error, got %v", err)
	}
}

func TestZZ_IngestSingleOrder_ResolveSKUError(t *testing.T) {
	repo := newZZFakeRepo()
	repo.getMappingErr = errors.New("mapping down")
	uc := zzBuild(repo, &zzFakeCreator{}, &zzFakeApprover{}, newZZFakeStockChecker(), &zzFakeRater{})

	_, _, err := uc.IngestSingleOrder(context.Background(), SingleOrderRequest{
		TenantID: uuid.New(), CreatorID: uuid.New(), WarehouseID: uuid.New(),
		Platform: PlatformShopify, PlatformOrderNo: "#1",
		Lines: []OrderRow{zzLine("#1", "S1", 1, "10.00", "CNY")},
	})
	if err == nil || !strings.Contains(err.Error(), "resolve sku") {
		t.Errorf("expected resolve sku error, got %v", err)
	}
}

func TestZZ_IngestSingleOrder_FXError(t *testing.T) {
	repo := newZZFakeRepo()
	repo.addMapping("shopify", "S1", uuid.New())
	rater := &zzFakeRater{err: errors.New("fx down")}
	uc := zzBuild(repo, &zzFakeCreator{}, &zzFakeApprover{}, newZZFakeStockChecker(), rater)

	_, _, err := uc.IngestSingleOrder(context.Background(), SingleOrderRequest{
		TenantID: uuid.New(), CreatorID: uuid.New(), WarehouseID: uuid.New(),
		Platform: PlatformShopify, PlatformOrderNo: "#1",
		Lines: []OrderRow{zzLine("#1", "S1", 1, "10.00", "USD")},
	})
	if err == nil || !strings.Contains(err.Error(), "fx for order") {
		t.Errorf("expected fx error, got %v", err)
	}
}

func TestZZ_IngestSingleOrder_CreateError(t *testing.T) {
	repo := newZZFakeRepo()
	repo.addMapping("shopify", "S1", uuid.New())
	creator := &zzFakeCreator{err: errors.New("insert failed")}
	uc := zzBuild(repo, creator, &zzFakeApprover{}, newZZFakeStockChecker(), &zzFakeRater{})

	_, _, err := uc.IngestSingleOrder(context.Background(), SingleOrderRequest{
		TenantID: uuid.New(), CreatorID: uuid.New(), WarehouseID: uuid.New(),
		Platform: PlatformShopify, PlatformOrderNo: "#1",
		Lines: []OrderRow{zzLine("#1", "S1", 1, "10.00", "CNY")},
	})
	if err == nil || !strings.Contains(err.Error(), "create sale bill for order") {
		t.Errorf("expected create error, got %v", err)
	}
}

func TestZZ_IngestSingleOrder_ApproveFailure_ReturnsSkippedNotError(t *testing.T) {
	repo := newZZFakeRepo()
	repo.addMapping("shopify", "S1", uuid.New())
	approver := &zzFakeApprover{err: errors.New("oversell")}
	uc := zzBuild(repo, &zzFakeCreator{}, approver, newZZFakeStockChecker(), &zzFakeRater{})

	imported, skipped, err := uc.IngestSingleOrder(context.Background(), SingleOrderRequest{
		TenantID: uuid.New(), CreatorID: uuid.New(), WarehouseID: uuid.New(),
		Platform: PlatformShopify, PlatformOrderNo: "#1",
		Lines: []OrderRow{zzLine("#1", "S1", 1, "10.00", "CNY")},
	})
	if err != nil {
		t.Fatalf("approve failure must be a soft skip, not an error: %v", err)
	}
	if skipped == nil || !strings.HasPrefix(skipped.Reason, "approve_failed:") {
		t.Fatalf("expected approve_failed skip, got %+v", skipped)
	}
	if imported.BillID != uuid.Nil {
		t.Error("expected zero ImportedOrder on approve failure")
	}
}

// MarkOrderSeen failure is soft: the bill is already committed, so
// ingestOneOrder returns (ImportedOrder{MarkSeenError set}, nil, nil).
func TestZZ_IngestSingleOrder_MarkOrderSeenFailure_SoftFail(t *testing.T) {
	repo := newZZFakeRepo()
	repo.addMapping("shopify", "S1", uuid.New())
	repo.markOrderSeenErr = errors.New("dedup write failed")
	creator := &zzFakeCreator{}
	approver := &zzFakeApprover{}
	uc := zzBuild(repo, creator, approver, newZZFakeStockChecker(), &zzFakeRater{})

	imported, skipped, err := uc.IngestSingleOrder(context.Background(), SingleOrderRequest{
		TenantID: uuid.New(), CreatorID: uuid.New(), WarehouseID: uuid.New(),
		Platform: PlatformShopify, PlatformOrderNo: "#1",
		Lines: []OrderRow{zzLine("#1", "S1", 1, "10.00", "CNY")},
	})
	if err != nil {
		t.Fatalf("MarkOrderSeen failure must be soft (nil error), got %v", err)
	}
	if skipped != nil {
		t.Fatalf("expected nil skipped on soft mark-seen failure, got %+v", skipped)
	}
	if imported.MarkSeenError == "" {
		t.Error("expected MarkSeenError to be set")
	}
	if imported.BillID == uuid.Nil {
		t.Error("expected bill to have been committed (non-nil BillID) despite mark-seen failure")
	}
	// Bill was still created and approved -- not rolled back.
	if len(creator.calls) != 1 || len(approver.calls) != 1 {
		t.Errorf("expected bill created+approved despite soft mark-seen failure, got creator=%d approver=%d", len(creator.calls), len(approver.calls))
	}
}

// ----- IngestCancelOrder ----------------------------------------------------

func TestZZ_IngestCancelOrder_Guards(t *testing.T) {
	cases := []struct {
		name string
		req  CancelRequest
		want string
	}{
		{"nil tenant", CancelRequest{CreatorID: uuid.New(), Platform: PlatformShopify, PlatformOrderNo: "#1"}, "tenant_id is required"},
		{"nil creator", CancelRequest{TenantID: uuid.New(), Platform: PlatformShopify, PlatformOrderNo: "#1"}, "creator_id is required"},
		{"bad platform", CancelRequest{TenantID: uuid.New(), CreatorID: uuid.New(), Platform: Platform("bogus"), PlatformOrderNo: "#1"}, "unknown platform"},
		{"empty order no", CancelRequest{TenantID: uuid.New(), CreatorID: uuid.New(), Platform: PlatformShopify, PlatformOrderNo: ""}, "platform_order_no is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uc := zzBuildWithReturn(newZZFakeRepo(), &zzFakeReturnCreator{}, &zzFakeReturnApprover{})
			_, err := uc.IngestCancelOrder(context.Background(), tc.req)
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%s: got %v, want substring %q", tc.name, err, tc.want)
			}
		})
	}
}

func TestZZ_IngestCancelOrder_ReturnHandlersNotWired(t *testing.T) {
	uc := NewImportOrdersUseCase(newZZFakeRepo(), &zzFakeCreator{}, &zzFakeApprover{}, newZZFakeStockChecker(), nil, &zzFakeRater{}, "CNY")
	_, err := uc.IngestCancelOrder(context.Background(), CancelRequest{
		TenantID: uuid.New(), CreatorID: uuid.New(), Platform: PlatformShopify, PlatformOrderNo: "#1",
	})
	if err == nil || !strings.Contains(err.Error(), "not wired") {
		t.Errorf("expected 'not wired' error, got %v", err)
	}
}

func TestZZ_IngestCancelOrder_CancelDedupCheckError(t *testing.T) {
	repo := newZZFakeRepo()
	repo.isCancelSeenErr = errors.New("db down")
	uc := zzBuildWithReturn(repo, &zzFakeReturnCreator{}, &zzFakeReturnApprover{})
	_, err := uc.IngestCancelOrder(context.Background(), CancelRequest{
		TenantID: uuid.New(), CreatorID: uuid.New(), Platform: PlatformShopify, PlatformOrderNo: "#1",
	})
	if err == nil || !strings.Contains(err.Error(), "cancel dedup check for order") {
		t.Errorf("expected cancel dedup error, got %v", err)
	}
}

func TestZZ_IngestCancelOrder_LookupOriginalError(t *testing.T) {
	repo := newZZFakeRepo()
	repo.isOrderSeenErr = errors.New("db down")
	uc := zzBuildWithReturn(repo, &zzFakeReturnCreator{}, &zzFakeReturnApprover{})
	_, err := uc.IngestCancelOrder(context.Background(), CancelRequest{
		TenantID: uuid.New(), CreatorID: uuid.New(), Platform: PlatformShopify, PlatformOrderNo: "#1",
	})
	if err == nil || !strings.Contains(err.Error(), "lookup original order") {
		t.Errorf("expected lookup original order error, got %v", err)
	}
}

func TestZZ_IngestCancelOrder_ReturnCreateError(t *testing.T) {
	repo := newZZFakeRepo()
	repo.seen["shopify:#1"] = uuid.New()
	retCreator := &zzFakeReturnCreator{err: errors.New("create failed")}
	uc := zzBuildWithReturn(repo, retCreator, &zzFakeReturnApprover{})
	_, err := uc.IngestCancelOrder(context.Background(), CancelRequest{
		TenantID: uuid.New(), CreatorID: uuid.New(), Platform: PlatformShopify, PlatformOrderNo: "#1",
	})
	if err == nil || !strings.Contains(err.Error(), "create reversal bill for order") {
		t.Errorf("expected create reversal bill error, got %v", err)
	}
}

func TestZZ_IngestCancelOrder_ReturnApproveError(t *testing.T) {
	repo := newZZFakeRepo()
	repo.seen["shopify:#1"] = uuid.New()
	retApprover := &zzFakeReturnApprover{err: errors.New("approve failed")}
	uc := zzBuildWithReturn(repo, &zzFakeReturnCreator{}, retApprover)
	_, err := uc.IngestCancelOrder(context.Background(), CancelRequest{
		TenantID: uuid.New(), CreatorID: uuid.New(), Platform: PlatformShopify, PlatformOrderNo: "#1",
	})
	if err == nil || !strings.Contains(err.Error(), "approve reversal bill for order") {
		t.Errorf("expected approve reversal bill error, got %v", err)
	}
}

// MarkCancelSeen failure is soft: the reversal bill IS committed, so a
// non-nil CancelResult is returned alongside the error.
func TestZZ_IngestCancelOrder_MarkCancelSeenFailure_SoftFail(t *testing.T) {
	repo := newZZFakeRepo()
	repo.seen["shopify:#1"] = uuid.New()
	repo.markCancelSeenErr = errors.New("dedup write failed")
	retCreator := &zzFakeReturnCreator{}
	retApprover := &zzFakeReturnApprover{}
	uc := zzBuildWithReturn(repo, retCreator, retApprover)

	result, err := uc.IngestCancelOrder(context.Background(), CancelRequest{
		TenantID: uuid.New(), CreatorID: uuid.New(), Platform: PlatformShopify, PlatformOrderNo: "#1",
	})
	if err == nil || !strings.Contains(err.Error(), "mark_cancel_seen failed") {
		t.Fatalf("expected mark_cancel_seen error, got %v", err)
	}
	if result == nil || result.ReversalBillID == uuid.Nil {
		t.Error("expected non-nil CancelResult with committed reversal bill despite soft mark-seen failure")
	}
	if len(retCreator.calls) != 1 || len(retApprover.calls) != 1 {
		t.Error("expected reversal bill to have been created+approved (not rolled back)")
	}
}

// ----- IngestRefund ----------------------------------------------------------

func zzRefundReq(overrides func(*RefundRequest)) RefundRequest {
	req := RefundRequest{
		TenantID: uuid.New(), CreatorID: uuid.New(), WarehouseID: uuid.New(),
		Platform: PlatformShopify, PlatformOrderNo: "#1", PlatformRefundID: "R1",
		Currency: "CNY", RefundDate: time.Now().UTC(),
		Lines: []RefundLine{{PlatformSKU: "S1", Qty: decimal.NewFromInt(1), RefundAmount: decimal.NewFromFloat(10)}},
	}
	if overrides != nil {
		overrides(&req)
	}
	return req
}

func TestZZ_IngestRefund_Guards(t *testing.T) {
	cases := []struct {
		name string
		req  RefundRequest
		want string
	}{
		{"nil tenant", zzRefundReq(func(r *RefundRequest) { r.TenantID = uuid.Nil }), "tenant_id is required"},
		{"nil creator", zzRefundReq(func(r *RefundRequest) { r.CreatorID = uuid.Nil }), "creator_id is required"},
		{"nil warehouse", zzRefundReq(func(r *RefundRequest) { r.WarehouseID = uuid.Nil }), "warehouse_id is required"},
		{"bad platform", zzRefundReq(func(r *RefundRequest) { r.Platform = Platform("bogus") }), "unknown platform"},
		{"empty order no", zzRefundReq(func(r *RefundRequest) { r.PlatformOrderNo = "" }), "platform_order_no is required"},
		{"empty refund id", zzRefundReq(func(r *RefundRequest) { r.PlatformRefundID = "" }), "platform_refund_id is required"},
		{"zero lines", zzRefundReq(func(r *RefundRequest) { r.Lines = nil }), "refund has no lines"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uc := zzBuildWithReturn(newZZFakeRepo(), &zzFakeReturnCreator{}, &zzFakeReturnApprover{})
			_, err := uc.IngestRefund(context.Background(), tc.req)
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%s: got %v, want substring %q", tc.name, err, tc.want)
			}
		})
	}
}

func TestZZ_IngestRefund_ReturnHandlersNotWired(t *testing.T) {
	uc := NewImportOrdersUseCase(newZZFakeRepo(), &zzFakeCreator{}, &zzFakeApprover{}, newZZFakeStockChecker(), nil, &zzFakeRater{}, "CNY")
	_, err := uc.IngestRefund(context.Background(), zzRefundReq(nil))
	if err == nil || !strings.Contains(err.Error(), "not wired") {
		t.Errorf("expected 'not wired' error, got %v", err)
	}
}

func TestZZ_IngestRefund_DedupCheckError(t *testing.T) {
	repo := newZZFakeRepo()
	repo.isRefundSeenErr = errors.New("db down")
	uc := zzBuildWithReturn(repo, &zzFakeReturnCreator{}, &zzFakeReturnApprover{})
	_, err := uc.IngestRefund(context.Background(), zzRefundReq(nil))
	if err == nil || !strings.Contains(err.Error(), "refund dedup check for") {
		t.Errorf("expected refund dedup check error, got %v", err)
	}
}

func TestZZ_IngestRefund_ResolveSKUError(t *testing.T) {
	repo := newZZFakeRepo()
	repo.getMappingErr = errors.New("mapping down")
	uc := zzBuildWithReturn(repo, &zzFakeReturnCreator{}, &zzFakeReturnApprover{})
	_, err := uc.IngestRefund(context.Background(), zzRefundReq(nil))
	if err == nil || !strings.Contains(err.Error(), "refund resolve sku") {
		t.Errorf("expected refund resolve sku error, got %v", err)
	}
}

func TestZZ_IngestRefund_FXError(t *testing.T) {
	repo := newZZFakeRepo()
	repo.addMapping("shopify", "S1", uuid.New())
	rater := &zzFakeRater{err: errors.New("fx down")}
	uc := NewImportOrdersUseCase(repo, &zzFakeCreator{}, &zzFakeApprover{}, newZZFakeStockChecker(), nil, rater, "CNY").
		WithReturnHandlers(&zzFakeReturnCreator{}, &zzFakeReturnApprover{})
	req := zzRefundReq(func(r *RefundRequest) { r.Currency = "USD" })
	_, err := uc.IngestRefund(context.Background(), req)
	if err == nil || !strings.Contains(err.Error(), "fx for refund") {
		t.Errorf("expected fx for refund error, got %v", err)
	}
}

func TestZZ_IngestRefund_CreateError(t *testing.T) {
	repo := newZZFakeRepo()
	repo.addMapping("shopify", "S1", uuid.New())
	retCreator := &zzFakeReturnCreator{err: errors.New("create failed")}
	uc := zzBuildWithReturn(repo, retCreator, &zzFakeReturnApprover{})
	_, err := uc.IngestRefund(context.Background(), zzRefundReq(nil))
	if err == nil || !strings.Contains(err.Error(), "create refund bill for") {
		t.Errorf("expected create refund bill error, got %v", err)
	}
}

func TestZZ_IngestRefund_ApproveError(t *testing.T) {
	repo := newZZFakeRepo()
	repo.addMapping("shopify", "S1", uuid.New())
	retApprover := &zzFakeReturnApprover{err: errors.New("approve failed")}
	uc := zzBuildWithReturn(repo, &zzFakeReturnCreator{}, retApprover)
	_, err := uc.IngestRefund(context.Background(), zzRefundReq(nil))
	if err == nil || !strings.Contains(err.Error(), "approve refund bill for") {
		t.Errorf("expected approve refund bill error, got %v", err)
	}
}

// MarkRefundSeen failure is soft: bill committed, non-nil result + error.
func TestZZ_IngestRefund_MarkRefundSeenFailure_SoftFail(t *testing.T) {
	repo := newZZFakeRepo()
	repo.addMapping("shopify", "S1", uuid.New())
	repo.markRefundSeenErr = errors.New("dedup write failed")
	retCreator := &zzFakeReturnCreator{}
	retApprover := &zzFakeReturnApprover{}
	uc := zzBuildWithReturn(repo, retCreator, retApprover)

	result, err := uc.IngestRefund(context.Background(), zzRefundReq(nil))
	if err == nil || !strings.Contains(err.Error(), "mark_refund_seen failed") {
		t.Fatalf("expected mark_refund_seen error, got %v", err)
	}
	if result == nil || result.BillID == uuid.Nil {
		t.Error("expected non-nil RefundResult with committed bill despite soft mark-seen failure")
	}
	if len(retCreator.calls) != 1 || len(retApprover.calls) != 1 {
		t.Error("expected refund bill to have been created+approved (not rolled back)")
	}
}

// ----- CSV parser tests (direct, whitebox) ----------------------------------

func TestZZ_ParseCSV_UnknownPlatform(t *testing.T) {
	_, err := parseCSV(Platform("ebay"), []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "no parser for platform") {
		t.Errorf("expected no-parser error, got %v", err)
	}
}

func TestZZ_ParseAmazonCSV_EmptyData(t *testing.T) {
	_, err := parseAmazonCSV(nil)
	if err == nil || !strings.Contains(err.Error(), "read header") {
		t.Errorf("expected read-header error, got %v", err)
	}
}

func TestZZ_ParseAmazonCSV_MissingColumn(t *testing.T) {
	// Header omits the currency column entirely.
	csv := []byte("order-id,sku,quantity-purchased,item-price,purchase-date\nORD-1,SKU-1,1,10.00,2026-01-01\n")
	_, err := parseAmazonCSV(csv)
	if err == nil || !strings.Contains(err.Error(), `missing required column "currency"`) {
		t.Errorf("expected missing column error, got %v", err)
	}
}

func TestZZ_ParseAmazonCSV_MalformedLine(t *testing.T) {
	// A bare quote inside an unquoted field is a CSV syntax error surfaced on read.
	csv := []byte("order-id,sku,quantity-purchased,item-price,currency,purchase-date\nORD-1,SKU-1,1,10.00,CNY,2026\"-01-01\n")
	_, err := parseAmazonCSV(csv)
	if err == nil {
		t.Fatal("expected CSV syntax error for malformed line")
	}
	if !strings.Contains(err.Error(), "line") {
		t.Errorf("expected line-numbered error, got %v", err)
	}
}

func TestZZ_ParseAmazonCSV_QtyInvalid(t *testing.T) {
	cases := []string{"0", "-1", "abc", ""}
	for _, q := range cases {
		t.Run("qty="+q, func(t *testing.T) {
			csv := zzAmazonCSV("ORD-1,SKU-1," + q + ",10.00,CNY,2026-01-01")
			_, err := parseAmazonCSV(csv)
			if err == nil || !strings.Contains(err.Error(), "invalid qty") {
				t.Errorf("expected invalid qty error for %q, got %v", q, err)
			}
		})
	}
}

func TestZZ_ParseAmazonCSV_NegativeUnitPrice(t *testing.T) {
	csv := zzAmazonCSV("ORD-1,SKU-1,1,-5.00,CNY,2026-01-01")
	_, err := parseAmazonCSV(csv)
	if err == nil || !strings.Contains(err.Error(), "invalid unit_price") {
		t.Errorf("expected invalid unit_price error, got %v", err)
	}
}

func TestZZ_ParseAmazonCSV_UnparseableDate(t *testing.T) {
	csv := zzAmazonCSV("ORD-1,SKU-1,1,10.00,CNY,not-a-date")
	_, err := parseAmazonCSV(csv)
	if err == nil || !strings.Contains(err.Error(), "cannot parse order_date") {
		t.Errorf("expected order_date parse error, got %v", err)
	}
}

func TestZZ_ParseAmazonCSV_AllDateLayouts(t *testing.T) {
	cases := []struct {
		raw  string
		want time.Time
	}{
		{"2026-01-15T10:30:00Z", time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)},
		{"2026-01-15T10:30:00+08:00", time.Date(2026, 1, 15, 2, 30, 0, 0, time.UTC)},
		{"2026-01-15 10:30:00", time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)},
		{"2026-01-15", time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)},
		{"01/15/2026", time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)},
		{"2026/01/15", time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			csv := zzAmazonCSV("ORD-1,SKU-1,1,10.00,CNY," + tc.raw)
			rows, err := parseAmazonCSV(csv)
			if err != nil {
				t.Fatalf("parseAmazonCSV(%q): %v", tc.raw, err)
			}
			if len(rows) != 1 {
				t.Fatalf("expected 1 row, got %d", len(rows))
			}
			if !rows[0].OrderDate.Equal(tc.want) {
				t.Errorf("layout %q: got %v, want %v", tc.raw, rows[0].OrderDate, tc.want)
			}
		})
	}
}

func TestZZ_ParseShopifyCSV_MissingColumn(t *testing.T) {
	csv := []byte("Name,Lineitem sku,Lineitem quantity,Lineitem price,Created at\n#1001,SKU-1,1,10.00,2026-01-01\n")
	_, err := parseShopifyCSV(csv)
	if err == nil || !strings.Contains(err.Error(), `missing required column "currency"`) {
		t.Errorf("expected missing column error, got %v", err)
	}
}

func TestZZ_ParseShopifyCSV_BlankSKULineSkipped(t *testing.T) {
	csv := zzShopifyCSV(
		"#1001,,1,5.00,CNY,2026-01-01", // shipping/fee line: blank SKU, skipped
		"#1001,REAL-SKU,2,10.00,CNY,2026-01-01",
	)
	rows, err := parseShopifyCSV(csv)
	if err != nil {
		t.Fatalf("parseShopifyCSV: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected blank-sku line skipped, leaving 1 row, got %d", len(rows))
	}
	if rows[0].PlatformSKU != "REAL-SKU" {
		t.Errorf("expected surviving row to be REAL-SKU, got %q", rows[0].PlatformSKU)
	}
}

func TestZZ_ParseShopifyCSV_MalformedLine(t *testing.T) {
	csv := []byte("Name,Lineitem sku,Lineitem quantity,Lineitem price,Currency,Created at\n#1001,SKU-1,1,10.00,CNY,2026\"-01-01\n")
	_, err := parseShopifyCSV(csv)
	if err == nil {
		t.Fatal("expected CSV syntax error for malformed line")
	}
	if !strings.Contains(err.Error(), "line") {
		t.Errorf("expected line-numbered error, got %v", err)
	}
}

func TestZZ_ColumnIndex_AliasesCaseInsensitive(t *testing.T) {
	header := []string{" ORDER-ID ", "SKU", "Quantity-Purchased", "Item-Price", "Currency", "Purchase-Date"}
	idx, err := columnIndex(header, map[string][]string{
		"order_id": {"order-id"},
		"sku":      {"sku"},
	})
	if err != nil {
		t.Fatalf("columnIndex: %v", err)
	}
	if idx["order_id"] != 0 || idx["sku"] != 1 {
		t.Errorf("expected case/whitespace-insensitive match, got %+v", idx)
	}
}

func TestZZ_ColumnIndex_MissingField(t *testing.T) {
	header := []string{"order-id"}
	_, err := columnIndex(header, map[string][]string{
		"sku": {"sku", "asin"},
	})
	if err == nil || !strings.Contains(err.Error(), `missing required column "sku"`) {
		t.Errorf("expected missing column error naming the field, got %v", err)
	}
	if !strings.Contains(err.Error(), "sku, asin") {
		t.Errorf("expected error to list tried aliases, got %v", err)
	}
}

// ----- resolveSKU direct (hint priority over persisted mapping) -------------

func TestZZ_ResolveSKU_HintTakesPriorityOverMapping(t *testing.T) {
	repo := newZZFakeRepo()
	mappedID := uuid.New()
	hintID := uuid.New()
	repo.addMapping("amazon", "SKU-1", mappedID)
	uc := zzBuild(repo, &zzFakeCreator{}, &zzFakeApprover{}, newZZFakeStockChecker(), &zzFakeRater{})

	hintMap := map[string]uuid.UUID{"SKU-1": hintID}
	pid, unknown, err := uc.resolveSKU(context.Background(), uuid.New(), "amazon", "SKU-1", hintMap)
	if err != nil {
		t.Fatalf("resolveSKU: %v", err)
	}
	if unknown {
		t.Fatal("expected resolved, not unknown")
	}
	if pid != hintID {
		t.Errorf("expected hint to win over persisted mapping: got %s, want %s", pid, hintID)
	}
}

func TestZZ_ResolveSKU_UnknownWhenNilMapping(t *testing.T) {
	uc := zzBuild(newZZFakeRepo(), &zzFakeCreator{}, &zzFakeApprover{}, newZZFakeStockChecker(), &zzFakeRater{})
	pid, unknown, err := uc.resolveSKU(context.Background(), uuid.New(), "amazon", "NO-SUCH", map[string]uuid.UUID{})
	if err != nil {
		t.Fatalf("resolveSKU: %v", err)
	}
	if !unknown {
		t.Error("expected unknown=true when mapping is nil")
	}
	if pid != uuid.Nil {
		t.Errorf("expected uuid.Nil product id, got %s", pid)
	}
}

func TestZZ_ResolveSKU_RepoError(t *testing.T) {
	repo := newZZFakeRepo()
	repo.getMappingErr = errors.New("boom")
	uc := zzBuild(repo, &zzFakeCreator{}, &zzFakeApprover{}, newZZFakeStockChecker(), &zzFakeRater{})
	_, _, err := uc.resolveSKU(context.Background(), uuid.New(), "amazon", "SKU-1", map[string]uuid.UUID{})
	if err == nil {
		t.Fatal("expected repo error to propagate")
	}
}

// ----- appendUniqueSKU direct ------------------------------------------------

func TestZZ_AppendUniqueSKU(t *testing.T) {
	list := []UnknownSKU{{Platform: "amazon", PlatformSKU: "A"}}
	same := appendUniqueSKU(list, UnknownSKU{Platform: "amazon", PlatformSKU: "A"})
	if len(same) != 1 {
		t.Errorf("expected duplicate not appended, got len=%d", len(same))
	}
	grown := appendUniqueSKU(list, UnknownSKU{Platform: "amazon", PlatformSKU: "B"})
	if len(grown) != 2 {
		t.Errorf("expected new sku appended, got len=%d", len(grown))
	}
}
