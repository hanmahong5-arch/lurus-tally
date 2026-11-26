package importing

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// orderGroup groups CSV rows belonging to a single platform order number.
type orderGroup struct {
	orderDate time.Time
	currency  string
	lines     []OrderRow
}

// groupRowsByOrder groups parsed CSV rows by PlatformOrderNo in encounter order.
func groupRowsByOrder(rows []OrderRow) (map[string]*orderGroup, []string) {
	orderMap := make(map[string]*orderGroup)
	var orderKeys []string
	for _, row := range rows {
		if _, exists := orderMap[row.PlatformOrderNo]; !exists {
			orderMap[row.PlatformOrderNo] = &orderGroup{
				orderDate: row.OrderDate,
				currency:  row.Currency,
			}
			orderKeys = append(orderKeys, row.PlatformOrderNo)
		}
		orderMap[row.PlatformOrderNo].lines = append(orderMap[row.PlatformOrderNo].lines, row)
	}
	return orderMap, orderKeys
}

// Execute parses the CSV, resolves SKUs, deduplicates, converts FX, and commits one sale bill per order.
func (uc *ImportOrdersUseCase) Execute(ctx context.Context, req ImportRequest) (*ImportResult, error) {
	if req.TenantID == uuid.Nil {
		return nil, fmt.Errorf("importing: tenant_id is required")
	}
	if req.CreatorID == uuid.Nil {
		return nil, fmt.Errorf("importing: creator_id is required")
	}
	if req.WarehouseID == uuid.Nil {
		return nil, fmt.Errorf("importing: warehouse_id is required")
	}
	if err := req.Platform.Validate(); err != nil {
		return nil, err
	}
	if len(req.CSVData) == 0 {
		return nil, fmt.Errorf("importing: csv_data is empty")
	}
	if uc.whChecker != nil {
		if err := uc.whChecker.BelongsToTenant(ctx, req.TenantID, req.WarehouseID); err != nil {
			return nil, fmt.Errorf("importing: warehouse not in tenant: %w", err)
		}
	}

	rows, err := parseCSV(req.Platform, req.CSVData)
	if err != nil {
		return nil, fmt.Errorf("importing: parse csv: %w", err)
	}

	hintMap := buildHintMap(req.SKUHints)
	orderMap, orderKeys := groupRowsByOrder(rows)
	result := &ImportResult{}

	for _, orderNo := range orderKeys {
		if err := uc.executeBatchOrder(ctx, req, orderNo, orderMap[orderNo], hintMap, result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// executeBatchOrder processes a single order group within Execute's CSV batch loop.
func (uc *ImportOrdersUseCase) executeBatchOrder(
	ctx context.Context,
	req ImportRequest,
	orderNo string,
	grp *orderGroup,
	hintMap map[string]uuid.UUID,
	result *ImportResult,
) error {
	seen, existingBillID, err := uc.repo.IsOrderSeen(ctx, req.TenantID, string(req.Platform), orderNo)
	if err != nil {
		return fmt.Errorf("importing: dedup check for order %s: %w", orderNo, err)
	}
	if seen {
		result.Skipped = append(result.Skipped, SkippedOrder{
			PlatformOrderNo: orderNo,
			Reason:          fmt.Sprintf("duplicate:bill_id=%s", existingBillID),
		})
		return nil
	}

	resolved := make([]resolvedOrderLine, 0, len(grp.lines))
	hasUnknown := false
	for _, row := range grp.lines {
		productID, unknown, err := uc.resolveSKU(ctx, req.TenantID, string(req.Platform), row.PlatformSKU, hintMap)
		if err != nil {
			return fmt.Errorf("importing: resolve sku %s: %w", row.PlatformSKU, err)
		}
		if unknown {
			result.UnknownSKUs = appendUniqueSKU(result.UnknownSKUs, UnknownSKU{
				Platform:    string(req.Platform),
				PlatformSKU: row.PlatformSKU,
			})
			hasUnknown = true
		}
		resolved = append(resolved, resolvedOrderLine{row: row, productID: productID})
	}
	if hasUnknown {
		result.Skipped = append(result.Skipped, SkippedOrder{PlatformOrderNo: orderNo, Reason: "unknown_sku"})
		return nil
	}

	convertedLines, err := uc.convertOrderLines(ctx, req.TenantID, orderNo, resolved)
	if err != nil {
		return err
	}

	if req.DryRun {
		oversells, err := uc.checkOversellLines(ctx, req.TenantID, req.WarehouseID, orderNo, convertedLines)
		if err != nil {
			return err
		}
		result.Oversells = append(result.Oversells, oversells...)
		result.Imported = append(result.Imported, ImportedOrder{
			PlatformOrderNo: orderNo,
			BillID:          uuid.Nil,
			BillNo:          "(preview)",
		})
		return nil
	}

	warehouseID := req.WarehouseID
	out, err := uc.saleCreator.Create(ctx, SaleCreatorInput{
		TenantID:    req.TenantID,
		CreatorID:   req.CreatorID,
		WarehouseID: &warehouseID,
		BillDate:    grp.orderDate,
		Remark:      fmt.Sprintf("import:%s:%s", req.Platform, orderNo),
		Items:       toSaleLineItems(convertedLines),
	})
	if err != nil {
		return fmt.Errorf("importing: create sale bill for order %s: %w", orderNo, err)
	}

	if err := uc.saleApprover.Approve(ctx, SaleApproverInput{
		TenantID:  req.TenantID,
		BillID:    out.BillID,
		CreatorID: req.CreatorID,
	}); err != nil {
		result.Skipped = append(result.Skipped, SkippedOrder{
			PlatformOrderNo: orderNo,
			Reason:          fmt.Sprintf("approve_failed:%s", err.Error()),
		})
		return nil
	}

	if err := uc.repo.MarkOrderSeen(ctx, req.TenantID, string(req.Platform), orderNo, out.BillID); err != nil {
		result.Imported = append(result.Imported, ImportedOrder{
			PlatformOrderNo: orderNo,
			BillID:          out.BillID,
			BillNo:          out.BillNo,
			MarkSeenError:   err.Error(),
		})
		return nil
	}

	if err := uc.persistHintMappings(ctx, req.TenantID, string(req.Platform), resolved, hintMap); err != nil {
		return err
	}

	result.Imported = append(result.Imported, ImportedOrder{
		PlatformOrderNo: orderNo,
		BillID:          out.BillID,
		BillNo:          out.BillNo,
	})
	return nil
}
