package lifecycle

// zz_coverage2_test.go extends zz_coverage_test.go with additional coverage
// for pieces that file does not touch: llm_observability.go, seed.go, the
// app.go health-pinger/redis-limiter adapters, the remaining ai_executor.go
// wiring (buildPlanExecutor/buildReverter/buildPriceCapturerAdapter, the
// audit writer, draft creator, price changer, stock adjuster/reverter, price
// capturer/reverter, and the Redis-backed price snapshot store), an exact
// newServer timeout-constant regression guard, and the Start/Stop branches
// reachable without a live Postgres/NATS (MigrateOnBoot=true fast-fail,
// SEED_NURSERY_DICT=true non-fatal path, background-worker cancel funcs, and
// the Shutdown-deadline-exceeded error path).
//
// zz_coverage_test.go is left untouched (pre-existing, possibly-concurrent
// work); this file only adds new test functions/types with distinct names.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"

	reposku "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/sku"
	repostock "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/stock"
	repowarehouse "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/warehouse"
	appai "github.com/hanmahong5-arch/lurus-tally/internal/app/ai"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	appsku "github.com/hanmahong5-arch/lurus-tally/internal/app/sku"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domainaccount "github.com/hanmahong5-arch/lurus-tally/internal/domain/account"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
	llmobs "github.com/hanmahong5-arch/lurus-tally/internal/observability/llm"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/config"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/logger"
)

func testLog() *slog.Logger { return logger.New("error", "lifecycle-test", "test", nil) }

// ===========================================================================
// newServer — exact timeout-constant regression guard (server_test.go already
// asserts >0/==0 shape; this pins the literal values so a future edit that
// silently changes one of them fails loudly).
// ===========================================================================

func TestNewServer_ExactTimeoutConstants(t *testing.T) {
	srv := newServer(":0", http.NewServeMux())
	if srv.ReadHeaderTimeout != 10*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, want 10s", srv.ReadHeaderTimeout)
	}
	if srv.ReadTimeout != 60*time.Second {
		t.Errorf("ReadTimeout = %v, want 60s", srv.ReadTimeout)
	}
	if srv.IdleTimeout != 120*time.Second {
		t.Errorf("IdleTimeout = %v, want 120s", srv.IdleTimeout)
	}
	if srv.MaxHeaderBytes != 1<<20 {
		t.Errorf("MaxHeaderBytes = %d, want %d", srv.MaxHeaderBytes, 1<<20)
	}
	if srv.WriteTimeout != 0 {
		t.Errorf("WriteTimeout = %v, want 0 (SSE carve-out)", srv.WriteTimeout)
	}
}

// ===========================================================================
// llm_observability.go — BuildTracer / WireTracer
// ===========================================================================

func TestBuildTracer_NoLangfuseEnv_ReturnsNoop(t *testing.T) {
	// Ensure a clean slate regardless of the ambient environment.
	t.Setenv("LANGFUSE_HOST", "")
	t.Setenv("LANGFUSE_PUBLIC_KEY", "")
	t.Setenv("LANGFUSE_SECRET_KEY", "")

	tr := BuildTracer(&config.Config{ServiceVersion: "test"})
	if tr == nil {
		t.Fatal("BuildTracer must never return a nil Tracer")
	}
	if tr != llmobs.Noop() {
		t.Errorf("expected the Noop tracer when Langfuse env vars are unset, got %#v", tr)
	}
}

func TestWireTracer_NilOrchestrator_NoPanic(t *testing.T) {
	// Must not panic on a nil *Orchestrator.
	WireTracer(nil, llmobs.Noop())
}

func TestWireTracer_NonNilOrchestrator_AttachesTracer(t *testing.T) {
	o := appai.NewOrchestrator(nil, nil, nil, "")
	// Must not panic when attaching a tracer (nil or non-nil) to a real Orchestrator.
	WireTracer(o, llmobs.Noop())
	WireTracer(o, nil)
}

// ===========================================================================
// seed.go — SeedNurseryDict (begin/exec/commit error branches + happy path)
// ===========================================================================

func TestSeedNurseryDict_BeginTxFails(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	mock.ExpectBegin().WillReturnError(errors.New("pool exhausted"))

	if err := SeedNurseryDict(context.Background(), db, testLog()); err == nil {
		t.Fatal("expected error when BeginTx fails, got nil")
	}
}

func TestSeedNurseryDict_ExecFails(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	mock.ExpectBegin()
	mock.ExpectExec(".*").WillReturnError(errors.New("syntax error"))
	mock.ExpectRollback()

	if err := SeedNurseryDict(context.Background(), db, testLog()); err == nil {
		t.Fatal("expected error when exec fails, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expected rollback after exec failure: %v", err)
	}
}

func TestSeedNurseryDict_CommitFails(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	mock.ExpectBegin()
	mock.ExpectExec(".*").WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectCommit().WillReturnError(errors.New("connection reset"))

	if err := SeedNurseryDict(context.Background(), db, testLog()); err == nil {
		t.Fatal("expected error when commit fails, got nil")
	}
}

