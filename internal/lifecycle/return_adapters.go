package lifecycle

import (
	"context"
	"errors"

	"github.com/google/uuid"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	appimporting "github.com/hanmahong5-arch/lurus-tally/internal/app/importing"
)

// return_adapters.go bridges the order-import use case's ReturnCreator / ReturnApprover
// ports to the dedicated return-bill use cases. Kept here (wiring layer) so the importing
// package stays decoupled from app/bill internals.

// importReturnCreator adapts bill.CreateReturnBillUseCase to importing.ReturnCreator.
type importReturnCreator struct {
	uc *appbill.CreateReturnBillUseCase
}

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
	warehouseID := in.WarehouseID
	// A platform cancellation carries no lines: return the whole original sale, from
	// the warehouse it shipped out of.
	if len(items) == 0 {
		if ref, ok := appbill.ParseOriginRef(in.Remark); ok && ref.BillID != uuid.Nil {
			wh, cloned, err := a.uc.CloneOriginal(ctx, in.TenantID, ref.BillID)
			if err != nil {
				if errors.Is(err, appbill.ErrNothingToReverse) {
					return nil, appimporting.ErrNothingToReverse
				}
				return nil, err
			}
			items = cloned
			if warehouseID == nil {
				warehouseID = wh
			}
		}
	}
	// A platform refund is capped to what the order still has left, so one that follows
	// a cancellation (or the reverse) does not restock the same goods twice.
	if len(in.Items) > 0 {
		capped, err := a.uc.CapToRemaining(ctx, in.TenantID, in.Remark, items)
		if err != nil {
			if errors.Is(err, appbill.ErrNothingToReverse) {
				return nil, appimporting.ErrNothingToReverse
			}
			return nil, err
		}
		items = capped
	}
	out, err := a.uc.Execute(ctx, appbill.CreateReturnRequest{
		TenantID:    in.TenantID,
		CreatorID:   in.CreatorID,
		WarehouseID: warehouseID,
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
type importReturnApprover struct {
	uc *appbill.ApproveReturnBillUseCase
}

func (a importReturnApprover) Approve(ctx context.Context, in appimporting.ReturnApproverInput) error {
	err := a.uc.Execute(ctx, appbill.ApproveReturnRequest{
		TenantID:  in.TenantID,
		BillID:    in.BillID,
		CreatorID: in.CreatorID,
	})
	if errors.Is(err, appbill.ErrNothingToReverse) {
		return appimporting.ErrNothingToReverse
	}
	return err
}
