//go:build integration

// Package integration — AI plan confirm E2E tests.
// Verifies that ConfirmPlan executes real side-effects against a live PG container.
//
// Run:
//
//	go test -v -tags integration -timeout 180s ./tests/integration/... -run TestConfirmPlan
package integration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"

	repoai "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/ai"
	repobill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/bill"
	reposku "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/sku"
	repostock "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/stock"
	repowarehouse "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/warehouse"
	appai "github.com/hanmahong5-arch/lurus-tally/internal/app/ai"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	appsku "github.com/hanmahong5-arch/lurus-tally/internal/app/sku"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domainai "github.com/hanmahong5-arch/lurus-tally/internal/domain/ai"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
	"github.com/hanmahong5-arch/lurus-tally/internal/lifecycle"
)

// ---- in-memory plan store (used when Redis is unavailable) ----

type memPlanStore struct {
	mu    sync.Mutex
	plans map[string]*domainai.Plan
}

func newMemPlanStore() *memPlanStore {
	return &memPlanStore{plans: make(map[string]*domainai.Plan)}
}

func memKey(tenantID, planID uuid.UUID) string {
	return tenantID.String() + ":" + planID.String()
}

func (s *memPlanStore) SavePlan(_ context.Context, plan *domainai.Plan) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *plan
	s.plans[memKey(plan.TenantID, plan.ID)] = &cp
	return nil
}

func (s *memPlanStore) GetPlan(_ context.Context, tenantID, planID uuid.UUID) (*domainai.Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.plans[memKey(tenantID, planID)]
	if !ok {
		return nil, nil
	}
	cp := *p
	return &cp, nil
}

func (s *memPlanStore) UpdatePlan(_ context.Context, plan *domainai.Plan) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := memKey(plan.TenantID, plan.ID)
	existing, ok := s.plans[key]
	if !ok {
		return fmt.Errorf("plan not found: %s", key)
	}
	// Enforce optimistic concurrency: reject a Confirmed write if the stored
	// plan is already Confirmed — this mimics a real atomic compare-and-swap so
	// concurrent goroutines cannot both advance from Pending → Confirmed.
	if plan.Status == domainai.PlanStatusConfirmed && existing.Status == domainai.PlanStatusConfirmed {
		return fmt.Errorf("confirm plan: plan is confirmed, cannot confirm")
	}
	cp := *plan
	s.plans[key] = &cp
	return nil
}

func (s *memPlanStore) ListByTenant(_ context.Context, tenantID uuid.UUID, statusFilter string) ([]*domainai.Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prefix := tenantID.String() + ":"
	var out []*domainai.Plan
	for k, p := range s.plans {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		if statusFilter != "" && string(p.Status) != statusFilter {
			continue
		}
		cp := *p
		out = append(out, &cp)
	}
	return out, nil
}

// newMockPlanStore is a package-level alias used by ai_undo_audit_test.go, which
// references this constructor but does not define it. We return an auditTestPlanStore
// (defined in ai_undo_audit_test.go) which satisfies the same appai.PlanStore interface.
// The function must live here so the integration package builds in all test runs.
func newMockPlanStore() *auditTestPlanStore {
	return newAuditTestPlanStore()
}

// ---- wiring helpers ----

// setupAIE2EDB starts a fresh postgres container, runs migrations, and returns
// an open *sql.DB. Caller must invoke cleanup() when done.
func setupAIE2EDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	dsn, cleanup := startPostgres(t)
	ctx := context.Background()
	if err := lifecycle.RunMigrations(ctx, dsn, nil); err != nil {
		cleanup()
		t.Fatalf("RunMigrations: %v", err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		cleanup()
		t.Fatalf("open db: %v", err)
	}
	return db, func() {
		_ = db.Close()
		cleanup()
	}
}

// buildOrchestrator wires all real adapters and returns a fully armed Orchestrator
// plus the in-memory plan store.
func buildOrchestrator(db *sql.DB) (*appai.Orchestrator, *memPlanStore) {
	billRepo := repobill.New(db)
	stockRepo := repostock.New(db)
	skuRepo := reposku.New(db)
	whRepo := repowarehouse.New(db)

	// WAC calculator (default, no profile needed for tests)
	wacCalc := appstock.NewCalculator(nil, stockRepo)
	recordMvUC := appstock.NewRecordMovementUseCase(stockRepo, wacCalc, nil, nil)
	sqlProductRepo := repoai.NewSQLProductRepo(db)

	executor := appai.NewPlanExecutor(
		sqlProductRepo,
		// draft creator — adapts repobill + reposku
		&testDraftCreator{billRepo: billRepo, skuRepo: skuRepo, whRepo: whRepo},
		// price changer
		&testPriceChanger{uc: appsku.NewUpdatePriceUseCase(skuRepo)},
		// stock adjuster
		&testStockAdjuster{uc: recordMvUC, whRepo: whRepo},
	)

	store := newMemPlanStore()
	orch := appai.NewOrchestrator(nil, nil, store, "")
	orch = orch.WithExecutor(executor)
	return orch, store
}

