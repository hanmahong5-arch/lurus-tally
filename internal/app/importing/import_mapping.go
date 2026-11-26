package importing

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// resolvedOrderLine pairs an OrderRow with its resolved Tally product ID.
type resolvedOrderLine struct {
	row       OrderRow
	productID uuid.UUID
}

// convertedOrderLine holds a line whose unit price has been converted into targetCurrency.
type convertedOrderLine struct {
	productID   uuid.UUID
	platformSKU string
	qty         decimal.Decimal
	unitPrice   decimal.Decimal
}

// buildHintMap indexes caller-supplied SKU hints by platform SKU.
func buildHintMap(hints []SKUHint) map[string]uuid.UUID {
	hintMap := make(map[string]uuid.UUID, len(hints))
	for _, h := range hints {
		hintMap[h.PlatformSKU] = h.ProductID
	}
	return hintMap
}

// resolveSKU resolves a platformSKU to a product_id via hintMap first, then import_sku_map.
func (uc *ImportOrdersUseCase) resolveSKU(
	ctx context.Context,
	tenantID uuid.UUID,
	platform, platformSKU string,
	hintMap map[string]uuid.UUID,
) (uuid.UUID, bool, error) {
	if pid, ok := hintMap[platformSKU]; ok {
		return pid, false, nil
	}
	m, err := uc.repo.GetMapping(ctx, tenantID, platform, platformSKU)
	if err != nil {
		return uuid.Nil, false, err
	}
	if m == nil {
		return uuid.Nil, true, nil
	}
	return m.ProductID, false, nil
}

// appendUniqueSKU appends s to list only when its PlatformSKU is not already present.
func appendUniqueSKU(list []UnknownSKU, s UnknownSKU) []UnknownSKU {
	for _, existing := range list {
		if existing.PlatformSKU == s.PlatformSKU {
			return list
		}
	}
	return append(list, s)
}

// convertPriceFX converts price from currency into targetCurrency at date when needed.
func (uc *ImportOrdersUseCase) convertPriceFX(
	ctx context.Context,
	tenantID uuid.UUID,
	price decimal.Decimal,
	currency string,
	date time.Time,
) (decimal.Decimal, error) {
	srcCur := strings.ToUpper(currency)
	if srcCur == "" || srcCur == uc.targetCurrency {
		return price, nil
	}
	rate, err := uc.currencyRate.GetRate(ctx, tenantID, srcCur, uc.targetCurrency, date)
	if err != nil {
		return decimal.Zero, err
	}
	return price.Mul(rate).Round(4), nil
}

// convertOrderLines applies FX conversion to every resolved line of an order.
func (uc *ImportOrdersUseCase) convertOrderLines(
	ctx context.Context,
	tenantID uuid.UUID,
	orderNo string,
	resolved []resolvedOrderLine,
) ([]convertedOrderLine, error) {
	converted := make([]convertedOrderLine, 0, len(resolved))
	for _, rl := range resolved {
		price, err := uc.convertPriceFX(ctx, tenantID, rl.row.UnitPrice, rl.row.Currency, rl.row.OrderDate)
		if err != nil {
			return nil, fmt.Errorf("importing: fx for order %s: %w", orderNo, err)
		}
		converted = append(converted, convertedOrderLine{
			productID:   rl.productID,
			platformSKU: rl.row.PlatformSKU,
			qty:         rl.row.Qty,
			unitPrice:   price,
		})
	}
	return converted, nil
}

// checkOversellLines returns OversellRow entries for lines exceeding available warehouse stock.
func (uc *ImportOrdersUseCase) checkOversellLines(
	ctx context.Context,
	tenantID, warehouseID uuid.UUID,
	orderNo string,
	lines []convertedOrderLine,
) ([]OversellRow, error) {
	var oversells []OversellRow
	for _, cl := range lines {
		avail, err := uc.stockChecker.AvailableQty(ctx, tenantID, cl.productID, warehouseID)
		if err != nil {
			return nil, fmt.Errorf("importing: stock check for order %s product %s: %w", orderNo, cl.productID, err)
		}
		if cl.qty.GreaterThan(avail) {
			oversells = append(oversells, OversellRow{
				PlatformOrderNo: orderNo,
				PlatformSKU:     cl.platformSKU,
				ProductID:       cl.productID,
				Requested:       cl.qty,
				Available:       avail,
			})
		}
	}
	return oversells, nil
}

// persistHintMappings persists any caller-supplied SKU hints used in the resolved order lines.
func (uc *ImportOrdersUseCase) persistHintMappings(
	ctx context.Context,
	tenantID uuid.UUID,
	platform string,
	resolved []resolvedOrderLine,
	hintMap map[string]uuid.UUID,
) error {
	for _, rl := range resolved {
		if hintProductID, ok := hintMap[rl.row.PlatformSKU]; ok {
			if err := uc.repo.UpsertMapping(ctx, &SKUMapping{
				TenantID:    tenantID,
				Platform:    platform,
				PlatformSKU: rl.row.PlatformSKU,
				ProductID:   hintProductID,
			}); err != nil {
				return fmt.Errorf("importing: persist mapping for sku %s: %w", rl.row.PlatformSKU, err)
			}
		}
	}
	return nil
}

// toSaleLineItems converts convertedOrderLine values into numbered SaleLineItem values.
func toSaleLineItems(lines []convertedOrderLine) []SaleLineItem {
	items := make([]SaleLineItem, len(lines))
	for i, cl := range lines {
		items[i] = SaleLineItem{
			ProductID: cl.productID,
			LineNo:    i + 1,
			Qty:       cl.qty,
			UnitPrice: cl.unitPrice,
		}
	}
	return items
}
