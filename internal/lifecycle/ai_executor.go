package lifecycle

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
	reposku "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/sku"
	repostock "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/stock"
	repowarehouse "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/warehouse"
	appacct "github.com/hanmahong5-arch/lurus-tally/internal/app/account"
	appai "github.com/hanmahong5-arch/lurus-tally/internal/app/ai"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	appsku "github.com/hanmahong5-arch/lurus-tally/internal/app/sku"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// buildPlanExecutor wires the AI plan executor over the existing bill / sku /
// stock use cases. It is the bridge that turns a confirmed AI plan into real
// side effects (PO draft, price change, stock adjust).
// Returns *DefaultPlanExecutor (not the interface) so callers can call
// WithPriceSnapshot before passing to the orchestrator.
func buildPlanExecutor(
	db *sql.DB,
	billRepo appbill.BillRepo,
	recordMovementUC *appstock.RecordMovementUseCase,
	products appai.ProductRepo,
) *appai.DefaultPlanExecutor {
	skuRepo := reposku.New(db)
	whRepo := repowarehouse.New(db)
	return appai.NewPlanExecutor(
		products,
		&aiDraftCreator{uc: appbill.NewCreatePurchaseDraftUseCase(billRepo), skuRepo: skuRepo},
		&aiPriceChanger{uc: appsku.NewUpdatePriceUseCase(skuRepo)},
		&aiStockAdjuster{db: db, uc: recordMovementUC, whRepo: whRepo},
	)
}

// aiAuditWriter persists AI plan executions to the account audit log so they
// surface in the "活动日志" tab. Write failures are logged, not surfaced — the
// side effect already committed by the time we audit.
type aiAuditWriter struct {
	uc  *appacct.AppendAuditLog
	log *slog.Logger
}

func newAIAuditWriter(auditRepo appacct.AuditRepo, log *slog.Logger) *aiAuditWriter {
	return &aiAuditWriter{uc: appacct.NewAppendAuditLog(auditRepo), log: log}
}

func (a *aiAuditWriter) Write(ctx context.Context, rec appai.AuditRecord) error {
	err := a.uc.Execute(ctx, appacct.AppendInput{
		TenantID:   rec.TenantID,
		ActorID:    rec.ActorID.String(),
		Action:     rec.Action,
		TargetKind: rec.TargetKind,
		TargetID:   rec.TargetID,
		Payload:    rec.Payload,
	})
	if err != nil && a.log != nil {
		a.log.Warn("ai audit write failed",
			slog.Any("error", err),
			slog.String("action", rec.Action),
			slog.String("tenant_id", rec.TenantID.String()))
	}
	return err
}

// aiDraftCreator turns resolved AI draft lines into a real purchase draft.
// Unit price defaults to each product's default-SKU purchase_price (0 when none).
type aiDraftCreator struct {
	uc      *appbill.CreatePurchaseDraftUseCase
	skuRepo *reposku.Repo
}

func (a *aiDraftCreator) CreatePurchaseDraft(ctx context.Context, tenantID, actorID uuid.UUID, lines []appai.DraftLine) (uuid.UUID, string, error) {
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
	// A missing SKU price is non-fatal: the draft is created with unit price 0 so
	// the operator can fill it in before approval (drafts are not yet committed to stock).

	items := make([]appbill.CreatePurchaseItemInput, 0, len(lines))
	for i, l := range lines {
		items = append(items, appbill.CreatePurchaseItemInput{
			ProductID: l.ProductID,
			LineNo:    i + 1,
			Qty:       l.Qty,
			UnitPrice: priceByProduct[l.ProductID],
		})
	}

	out, err := a.uc.Execute(ctx, appbill.CreatePurchaseDraftRequest{
		TenantID:  tenantID,
		CreatorID: actorID,
		BillDate:  time.Now().UTC(),
		Remark:    "AI assistant — replenishment draft",
		Items:     items,
	})
	if err != nil {
		return uuid.Nil, "", err
	}
	return out.BillID, out.BillNo, nil
}