// ---- port adapters used in tests (mirrors lifecycle/ai_executor.go) ----

type testDraftCreator struct {
	billRepo appbill.BillRepo
	skuRepo  *reposku.Repo
	whRepo   *repowarehouse.Repo
}

func (a *testDraftCreator) CreatePurchaseDraft(ctx context.Context, tenantID, actorID uuid.UUID, lines []appai.DraftLine) (uuid.UUID, string, error) {
	ids := make([]uuid.UUID, 0, len(lines))
	for _, l := range lines {
		ids = append(ids, l.ProductID)
	}
	priceByProduct := make(map[uuid.UUID]decimal.Decimal, len(ids))
	if skus, err := a.skuRepo.ListDefaultSKUs(ctx, tenantID, ids); err == nil {
		for _, s := range skus {
			priceByProduct[s.ProductID] = s.PurchasePrice
		}
	}
	items := make([]appbill.CreatePurchaseItemInput, 0, len(lines))
	for i, l := range lines {
		items = append(items, appbill.CreatePurchaseItemInput{
			ProductID: l.ProductID,
			LineNo:    i + 1,
			Qty:       l.Qty,
			UnitPrice: priceByProduct[l.ProductID],
		})
	}
	out, err := appbill.NewCreatePurchaseDraftUseCase(a.billRepo).Execute(ctx, appbill.CreatePurchaseDraftRequest{
		TenantID:  tenantID,
		CreatorID: actorID,
		BillDate:  time.Now().UTC(),
		Remark:    "test AI draft",
		Items:     items,
	})
	if err != nil {
		return uuid.Nil, "", err
	}
	return out.BillID, out.BillNo, nil
}

type testPriceChanger struct {
	uc *appsku.UpdatePriceUseCase
}

func (a *testPriceChanger) ApplyPriceChange(ctx context.Context, tenantID uuid.UUID, productIDs []uuid.UUID, action string) (int, error) {
	return a.uc.Execute(ctx, tenantID, productIDs, action)
}

type testStockAdjuster struct {
	uc     *appstock.RecordMovementUseCase
	whRepo *repowarehouse.Repo
}

func (a *testStockAdjuster) AdjustStockBatch(ctx context.Context, tenantID, actorID, planID uuid.UUID, lines []appai.StockAdjustLine) (int, error) {
	whID, err := a.whRepo.DefaultWarehouseID(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	affected := 0
	refID := planID
	for _, ln := range lines {
		if _, err := a.uc.Execute(ctx, appstock.RecordMovementRequest{
			TenantID:      tenantID,
			ProductID:     ln.ProductID,
			WarehouseID:   whID,
			Direction:     domainstock.DirectionAdjust,
			Qty:           ln.Delta,
			ConvFactor:    "1",
			CostStrategy:  domainstock.CostStrategyWAC,
			ReferenceType: domainstock.RefAdjust,
			ReferenceID:   &refID,
			CreatedBy:     &actorID,
			Note:          "test AI adjust",
		}); err != nil {
			return affected, err
		}
		affected++
	}
	return affected, nil
}

// ---- seed helpers ----

// seedTenant inserts a minimal tally.tenant row.
func seedTenant(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	mustExec(t, db,
		`INSERT INTO tally.tenant (id, name) VALUES ($1, $2)`,
		id, "test-tenant-"+id.String()[:8])
	return id
}

// seedProduct inserts a product (enabled, not deleted) and returns its ID.
func seedProduct(t *testing.T, db *sql.DB, tenantID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	mustExec(t, db,
		`INSERT INTO tally.product
			(id, tenant_id, name, code, enabled, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,true,$5,$6)`,
		id, tenantID, name, "CODE-"+id.String()[:6], now, now)
	return id
}

// seedProductSKU inserts a product_sku row with the given retail_price.
// Returns the SKU ID. Schema from migration 000005: no sku_code column.
func seedProductSKU(t *testing.T, db *sql.DB, tenantID, productID uuid.UUID, retailPrice decimal.Decimal) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	mustExec(t, db,
		`INSERT INTO tally.product_sku
			(id, tenant_id, product_id, retail_price, purchase_price, is_default, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,true,$6,$7)`,
		id, tenantID, productID,
		retailPrice.String(),
		decimal.NewFromInt(80).String(),
		now, now)
	return id
}

