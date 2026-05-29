package lifecycle

import (
	"context"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	appimporting "github.com/hanmahong5-arch/lurus-tally/internal/app/importing"
)

// return_adapters.go bridges the order-import use case's ReturnCreator / ReturnApprover
// ports to the dedicated return-bill use cases. Kept here (wiring layer) so the importing
// package stays decoupled from app/bill internals.

// importReturnCreator adapts bill.CreateReturnBillUseCase to importing.ReturnCreator.
type importReturnCreator struct{ uc *appbill.CreateReturnBillUseCase }

func (a importReturnCreator) Create(ctx context.Context, in appimporting.ReturnCreatorInput) (*appimporting.ReturnCreatorOutput, error) {
	items := make([]appbill.ReturnItem, len(in.Items))
	for i, it := range in.Items {
		items[i] = appbill.ReturnItem{
			ProductID: it.ProductID,
			LineNo:    it.LineNo,
			Qty:       it.Qty,
			UnitPrice: it.UnitPrice,
		}
	}
	out, err := a.uc.Execute(ctx, appbill.CreateReturnRequest{
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
	return &appimporting.ReturnCreatorOutput{BillID: out.BillID, BillNo: out.BillNo}, nil
}

// importReturnApprover adapts bill.ApproveReturnBillUseCase to importing.ReturnApprover.
type importReturnApprover struct{ uc *appbill.ApproveReturnBillUseCase }

func (a importReturnApprover) Approve(ctx context.Context, in appimporting.ReturnApproverInput) error {
	return a.uc.Execute(ctx, appbill.ApproveReturnRequest{
		TenantID:  in.TenantID,
		BillID:    in.BillID,
		CreatorID: in.CreatorID,
	})
}
