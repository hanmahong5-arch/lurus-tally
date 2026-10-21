package stock

import (
	"context"
	"database/sql"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// maxAdjustItems bounds one count sheet so a single request stays one short transaction.
const maxAdjustItems = 500

// AdjustCostRequired is the AdjustError.Code of a count that brings stock in with no known
// price: the caller has to supply one per product (AdjustItem.UnitCost).
const AdjustCostRequired = "cost_required"

// AdjustError is a stock-count validation failure with a plain Chinese message. Code and
// ProductIDs are set when the caller can fix it by supplying more input.
type AdjustError struct {
	Msg        string
	Code       string
	ProductIDs []uuid.UUID
}

func (e *AdjustError) Error() string { return e.Msg }

// AdjustItem is one counted product: CountedQty is the physical quantity in the base unit.
// UnitCost is optional: the purchase price per base unit to value newly counted stock at,
// needed only when the book quantity is zero and no price is on file.
type AdjustItem struct {
	ProductID  uuid.UUID
	CountedQty decimal.Decimal
	UnitCost   decimal.Decimal
}

// AdjustRequest is one stock-count sheet for a single warehouse.
type AdjustRequest struct {
	TenantID    uuid.UUID
	WarehouseID uuid.UUID
	Items       []AdjustItem
	Remark      string
	CreatedBy   *uuid.UUID
}

// AdjustLine reports what the count did to one product.
type AdjustLine struct {
	ProductID uuid.UUID       `json:"product_id"`
	Before    decimal.Decimal `json:"before"`
	Counted   decimal.Decimal `json:"counted"`
	Delta     decimal.Decimal `json:"delta"`
}

// AdjustResult summarises a count sheet; AdjustID is the reference_id shared by its movements.
type AdjustResult struct {
	AdjustID  uuid.UUID    `json:"adjust_id"`
	Adjusted  int          `json:"adjusted"`
	Unchanged int          `json:"unchanged"`
	Lines     []AdjustLine `json:"items"`
}

// OwnershipChecker verifies inside the count transaction that the warehouse and every
// product belong to the tenant and are not deleted. Foreign keys are not tenant-scoped,
// so without this check another tenant's ids would be accepted. The production repo
// implements it; AdjustUseCase skips the check for repos that do not.
type OwnershipChecker interface {
	// CheckOwned reports whether the warehouse is a live row of the tenant and how many
	// of productIDs are live products of the tenant.
	CheckOwned(ctx context.Context, tx *sql.Tx, tenantID, warehouseID uuid.UUID, productIDs []uuid.UUID) (warehouseOK bool, products int, err error)
}

// AdjustCostSource is an optional capability of the repo: the price to value stock counted
// into an empty book at, taken from the product's default purchase price or its cost in
// another warehouse. Zero means nothing is on file. The production repo implements it.
type AdjustCostSource interface {
	FallbackCost(ctx context.Context, tx *sql.Tx, tenantID, productID, warehouseID uuid.UUID) (decimal.Decimal, error)
}

// AdjustUseCase turns a physical count into adjust movements: for every product the
// difference between the counted and the book quantity is applied through
// RecordMovementUseCase (and so InventoryCalculator.ApplyMovement). Adjustments never
// change the average cost.
type AdjustUseCase struct {
	repo   StockRepo
	record *RecordMovementUseCase
}

// NewAdjustUseCase constructs the use case.
func NewAdjustUseCase(repo StockRepo, record *RecordMovementUseCase) *AdjustUseCase {
	return &AdjustUseCase{repo: repo, record: record}
}

// Execute applies the whole sheet in one transaction: either every difference is
// written or none is. Products already matching the count produce no movement.
// Validation failures are *AdjustError.
func (uc *AdjustUseCase) Execute(ctx context.Context, req AdjustRequest) (*AdjustResult, error) {
	items, err := validateAdjust(req)
	if err != nil {
		return nil, err
	}
	res := &AdjustResult{AdjustID: uuid.New()}

	txErr := uc.repo.WithTx(ctx, func(tx *sql.Tx) error {
		res.Adjusted, res.Unchanged, res.Lines = 0, 0, res.Lines[:0]
		if oc, ok := uc.repo.(OwnershipChecker); ok {
			ids := make([]uuid.UUID, len(items))
			for i, it := range items {
				ids[i] = it.ProductID
			}
			whOK, n, err := oc.CheckOwned(ctx, tx, req.TenantID, req.WarehouseID, ids)
			if err != nil {
				return fmt.Errorf("stock adjust: check ownership: %w", err)
			}
			if !whOK || n != len(ids) {
				return &AdjustError{Msg: "盘点单里有商品或仓库不存在，请刷新页面后重试"}
			}
		}
		var needCost []uuid.UUID
		for _, it := range items {
			// Lock first so the book quantity read below cannot move before the movement lands.
			if err := uc.repo.AcquireAdvisoryLock(ctx, tx, req.TenantID, it.ProductID, req.WarehouseID); err != nil {
				return fmt.Errorf("stock adjust: acquire advisory lock: %w", err)
			}
			snap, err := uc.repo.SelectForUpdate(ctx, tx, req.TenantID, it.ProductID, req.WarehouseID)
			if err != nil {
				return fmt.Errorf("stock adjust: load snapshot: %w", err)
			}
			before := decimal.Zero
			if snap != nil {
				before = snap.OnHandQty
			}
			line := AdjustLine{ProductID: it.ProductID, Before: before, Counted: it.CountedQty, Delta: it.CountedQty.Sub(before)}
			res.Lines = append(res.Lines, line)
			if line.Delta.IsZero() {
				res.Unchanged++
				continue
			}
			var cost decimal.Decimal
			if line.Delta.IsPositive() {
				// Stock counted in is valued: the keeper's price, else the cost already on the
				// snapshot, else the product's purchase price. Only an empty book with none of
				// them is an error, otherwise the stock would enter at zero cost.
				cost = it.UnitCost
				if !cost.IsPositive() && snap != nil {
					cost = snap.UnitCost
				}
				if !cost.IsPositive() {
					if src, ok := uc.repo.(AdjustCostSource); ok {
						if cost, err = src.FallbackCost(ctx, tx, req.TenantID, it.ProductID, req.WarehouseID); err != nil {
							return fmt.Errorf("stock adjust: fallback cost: %w", err)
						}
					}
				}
				if !cost.IsPositive() {
					cost = decimal.Zero
					if before.IsZero() {
						needCost = append(needCost, it.ProductID)
						continue
					}
				}
			}
			ref := res.AdjustID
			if _, err := uc.record.ExecuteInTx(ctx, tx, RecordMovementRequest{
				TenantID:      req.TenantID,
				ProductID:     it.ProductID,
				WarehouseID:   req.WarehouseID,
				Direction:     domain.DirectionAdjust,
				Qty:           line.Delta,
				UnitCost:      cost,
				ReferenceType: domain.RefAdjust,
				ReferenceID:   &ref,
				CreatedBy:     req.CreatedBy,
				Note:          req.Remark,
			}); err != nil {
				return err
			}
			res.Adjusted++
		}
		if len(needCost) > 0 {
			return &AdjustError{Msg: "有商品账面是 0 又没有进价，请填写它们的进货单价后再提交", Code: AdjustCostRequired, ProductIDs: needCost}
		}
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	return res, nil
}

// validateAdjust checks the sheet and returns the items in a stable product order,
// so concurrent sheets always take the per-product locks in the same order.
func validateAdjust(req AdjustRequest) ([]AdjustItem, error) {
	if req.TenantID == uuid.Nil {
		return nil, fmt.Errorf("stock adjust: tenant_id is required")
	}
	if req.WarehouseID == uuid.Nil {
		return nil, &AdjustError{Msg: "请先选择要盘点的仓库"}
	}
	if len(req.Items) == 0 {
		return nil, &AdjustError{Msg: "请至少填写一个商品的盘点数量"}
	}
	if len(req.Items) > maxAdjustItems {
		return nil, &AdjustError{Msg: fmt.Sprintf("一次最多盘点 %d 个商品，请分批提交", maxAdjustItems)}
	}
	seen := make(map[uuid.UUID]struct{}, len(req.Items))
	items := make([]AdjustItem, 0, len(req.Items))
	for _, it := range req.Items {
		if it.ProductID == uuid.Nil {
			return nil, &AdjustError{Msg: "盘点单里有商品信息缺失，请刷新页面后重试"}
		}
		if it.CountedQty.IsNegative() {
			return nil, &AdjustError{Msg: "盘点数量不能为负数"}
		}
		if it.UnitCost.IsNegative() {
			return nil, &AdjustError{Msg: "进货单价不能为负数"}
		}
		if _, dup := seen[it.ProductID]; dup {
			return nil, &AdjustError{Msg: "同一个商品在盘点单里出现了多次，请合并后再提交"}
		}
		seen[it.ProductID] = struct{}{}
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ProductID.String() < items[j].ProductID.String() })
	return items, nil
}