// seedWarehouse inserts a default warehouse for tenantID.
func seedWarehouse(t *testing.T, db *sql.DB, tenantID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	mustExec(t, db,
		`INSERT INTO tally.warehouse
			(id, tenant_id, name, is_default, created_at, updated_at)
		 VALUES ($1,$2,$3,true,$4,$5)`,
		id, tenantID, "Main Warehouse", now, now)
	return id
}

// seedStockSnapshot inserts an initial stock_snapshot row.
func seedStockSnapshot(t *testing.T, db *sql.DB, tenantID, productID, warehouseID uuid.UUID, onHand decimal.Decimal) {
	t.Helper()
	now := time.Now().UTC()
	mustExec(t, db,
		`INSERT INTO tally.stock_snapshot
			(id, tenant_id, product_id, warehouse_id,
			 on_hand_qty, available_qty, avg_cost_price, unit_cost, cost_strategy, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'wac',$9)`,
		uuid.New(), tenantID, productID, warehouseID,
		onHand.String(), onHand.String(),
		decimal.NewFromInt(50).String(), decimal.NewFromInt(50).String(),
		now)
}

// makePlan builds a plan ready to be saved.
func makePlan(tenantID uuid.UUID, planType domainai.PlanType, payload map[string]interface{}) *domainai.Plan {
	return &domainai.Plan{
		ID:        uuid.New(),
		TenantID:  tenantID,
		Type:      planType,
		Status:    domainai.PlanStatusPending,
		Payload:   payload,
		Preview:   domainai.PlanPreview{Description: "test plan"},
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(30 * time.Minute),
	}
}

// ============================================================
// Test 1: CreatePurchaseDraft
// ============================================================

