//go:build integration

// Package integration — AI audit + undo integration tests.
//
// Red-line: every AI write to stock must be (a) audited and (b) undoable
// within 30 s. These tests prove the backend audit path against a real
// PostgreSQL container.
//
// Run: go test -v -tags integration -timeout 180s ./tests/integration/...
package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	repoaccount "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/account"
	repostock "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/stock"
	repowarehouse "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/warehouse"
	appacct "github.com/hanmahong5-arch/lurus-tally/internal/app/account"
	appai "github.com/hanmahong5-arch/lurus-tally/internal/app/ai"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domainai "github.com/hanmahong5-arch/lurus-tally/internal/domain/ai"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
	"github.com/hanmahong5-arch/lurus-tally/internal/lifecycle"
)

// ── helpers ──────────────────────────────────────────────────────────────────

// auditSetup runs migrations, opens a *sql.DB, and inserts the minimum tenant +
// product + warehouse rows required for AI plan execution tests.
func auditSetup(t *testing.T) (*sql.DB, uuid.UUID, uuid.UUID, uuid.UUID, func()) {
	t.Helper()
	dsn, pgCleanup := startPostgres(t)

	ctx := context.Background()
	if err := lifecycle.RunMigrations(ctx, dsn, nil); err != nil {
		pgCleanup()
		t.Fatalf("auditSetup: RunMigrations: %v", err)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		pgCleanup()
		t.Fatalf("auditSetup: open db: %v", err)
	}

	tenantID := uuid.New()
	productID := uuid.New()
	warehouseID := uuid.New()
	now := time.Now().UTC()

	// Insert tenant row (referenced by FK on many tables).
	mustExec(t, db, `INSERT INTO tally.tenant (id, name) VALUES ($1, $2)`, tenantID, "audit-test-tenant")

	// Insert product (required for FK on stock_snapshot / stock_movement).
	mustExec(t, db, `
		INSERT INTO tally.product
			(id, tenant_id, code, name, enabled, created_at, updated_at)
		VALUES
			($1, $2, $3, $4, true, $5, $5)`,
		productID, tenantID, "PROD-AUDIT-001", "Audit Test Product", now)

	// Insert warehouse (required by stock adjuster).
	mustExec(t, db, `
		INSERT INTO tally.warehouse
			(id, tenant_id, name, is_default, created_at, updated_at)
		VALUES
			($1, $2, $3, true, $4, $4)`,
		warehouseID, tenantID, "Default Warehouse", now)

	cleanup := func() {
		_ = db.Close()
		pgCleanup()
	}
	return db, tenantID, productID, warehouseID, cleanup
}

// auditTestPlanStore is an in-memory PlanStore for use in integration tests.
// It implements appai.PlanStore without depending on the unexported unit-test mock.
type auditTestPlanStore struct {
	plans map[string]*domainai.Plan
	mu    sync.Mutex
}

func newAuditTestPlanStore() *auditTestPlanStore {
	return &auditTestPlanStore{plans: make(map[string]*domainai.Plan)}
}

func (m *auditTestPlanStore) SavePlan(_ context.Context, plan *domainai.Plan) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *plan
	m.plans[plan.ID.String()] = &cp
	return nil
}

func (m *auditTestPlanStore) GetPlan(_ context.Context, _, planID uuid.UUID) (*domainai.Plan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.plans[planID.String()]
	if !ok {
		return nil, nil
	}
	cp := *p
	return &cp, nil
}

func (m *auditTestPlanStore) UpdatePlan(_ context.Context, plan *domainai.Plan) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *plan
	m.plans[plan.ID.String()] = &cp
	return nil
}

