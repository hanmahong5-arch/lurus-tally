package lifecycle

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	appcurrency "github.com/hanmahong5-arch/lurus-tally/internal/app/currency"
	appimporting "github.com/hanmahong5-arch/lurus-tally/internal/app/importing"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
)

// import_adapters.go bridges the order-import use case's narrow ports to the
// existing sale / stock / currency use cases. Kept here (wiring layer) so the
// importing package stays decoupled from bill/stock/currency.

// importSaleCreator adapts bill.CreateSaleUseCase to importing.SaleCreator.
type importSaleCreator struct{ uc *appbill.CreateSaleUseCase }

func (a importSaleCreator) Create(ctx context.Context, in appimporting.SaleCreatorInput) (*appimporting.SaleCreatorOutput, error) {
	warehouseID := uuid.Nil
	if in.WarehouseID != nil {
		warehouseID = *in.WarehouseID
	}
	items := make([]appbill.SaleItem, len(in.Items))
	for i, it := range in.Items {
		items[i] = appbill.SaleItem{
			ProductID:   it.ProductID,
			WarehouseID: warehouseID,
			LineNo:      it.LineNo,
			Qty:         it.Qty,
			UnitPrice:   it.UnitPrice,
			ConvFactor:  "1",
		}
	}
	out, err := a.uc.Execute(ctx, appbill.CreateSaleRequest{
		TenantID:    in.TenantID,
		CreatorID:   in.CreatorID,
		WarehouseID: in.WarehouseID,
		BillDate:    in.BillDate,
		Remark:      in.Remark,
		Items:       items,
	})
	if err != nil {
		return nil, err
	}
	return &appimporting.SaleCreatorOutput{BillID: out.BillID, BillNo: out.BillNo}, nil
}

// importSaleApprover adapts bill.ApproveSaleUseCase to importing.SaleApprover.
type importSaleApprover struct{ uc *appbill.ApproveSaleUseCase }

func (a importSaleApprover) Approve(ctx context.Context, in appimporting.SaleApproverInput) error {
	return a.uc.Execute(ctx, appbill.ApproveSaleRequest{
		TenantID:  in.TenantID,
		BillID:    in.BillID,
		CreatorID: in.CreatorID,
	})
}

// importStockChecker adapts stock.GetSnapshotUseCase to importing.StockChecker.
type importStockChecker struct{ uc *appstock.GetSnapshotUseCase }

func (a importStockChecker) AvailableQty(ctx context.Context, tenantID, productID, warehouseID uuid.UUID) (decimal.Decimal, error) {
	snap, err := a.uc.Execute(ctx, tenantID, productID, warehouseID)
	if err != nil {
		return decimal.Zero, err
	}
	if snap == nil {
		return decimal.Zero, nil
	}
	return snap.AvailableQty, nil
}

// importCurrencyRater adapts currency.GetRateUseCase to importing.CurrencyRater.
type importCurrencyRater struct{ uc *appcurrency.GetRateUseCase }

func (a importCurrencyRater) GetRate(ctx context.Context, tenantID uuid.UUID, from, to string, date time.Time) (decimal.Decimal, error) {
	res, err := a.uc.Execute(ctx, tenantID, from, to, date)
	if err != nil {
		return decimal.Zero, err
	}
	return res.Rate, nil
}
