package lifecycle

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	reposku "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/sku"
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
func buildPlanExecutor(
	db *sql.DB,
	billRepo appbill.BillRepo,
	recordMovementUC *appstock.RecordMovementUseCase,
	products appai.ProductRepo,
) appai.PlanExecutor {
	skuRepo := reposku.New(db)
	whRepo := repowarehouse.New(db)
	return appai.NewPlanExecutor(
		products,
		&aiDraftCreator{uc: appbill.NewCreatePurchaseDraftUseCase(billRepo), skuRepo: skuRepo},
		&aiPriceChanger{uc: appsku.NewUpdatePriceUseCase(skuRepo)},
		&aiStockAdjuster{uc: recordMovementUC, whRepo: whRepo},
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

// aiStockAdjuster records a single adjust movement in the tenant's default warehouse.
type aiStockAdjuster struct {
	uc     *appstock.RecordMovementUseCase
	whRepo *repowarehouse.Repo
}

func (a *aiStockAdjuster) AdjustStock(ctx context.Context, tenantID, actorID, productID uuid.UUID, delta decimal.Decimal) error {
	warehouseID, err := a.whRepo.DefaultWarehouseID(ctx, tenantID)
	if err != nil {
		return err
	}
	_, err = a.uc.Execute(ctx, appstock.RecordMovementRequest{
		TenantID:      tenantID,
		ProductID:     productID,
		WarehouseID:   warehouseID,
		Direction:     domainstock.DirectionAdjust,
		Qty:           delta,
		ConvFactor:    "1",
		CostStrategy:  domainstock.CostStrategyWAC,
		ReferenceType: domainstock.RefAdjust,
		CreatedBy:     &actorID,
		Note:          "AI assistant — bulk stock adjust",
	})
	return err
}