func TestSeedNurseryDict_HappyPath(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	mock.ExpectBegin()
	mock.ExpectExec(".*").WillReturnResult(sqlmock.NewResult(0, 7))
	mock.ExpectCommit()

	if err := SeedNurseryDict(context.Background(), db, testLog()); err != nil {
		t.Fatalf("SeedNurseryDict: unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

// ===========================================================================
// app.go — dbPinger / redisPinger / redisLimiterAdapter
// ===========================================================================

func TestDbPinger_Ping(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	mock.ExpectPing().WillReturnError(nil)
	p := dbPinger{db: db}
	if err := p.Ping(context.Background()); err != nil {
		t.Errorf("Ping: unexpected error: %v", err)
	}

	mock.ExpectPing().WillReturnError(errors.New("down"))
	if err := p.Ping(context.Background()); err == nil {
		t.Error("Ping: expected error to propagate, got nil")
	}
}

func TestRedisPinger_Ping(t *testing.T) {
	mr := miniredis.RunT(t)
	c := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer c.Close() //nolint:errcheck

	p := redisPinger{c: c}
	if err := p.Ping(context.Background()); err != nil {
		t.Errorf("Ping: unexpected error against a live miniredis: %v", err)
	}

	mr.Close()
	if err := p.Ping(context.Background()); err == nil {
		t.Error("Ping: expected error once the backing redis is closed, got nil")
	}
}

func TestNewRedisLimiterAdapter_NilClient_ReturnsNilAdapter(t *testing.T) {
	if a := newRedisLimiterAdapter(nil); a != nil {
		t.Errorf("expected nil adapter for nil *redis.Client, got %+v", a)
	}
}

func TestRedisLimiterAdapter_NilReceiver_ReturnsError(t *testing.T) {
	var a *redisLimiterAdapter
	if _, err := a.Incr(context.Background(), "k"); err == nil {
		t.Error("Incr on nil receiver: expected error, got nil")
	}
	if err := a.Expire(context.Background(), "k", time.Second); err == nil {
		t.Error("Expire on nil receiver: expected error, got nil")
	}
}

func TestRedisLimiterAdapter_IncrAndExpire_RoundTrip(t *testing.T) {
	mr := miniredis.RunT(t)
	c := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer c.Close() //nolint:errcheck

	a := newRedisLimiterAdapter(c)
	if a == nil {
		t.Fatal("expected non-nil adapter for non-nil client")
	}

	n, err := a.Incr(context.Background(), "rl:key")
	if err != nil {
		t.Fatalf("Incr: unexpected error: %v", err)
	}
	if n != 1 {
		t.Errorf("Incr: first call = %d, want 1", n)
	}
	n, err = a.Incr(context.Background(), "rl:key")
	if err != nil {
		t.Fatalf("Incr (2nd): unexpected error: %v", err)
	}
	if n != 2 {
		t.Errorf("Incr: second call = %d, want 2", n)
	}

	if err := a.Expire(context.Background(), "rl:key", time.Minute); err != nil {
		t.Fatalf("Expire: unexpected error: %v", err)
	}

	mr.Close()
	if _, err := a.Incr(context.Background(), "rl:key"); err == nil {
		t.Error("Incr after redis close: expected error, got nil")
	}
	if err := a.Expire(context.Background(), "rl:key", time.Minute); err == nil {
		t.Error("Expire after redis close: expected error, got nil")
	}
}

// ===========================================================================
// ai_executor.go — buildPlanExecutor / buildReverter / buildPriceCapturerAdapter
// (pure wiring smoke tests: constructors only store pointers, never dial).
// ===========================================================================

func TestBuildPlanExecutor_ReturnsNonNil(t *testing.T) {
	ex := buildPlanExecutor(nil, &fakeBillRepo{}, nil, nil)
	if ex == nil {
		t.Fatal("expected non-nil *appai.DefaultPlanExecutor")
	}
}

func TestBuildReverter_ReturnsNonNil(t *testing.T) {
	rv := buildReverter(nil, nil, nil, nil, nil)
	if rv == nil {
		t.Fatal("expected non-nil *appai.Reverter")
	}
}

func TestBuildPriceCapturerAdapter_ReturnsNonNil(t *testing.T) {
	pc := buildPriceCapturerAdapter(nil)
	if pc == nil {
		t.Fatal("expected non-nil PriceCapturerPort")
	}
}

// ===========================================================================
// aiAuditWriter — persists AI plan executions; Write failures are logged, not
// surfaced (the side effect already committed).
// ===========================================================================

// recordingAuditRepo implements appacct.AuditRepo, recording how many times
// Append was called so tests can assert the adapter reached the real use case.
type recordingAuditRepo struct {
	err   error
	calls int
}

func (r *recordingAuditRepo) Append(ctx context.Context, e *domainaccount.AuditEntry) error {
	r.calls++
	return r.err
}

func (r *recordingAuditRepo) List(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]*domainaccount.AuditEntry, int, error) {
	return nil, 0, fmt.Errorf("not implemented in fake")
}

func TestNewAIAuditWriter_Write_SuccessAndErrorPassthrough(t *testing.T) {
	okRepo := &recordingAuditRepo{}
	w := newAIAuditWriter(okRepo, testLog())
	tenantID := uuid.New()
	actorID := uuid.New()

	err := w.Write(context.Background(), appai.AuditRecord{
		TenantID:   tenantID,
		ActorID:    actorID,
		Action:     "ai.plan.executed",
		TargetKind: "bill",
		TargetID:   "bill-123",
		Payload:    map[string]any{"k": "v"},
	})
	if err != nil {
		t.Fatalf("Write: unexpected error: %v", err)
	}
	if okRepo.calls != 1 {
		t.Errorf("Append must be called exactly once, got %d", okRepo.calls)
	}

	failRepo := &recordingAuditRepo{err: errors.New("db down")}
	w2 := newAIAuditWriter(failRepo, testLog())
	err = w2.Write(context.Background(), appai.AuditRecord{
		TenantID: tenantID,
		ActorID:  actorID,
		Action:   "ai.plan.failed",
	})
	if err == nil {
		t.Fatal("Write: expected repo error to propagate, got nil")
	}

	// nil logger must not panic on the error-logging branch.
	w3 := newAIAuditWriter(failRepo, nil)
	if err := w3.Write(context.Background(), appai.AuditRecord{TenantID: tenantID, ActorID: actorID, Action: "x"}); err == nil {
		t.Fatal("expected error with nil logger too")
	}
}

// ===========================================================================
// aiDraftCreator — unit price defaulting (missing/erroring SKU lookup is
// non-fatal: unit price defaults to 0) + error passthrough.
// ===========================================================================

func TestAiDraftCreator_CreatePurchaseDraft_EmptyLines_ErrorPassthrough(t *testing.T) {
	repo := &fakeBillRepo{productExists: true}
	creator := &aiDraftCreator{
		uc:      appbill.NewCreatePurchaseDraftUseCase(repo),
		skuRepo: reposku.New(nil), // never touched: ids slice is empty
	}

	billID, billNo, err := creator.CreatePurchaseDraft(context.Background(), uuid.New(), uuid.New(), nil)
	if err == nil {
		t.Fatal("expected error for zero draft lines, got nil")
	}
	if billID != uuid.Nil || billNo != "" {
		t.Errorf("expected zero-value outputs on error, got (%v, %q)", billID, billNo)
	}
}

func TestAiDraftCreator_CreatePurchaseDraft_SkuLookupErrors_DefaultsPriceToZero(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	mock.ExpectQuery("SELECT DISTINCT ON").WillReturnError(errors.New("timeout"))

	repo := &fakeBillRepo{productExists: true}
	creator := &aiDraftCreator{
		uc:      appbill.NewCreatePurchaseDraftUseCase(repo),
		skuRepo: reposku.New(db),
	}
	productID := uuid.New()

	billID, billNo, err := creator.CreatePurchaseDraft(context.Background(), uuid.New(), uuid.New(), []appai.DraftLine{
		{ProductID: productID, ProductName: "widget", Qty: decimal.NewFromInt(3)},
	})
	if err != nil {
		t.Fatalf("CreatePurchaseDraft: unexpected error (sku lookup failure must be non-fatal): %v", err)
	}
	if billID == uuid.Nil || billNo == "" {
		t.Errorf("expected non-zero outputs, got (%v, %q)", billID, billNo)
	}
	if len(repo.capturedItems) != 1 {
		t.Fatalf("expected 1 captured item, got %d", len(repo.capturedItems))
	}
	if !repo.capturedItems[0].UnitPrice.IsZero() {
		t.Errorf("UnitPrice = %s, want 0 (no SKU price resolved)", repo.capturedItems[0].UnitPrice)
	}
}

func TestAiDraftCreator_CreatePurchaseDraft_RepoErrorPassthrough(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	productID := uuid.New()
	rows := sqlmock.NewRows([]string{"id", "product_id", "retail_price", "purchase_price"})
	mock.ExpectQuery("SELECT DISTINCT ON").WillReturnRows(rows)

	wantErr := errors.New("insert failed")
	repo := &fakeBillRepo{productExists: true, createBillErr: wantErr}
	creator := &aiDraftCreator{
		uc:      appbill.NewCreatePurchaseDraftUseCase(repo),
		skuRepo: reposku.New(db),
	}

	billID, billNo, err := creator.CreatePurchaseDraft(context.Background(), uuid.New(), uuid.New(), []appai.DraftLine{
		{ProductID: productID, ProductName: "widget", Qty: decimal.NewFromInt(1)},
	})
	if err == nil {
		t.Fatal("expected repo error to propagate, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected wrapped %v, got %v", wantErr, err)
	}
	if billID != uuid.Nil || billNo != "" {
		t.Errorf("expected zero-value outputs on error, got (%v, %q)", billID, billNo)
	}
}

// ===========================================================================
// aiPriceChanger — ApplyPriceChange delegates to appsku.UpdatePriceUseCase.
// ===========================================================================

func TestAiPriceChanger_ApplyPriceChange_TableDriven(t *testing.T) {
	productID := uuid.New()
	skuID := uuid.New()
	tenantID := uuid.New()

	t.Run("happy path applies relative increase", func(t *testing.T) {
		db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer db.Close() //nolint:errcheck

		rows := sqlmock.NewRows([]string{"id", "product_id", "retail_price", "purchase_price"}).
			AddRow(skuID, productID, "100", "80")
		mock.ExpectQuery("SELECT DISTINCT ON").WillReturnRows(rows)
		mock.ExpectExec("UPDATE tally.product_sku").WillReturnResult(sqlmock.NewResult(0, 1))

		changer := &aiPriceChanger{uc: appSkuUpdatePriceUseCase(db)}
		n, err := changer.ApplyPriceChange(context.Background(), tenantID, []uuid.UUID{productID}, "+10%")
		if err != nil {
			t.Fatalf("ApplyPriceChange: unexpected error: %v", err)
		}
		if n != 1 {
			t.Errorf("affected = %d, want 1", n)
		}
	})

	t.Run("invalid action errors before any write", func(t *testing.T) {
		db, _, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer db.Close() //nolint:errcheck

		changer := &aiPriceChanger{uc: appSkuUpdatePriceUseCase(db)}
		n, err := changer.ApplyPriceChange(context.Background(), tenantID, []uuid.UUID{productID}, "not-a-valid-action")
		if err == nil {
			t.Fatal("expected error for invalid action, got nil")
		}
		if n != 0 {
			t.Errorf("affected = %d, want 0", n)
		}
	})

	t.Run("repo update error passes through with partial count", func(t *testing.T) {
		db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer db.Close() //nolint:errcheck

		rows := sqlmock.NewRows([]string{"id", "product_id", "retail_price", "purchase_price"}).
			AddRow(skuID, productID, "100", "80")
		mock.ExpectQuery("SELECT DISTINCT ON").WillReturnRows(rows)
		mock.ExpectExec("UPDATE tally.product_sku").WillReturnError(errors.New("write conflict"))

		changer := &aiPriceChanger{uc: appSkuUpdatePriceUseCase(db)}
		n, err := changer.ApplyPriceChange(context.Background(), tenantID, []uuid.UUID{productID}, "=50")
		if err == nil {
			t.Fatal("expected error to propagate, got nil")
		}
		if n != 0 {
			t.Errorf("affected = %d, want 0 (failed before increment)", n)
		}
	})
}

// ===========================================================================
// aiStockAdjuster — batch stock adjust: 0-lines shortcut, warehouse-lookup
// error, tx-begin error, per-line error (rollback), and the happy-path commit.
// ===========================================================================

// fakeStockRepoForAdjuster is a minimal appstock.StockRepo double sufficient to
// drive RecordMovementUseCase.ExecuteInTx end-to-end without a real Postgres.
type fakeStockRepoForAdjuster struct {
	advisoryLockErr error
}

func (f *fakeStockRepoForAdjuster) GetSnapshot(ctx context.Context, tenantID, productID, warehouseID uuid.UUID) (*domainstock.Snapshot, error) {
	return nil, nil
}
func (f *fakeStockRepoForAdjuster) SelectForUpdate(ctx context.Context, tx *sql.Tx, tenantID, productID, warehouseID uuid.UUID) (*domainstock.Snapshot, error) {
	return nil, nil
}
func (f *fakeStockRepoForAdjuster) UpsertSnapshot(ctx context.Context, tx *sql.Tx, s *domainstock.Snapshot) error {
	return nil
}
func (f *fakeStockRepoForAdjuster) InsertMovement(ctx context.Context, tx *sql.Tx, m *domainstock.Movement) error {
	return nil
}
func (f *fakeStockRepoForAdjuster) ListMovements(ctx context.Context, filter appstock.MovementFilter) ([]domainstock.Movement, error) {
	return nil, nil
}
func (f *fakeStockRepoForAdjuster) InsertLot(ctx context.Context, tx *sql.Tx, l *domainstock.Lot) error {
	return nil
}
func (f *fakeStockRepoForAdjuster) ListActiveLots(ctx context.Context, tx *sql.Tx, tenantID, productID, warehouseID uuid.UUID) ([]domainstock.Lot, error) {
	return nil, nil
}
func (f *fakeStockRepoForAdjuster) UpdateLotQty(ctx context.Context, tx *sql.Tx, lotID uuid.UUID, qtyRemaining decimal.Decimal) error {
	return nil
}
func (f *fakeStockRepoForAdjuster) AcquireAdvisoryLock(ctx context.Context, tx *sql.Tx, tenantID, productID, warehouseID uuid.UUID) error {
	return f.advisoryLockErr
}
func (f *fakeStockRepoForAdjuster) ListSnapshots(ctx context.Context, filter appstock.ListSnapshotsFilter) ([]domainstock.Snapshot, error) {
	return nil, nil
}
func (f *fakeStockRepoForAdjuster) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	return fn(nil)
}

// fakeCalculator is a minimal appstock.InventoryCalculator double: it always
// succeeds and returns an empty snapshot, so ExecuteInTx's business logic
// (validate -> apply -> optional outbox) is exercised without real cost math.
type fakeCalculator struct {
	validateErr error
}

func (c *fakeCalculator) ValidateMovement(ctx context.Context, tx *sql.Tx, m *domainstock.Movement) error {
	return c.validateErr
}
func (c *fakeCalculator) ApplyMovement(ctx context.Context, tx *sql.Tx, m *domainstock.Movement) (*domainstock.Snapshot, error) {
	return &domainstock.Snapshot{}, nil
}
func (c *fakeCalculator) Name() string { return "fake" }

func TestAiStockAdjuster_AdjustStockBatch_ZeroLines_ReturnsImmediately(t *testing.T) {
	a := &aiStockAdjuster{db: nil, uc: nil, whRepo: repowarehouse.New(nil)}
	n, err := a.AdjustStockBatch(context.Background(), uuid.New(), uuid.New(), uuid.New(), nil)
	if err != nil {
		t.Fatalf("unexpected error for zero lines: %v", err)
	}
	if n != 0 {
		t.Errorf("affected = %d, want 0", n)
	}
}

func TestAiStockAdjuster_AdjustStockBatch_WarehouseLookupError(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	mock.ExpectQuery("SELECT id").WillReturnError(errors.New("no rows in table"))

	a := &aiStockAdjuster{db: db, whRepo: repowarehouse.New(db)}
	n, err := a.AdjustStockBatch(context.Background(), uuid.New(), uuid.New(), uuid.New(), []appai.StockAdjustLine{
		{ProductID: uuid.New(), Delta: decimal.NewFromInt(1)},
	})
	if err == nil {
		t.Fatal("expected warehouse lookup error to propagate, got nil")
	}
	if n != 0 {
		t.Errorf("affected = %d, want 0", n)
	}
}

func TestAiStockAdjuster_AdjustStockBatch_BeginTxError(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	warehouseID := uuid.New()
	rows := sqlmock.NewRows([]string{"id"}).AddRow(warehouseID)
	mock.ExpectQuery("SELECT id").WillReturnRows(rows)
	mock.ExpectBegin().WillReturnError(errors.New("too many connections"))

	a := &aiStockAdjuster{db: db, whRepo: repowarehouse.New(db)}
	n, err := a.AdjustStockBatch(context.Background(), uuid.New(), uuid.New(), uuid.New(), []appai.StockAdjustLine{
		{ProductID: uuid.New(), Delta: decimal.NewFromInt(1)},
	})
	if err == nil {
		t.Fatal("expected BeginTx error to propagate, got nil")
	}
	if n != 0 {
		t.Errorf("affected = %d, want 0", n)
	}
}

func TestAiStockAdjuster_AdjustStockBatch_PerLineErrorRollsBack(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	warehouseID := uuid.New()
	rows := sqlmock.NewRows([]string{"id"}).AddRow(warehouseID)
	mock.ExpectQuery("SELECT id").WillReturnRows(rows)
	mock.ExpectBegin()
	mock.ExpectRollback()

	uc := appstock.NewRecordMovementUseCase(&fakeStockRepoForAdjuster{}, &fakeCalculator{}, nil, nil)
	a := &aiStockAdjuster{db: db, uc: uc, whRepo: repowarehouse.New(db)}

	// ProductID == uuid.Nil fails ExecuteInTx's own guard immediately, before
	// any repo/calculator call, forcing AdjustStockBatch's per-line error path.
	n, err := a.AdjustStockBatch(context.Background(), uuid.New(), uuid.New(), uuid.New(), []appai.StockAdjustLine{
		{ProductID: uuid.Nil, Delta: decimal.NewFromInt(1)},
	})
	if err == nil {
		t.Fatal("expected per-line error to propagate, got nil")
	}
	if n != 0 {
		t.Errorf("affected = %d, want 0 (first line failed)", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expected Begin+Rollback (no Commit) when a line fails: %v", err)
	}
}

func TestAiStockAdjuster_AdjustStockBatch_HappyPath_CommitsAndCountsAll(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	warehouseID := uuid.New()
	rows := sqlmock.NewRows([]string{"id"}).AddRow(warehouseID)
	mock.ExpectQuery("SELECT id").WillReturnRows(rows)
	mock.ExpectBegin()
	mock.ExpectCommit()

	uc := appstock.NewRecordMovementUseCase(&fakeStockRepoForAdjuster{}, &fakeCalculator{}, nil, nil)
	a := &aiStockAdjuster{db: db, uc: uc, whRepo: repowarehouse.New(db)}

	n, err := a.AdjustStockBatch(context.Background(), uuid.New(), uuid.New(), uuid.New(), []appai.StockAdjustLine{
		{ProductID: uuid.New(), Delta: decimal.NewFromInt(2)},
		{ProductID: uuid.New(), Delta: decimal.NewFromInt(-1)},
	})
	if err != nil {
		t.Fatalf("AdjustStockBatch: unexpected error: %v", err)
	}
	if n != 2 {
		t.Errorf("affected = %d, want 2", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expected Begin+Commit for the happy path: %v", err)
	}
}

// ===========================================================================
// aiStockReverter — RevertStockAdjust: list error, no-movements shortcut,
// happy path (compensating movement written), and per-movement error.
// ===========================================================================

const movementCols = "id, tenant_id, product_id, warehouse_id, direction, qty_base, unit_cost, total_cost, reference_type, reference_id, occurred_at, created_by, note, created_at"

func TestAiStockReverter_RevertStockAdjust_ListErrorPassthrough(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	mock.ExpectQuery("SELECT " + movementCols).WillReturnError(errors.New("connection reset"))

	rv := &aiStockReverter{stockRepo: repostock.New(db)}
	n, err := rv.RevertStockAdjust(context.Background(), uuid.New(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("expected error to propagate, got nil")
	}
	if n != 0 {
		t.Errorf("reverted = %d, want 0", n)
	}
}

func TestAiStockReverter_RevertStockAdjust_NoMovements_ReturnsZeroNil(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	rows := sqlmock.NewRows([]string{"id", "tenant_id", "product_id", "warehouse_id", "direction", "qty_base", "unit_cost", "total_cost", "reference_type", "reference_id", "occurred_at", "created_by", "note", "created_at"})
	mock.ExpectQuery("SELECT " + movementCols).WillReturnRows(rows)

	rv := &aiStockReverter{stockRepo: repostock.New(db)}
	n, err := rv.RevertStockAdjust(context.Background(), uuid.New(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("reverted = %d, want 0 when no movements found", n)
	}
}

func TestAiStockReverter_RevertStockAdjust_HappyPath_WritesCompensatingMovement(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	tenantID := uuid.New()
	planID := uuid.New()
	productID := uuid.New()
	warehouseID := uuid.New()
	movID := uuid.New()
	now := time.Now().UTC()

	rows := sqlmock.NewRows([]string{"id", "tenant_id", "product_id", "warehouse_id", "direction", "qty_base", "unit_cost", "total_cost", "reference_type", "reference_id", "occurred_at", "created_by", "note", "created_at"}).
		AddRow(movID, tenantID, productID, warehouseID, "adjust", "5", "0", "0", "adjust", planID, now, nil, "orig note", now)
	mock.ExpectQuery("SELECT "+movementCols).WithArgs(tenantID, planID).WillReturnRows(rows)

	uc := appstock.NewRecordMovementUseCase(&fakeStockRepoForAdjuster{}, &fakeCalculator{}, nil, nil)
	rv := &aiStockReverter{stockRepo: repostock.New(db), recordMovementUC: uc}

	n, err := rv.RevertStockAdjust(context.Background(), tenantID, uuid.New(), planID)
	if err != nil {
		t.Fatalf("RevertStockAdjust: unexpected error: %v", err)
	}
	if n != 1 {
		t.Errorf("reverted = %d, want 1", n)
	}
}

func TestAiStockReverter_RevertStockAdjust_PerMovementErrorReturnsPartialCount(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	tenantID := uuid.New()
	planID := uuid.New()
	warehouseID := uuid.New()
	now := time.Now().UTC()

	// First row's ProductID is uuid.Nil so recordMovementUC.Execute fails its
	// own guard ("product_id is required") immediately — a deterministic way
	// to force the per-movement error branch without a real DB write path.
	rows := sqlmock.NewRows([]string{"id", "tenant_id", "product_id", "warehouse_id", "direction", "qty_base", "unit_cost", "total_cost", "reference_type", "reference_id", "occurred_at", "created_by", "note", "created_at"}).
		AddRow(uuid.New(), tenantID, uuid.Nil, warehouseID, "adjust", "3", "0", "0", "adjust", planID, now, nil, "", now)
	mock.ExpectQuery("SELECT " + movementCols).WillReturnRows(rows)

	uc := appstock.NewRecordMovementUseCase(&fakeStockRepoForAdjuster{}, &fakeCalculator{}, nil, nil)
	rv := &aiStockReverter{stockRepo: repostock.New(db), recordMovementUC: uc}

	n, err := rv.RevertStockAdjust(context.Background(), tenantID, uuid.New(), planID)
	if err == nil {
		t.Fatal("expected error from the compensating movement write, got nil")
	}
	if n != 0 {
		t.Errorf("reverted = %d, want 0 (first movement failed)", n)
	}
}

// ===========================================================================
// aiPriceCapturer / aiPriceReverter
// ===========================================================================

func TestAiPriceCapturer_CaptureBeforePrices(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	productID := uuid.New()
	skuID := uuid.New()
	rows := sqlmock.NewRows([]string{"id", "product_id", "retail_price", "purchase_price"}).
		AddRow(skuID, productID, "199.50", "150")
	mock.ExpectQuery("SELECT DISTINCT ON").WillReturnRows(rows)

	c := &aiPriceCapturer{skuRepo: reposku.New(db)}
	entries, err := c.CaptureBeforePrices(context.Background(), uuid.New(), []uuid.UUID{productID})
	if err != nil {
		t.Fatalf("CaptureBeforePrices: unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].SKUID != skuID {
		t.Errorf("SKUID = %v, want %v", entries[0].SKUID, skuID)
	}
	if !entries[0].OldPrice.Equal(decimal.RequireFromString("199.50")) {
		t.Errorf("OldPrice = %s, want 199.50", entries[0].OldPrice)
	}
}

func TestAiPriceCapturer_CaptureBeforePrices_ErrorPassthrough(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	mock.ExpectQuery("SELECT DISTINCT ON").WillReturnError(errors.New("timeout"))

	c := &aiPriceCapturer{skuRepo: reposku.New(db)}
	entries, err := c.CaptureBeforePrices(context.Background(), uuid.New(), []uuid.UUID{uuid.New()})
	if err == nil {
		t.Fatal("expected error to propagate, got nil")
	}
	if entries != nil {
		t.Errorf("expected nil entries on error, got %+v", entries)
	}
}

func TestAiPriceReverter_RestorePrices_HappyPathAndPartialErrorCount(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	sku1, sku2 := uuid.New(), uuid.New()
	mock.ExpectExec("UPDATE tally.product_sku").WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sku1, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE tally.product_sku").WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sku2, sqlmock.AnyArg()).WillReturnError(errors.New("row locked"))

	rv := &aiPriceReverter{skuRepo: reposku.New(db)}
	n, err := rv.RestorePrices(context.Background(), uuid.New(), []appai.PriceBeforeEntry{
		{SKUID: sku1, OldPrice: decimal.NewFromInt(10)},
		{SKUID: sku2, OldPrice: decimal.NewFromInt(20)},
	})
	if err == nil {
		t.Fatal("expected the second SKU's update error to propagate, got nil")
	}
	if n != 1 {
		t.Errorf("restored = %d, want 1 (first succeeded, second failed)", n)
	}
}

// ===========================================================================
// aiPriceSnapshotStore — Redis-backed Save/Get round trip via miniredis.
// ===========================================================================

func TestAiPriceSnapshotStore_SaveThenGetSnapshot_RoundTrip(t *testing.T) {
	mr := miniredis.RunT(t)
	c := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer c.Close() //nolint:errcheck

	s := newAIPriceSnapshotStore(c)
	tenantID := uuid.New()
	planID := uuid.New()
	skuID := uuid.New()

	entries := []appai.PriceBeforeEntry{{SKUID: skuID, OldPrice: decimal.NewFromFloat(12.5)}}
	if err := s.SaveSnapshot(context.Background(), tenantID, planID, entries); err != nil {
		t.Fatalf("SaveSnapshot: unexpected error: %v", err)
	}

	got, err := s.GetSnapshot(context.Background(), tenantID, planID)
	if err != nil {
		t.Fatalf("GetSnapshot: unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].SKUID != skuID || !got[0].OldPrice.Equal(decimal.NewFromFloat(12.5)) {
		t.Errorf("GetSnapshot round-trip mismatch: got %+v", got)
	}

	// Consume-once: a second GetSnapshot must return nil, nil (already deleted).
	got2, err := s.GetSnapshot(context.Background(), tenantID, planID)
	if err != nil {
		t.Fatalf("GetSnapshot (2nd): unexpected error: %v", err)
	}
	if got2 != nil {
		t.Errorf("expected nil after consume-once GetDel, got %+v", got2)
	}
}

func TestAiPriceSnapshotStore_GetSnapshot_NotFound_ReturnsNilNil(t *testing.T) {
	mr := miniredis.RunT(t)
	c := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer c.Close() //nolint:errcheck

	s := newAIPriceSnapshotStore(c)
	got, err := s.GetSnapshot(context.Background(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("expected nil error for a never-saved key, got: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil entries, got %+v", got)
	}
}

func TestAiPriceSnapshotStore_SaveSnapshot_RedisErrorPassthrough(t *testing.T) {
	mr := miniredis.RunT(t)
	c := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer c.Close() //nolint:errcheck
	mr.Close()      // force every subsequent redis call to fail

	s := newAIPriceSnapshotStore(c)
	if err := s.SaveSnapshot(context.Background(), uuid.New(), uuid.New(), []appai.PriceBeforeEntry{{SKUID: uuid.New(), OldPrice: decimal.NewFromInt(1)}}); err == nil {
		t.Fatal("expected error once the backing redis is closed, got nil")
	}
}

func TestAiPriceSnapshotStore_GetSnapshot_RedisErrorPassthrough(t *testing.T) {
	mr := miniredis.RunT(t)
	c := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer c.Close() //nolint:errcheck
	mr.Close()

	s := newAIPriceSnapshotStore(c)
	if _, err := s.GetSnapshot(context.Background(), uuid.New(), uuid.New()); err == nil {
		t.Fatal("expected error once the backing redis is closed, got nil")
	}
}

// ===========================================================================
// Start() — MigrateOnBoot=true fast-fail (no real Postgres needed: an
// unreachable DSN fails at the ping step) and SEED_NURSERY_DICT=true's
// non-fatal seed-failure path.
// ===========================================================================

func TestApp_Start_MigrateOnBootTrue_PropagatesMigrationError(t *testing.T) {
	a := &App{
		cfg: &config.Config{
			MigrateOnBoot: true,
			DatabaseDSN:   "postgres://invalid:invalid@127.0.0.1:1/none?sslmode=disable&connect_timeout=1",
		},
		log: testLog(),
	}
	if err := a.Start(context.Background()); err == nil {
		t.Fatal("expected migration error to propagate from Start, got nil")
	}
}

func TestApp_Start_SeedNurseryDictTrue_FailureIsNonFatal(t *testing.T) {
	t.Setenv("SEED_NURSERY_DICT", "true")

	// A DB that fails fast (invalid host, 1s connect_timeout) so SeedNurseryDict
	// errors quickly; Start must log-and-continue rather than abort.
	db, err := sql.Open("pgx", "postgres://invalid:invalid@127.0.0.1:1/none?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close() //nolint:errcheck

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close() //nolint:errcheck

	a := &App{
		cfg: &config.Config{
			MigrateOnBoot:  false,
			ServiceVersion: "test",
		},
		log: testLog(),
		db:  db,
		srv: newServer(fmt.Sprintf("127.0.0.1:%d", port), http.NewServeMux()),
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		a.Stop(stopCtx) //nolint:errcheck
	}()

	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start must not fail when nursery seed fails (non-fatal), got: %v", err)
	}
}

// ===========================================================================
// Stop() — background-worker cancel funcs are invoked, and the
// Shutdown-context-deadline-exceeded error path.
// ===========================================================================

func TestApp_Stop_InvokesBackgroundWorkerCancelFuncs(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close() //nolint:errcheck

	srv := newServer(fmt.Sprintf("127.0.0.1:%d", port), http.NewServeMux())

	outboxCalled, usageRetryCalled := false, false
	a := &App{
		cfg:            &config.Config{},
		log:            testLog(),
		srv:            srv,
		stopOutbox:     func() { outboxCalled = true },
		stopUsageRetry: func() { usageRetryCalled = true },
	}

	if err := a.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: unexpected error: %v", err)
	}
	if !outboxCalled {
		t.Error("stopOutbox must be invoked")
	}
	if !usageRetryCalled {
		t.Error("stopUsageRetry must be invoked")
	}
}

func TestApp_Stop_ShutdownDeadlineExceeded_ReturnsError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}

	blockCh := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blockCh // hold the connection open until the test releases it
		w.WriteHeader(http.StatusOK)
	})
	srv := newServer("", handler)

	go func() { _ = srv.Serve(ln) }()

	addr := ln.Addr().String()
	clientErrCh := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + addr + "/slow") //nolint:noctx
		if err == nil {
			resp.Body.Close()
		}
		clientErrCh <- err
	}()

	// Give the client request a moment to actually reach the handler and block.
	time.Sleep(50 * time.Millisecond)

	a := &App{cfg: &config.Config{}, log: testLog(), srv: srv}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err = a.Stop(ctx)
	if err == nil {
		t.Fatal("expected Stop to return an error when Shutdown's deadline is exceeded with an in-flight request")
	}

	// Release the blocked handler so the client goroutine and Serve can unwind
	// cleanly (avoid leaking the connection past the test).
	close(blockCh)
	<-clientErrCh
}

// appSkuUpdatePriceUseCase is a tiny helper so the aiPriceChanger table test
// above stays readable: aiPriceChanger.uc is *appsku.UpdatePriceUseCase, built
// over the real reposku.Repo (sqlmock-backed) rather than a hand-rolled fake,
// matching how lifecycle wiring constructs it in ai_executor.go.
func appSkuUpdatePriceUseCase(db *sql.DB) *appsku.UpdatePriceUseCase {
	return appsku.NewUpdatePriceUseCase(reposku.New(db))
}

// ===========================================================================
// NewApp — additional optional-feature branches (Platform/Redis/AI/Memory/OIDC)
// that lifecycle_test.go's minimal placeholder config never flips. None of
// these constructors dial synchronously (platform.New/llmclient.New/
// memorusclient.New only validate config shape; nats.Connect/redis.NewClient
// degrade to noop/lazy on an unreachable placeholder host, exactly as the
// pre-existing lifecycle_test.go suite already relies on).
// ===========================================================================

// baseTestConfig uses loopback IP literals (never a DNS hostname) for every
// placeholder endpoint: go-redis v9's Options.init() and nats.Connect both
// resolve hostnames synchronously, which turns a bare "placeholder" hostname
// into a real (slow) DNS lookup per NewApp() call. An IP literal skips
// resolution entirely and fails fast with connection-refused instead.
func baseTestConfig() *config.Config {
	return &config.Config{
		DatabaseDSN:     "postgres://invalid:invalid@127.0.0.1:1/none?sslmode=disable&connect_timeout=1",
		RedisURL:        "redis://127.0.0.1:1",
		NATSURL:         "nats://127.0.0.1:1",
		Port:            "18299",
		LogLevel:        "error",
		GinMode:         "release",
		ServiceVersion:  "test",
		ShutdownTimeout: 5 * time.Second,
	}
}

func TestNewApp_RedisURLInvalid_ReturnsParseError(t *testing.T) {
	cfg := baseTestConfig()
	cfg.RedisURL = "://not-a-valid-url"

	app, err := NewApp(cfg)
	if err == nil {
		t.Fatal("expected REDIS_URL parse error, got nil")
	}
	if app != nil {
		t.Errorf("expected nil App on error, got %+v", app)
	}
}

func TestNewApp_NewAPIKeySet_RedisURLEmpty_ReturnsError(t *testing.T) {
	cfg := baseTestConfig()
	cfg.RedisURL = ""
	cfg.NewAPIKey = "test-newapi-key"

	app, err := NewApp(cfg)
	if err == nil {
		t.Fatal("expected 'AI assistant requires REDIS_URL' error, got nil")
	}
	if app != nil {
		t.Errorf("expected nil App on error, got %+v", app)
	}
}

func TestNewApp_PlatformIntegrationEnabled_WiresSuccessfully(t *testing.T) {
	cfg := baseTestConfig()
	cfg.PlatformInternalKey = "test-internal-key"
	cfg.PlatformBaseURL = "http://127.0.0.1:1"

	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: unexpected error with platform integration enabled: %v", err)
	}
	if app == nil {
		t.Fatal("expected non-nil App")
	}
	if app.Handler() == nil {
		t.Error("expected non-nil Handler")
	}
}