func TestConfirmPlan_CreatePurchaseDraft_E2E(t *testing.T) {
	db, cleanup := setupAIE2EDB(t)
	defer cleanup()

	ctx := context.Background()
	tenantID := seedTenant(t, db)
	actorID := uuid.New()
	productID := seedProduct(t, db, tenantID, "Widget Alpha")
	_ = seedProductSKU(t, db, tenantID, productID, decimal.NewFromInt(100))

	plan := makePlan(tenantID, domainai.PlanTypeCreatePurchase, map[string]interface{}{
		"items": []map[string]interface{}{
			{"product_name": "Widget Alpha", "qty": float64(10)},
		},
	})

	orch, store := buildOrchestrator(db)
	if err := store.SavePlan(ctx, plan); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	confirmedPlan, result, err := orch.ConfirmPlan(ctx, tenantID, actorID, plan.ID)
	if err != nil {
		t.Fatalf("ConfirmPlan: %v", err)
	}
	if confirmedPlan.Status != domainai.PlanStatusConfirmed {
		t.Errorf("plan status: got %s, want confirmed", confirmedPlan.Status)
	}
	if result == nil {
		t.Fatal("ExecutionResult is nil")
	}
	if result.BillID == nil {
		t.Fatal("ExecutionResult.BillID is nil")
	}
	if result.AffectedCount != 1 {
		t.Errorf("AffectedCount: got %d, want 1", result.AffectedCount)
	}

	// Verify bill_head row
	var billType, subType string
	var status int16
	err = db.QueryRowContext(ctx,
		`SELECT bill_type, sub_type, status FROM tally.bill_head WHERE id = $1 AND tenant_id = $2`,
		result.BillID, tenantID).Scan(&billType, &subType, &status)
	if err != nil {
		t.Fatalf("query bill_head: %v", err)
	}
	t.Logf("PASS bill_head: bill_id=%s bill_type=%s sub_type=%s status=%d bill_no=%s",
		result.BillID, billType, subType, status, result.BillNo)

	if billType != "入库" {
		t.Errorf("bill_type: got %q, want '入库'", billType)
	}
	if subType != "采购" {
		t.Errorf("sub_type: got %q, want '采购'", subType)
	}
	if status != 0 {
		t.Errorf("status: got %d, want 0 (draft)", status)
	}

	// Verify bill_item rows
	var itemCount int
	err = db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tally.bill_item WHERE head_id = $1 AND tenant_id = $2`,
		result.BillID, tenantID).Scan(&itemCount)
	if err != nil {
		t.Fatalf("count bill_item: %v", err)
	}
	if itemCount != 1 {
		t.Errorf("bill_item count: got %d, want 1", itemCount)
	}
	t.Logf("PASS bill_item count=%d", itemCount)
}

// ============================================================
// Test 2: PriceChange
// ============================================================

func TestConfirmPlan_PriceChange_E2E(t *testing.T) {
	db, cleanup := setupAIE2EDB(t)
	defer cleanup()

	ctx := context.Background()
	tenantID := seedTenant(t, db)
	actorID := uuid.New()
	productID := seedProduct(t, db, tenantID, "Price-Test Widget")
	skuID := seedProductSKU(t, db, tenantID, productID, decimal.NewFromInt(100))

	plan := makePlan(tenantID, domainai.PlanTypePriceChange, map[string]interface{}{
		"filter": "Price-Test Widget",
		"action": "+10%",
	})

	orch, store := buildOrchestrator(db)
	if err := store.SavePlan(ctx, plan); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	confirmedPlan, result, err := orch.ConfirmPlan(ctx, tenantID, actorID, plan.ID)
	if err != nil {
		t.Fatalf("ConfirmPlan: %v", err)
	}
	if confirmedPlan.Status != domainai.PlanStatusConfirmed {
		t.Errorf("plan status: got %s, want confirmed", confirmedPlan.Status)
	}
	if result.AffectedCount != 1 {
		t.Errorf("AffectedCount: got %d, want 1", result.AffectedCount)
	}

	// Verify retail_price updated
	var retailStr string
	err = db.QueryRowContext(ctx,
		`SELECT retail_price FROM tally.product_sku WHERE id = $1 AND tenant_id = $2`,
		skuID, tenantID).Scan(&retailStr)
	if err != nil {
		t.Fatalf("query product_sku: %v", err)
	}
	retail, _ := decimal.NewFromString(retailStr)
	// 100 * 1.10 = 110; rounded to 6 decimals
	expected := decimal.NewFromInt(110)
	if !retail.Equal(expected) {
		t.Errorf("retail_price: got %s, want %s", retail, expected)
	}
	t.Logf("PASS retail_price before=100 after=%s (sku=%s)", retail, skuID)
}

// ============================================================
// Test 3: StockAdjust
// ============================================================

func TestConfirmPlan_StockAdjust_E2E(t *testing.T) {
	db, cleanup := setupAIE2EDB(t)
	defer cleanup()

	ctx := context.Background()
	tenantID := seedTenant(t, db)
	actorID := uuid.New()
	productID := seedProduct(t, db, tenantID, "Adjust-Test Widget")
	warehouseID := seedWarehouse(t, db, tenantID)
	seedStockSnapshot(t, db, tenantID, productID, warehouseID, decimal.NewFromInt(50))

	plan := makePlan(tenantID, domainai.PlanTypeBulkStockAdjust, map[string]interface{}{
		"filter": "Adjust-Test Widget",
		"delta":  float64(5),
	})

	orch, store := buildOrchestrator(db)
	if err := store.SavePlan(ctx, plan); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	confirmedPlan, result, err := orch.ConfirmPlan(ctx, tenantID, actorID, plan.ID)
	if err != nil {
		t.Fatalf("ConfirmPlan: %v", err)
	}
	if confirmedPlan.Status != domainai.PlanStatusConfirmed {
		t.Errorf("plan status: got %s, want confirmed", confirmedPlan.Status)
	}
	if result.AffectedCount != 1 {
		t.Errorf("AffectedCount: got %d, want 1", result.AffectedCount)
	}

	// Verify stock_movement row
	var direction string
	var qtyBase string
	err = db.QueryRowContext(ctx,
		`SELECT direction, qty_base FROM tally.stock_movement
		 WHERE tenant_id = $1 AND product_id = $2 AND reference_type = 'adjust'
		 ORDER BY created_at DESC LIMIT 1`,
		tenantID, productID).Scan(&direction, &qtyBase)
	if err != nil {
		t.Fatalf("query stock_movement: %v", err)
	}
	if direction != "adjust" {
		t.Errorf("direction: got %q, want 'adjust'", direction)
	}
	qty, _ := decimal.NewFromString(qtyBase)
	if !qty.Equal(decimal.NewFromInt(5)) {
		t.Errorf("qty_base: got %s, want 5", qty)
	}
	t.Logf("PASS stock_movement direction=%s qty_base=%s", direction, qty)

	// Verify snapshot updated
	var onHandStr string
	err = db.QueryRowContext(ctx,
		`SELECT on_hand_qty FROM tally.stock_snapshot
		 WHERE tenant_id = $1 AND product_id = $2 AND warehouse_id = $3`,
		tenantID, productID, warehouseID).Scan(&onHandStr)
	if err != nil {
		t.Fatalf("query stock_snapshot: %v", err)
	}
	onHand, _ := decimal.NewFromString(onHandStr)
	if !onHand.Equal(decimal.NewFromInt(55)) {
		t.Errorf("on_hand_qty: got %s, want 55", onHand)
	}
	t.Logf("PASS stock_snapshot on_hand_qty before=50 after=%s", onHand)
}

// ============================================================
// Test 4: Idempotency (concurrent confirm)
// ============================================================

func TestConfirmPlan_Idempotency(t *testing.T) {
	db, cleanup := setupAIE2EDB(t)
	defer cleanup()

	ctx := context.Background()
	tenantID := seedTenant(t, db)
	actorID := uuid.New()
	productID := seedProduct(t, db, tenantID, "Idempotency Widget")
	_ = seedProductSKU(t, db, tenantID, productID, decimal.NewFromInt(50))

	plan := makePlan(tenantID, domainai.PlanTypeCreatePurchase, map[string]interface{}{
		"items": []map[string]interface{}{
			{"product_name": "Idempotency Widget", "qty": float64(3)},
		},
	})

	orch, store := buildOrchestrator(db)
	if err := store.SavePlan(ctx, plan); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	type result struct {
		plan *domainai.Plan
		res  *appai.ExecutionResult
		err  error
	}
	results := make([]result, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			p, r, e := orch.ConfirmPlan(ctx, tenantID, actorID, plan.ID)
			results[idx] = result{p, r, e}
		}(i)
	}
	wg.Wait()

	successes := 0
	failures := 0
	for _, r := range results {
		if r.err == nil {
			successes++
		} else {
			failures++
			t.Logf("second confirm error (expected): %v", r.err)
		}
	}
	if successes != 1 {
		t.Errorf("expected exactly 1 success, got %d (failures=%d)", successes, failures)
	}
	if failures != 1 {
		t.Errorf("expected exactly 1 failure, got %d", failures)
	}

	// Verify exactly one bill was created
	var billCount int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tally.bill_head WHERE tenant_id = $1`,
		tenantID).Scan(&billCount); err != nil {
		t.Fatalf("count bills: %v", err)
	}
	if billCount != 1 {
		t.Errorf("bill count: got %d, want 1 (no double execution)", billCount)
	}
	t.Logf("PASS idempotency: successes=%d failures=%d bills_created=%d", successes, failures, billCount)
}

