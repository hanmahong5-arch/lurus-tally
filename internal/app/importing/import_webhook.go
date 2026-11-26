package importing

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// SingleOrderRequest is the input to IngestSingleOrder for webhook-delivered orders.
type SingleOrderRequest struct {
	TenantID           uuid.UUID
	CreatorID          uuid.UUID
	WarehouseID        uuid.UUID
	Platform           Platform
	PlatformOrderNo    string
	PlatformOrderAlias string
	Lines              []OrderRow
}

// IngestSingleOrder ingests one webhook-delivered order through the dedup, SKU, FX, and bill pipeline.
func (uc *ImportOrdersUseCase) IngestSingleOrder(ctx context.Context, req SingleOrderRequest) (ImportedOrder, *SkippedOrder, error) {
	if req.TenantID == uuid.Nil {
		return ImportedOrder{}, nil, fmt.Errorf("importing: tenant_id is required")
	}
	if req.CreatorID == uuid.Nil {
		return ImportedOrder{}, nil, fmt.Errorf("importing: creator_id is required")
	}
	if req.WarehouseID == uuid.Nil {
		return ImportedOrder{}, nil, fmt.Errorf("importing: warehouse_id is required")
	}
	if err := req.Platform.Validate(); err != nil {
		return ImportedOrder{}, nil, err
	}
	if req.PlatformOrderNo == "" {
		return ImportedOrder{}, nil, fmt.Errorf("importing: platform_order_no is required")
	}
	if len(req.Lines) == 0 {
		return ImportedOrder{}, nil, fmt.Errorf("importing: at least one order line is required")
	}

	if cancelled, _, _, cerr := uc.repo.IsCancelSeen(ctx, req.TenantID, string(req.Platform), req.PlatformOrderNo); cerr != nil {
		return ImportedOrder{}, nil, fmt.Errorf("importing: cancel check for order %s: %w", req.PlatformOrderNo, cerr)
	} else if cancelled {
		return ImportedOrder{}, &SkippedOrder{PlatformOrderNo: req.PlatformOrderNo, Reason: "cancelled"}, nil
	}

	imported, skipped, err := uc.ingestOneOrder(ctx, req.TenantID, req.CreatorID, req.WarehouseID,
		req.Platform, req.PlatformOrderNo, req.Lines)
	if err == nil && skipped == nil && imported.MarkSeenError == "" &&
		req.PlatformOrderAlias != "" && req.PlatformOrderAlias != req.PlatformOrderNo {
		_ = uc.repo.MarkOrderSeen(ctx, req.TenantID, string(req.Platform), req.PlatformOrderAlias, imported.BillID)
	}
	return imported, skipped, err
}

// ingestOneOrder executes dedup, SKU resolution, FX conversion, bill creation, approval, and mark-seen.
func (uc *ImportOrdersUseCase) ingestOneOrder(
	ctx context.Context,
	tenantID, creatorID, warehouseID uuid.UUID,
	platform Platform,
	orderNo string,
	lines []OrderRow,
) (ImportedOrder, *SkippedOrder, error) {
	seen, existingBillID, err := uc.repo.IsOrderSeen(ctx, tenantID, string(platform), orderNo)
	if err != nil {
		return ImportedOrder{}, nil, fmt.Errorf("importing: dedup check for order %s: %w", orderNo, err)
	}
	if seen {
		return ImportedOrder{}, &SkippedOrder{
			PlatformOrderNo: orderNo,
			Reason:          fmt.Sprintf("duplicate:bill_id=%s", existingBillID),
		}, nil
	}

	resolved := make([]resolvedOrderLine, 0, len(lines))
	emptyHints := map[string]uuid.UUID{}
	for _, row := range lines {
		productID, unknown, resolveErr := uc.resolveSKU(ctx, tenantID, string(platform), row.PlatformSKU, emptyHints)
		if resolveErr != nil {
			return ImportedOrder{}, nil, fmt.Errorf("importing: resolve sku %s: %w", row.PlatformSKU, resolveErr)
		}
		if unknown {
			return ImportedOrder{}, &SkippedOrder{
				PlatformOrderNo: orderNo,
				Reason:          fmt.Sprintf("unknown_sku:%s", row.PlatformSKU),
			}, nil
		}
		resolved = append(resolved, resolvedOrderLine{row: row, productID: productID})
	}

	convertedLines, err := uc.convertOrderLines(ctx, tenantID, orderNo, resolved)
	if err != nil {
		return ImportedOrder{}, nil, err
	}

	out, err := uc.saleCreator.Create(ctx, SaleCreatorInput{
		TenantID:    tenantID,
		CreatorID:   creatorID,
		WarehouseID: &warehouseID,
		BillDate:    lines[0].OrderDate,
		Remark:      fmt.Sprintf("import:%s:%s", platform, orderNo),
		Items:       toSaleLineItems(convertedLines),
	})
	if err != nil {
		return ImportedOrder{}, nil, fmt.Errorf("importing: create sale bill for order %s: %w", orderNo, err)
	}

	if err := uc.saleApprover.Approve(ctx, SaleApproverInput{
		TenantID:  tenantID,
		BillID:    out.BillID,
		CreatorID: creatorID,
	}); err != nil {
		return ImportedOrder{}, &SkippedOrder{
			PlatformOrderNo: orderNo,
			Reason:          fmt.Sprintf("approve_failed:%s", err.Error()),
		}, nil
	}

	if err := uc.repo.MarkOrderSeen(ctx, tenantID, string(platform), orderNo, out.BillID); err != nil {
		return ImportedOrder{
			PlatformOrderNo: orderNo,
			BillID:          out.BillID,
			BillNo:          out.BillNo,
			MarkSeenError:   err.Error(),
		}, nil, nil
	}

	return ImportedOrder{
		PlatformOrderNo: orderNo,
		BillID:          out.BillID,
		BillNo:          out.BillNo,
	}, nil, nil
}
