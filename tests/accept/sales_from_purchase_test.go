//go:build accept

package accept

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	repobill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/bill"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
)

func flowSaleFromPurchase(t *testing.T, ctx context.Context, db *sql.DB, e tenantEnv,
	createPurchase *appbill.CreatePurchaseDraftUseCase,
	approvePurchase *appbill.ApprovePurchaseUseCase,
	createSale *appbill.CreateSaleUseCase, approveSale *appbill.ApproveSaleUseCase,
	billRepo *repobill.Repo,
) salesFlowResult {
	const name = "sale_opened_from_purchase_pin"
	fr := salesFlowResult{Name: name, Details: map[string]string{}}

	purchaseID, purchaseItem := createAndApprovePurchase(t, ctx, db, e, createPurchase, approvePurchase, e.productD, 6, "9.50", time.Now().UTC())
	remBefore := batchRemainingByItem(t, ctx, db, e.tenant, purchaseItem)
	fr.Details["pinned_batch_remaining_before"] = remBefore.String()

	salePrice := decimal.NewFromInt(88)
	saleOut, err := createSale.Execute(ctx, appbill.CreateSaleRequest{
		TenantID: e.tenant, CreatorID: e.creator, PartnerID: &e.customer,
		Items: []appbill.SaleItem{{
			ProductID: e.productD, WarehouseID: e.warehouse, LineNo: 1,
			Qty: decimal.NewFromInt(4), UnitPrice: salePrice, SourceItemID: purchaseItem,
		}},
	})
	if err != nil {
		fr.Err = err.Error()
		return fr
	}
	items, _ := billRepo.GetBillItems(ctx, e.tenant, saleOut.BillID)
	if !items[0].UnitPrice.Equal(salePrice) {
		fr.Err = "sale copied purchase price"
		return fr
	}
	if err := approveSale.Execute(ctx, appbill.ApproveSaleRequest{
		TenantID: e.tenant, BillID: saleOut.BillID, CreatorID: e.creator,
	}); err != nil {
		fr.Err = "approve: " + err.Error()
		return fr
	}
	remAfter := batchRemainingByItem(t, ctx, db, e.tenant, purchaseItem)
	fr.Details["pinned_batch_remaining_after"] = remAfter.String()
	fr.Details["sale_unit_price"] = items[0].UnitPrice.String()
	fr.Details["purchase_unit_price"] = "9.50"
	fr.Details["sale_status"] = fmt.Sprint(billStatus(t, ctx, db, e.tenant, saleOut.BillID))
	fr.Details["source_purchase_id"] = purchaseID.String()
	if !remAfter.Equal(decimal.NewFromInt(2)) {
		fr.Err = fmt.Sprintf("pinned batch remaining %s want 2", remAfter)
		return fr
	}
	drawQty, err := pinnedDrawQty(t, ctx, db, e.tenant, items[0].ID, purchaseItem)
	if err != nil {
		fr.Err = err.Error()
		return fr
	}
	fr.Details["pinned_draw_qty"] = drawQty.String()
	if !drawQty.Equal(decimal.NewFromInt(4)) {
		fr.Err = fmt.Sprintf("pinned draw qty %s want 4", drawQty)
		return fr
	}
	fr.OK = true
	return fr
}