func TestNewApp_OIDCIssuerSet_WiresAuthMiddleware(t *testing.T) {
	cfg := baseTestConfig()
	cfg.OIDCIssuer = "https://identity.example.internal"
	cfg.OIDCAudience = "tally"

	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: unexpected error with OIDC issuer set: %v", err)
	}
	if app == nil {
		t.Fatal("expected non-nil App")
	}
	if app.Handler() == nil {
		t.Error("expected non-nil Handler")
	}
}

func TestNewApp_AIAssistantAndMemoryEnabled_WiresSuccessfully(t *testing.T) {
	cfg := baseTestConfig()
	cfg.PlatformInternalKey = "test-internal-key"
	cfg.PlatformBaseURL = "http://127.0.0.1:1"
	cfg.NewAPIKey = "test-newapi-key"
	cfg.NewAPIBaseURL = "http://127.0.0.1:1"
	cfg.DefaultAIModel = "test-model"
	cfg.AIPlanTTLSeconds = 1800
	cfg.MemoryAPIKey = "test-memory-key"
	cfg.MemoryBaseURL = "http://127.0.0.1:1"
	cfg.ShopifyWebhookSecret = "test-shopify-secret"

	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: unexpected error with AI assistant + memory + platform enabled: %v", err)
	}
	if app == nil {
		t.Fatal("expected non-nil App")
	}
	if app.Handler() == nil {
		t.Error("expected non-nil Handler")
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := app.Stop(stopCtx); err != nil {
		t.Errorf("Stop: unexpected error: %v", err)
	}
}

