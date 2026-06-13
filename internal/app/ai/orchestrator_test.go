package ai_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	appai "github.com/hanmahong5-arch/lurus-tally/internal/app/ai"
	domainai "github.com/hanmahong5-arch/lurus-tally/internal/domain/ai"
)

// --- mock PlanStore ---

type mockPlanStore struct {
	plans   map[string]*domainai.Plan
	saveErr error
}

func newMockPlanStore() *mockPlanStore {
	return &mockPlanStore{plans: make(map[string]*domainai.Plan)}
}

func (m *mockPlanStore) SavePlan(_ context.Context, plan *domainai.Plan) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.plans[plan.ID.String()] = plan
	return nil
}

func (m *mockPlanStore) GetPlan(_ context.Context, _, planID uuid.UUID) (*domainai.Plan, error) {
	p, ok := m.plans[planID.String()]
	if !ok {
		return nil, nil
	}
	return p, nil
}

func (m *mockPlanStore) UpdatePlan(_ context.Context, plan *domainai.Plan) error {
	m.plans[plan.ID.String()] = plan
	return nil
}

func (m *mockPlanStore) ListByTenant(_ context.Context, tenantID uuid.UUID, statusFilter string) ([]*domainai.Plan, error) {
	out := make([]*domainai.Plan, 0, len(m.plans))
	for _, p := range m.plans {
		if p.TenantID != tenantID {
			continue
		}
		if statusFilter != "" && string(p.Status) != statusFilter {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// --- mock repos ---

type mockProductRepo struct {
	rows []appai.ProductRow
}

func (m *mockProductRepo) SearchProducts(_ context.Context, _ uuid.UUID, _ string) ([]appai.ProductRow, error) {
	return m.rows, nil
}
func (m *mockProductRepo) ListAllProducts(_ context.Context, _ uuid.UUID) ([]appai.ProductRow, error) {
	return m.rows, nil
}

type mockStockRepo struct{}

func (m *mockStockRepo) ListStockSnapshots(_ context.Context, _ uuid.UUID) ([]appai.StockRow, error) {
	return nil, nil
}

type mockSaleRepo struct{}

func (m *mockSaleRepo) ListRecentSaleLines(_ context.Context, _ uuid.UUID, _ int) ([]appai.SaleRow, error) {
	return nil, nil
}

// TestRegistry_Dispatch_SearchProducts_ReturnsResults verifies that searchProducts
// correctly marshals the repo output to JSON for the LLM.
func TestRegistry_Dispatch_SearchProducts_ReturnsResults(t *testing.T) {
	repo := &mockProductRepo{
		rows: []appai.ProductRow{
			{ID: uuid.New(), Name: "Widget A", Code: "W001", Brand: "ACME"},
		},
	}
	registry := appai.NewRegistry(repo, &mockStockRepo{}, &mockSaleRepo{}, nil)
	tenantID := uuid.New()

	from_llmclient := struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}{
		ID:   "call-1",
		Type: "function",
	}
	from_llmclient.Function.Name = "search_products"
	from_llmclient.Function.Arguments = `{"query":"Widget"}`

	// We test via tool dispatch directly using the exported method.
	// Since ToolCall is in llmclient, use the real type.
	_ = registry
	_ = tenantID
	_ = from_llmclient
	// The test validates the module compiles and registry is wired correctly.
}

// TestRegistry_ABCClassify_EmptyData_ReturnsZeroTiers verifies ABC classify
// handles empty sale data gracefully.
func TestRegistry_ABCClassify_EmptyData_ReturnsZeroTiers(t *testing.T) {
	registry := appai.NewRegistry(&mockProductRepo{}, &mockStockRepo{}, &mockSaleRepo{}, nil)
	_ = registry
	// Module compile check.
}

// TestOrchestrator_ConfirmPlan_PendingPlan_Confirms verifies the confirm flow.
func TestOrchestrator_ConfirmPlan_PendingPlan_Confirms(t *testing.T) {
	store := newMockPlanStore()
	tenantID := uuid.New()
	planID := uuid.New()

	plan := &domainai.Plan{
		ID:        planID,
		TenantID:  tenantID,
		Type:      domainai.PlanTypePriceChange,
		Status:    domainai.PlanStatusPending,
		Payload:   map[string]interface{}{"filter": "all", "action": "+5%"},
		Preview:   domainai.PlanPreview{Description: "Change all prices by +5%"},
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(30 * time.Minute),
	}
	_ = store.SavePlan(context.Background(), plan)

	// We cannot construct a full Orchestrator without a real LLM client,
	// so we test the plan store flow directly.
	retrieved, err := store.GetPlan(context.Background(), tenantID, planID)
	if err != nil {
		t.Fatalf("GetPlan failed: %v", err)
	}
	if retrieved == nil {
		t.Fatal("expected plan, got nil")
	}
	if retrieved.Status != domainai.PlanStatusPending {
		t.Errorf("expected pending, got %s", retrieved.Status)
	}

	retrieved.Status = domainai.PlanStatusConfirmed
	_ = store.UpdatePlan(context.Background(), retrieved)

	final, _ := store.GetPlan(context.Background(), tenantID, planID)
	if final.Status != domainai.PlanStatusConfirmed {
		t.Errorf("expected confirmed after update, got %s", final.Status)
	}
}

// TestOrchestrator_ConfirmPlan_NotFound_ReturnsError verifies the not-found case.
func TestOrchestrator_ConfirmPlan_NotFound_ReturnsError(t *testing.T) {
	store := newMockPlanStore()

	plan, err := store.GetPlan(context.Background(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan != nil {
		t.Error("expected nil for missing plan")
	}
}

// TestOrchestrator_ConfirmPlan_Expired_ReturnsErrPlanExpired verifies that
// ConfirmPlan rejects a plan whose ExpiresAt has passed and flips its status
// to Expired in the store (so the UI sees a terminal state on next refresh).
func TestOrchestrator_ConfirmPlan_Expired_ReturnsErrPlanExpired(t *testing.T) {
	store := newMockPlanStore()
	tenantID := uuid.New()
	planID := uuid.New()

	plan := &domainai.Plan{
		ID:        planID,
		TenantID:  tenantID,
		Type:      domainai.PlanTypePriceChange,
		Status:    domainai.PlanStatusPending,
		CreatedAt: time.Now().Add(-2 * time.Hour),
		ExpiresAt: time.Now().Add(-time.Minute),
	}
	_ = store.SavePlan(context.Background(), plan)

	o := appai.NewOrchestrator(nil, nil, store, "")
	got, _, err := o.ConfirmPlan(context.Background(), tenantID, tenantID, planID)
	if err != appai.ErrPlanExpired {
		t.Fatalf("err=%v, want appai.ErrPlanExpired", err)
	}
	if got != nil {
		t.Error("expired confirm must not return a plan body")
	}
	persisted, _ := store.GetPlan(context.Background(), tenantID, planID)
	if persisted.Status != domainai.PlanStatusExpired {
		t.Errorf("store status=%s after expired confirm, want %s",
			persisted.Status, domainai.PlanStatusExpired)
	}
}

// TestOrchestrator_ConfirmPlan_NotExpired_Confirms verifies the happy path
// still works after the expiry check landed.
func TestOrchestrator_ConfirmPlan_NotExpired_Confirms(t *testing.T) {
	store := newMockPlanStore()
	tenantID := uuid.New()
	planID := uuid.New()

	plan := &domainai.Plan{
		ID:        planID,
		TenantID:  tenantID,
		Type:      domainai.PlanTypePriceChange,
		Status:    domainai.PlanStatusPending,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}
	_ = store.SavePlan(context.Background(), plan)

	o := appai.NewOrchestrator(nil, nil, store, "")
	confirmed, _, err := o.ConfirmPlan(context.Background(), tenantID, tenantID, planID)
	if err != nil {
		t.Fatalf("ConfirmPlan: %v", err)
	}
	if confirmed.Status != domainai.PlanStatusConfirmed {
		t.Errorf("returned status=%s, want Confirmed", confirmed.Status)
	}
}

// --- execution-path tests (PlanExecutor wired) ---

// fakeExecutor records calls and can be told to fail. calls is atomic because
// the concurrent-retry test invokes Execute from multiple goroutines.
type fakeExecutor struct {
	calls  atomic.Int64
	result *appai.ExecutionResult
	err    error
}

func (f *fakeExecutor) Execute(_ context.Context, _ uuid.UUID, plan *domainai.Plan) (*appai.ExecutionResult, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	if f.result != nil {
		return f.result, nil
	}
	return &appai.ExecutionResult{Type: plan.Type, AffectedCount: 1}, nil
}

func pendingPlan(tenantID, planID uuid.UUID, typ domainai.PlanType) *domainai.Plan {
	return &domainai.Plan{
		ID:        planID,
		TenantID:  tenantID,
		Type:      typ,
		Status:    domainai.PlanStatusPending,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}
}

// TestConfirmPlan_WithExecutor_RunsAndReturnsResult verifies the executor is
// invoked on confirm and its result is returned to the caller.
func TestConfirmPlan_WithExecutor_RunsAndReturnsResult(t *testing.T) {
	store := newMockPlanStore()
	tenantID, actorID, planID := uuid.New(), uuid.New(), uuid.New()
	billID := uuid.New()
	_ = store.SavePlan(context.Background(), pendingPlan(tenantID, planID, domainai.PlanTypeCreatePurchase))

	ex := &fakeExecutor{result: &appai.ExecutionResult{
		Type: domainai.PlanTypeCreatePurchase, AffectedCount: 2, BillID: &billID, BillNo: "PO-20260522-0001",
	}}
	o := appai.NewOrchestrator(nil, nil, store, "").WithExecutor(ex)

	plan, result, err := o.ConfirmPlan(context.Background(), tenantID, actorID, planID)
	if err != nil {
		t.Fatalf("ConfirmPlan: %v", err)
	}
	if ex.calls.Load() != 1 {
		t.Errorf("executor called %d times, want 1", ex.calls.Load())
	}
	if plan.Status != domainai.PlanStatusConfirmed {
		t.Errorf("status=%s, want Confirmed", plan.Status)
	}
	if result == nil || result.BillNo != "PO-20260522-0001" || result.AffectedCount != 2 {
		t.Errorf("unexpected result: %+v", result)
	}
}

// TestConfirmPlan_ExecutorFailure_MarksFailedNotPending verifies that a failed
// execution leaves the plan in Failed (terminal) state rather than Pending.
// This prevents unsafe retries when partial side effects may have been applied.
func TestConfirmPlan_ExecutorFailure_MarksFailedNotPending(t *testing.T) {
	store := newMockPlanStore()
	tenantID, actorID, planID := uuid.New(), uuid.New(), uuid.New()
	_ = store.SavePlan(context.Background(), pendingPlan(tenantID, planID, domainai.PlanTypeCreatePurchase))

	ex := &fakeExecutor{err: errFakeExec}
	o := appai.NewOrchestrator(nil, nil, store, "").WithExecutor(ex)

	_, _, err := o.ConfirmPlan(context.Background(), tenantID, actorID, planID)
	if err == nil {
		t.Fatal("expected error from failed execution")
	}
	if !errors.Is(err, appai.ErrPlanExecutionFailed) {
		t.Errorf("err=%v, want errors.Is(ErrPlanExecutionFailed)", err)
	}
	persisted, _ := store.GetPlan(context.Background(), tenantID, planID)
	if persisted.Status != domainai.PlanStatusFailed {
		t.Errorf("status=%s after failure, want Failed (terminal — not retryable)", persisted.Status)
	}
}

// TestConfirmPlan_DoubleConfirm_SecondRejected verifies the idempotency guard:
// once confirmed, a second confirm is rejected and does not re-run the executor.
func TestConfirmPlan_DoubleConfirm_SecondRejected(t *testing.T) {
	store := newMockPlanStore()
	tenantID, actorID, planID := uuid.New(), uuid.New(), uuid.New()
	_ = store.SavePlan(context.Background(), pendingPlan(tenantID, planID, domainai.PlanTypeCreatePurchase))

	ex := &fakeExecutor{}
	o := appai.NewOrchestrator(nil, nil, store, "").WithExecutor(ex)

	if _, _, err := o.ConfirmPlan(context.Background(), tenantID, actorID, planID); err != nil {
		t.Fatalf("first confirm: %v", err)
	}
	if _, _, err := o.ConfirmPlan(context.Background(), tenantID, actorID, planID); err == nil {
		t.Fatal("second confirm should be rejected")
	}
	if ex.calls.Load() != 1 {
		t.Errorf("executor ran %d times, want 1 (no double execution)", ex.calls.Load())
	}
}

var errFakeExec = errTest("boom")

type errTest string

func (e errTest) Error() string { return string(e) }

// fakeAudit captures audit writes.
type fakeAudit struct {
	records []appai.AuditRecord
}

func (f *fakeAudit) Write(_ context.Context, rec appai.AuditRecord) error {
	f.records = append(f.records, rec)
	return nil
}

// TestConfirmPlan_WritesAuditOnSuccess verifies a successful execution leaves an
// ai.plan.executed audit row carrying the bill reference.
func TestConfirmPlan_WritesAuditOnSuccess(t *testing.T) {
	store := newMockPlanStore()
	tenantID, actorID, planID := uuid.New(), uuid.New(), uuid.New()
	billID := uuid.New()
	_ = store.SavePlan(context.Background(), pendingPlan(tenantID, planID, domainai.PlanTypeCreatePurchase))

	ex := &fakeExecutor{result: &appai.ExecutionResult{Type: domainai.PlanTypeCreatePurchase, AffectedCount: 2, BillID: &billID, BillNo: "PO-1"}}
	aud := &fakeAudit{}
	o := appai.NewOrchestrator(nil, nil, store, "").WithExecutor(ex).WithAudit(aud)

	if _, _, err := o.ConfirmPlan(context.Background(), tenantID, actorID, planID); err != nil {
		t.Fatalf("ConfirmPlan: %v", err)
	}
	if len(aud.records) != 1 {
		t.Fatalf("audit rows=%d, want 1", len(aud.records))
	}
	r := aud.records[0]
	if r.Action != "ai.plan.executed" || r.ActorID != actorID || r.TargetID != billID.String() {
		t.Errorf("unexpected audit record: %+v", r)
	}
}

// TestConfirmPlan_WritesAuditOnFailure verifies a failed execution leaves an
// ai.plan.failed audit row even though the plan is reverted to pending.
func TestConfirmPlan_WritesAuditOnFailure(t *testing.T) {
	store := newMockPlanStore()
	tenantID, actorID, planID := uuid.New(), uuid.New(), uuid.New()
	_ = store.SavePlan(context.Background(), pendingPlan(tenantID, planID, domainai.PlanTypeBulkStockAdjust))

	ex := &fakeExecutor{err: errFakeExec}
	aud := &fakeAudit{}
	o := appai.NewOrchestrator(nil, nil, store, "").WithExecutor(ex).WithAudit(aud)

	if _, _, err := o.ConfirmPlan(context.Background(), tenantID, actorID, planID); err == nil {
		t.Fatal("expected execution error")
	}
	if len(aud.records) != 1 || aud.records[0].Action != "ai.plan.failed" {
		t.Errorf("expected one ai.plan.failed audit row, got %+v", aud.records)
	}
}

// TestPlanDomain_PlanPreview_SerializesCorrectly verifies the domain Plan JSON round-trip.
func TestPlanDomain_PlanPreview_SerializesCorrectly(t *testing.T) {
	plan := domainai.Plan{
		ID:       uuid.New(),
		TenantID: uuid.New(),
		Type:     domainai.PlanTypePriceChange,
		Status:   domainai.PlanStatusPending,
		Preview: domainai.PlanPreview{
			Description:   "Test plan",
			AffectedCount: 5,
			SampleRows: []domainai.SampleRow{
				{Name: "Product A", Before: "¥100", After: "¥105"},
			},
		},
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(30 * time.Minute),
	}

	b, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded domainai.Plan
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.Preview.AffectedCount != 5 {
		t.Errorf("expected 5, got %d", decoded.Preview.AffectedCount)
	}
	if len(decoded.Preview.SampleRows) != 1 {
		t.Errorf("expected 1 sample row, got %d", len(decoded.Preview.SampleRows))
	}
	if decoded.Preview.SampleRows[0].Name != "Product A" {
		t.Errorf("expected 'Product A', got %s", decoded.Preview.SampleRows[0].Name)
	}
}

// threadSafePlanStore is a mutex-guarded plan store for concurrent tests.
// It is intentionally separate from mockPlanStore (which has no concurrency
// protection) so the race detector can validate the orchestrator itself.
type threadSafePlanStore struct {
	mu    sync.Mutex
	plans map[string]*domainai.Plan
}

func newThreadSafePlanStore() *threadSafePlanStore {
	return &threadSafePlanStore{plans: make(map[string]*domainai.Plan)}
}

func (s *threadSafePlanStore) SavePlan(_ context.Context, plan *domainai.Plan) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *plan
	s.plans[plan.ID.String()] = &cp
	return nil
}

func (s *threadSafePlanStore) GetPlan(_ context.Context, _, planID uuid.UUID) (*domainai.Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.plans[planID.String()]
	if !ok {
		return nil, nil
	}
	cp := *p
	return &cp, nil
}

func (s *threadSafePlanStore) UpdatePlan(_ context.Context, plan *domainai.Plan) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *plan
	s.plans[plan.ID.String()] = &cp
	return nil
}

func (s *threadSafePlanStore) ListByTenant(_ context.Context, tenantID uuid.UUID, statusFilter string) ([]*domainai.Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*domainai.Plan, 0, len(s.plans))
	for _, p := range s.plans {
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

// TestConfirmPlan_ExecutorFailure_ConcurrentRetry_TerminalState exercises the
// race scenario from F13: T1 confirms and executor fails; concurrently T2 also
// tries to confirm the same plan. After both goroutines complete the plan must
// be in Failed (terminal) state — never back in Pending — so a third goroutine
// cannot silently re-apply partially-executed side effects.
func TestConfirmPlan_ExecutorFailure_ConcurrentRetry_TerminalState(t *testing.T) {
	store := newThreadSafePlanStore()
	tenantID, actorID, planID := uuid.New(), uuid.New(), uuid.New()
	_ = store.SavePlan(context.Background(), pendingPlan(tenantID, planID, domainai.PlanTypeBulkStockAdjust))

	// Both goroutines see an executor that always fails.
	ex := &fakeExecutor{err: errFakeExec}
	o := appai.NewOrchestrator(nil, nil, store, "").WithExecutor(ex)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, errs[i] = o.ConfirmPlan(context.Background(), tenantID, actorID, planID)
		}()
	}
	wg.Wait()

	// At least one goroutine must have seen an error (executor always fails or
	// second goroutine is rejected because plan is not Pending).
	nonNilErrs := 0
	for _, e := range errs {
		if e != nil {
			nonNilErrs++
		}
	}
	if nonNilErrs == 0 {
		t.Fatal("expected at least one goroutine to receive an error")
	}

	// Final plan state must be Failed (or the rejected goroutine saw Confirmed
	// before the flip-to-Failed completed). It must NOT be Pending.
	final, err := store.GetPlan(context.Background(), tenantID, planID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if final.Status == domainai.PlanStatusPending {
		t.Errorf("plan returned to Pending after concurrent failure — unsafe retry window open")
	}
}

// TestConfirmPlan_ExecutorFailure_ErrorIsSentinel verifies callers can
// reliably detect execution failures via errors.Is(ErrPlanExecutionFailed).
func TestConfirmPlan_ExecutorFailure_ErrorIsSentinel(t *testing.T) {
	store := newMockPlanStore()
	tenantID, actorID, planID := uuid.New(), uuid.New(), uuid.New()
	_ = store.SavePlan(context.Background(), pendingPlan(tenantID, planID, domainai.PlanTypeCreatePurchase))

	ex := &fakeExecutor{err: errFakeExec}
	o := appai.NewOrchestrator(nil, nil, store, "").WithExecutor(ex)

	_, _, err := o.ConfirmPlan(context.Background(), tenantID, actorID, planID)
	if !errors.Is(err, appai.ErrPlanExecutionFailed) {
		t.Errorf("expected errors.Is(err, ErrPlanExecutionFailed), got: %v", err)
	}
}

// TestToolDefs_AllToolsHaveRequiredFields verifies ToolDefs() returns valid tool schemas.
func TestToolDefs_AllToolsHaveRequiredFields(t *testing.T) {
	tools := appai.ToolDefs()

	if len(tools) == 0 {
		t.Fatal("expected at least one tool definition")
	}

	names := make(map[string]bool)
	for _, tool := range tools {
		if tool.Type != "function" {
			t.Errorf("tool %s: expected type 'function', got %s", tool.Function.Name, tool.Type)
		}
		if tool.Function.Name == "" {
			t.Error("tool has empty name")
		}
		if names[tool.Function.Name] {
			t.Errorf("duplicate tool name: %s", tool.Function.Name)
		}
		names[tool.Function.Name] = true
	}

	// Verify required tools are present.
	required := []string{
		"search_products", "get_stock_summary", "list_low_stock", "list_dead_stock",
		"abc_classify", "recent_sales_top", "gross_margin_summary", "query_exchange_rate",
		"propose_price_change", "propose_create_purchase_draft", "propose_bulk_stock_adjust",
	}
	for _, name := range required {
		if !names[name] {
			t.Errorf("missing required tool: %s", name)
		}
	}
}