func (m *auditTestPlanStore) ListByTenant(_ context.Context, tenantID uuid.UUID, statusFilter string) ([]*domainai.Plan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*domainai.Plan, 0)
	for _, p := range m.plans {
		if p.TenantID != tenantID {
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

// directAuditWriter satisfies appai.AuditWriter without the lifecycle layer.
type directAuditWriter struct {
	uc *appacct.AppendAuditLog
}

func (d *directAuditWriter) Write(ctx context.Context, rec appai.AuditRecord) error {
	return d.uc.Execute(ctx, appacct.AppendInput{
		TenantID:   rec.TenantID,
		ActorID:    rec.ActorID.String(),
		Action:     rec.Action,
		TargetKind: rec.TargetKind,
		TargetID:   rec.TargetID,
		Payload:    rec.Payload,
	})
}

// staticProductRepo returns a fixed product list regardless of search term.
type staticProductRepo struct{ rows []appai.ProductRow }

func (r *staticProductRepo) SearchProducts(_ context.Context, _ uuid.UUID, _ string) ([]appai.ProductRow, error) {
	return r.rows, nil
}
func (r *staticProductRepo) ListAllProducts(_ context.Context, _ uuid.UUID) ([]appai.ProductRow, error) {
	return r.rows, nil
}

// noopDraftCreator satisfies appai.DraftCreatorPort but creates no real bill.
type noopDraftCreator struct{}

func (n *noopDraftCreator) CreatePurchaseDraft(_ context.Context, _, _ uuid.UUID, _ []appai.DraftLine) (uuid.UUID, string, error) {
	return uuid.New(), "FAKE-PO-001", nil
}

// noopPriceChanger satisfies appai.PriceChangerPort returning len(ids) affected.
type noopPriceChanger struct{}

func (n *noopPriceChanger) ApplyPriceChange(_ context.Context, _ uuid.UUID, ids []uuid.UUID, _ string) (int, error) {
	return len(ids), nil
}

// realStockAdjuster wraps the production RecordMovementUseCase.
type realStockAdjuster struct {
	uc     *appstock.RecordMovementUseCase
	whRepo interface {
		DefaultWarehouseID(ctx context.Context, tenantID uuid.UUID) (uuid.UUID, error)
	}
}

func (a *realStockAdjuster) AdjustStockBatch(ctx context.Context, tenantID, actorID, planID uuid.UUID, lines []appai.StockAdjustLine) (int, error) {
	whID, err := a.whRepo.DefaultWarehouseID(ctx, tenantID)
	if err != nil {
		return 0, fmt.Errorf("realStockAdjuster: resolve warehouse: %w", err)
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
			Note:          "audit integration test — stock adjust",
		}); err != nil {
			return affected, err
		}
		affected++
	}
	return affected, nil
}

// buildOrch constructs a fully-wired orchestrator backed by the provided store.
func buildOrch(db *sql.DB, productRows []appai.ProductRow, store *auditTestPlanStore) *appai.Orchestrator {
	auditRepo := repoaccount.NewAuditRepo(db)
	appendUC := appacct.NewAppendAuditLog(auditRepo)
	auditWriter := &directAuditWriter{uc: appendUC}

	stockRepo := repostock.New(db)
	calc := appstock.NewCalculator(nil, stockRepo)
	recordMvUC := appstock.NewRecordMovementUseCase(stockRepo, calc, nil, nil)
	whRepo := repowarehouse.New(db)

	executor := appai.NewPlanExecutor(
		&staticProductRepo{rows: productRows},
		&noopDraftCreator{},
		&noopPriceChanger{},
		&realStockAdjuster{uc: recordMvUC, whRepo: whRepo},
	)

	orch := appai.NewOrchestrator(nil, nil, store, "")
	orch.WithExecutor(executor)
	orch.WithAudit(auditWriter)
	return orch
}

// pendingPlan returns a new pending plan with expiresAt 1 hour from now.
func pendingPlan(tenantID uuid.UUID, planType domainai.PlanType, payload map[string]any, description string) *domainai.Plan {
	return &domainai.Plan{
		ID:       uuid.New(),
		TenantID: tenantID,
		Type:     planType,
		Status:   domainai.PlanStatusPending,
		Payload:  payload,
		Preview: domainai.PlanPreview{
			Description:   description,
			AffectedCount: 1,
		},
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().Add(time.Hour),
	}
}