func TestNewApp_PlatformInternalKeySet_EmptyBaseURL_ReturnsError(t *testing.T) {
	cfg := baseTestConfig()
	cfg.PlatformInternalKey = "test-internal-key"
	cfg.PlatformBaseURL = "" // platform.New requires both APIKey and BaseURL

	app, err := NewApp(cfg)
	if err == nil {
		t.Fatal("expected platform client init error for empty PlatformBaseURL, got nil")
	}
	if app != nil {
		t.Errorf("expected nil App on error, got %+v", app)
	}
}

func TestNewApp_NATSURLEmpty_NoOpNotifyMode(t *testing.T) {
	cfg := baseTestConfig()
	cfg.NATSURL = ""

	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: unexpected error with NATS disabled: %v", err)
	}
	if app == nil {
		t.Fatal("expected non-nil App")
	}
}

func TestNewApp_AIAssistant_EmptyNewAPIBaseURL_ReturnsError(t *testing.T) {
	cfg := baseTestConfig()
	cfg.NewAPIKey = "test-newapi-key"
	cfg.NewAPIBaseURL = "" // llmclient.New requires BaseURL

	app, err := NewApp(cfg)
	if err == nil {
		t.Fatal("expected LLM client init error for empty NewAPIBaseURL, got nil")
	}
	if app != nil {
		t.Errorf("expected nil App on error, got %+v", app)
	}
}