// aiPriceChanger applies a price action to matched products' default SKUs.
type aiPriceChanger struct {
	uc *appsku.UpdatePriceUseCase
}

func (a *aiPriceChanger) ApplyPriceChange(ctx context.Context, tenantID uuid.UUID, productIDs []uuid.UUID, action string) (int, error) {
	return a.uc.Execute(ctx, tenantID, productIDs, action)
}

// aiStockAdjuster records a batch of adjust movements atomically in the tenant's
// default warehouse. Every movement carries the plan ID as reference_id (NOT NULL
// since migration 000034) and the whole batch shares one DB transaction so a
// mid-batch failure rolls back every prior line — callers can safely retry.
type aiStockAdjuster struct {
	db     *sql.DB
	uc     *appstock.RecordMovementUseCase
	whRepo *repowarehouse.Repo
}

func (a *aiStockAdjuster) AdjustStockBatch(
	ctx context.Context,
	tenantID, actorID, planID uuid.UUID,
	lines []appai.StockAdjustLine,
) (int, error) {
	if len(lines) == 0 {
		return 0, nil
	}
	warehouseID, err := a.whRepo.DefaultWarehouseID(ctx, tenantID)
	if err != nil {
		return 0, err
	}

	tx, err := dbscope.BeginTx(ctx, a.db, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	affected := 0
	refID := planID
	for _, ln := range lines {
		if _, err := a.uc.ExecuteInTx(ctx, tx, appstock.RecordMovementRequest{
			TenantID:      tenantID,
			ProductID:     ln.ProductID,
			WarehouseID:   warehouseID,
			Direction:     domainstock.DirectionAdjust,
			Qty:           ln.Delta,
			ConvFactor:    "1",
			CostStrategy:  domainstock.CostStrategyWAC,
			ReferenceType: domainstock.RefAdjust,
			ReferenceID:   &refID,
			CreatedBy:     &actorID,
			Note:          "AI assistant — bulk stock adjust",
		}); err != nil {
			return 0, err
		}
		affected++
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return affected, nil
}

// buildReverter wires the AI plan reverter that handles undo for bulk_stock_adjust
// and price_change plans. The rdb parameter is the same Redis client used by the
// plan store — it serves as backing for the price-before snapshot TTL storage.
func buildReverter(
	db *sql.DB,
	stockRepo *repostock.Repo,
	recordMovementUC *appstock.RecordMovementUseCase,
	planStore appai.PlanStore,
	rdb *redis.Client,
) *appai.Reverter {
	skuRepo := reposku.New(db)
	whRepo := repowarehouse.New(db)
	return appai.NewReverter(
		planStore,
		&aiStockReverter{stockRepo: stockRepo, recordMovementUC: recordMovementUC, whRepo: whRepo},
		&aiPriceReverter{skuRepo: skuRepo},
		newAIPriceSnapshotStore(rdb),
	)
}

// buildPriceCapturerAdapter returns the PriceCapturerPort used to snapshot
// before-prices when a price-change plan is executed.
func buildPriceCapturerAdapter(db *sql.DB) appai.PriceCapturerPort {
	return &aiPriceCapturer{skuRepo: reposku.New(db)}
}

// ----- stock reverter -----

// aiStockReverter reverses bulk_stock_adjust movements by writing compensating
// movements (negated delta, reference_type="adjust_revert", same reference_id).
type aiStockReverter struct {
	stockRepo        *repostock.Repo
	recordMovementUC *appstock.RecordMovementUseCase
	whRepo           *repowarehouse.Repo
}

func (a *aiStockReverter) RevertStockAdjust(ctx context.Context, tenantID, actorID, planID uuid.UUID) (int, error) {
	movements, err := a.stockRepo.ListMovementsByReference(ctx, tenantID, planID)
	if err != nil {
		return 0, fmt.Errorf("stock reverter: list movements: %w", err)
	}
	if len(movements) == 0 {
		// No movements found — either the plan was not a stock-adjust or the
		// movements were already cleaned up. Return 0 without error so the
		// status flip above still takes effect.
		return 0, nil
	}

	reverted := 0
	for _, m := range movements {
		// Compensating direction: negate the original delta.
		// The original movement Direction is always "adjust"; the revert writes
		// another "adjust" movement with the negated qty so the net effect is zero.
		negated := m.QtyBase.Neg()
		_, err := a.recordMovementUC.Execute(ctx, appstock.RecordMovementRequest{
			TenantID:      tenantID,
			ProductID:     m.ProductID,
			WarehouseID:   m.WarehouseID,
			Direction:     domainstock.DirectionAdjust,
			Qty:           negated,
			ConvFactor:    "1",
			CostStrategy:  domainstock.CostStrategyWAC,
			ReferenceType: domainstock.RefAdjust,
			ReferenceID:   &planID,
			CreatedBy:     &actorID,
			Note:          "Tally assistant — undo stock adjust",
		})
		if err != nil {
			return reverted, fmt.Errorf("stock reverter: write compensating movement (product %s): %w", m.ProductID, err)
		}
		reverted++
	}
	return reverted, nil
}

// ----- price capturer -----

// aiPriceCapturer reads the current retail prices of the matched products so
// they can be written to the snapshot store before the price-change executes.
type aiPriceCapturer struct {
	skuRepo *reposku.Repo
}

func (a *aiPriceCapturer) CaptureBeforePrices(ctx context.Context, tenantID uuid.UUID, productIDs []uuid.UUID) ([]appai.PriceBeforeEntry, error) {
	skus, err := a.skuRepo.ListDefaultSKUs(ctx, tenantID, productIDs)
	if err != nil {
		return nil, fmt.Errorf("price capturer: list default skus: %w", err)
	}
	entries := make([]appai.PriceBeforeEntry, 0, len(skus))
	for _, s := range skus {
		entries = append(entries, appai.PriceBeforeEntry{
			SKUID:    s.SKUID,
			OldPrice: s.RetailPrice,
		})
	}
	return entries, nil
}

// ----- price reverter -----

// aiPriceReverter restores retail prices from a before-state snapshot.
type aiPriceReverter struct {
	skuRepo *reposku.Repo
}

func (a *aiPriceReverter) RestorePrices(ctx context.Context, tenantID uuid.UUID, entries []appai.PriceBeforeEntry) (int, error) {
	restored := 0
	for _, e := range entries {
		if err := a.skuRepo.UpdateRetailPrice(ctx, tenantID, e.SKUID, e.OldPrice); err != nil {
			return restored, fmt.Errorf("price reverter: restore sku %s: %w", e.SKUID, err)
		}
		restored++
	}
	return restored, nil
}

// ----- price snapshot store (Redis-backed) -----

const priceSnapshotKeyPrefix = "tally:ai:price_snap:"

// aiPriceSnapshotStore stores and retrieves pre-execution price snapshots in Redis.
// TTL matches appai.UndoTTLSeconds so the snapshot is guaranteed to expire once
// the undo window closes.
type aiPriceSnapshotStore struct {
	rdb *redis.Client
}

func newAIPriceSnapshotStore(rdb *redis.Client) *aiPriceSnapshotStore {
	return &aiPriceSnapshotStore{rdb: rdb}
}

func (s *aiPriceSnapshotStore) SaveSnapshot(ctx context.Context, tenantID, planID uuid.UUID, entries []appai.PriceBeforeEntry) error {
	b, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("price snapshot: marshal: %w", err)
	}
	key := priceSnapshotKeyPrefix + tenantID.String() + ":" + planID.String()
	return s.rdb.Set(ctx, key, string(b), appai.UndoTTLSeconds*time.Second).Err()
}

func (s *aiPriceSnapshotStore) GetSnapshot(ctx context.Context, tenantID, planID uuid.UUID) ([]appai.PriceBeforeEntry, error) {
	key := priceSnapshotKeyPrefix + tenantID.String() + ":" + planID.String()
	raw, err := s.rdb.GetDel(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, fmt.Errorf("price snapshot: get: %w", err)
	}
	var entries []appai.PriceBeforeEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, fmt.Errorf("price snapshot: unmarshal: %w", err)
	}
	return entries, nil
}