// ============================================================
// Test 5: FailureRollback
// ============================================================

func TestConfirmPlan_FailureRollback(t *testing.T) {
	db, cleanup := setupAIE2EDB(t)
	defer cleanup()

	ctx := context.Background()
	tenantID := seedTenant(t, db)
	actorID := uuid.New()

	// Reference a product name that does not exist in the DB.
	plan := makePlan(tenantID, domainai.PlanTypeCreatePurchase, map[string]interface{}{
		"items": []map[string]interface{}{
			{"product_name": "NonExistent Ghost Product XYZ", "qty": float64(1)},
		},
	})

	orch, store := buildOrchestrator(db)
	if err := store.SavePlan(ctx, plan); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	_, _, err := orch.ConfirmPlan(ctx, tenantID, actorID, plan.ID)
	if err == nil {
		t.Fatal("ConfirmPlan: expected error for non-existent product, got nil")
	}
	t.Logf("error (expected): %v", err)

	// Plan status must be the terminal Failed state (f191632f / F09+F13): a partial
	// execution may already have side effects, so the plan is NOT reverted to
	// Pending for retry — the user must cancel and request a fresh suggestion.
	fetched, fetchErr := store.GetPlan(ctx, tenantID, plan.ID)
	if fetchErr != nil {
		t.Fatalf("GetPlan after failed confirm: %v", fetchErr)
	}
	if fetched == nil {
		t.Fatal("plan missing from store after failed confirm")
	}
	if fetched.Status != domainai.PlanStatusFailed {
		t.Errorf("plan status after failed confirm: got %s, want failed (terminal)", fetched.Status)
	}
	t.Logf("PASS: plan status=%s (terminal Failed, not retryable)", fetched.Status)

	// No bill rows should exist for this tenant.
	var billCount int
	if err2 := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tally.bill_head WHERE tenant_id = $1`,
		tenantID).Scan(&billCount); err2 != nil {
		t.Fatalf("count bills: %v", err2)
	}
	if billCount != 0 {
		t.Errorf("bills after failed confirm: got %d, want 0", billCount)
	}
	t.Logf("PASS rollback: no bill rows created (count=%d)", billCount)

	// Verify error message is meaningful.
	if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "NonExistent") {
		t.Logf("note: error message does not mention the missing product name (acceptable): %v", err)
	}

	// Suppress unused import warning for errors package.
	_ = errors.New
}