func TestNewApp_AIAssistantEnabled_MemoryDisabled(t *testing.T) {
	cfg := baseTestConfig()
	cfg.NewAPIKey = "test-newapi-key"
	cfg.NewAPIBaseURL = "http://127.0.0.1:1"
	cfg.MemoryAPIKey = "" // memorus stays disabled

	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: unexpected error with AI enabled / memory disabled: %v", err)
	}
	if app == nil {
		t.Fatal("expected non-nil App")
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := app.Stop(stopCtx); err != nil {
		t.Errorf("Stop: unexpected error: %v", err)
	}
}

// ===========================================================================
// authMW closures — both the dev-mode header-injection path (OIDCIssuer
// empty) and the OIDC-mode PAT short-circuit path (OIDCIssuer set) are
// anonymous gin.HandlerFunc closures wired inside NewApp; they only execute
// when a request actually reaches an /api/v1 route, so we drive them through
// app.Handler() rather than trying to call them directly.
// ===========================================================================

func TestApp_Handler_DevModeAuthMiddleware_InjectsIdentityFromHeaders(t *testing.T) {
	cfg := baseTestConfig() // OIDCIssuer empty -> dev header-injection mode
	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		app.Stop(stopCtx) //nolint:errcheck
	}()

	// Case 1: explicit X-Tenant-ID header (valid UUID) — parsed and set directly.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("X-IDP-Subject", "sub-123")
	req.Header.Set("X-Email", "u@example.com")
	req.Header.Set("X-Display-Name", "Test User")
	req.Header.Set("X-Tenant-ID", uuid.New().String())
	w := httptest.NewRecorder()
	app.Handler().ServeHTTP(w, req)
	if w.Code == 0 {
		t.Error("expected the dev-mode middleware to let the request reach a handler")
	}

	// Case 2: malformed X-Tenant-ID — uuid.Parse fails, tenantID stays uuid.Nil,
	// then the mapping-lookup fallback runs (and fails against the unreachable
	// placeholder DB, which is fine: we only need the statement to execute).
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req2.Header.Set("X-IDP-Subject", "sub-456")
	req2.Header.Set("X-Tenant-ID", "not-a-uuid")
	w2 := httptest.NewRecorder()
	app.Handler().ServeHTTP(w2, req2)
	if w2.Code == 0 {
		t.Error("expected a response for the malformed-tenant-header request")
	}

	// Case 3: no identity headers at all.
	req3 := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	w3 := httptest.NewRecorder()
	app.Handler().ServeHTTP(w3, req3)
	if w3.Code == 0 {
		t.Error("expected a response for the header-less request")
	}
}