// queryAuditRows returns all account_audit_log rows for the given tenantID.
func queryAuditRows(t *testing.T, db *sql.DB, tenantID uuid.UUID) []map[string]any {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `
		SELECT id, tenant_id, actor_id, action,
		       COALESCE(target_kind,''), COALESCE(target_id,''),
		       payload::text, created_at
		FROM tally.account_audit_log
		WHERE tenant_id = $1
		ORDER BY created_at DESC`,
		tenantID)
	if err != nil {
		t.Fatalf("queryAuditRows: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var out []map[string]any
	for rows.Next() {
		var (
			id, tid, actorID, action, targetKind, targetID, payloadText string
			createdAt                                                   time.Time
		)
		if err := rows.Scan(&id, &tid, &actorID, &action, &targetKind, &targetID, &payloadText, &createdAt); err != nil {
			t.Fatalf("queryAuditRows scan: %v", err)
		}
		var payload map[string]any
		_ = json.Unmarshal([]byte(payloadText), &payload)
		out = append(out, map[string]any{
			"id":          id,
			"tenant_id":   tid,
			"actor_id":    actorID,
			"action":      action,
			"target_kind": targetKind,
			"target_id":   targetID,
			"payload":     payload,
			"created_at":  createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("queryAuditRows rows.Err: %v", err)
	}
	return out
}

// getSnapshotOnHand returns the on_hand_qty for a product/warehouse pair, or zero if no row yet.
func getSnapshotOnHand(t *testing.T, db *sql.DB, tenantID, productID, warehouseID uuid.UUID) decimal.Decimal {
	t.Helper()
	var qty string
	err := db.QueryRowContext(context.Background(), `
		SELECT on_hand_qty::text FROM tally.stock_snapshot
		WHERE tenant_id = $1 AND product_id = $2 AND warehouse_id = $3`,
		tenantID, productID, warehouseID).Scan(&qty)
	if err == sql.ErrNoRows {
		return decimal.Zero
	}
	if err != nil {
		t.Fatalf("getSnapshotOnHand: %v", err)
	}
	d, _ := decimal.NewFromString(qty)
	return d
}

// defaultWarehouseID returns the default warehouse ID for tenantID from the DB.
func defaultWarehouseID(t *testing.T, db *sql.DB, tenantID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.QueryRowContext(context.Background(), `
		SELECT id FROM tally.warehouse
		WHERE tenant_id = $1 AND deleted_at IS NULL
		ORDER BY is_default DESC, created_at ASC LIMIT 1`,
		tenantID).Scan(&id)
	if err != nil {
		t.Fatalf("defaultWarehouseID: %v", err)
	}
	return id
}

// ── Test 1: successful confirm writes an audit row ────────────────────────────

// TestAudit_OnPlanConfirm_WritesRow proves that confirming a stock-adjust plan
// results in exactly one account_audit_log row containing the expected fields.
func TestAudit_OnPlanConfirm_WritesRow(t *testing.T) {
	db, tenantID, productID, warehouseID, cleanup := auditSetup(t)
	defer cleanup()
	_ = warehouseID

	actorID := uuid.New()
	store := newAuditTestPlanStore()
	orch := buildOrch(db, []appai.ProductRow{{ID: productID, Name: "Audit Test Product"}}, store)

	plan := pendingPlan(tenantID, domainai.PlanTypeBulkStockAdjust,
		map[string]any{"filter": "Audit Test Product", "delta": 10.0},
		"Bulk stock adjust +10 for Audit Test Product")

	if err := store.SavePlan(context.Background(), plan); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	_, _, err := orch.ConfirmPlan(context.Background(), tenantID, actorID, plan.ID)
	if err != nil {
		t.Fatalf("ConfirmPlan: %v", err)
	}

	rows := queryAuditRows(t, db, tenantID)
	if len(rows) != 1 {
		t.Fatalf("expected exactly 1 audit row, got %d", len(rows))
	}

	row := rows[0]
	rowJSON, _ := json.MarshalIndent(row, "", "  ")
	t.Logf("audit row (evidence):\n%s", rowJSON)

	if row["actor_id"] != actorID.String() {
		t.Errorf("actor_id: got %q, want %q", row["actor_id"], actorID.String())
	}
	if row["action"] != "ai.plan.executed" {
		t.Errorf("action: got %q, want %q", row["action"], "ai.plan.executed")
	}

	payload, _ := row["payload"].(map[string]any)
	if payload == nil {
		t.Fatal("payload is nil or not a map")
	}
	if payload["plan_id"] != plan.ID.String() {
		t.Errorf("payload.plan_id: got %v, want %s", payload["plan_id"], plan.ID.String())
	}
	if payload["type"] != string(domainai.PlanTypeBulkStockAdjust) {
		t.Errorf("payload.type: got %v, want %s", payload["type"], domainai.PlanTypeBulkStockAdjust)
	}
	if _, ok := payload["affected_count"]; !ok {
		t.Error("payload.affected_count missing")
	}

	if ct, ok := row["created_at"].(time.Time); ok {
		if time.Since(ct) > 60*time.Second {
			t.Errorf("created_at too old: %v", ct)
		}
	} else {
		t.Errorf("created_at not a time.Time: %T %v", row["created_at"], row["created_at"])
	}
}

// ── Test 2: all three plan types produce distinguishable audit rows ───────────

// TestAudit_AcrossAllThreePlanTypes confirms that each of the three plan kinds
// produces its own audit row and the rows are distinguishable by payload.type.
func TestAudit_AcrossAllThreePlanTypes(t *testing.T) {
	db, tenantID, productID, warehouseID, cleanup := auditSetup(t)
	defer cleanup()
	_ = warehouseID

	actorID := uuid.New()
	store := newAuditTestPlanStore()
	orch := buildOrch(db, []appai.ProductRow{{ID: productID, Name: "TypeTest Product"}}, store)

	plans := []*domainai.Plan{
		pendingPlan(tenantID, domainai.PlanTypeCreatePurchase,
			map[string]any{
				"items": []map[string]any{{"product_name": "TypeTest Product", "qty": 5.0}},
			},
			"Purchase draft for TypeTest Product"),
		pendingPlan(tenantID, domainai.PlanTypePriceChange,
			map[string]any{"filter": "TypeTest Product", "action": "+5%"},
			"Price change for TypeTest Product"),
		pendingPlan(tenantID, domainai.PlanTypeBulkStockAdjust,
			map[string]any{"filter": "TypeTest Product", "delta": 3.0},
			"Stock adjust +3 for TypeTest Product"),
	}

	ctx := context.Background()
	for _, p := range plans {
		if err := store.SavePlan(ctx, p); err != nil {
			t.Fatalf("SavePlan(%s): %v", p.Type, err)
		}
		if _, _, err := orch.ConfirmPlan(ctx, tenantID, actorID, p.ID); err != nil {
			t.Fatalf("ConfirmPlan(%s): %v", p.Type, err)
		}
	}

	rows := queryAuditRows(t, db, tenantID)
	if len(rows) != 3 {
		t.Fatalf("expected 3 audit rows (one per plan type), got %d", len(rows))
	}

	typesSeen := map[string]bool{}
	for _, row := range rows {
		payload, _ := row["payload"].(map[string]any)
		if payload == nil {
			t.Errorf("row %v has nil payload", row["id"])
			continue
		}
		pt, _ := payload["type"].(string)
		if pt == "" {
			t.Errorf("row %v payload.type is empty", row["id"])
			continue
		}
		if typesSeen[pt] {
			t.Errorf("duplicate plan type in audit: %s", pt)
		}
		typesSeen[pt] = true

		rowJSON, _ := json.MarshalIndent(row, "", "  ")
		t.Logf("audit row for %s:\n%s", pt, rowJSON)
	}

	for _, pt := range []string{
		string(domainai.PlanTypeCreatePurchase),
		string(domainai.PlanTypePriceChange),
		string(domainai.PlanTypeBulkStockAdjust),
	} {
		if !typesSeen[pt] {
			t.Errorf("no audit row found for plan type %q", pt)
		}
	}
}

// ── Test 3: failed confirm still records an audit row ────────────────────────

// TestAudit_OnFailedConfirm_StillRecords verifies that a plan execution that
// fails (empty product repo → no products found) still writes an audit row
// with action "ai.plan.failed" and an error in the payload.
func TestAudit_OnFailedConfirm_StillRecords(t *testing.T) {
	db, tenantID, _, _, cleanup := auditSetup(t)
	defer cleanup()

	actorID := uuid.New()

	// Empty product repo — product lookup returns nothing, so executor errors.
	store := newAuditTestPlanStore()
	orch := buildOrch(db, nil, store)

	plan := pendingPlan(tenantID, domainai.PlanTypeCreatePurchase,
		map[string]any{
			"items": []map[string]any{{"product_name": "NonExistentProduct", "qty": 5.0}},
		},
		"Will fail — product not found")

	if err := store.SavePlan(context.Background(), plan); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	_, _, execErr := orch.ConfirmPlan(context.Background(), tenantID, actorID, plan.ID)
	if execErr == nil {
		t.Fatal("expected ConfirmPlan to fail for unresolved product, but it succeeded")
	}
	t.Logf("ConfirmPlan failed as expected: %v", execErr)

	rows := queryAuditRows(t, db, tenantID)
	if len(rows) != 1 {
		t.Fatalf("expected 1 audit row for failed plan, got %d", len(rows))
	}

	row := rows[0]
	rowJSON, _ := json.MarshalIndent(row, "", "  ")
	t.Logf("failure audit row (evidence):\n%s", rowJSON)

	if row["action"] != "ai.plan.failed" {
		t.Errorf("action: got %q, want %q", row["action"], "ai.plan.failed")
	}

	payload, _ := row["payload"].(map[string]any)
	if payload == nil {
		t.Fatal("payload is nil")
	}
	errMsg, _ := payload["error"].(string)
	if errMsg == "" {
		t.Error("payload.error must be non-empty for a failed plan")
	}
	t.Logf("failure reason in payload: %q", errMsg)
}

// ── Test 4: concurrent confirms — only one audit row ────────────────────────

// TestAudit_ConcurrentConfirms_OneAuditRow verifies that when two goroutines race
// to confirm the same plan, only one succeeds (the plan-status flip is the lock),
// and therefore only one audit row appears.
func TestAudit_ConcurrentConfirms_OneAuditRow(t *testing.T) {
	db, tenantID, productID, warehouseID, cleanup := auditSetup(t)
	defer cleanup()
	_ = warehouseID

	actorID := uuid.New()
	store := newAuditTestPlanStore()
	orch := buildOrch(db, []appai.ProductRow{{ID: productID, Name: "Concurrent Product"}}, store)

	plan := pendingPlan(tenantID, domainai.PlanTypeBulkStockAdjust,
		map[string]any{"filter": "Concurrent Product", "delta": 1.0},
		"Concurrent confirm idempotency test")

	if err := store.SavePlan(context.Background(), plan); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	var (
		wg        sync.WaitGroup
		successes int
		mu        sync.Mutex
	)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := orch.ConfirmPlan(context.Background(), tenantID, actorID, plan.ID)
			if err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	// Exactly one goroutine should have succeeded.
	if successes != 1 {
		t.Errorf("expected exactly 1 successful confirm, got %d", successes)
	}

	// Exactly one audit row must exist.
	rows := queryAuditRows(t, db, tenantID)
	t.Logf("audit rows after concurrent confirm: %d (want 1)", len(rows))
	if len(rows) != 1 {
		t.Errorf("expected 1 audit row, got %d", len(rows))
		for i, r := range rows {
			b, _ := json.MarshalIndent(r, "", "  ")
			t.Logf("row[%d]: %s", i, b)
		}
	}
}

// ── Test 5: undo / stock movement reversal shape check ───────────────────────

// TestUndo_StockMovementReversal_ShapeCheck verifies that a stock-adjust AI plan
// can be "undone" at the DB level by recording an opposite adjust movement.
//
// PARTIAL: the 30 s undo guarantee is enforced by the frontend undo stack
// (web/lib/undo/undo-stack.ts). This test proves the storage-layer reversal
// works; the time constraint is separately validated by frontend E2E tests.
func TestUndo_StockMovementReversal_ShapeCheck(t *testing.T) {
	db, tenantID, productID, warehouseID, cleanup := auditSetup(t)
	defer cleanup()
	_ = warehouseID

	actorID := uuid.New()
	store := newAuditTestPlanStore()
	orch := buildOrch(db, []appai.ProductRow{{ID: productID, Name: "Undo Test Product"}}, store)

	whID := defaultWarehouseID(t, db, tenantID)
	qtyBefore := getSnapshotOnHand(t, db, tenantID, productID, whID)
	t.Logf("on_hand_qty before confirm: %s", qtyBefore)

	delta := decimal.NewFromInt(10)
	plan := pendingPlan(tenantID, domainai.PlanTypeBulkStockAdjust,
		map[string]any{"filter": "Undo Test Product", "delta": delta.InexactFloat64()},
		"Undo test stock adjust +10")

	if err := store.SavePlan(context.Background(), plan); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	_, _, err := orch.ConfirmPlan(context.Background(), tenantID, actorID, plan.ID)
	if err != nil {
		t.Fatalf("ConfirmPlan: %v", err)
	}

	qtyAfterConfirm := getSnapshotOnHand(t, db, tenantID, productID, whID)
	t.Logf("on_hand_qty after confirm: %s (expected +%s)", qtyAfterConfirm, delta)

	if !qtyAfterConfirm.Equal(qtyBefore.Add(delta)) {
		t.Errorf("expected on_hand_qty = %s after +%s adjust, got %s",
			qtyBefore.Add(delta), delta, qtyAfterConfirm)
	}

	// Reversal: record an opposite adjust movement (-10) to undo.
	stockRepo := repostock.New(db)
	calc := appstock.NewCalculator(nil, stockRepo)
	reverseUC := appstock.NewRecordMovementUseCase(stockRepo, calc, nil, nil)
	revRefID := uuid.New()

	_, err = reverseUC.Execute(context.Background(), appstock.RecordMovementRequest{
		TenantID:      tenantID,
		ProductID:     productID,
		WarehouseID:   whID,
		Direction:     domainstock.DirectionAdjust,
		Qty:           delta.Neg(),
		ConvFactor:    "1",
		CostStrategy:  domainstock.CostStrategyWAC,
		ReferenceType: domainstock.RefAdjust,
		ReferenceID:   &revRefID,
		CreatedBy:     &actorID,
		Note:          "undo: reversal of AI adjust plan",
	})
	if err != nil {
		t.Fatalf("reversal movement: %v", err)
	}

	qtyAfterReversal := getSnapshotOnHand(t, db, tenantID, productID, whID)
	t.Logf("on_hand_qty after reversal: %s (want %s)", qtyAfterReversal, qtyBefore)

	if !qtyAfterReversal.Equal(qtyBefore) {
		t.Errorf("reversal did not restore qty: got %s, want %s", qtyAfterReversal, qtyBefore)
	}

	t.Log("PARTIAL: backend reversal via opposite adjust movement verified (storage layer).")
	t.Log("PARTIAL: the 30 s time-gate for undo lives in web/lib/undo/undo-stack.ts;")
	t.Log("         no backend time-fenced reverse endpoint exists — covered by FE E2E tests.")
}