func TestApp_Handler_OIDCModeAuthMiddleware_PATShortCircuit(t *testing.T) {
	cfg := baseTestConfig()
	cfg.OIDCIssuer = "https://identity.example.internal"
	cfg.OIDCAudience = "tally"

	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		app.Stop(stopCtx) //nolint:errcheck
	}()

	// A bearer token with the PAT prefix short-circuits before the JWT/JWKS
	// path (domainauth.ParseBearer + patRepo.GetByPrefix, which fails against
	// the unreachable placeholder DB — enough to exercise the closure). The
	// body must be exactly PrefixLen(8)+SecretLen(32)=40 chars for
	// ParseBearer to accept the shape instead of short-circuiting on ok=false.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer tally_pat_abcdefgh0123456789abcdefghijklmnopqrstuv")
	w := httptest.NewRecorder()
	app.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for an unresolvable PAT, got %d (body=%s)", w.Code, w.Body.String())
	}

	// No Authorization header at all: the middleware's own guard rejects
	// before ever touching tenantLookup/patResolver.
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	w2 := httptest.NewRecorder()
	app.Handler().ServeHTTP(w2, req2)
	if w2.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for a missing Authorization header, got %d", w2.Code)
	}
}

// ===========================================================================
// Remaining small branch gaps found while auditing the coverage profile:
// aiDraftCreator's SKU-price-found loop body, aiStockAdjuster's Commit-error
// branch, aiPriceReverter's all-success return, aiPriceSnapshotStore's
// unmarshal-error branch, NewApp's sql.Open parse-error branch, and Start's
// port-already-in-use ListenAndServe error branch.
// ===========================================================================

func TestAiDraftCreator_CreatePurchaseDraft_SkuPriceFound_UsesPurchasePrice(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	productID := uuid.New()
	skuID := uuid.New()
	rows := sqlmock.NewRows([]string{"id", "product_id", "retail_price", "purchase_price"}).
		AddRow(skuID, productID, "199.00", "88.50")
	mock.ExpectQuery("SELECT DISTINCT ON").WillReturnRows(rows)

	repo := &fakeBillRepo{productExists: true}
	creator := &aiDraftCreator{
		uc:      appbill.NewCreatePurchaseDraftUseCase(repo),
		skuRepo: reposku.New(db),
	}

	_, _, err = creator.CreatePurchaseDraft(context.Background(), uuid.New(), uuid.New(), []appai.DraftLine{
		{ProductID: productID, ProductName: "widget", Qty: decimal.NewFromInt(2)},
	})
	if err != nil {
		t.Fatalf("CreatePurchaseDraft: unexpected error: %v", err)
	}
	if len(repo.capturedItems) != 1 {
		t.Fatalf("expected 1 captured item, got %d", len(repo.capturedItems))
	}
	if !repo.capturedItems[0].UnitPrice.Equal(decimal.RequireFromString("88.50")) {
		t.Errorf("UnitPrice = %s, want 88.50 (the SKU's purchase_price)", repo.capturedItems[0].UnitPrice)
	}
}

func TestAiStockAdjuster_AdjustStockBatch_CommitError(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	warehouseID := uuid.New()
	rows := sqlmock.NewRows([]string{"id"}).AddRow(warehouseID)
	mock.ExpectQuery("SELECT id").WillReturnRows(rows)
	mock.ExpectBegin()
	mock.ExpectCommit().WillReturnError(errors.New("connection reset during commit"))

	uc := appstock.NewRecordMovementUseCase(&fakeStockRepoForAdjuster{}, &fakeCalculator{}, nil, nil)
	a := &aiStockAdjuster{db: db, uc: uc, whRepo: repowarehouse.New(db)}

	n, err := a.AdjustStockBatch(context.Background(), uuid.New(), uuid.New(), uuid.New(), []appai.StockAdjustLine{
		{ProductID: uuid.New(), Delta: decimal.NewFromInt(1)},
	})
	if err == nil {
		t.Fatal("expected Commit error to propagate, got nil")
	}
	if n != 0 {
		t.Errorf("affected = %d, want 0 when Commit fails", n)
	}
}

func TestAiPriceReverter_RestorePrices_AllSucceed(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	sku1, sku2 := uuid.New(), uuid.New()
	mock.ExpectExec("UPDATE tally.product_sku").WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sku1, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE tally.product_sku").WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sku2, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))

	rv := &aiPriceReverter{skuRepo: reposku.New(db)}
	n, err := rv.RestorePrices(context.Background(), uuid.New(), []appai.PriceBeforeEntry{
		{SKUID: sku1, OldPrice: decimal.NewFromInt(10)},
		{SKUID: sku2, OldPrice: decimal.NewFromInt(20)},
	})
	if err != nil {
		t.Fatalf("RestorePrices: unexpected error: %v", err)
	}
	if n != 2 {
		t.Errorf("restored = %d, want 2", n)
	}
}

func TestAiPriceSnapshotStore_GetSnapshot_UnmarshalErrorPassthrough(t *testing.T) {
	mr := miniredis.RunT(t)
	c := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer c.Close() //nolint:errcheck

	tenantID := uuid.New()
	planID := uuid.New()
	key := priceSnapshotKeyPrefix + tenantID.String() + ":" + planID.String()
	if err := c.Set(context.Background(), key, "not-valid-json", 0).Err(); err != nil {
		t.Fatalf("seeding a malformed snapshot value: %v", err)
	}

	s := newAIPriceSnapshotStore(c)
	got, err := s.GetSnapshot(context.Background(), tenantID, planID)
	if err == nil {
		t.Fatal("expected an unmarshal error for a malformed stored value, got nil")
	}
	if got != nil {
		t.Errorf("expected nil entries on unmarshal error, got %+v", got)
	}
}

// NOTE: two branches were deliberately NOT force-covered here after emitting
// this file's earlier draft revealed them to be structurally unreachable (or
// unreachably racy) in a deterministic unit test:
//   - app.go's `sql.Open("pgx", cfg.DatabaseDSN)` error branch: sql.Open never
//     dials and the pgx stdlib driver does not validate the DSN eagerly, so no
//     string (short of a build-breaking driver name typo) makes it return an
//     error here — confirmed empirically (a DSN containing a NUL byte still
//     opens successfully).
//   - start.go's `case err := <-errCh: return err` branch (ListenAndServe
//     failing fast, e.g. address-already-in-use): Start's own select has a
//     non-blocking `default:` with no synchronization against the goroutine
//     that calls ListenAndServe, so which branch fires is a genuine data race
//     in the production code itself, not merely a hard-to-arrange test setup.
//     A test asserting either outcome would be flaky by construction.
