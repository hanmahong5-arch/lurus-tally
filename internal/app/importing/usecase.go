// Package importing implements multi-platform order import (Amazon / Shopify CSV).
// V1 scope: CSV-only ingestion; API-direct import deferred to V2.
//
// Flow per order row:
//  1. Parse CSV into typed OrderRow values.
//  2. Resolve platform_sku → product_id via ImportRepo (persist new mappings learned
//     from caller-supplied SKUHints; unknown SKUs without hints are collected and
//     returned so the caller can surface a mapping UI before re-submitting).
//  3. Dedup by platform_order_no — skip orders already present in import_order_seen.
//  4. FX: convert unit_price to CNY via CurrencyRater on the order date.
//  5. Create a DRAFT sale bill via SaleCreator, then immediately approve it via
//     SaleApprover so stock is deducted.  Both happen per-order.
//  6. Mark the order seen in import_order_seen.
//
// Preview mode (DryRun=true): steps 5-6 are skipped; oversell rows (qty > available)
// are flagged and returned without any DB writes beyond step 1-2 resolution reads.
package importing

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ----- domain types ---------------------------------------------------------

// Platform identifies the source e-commerce platform.
type Platform string

const (
	PlatformAmazon  Platform = "amazon"
	PlatformShopify Platform = "shopify"
)

// Validate returns an error when the platform value is unrecognised.
func (p Platform) Validate() error {
	switch p {
	case PlatformAmazon, PlatformShopify:
		return nil
	default:
		return fmt.Errorf("importing: unknown platform %q; expected amazon or shopify", string(p))
	}
}

// SKUMapping is the persisted record linking a platform SKU to a Tally product.
type SKUMapping struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	Platform    string
	PlatformSKU string
	ProductID   uuid.UUID
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// OrderRow is a single normalised record parsed from a platform CSV.
type OrderRow struct {
	PlatformOrderNo string
	PlatformSKU     string
	Qty             decimal.Decimal
	UnitPrice       decimal.Decimal
	Currency        string // ISO-4217, e.g. "USD"
	OrderDate       time.Time
}

// SKUHint is a caller-supplied mapping override: "this platform_sku should map
// to this product_id". Provided during the mapping-confirmation step of the UI
// when the import finds unknown SKUs.
type SKUHint struct {
	PlatformSKU string
	ProductID   uuid.UUID
}

// ImportRequest is the input to ImportOrdersUseCase.Execute.
type ImportRequest struct {
	TenantID    uuid.UUID
	CreatorID   uuid.UUID
	WarehouseID uuid.UUID // destination warehouse for stock deduction
	Platform    Platform
	CSVData     []byte    // raw file bytes
	SKUHints    []SKUHint // optional caller-supplied mapping overrides
	DryRun      bool      // preview mode: no bills created, only oversell check
}

// ImportedOrder summarises one successfully imported order.
type ImportedOrder struct {
	PlatformOrderNo string
	BillID          uuid.UUID
	BillNo          string
}

// SkippedOrder records an order that was not imported and the reason.
type SkippedOrder struct {
	PlatformOrderNo string
	Reason          string // "duplicate" | "unknown_sku:<sku>" | "oversell:<product_id>"
}

// OversellRow is populated in preview (DryRun) mode for orders that would
// exceed the current available stock for at least one line item.
type OversellRow struct {
	PlatformOrderNo string
	PlatformSKU     string
	ProductID       uuid.UUID
	Requested       decimal.Decimal
	Available       decimal.Decimal
}

// UnknownSKU is a platform_sku that has no mapping and no caller-supplied hint.
type UnknownSKU struct {
	Platform    string
	PlatformSKU string
}

// ImportResult is the output of ImportOrdersUseCase.Execute.
type ImportResult struct {
	Imported    []ImportedOrder
	Skipped     []SkippedOrder
	Oversells   []OversellRow // non-empty only in DryRun mode
	UnknownSKUs []UnknownSKU  // platform SKUs that need mapping before re-import
}

// ----- interfaces -----------------------------------------------------------

// ImportRepo is the persistence contract for import_sku_map and import_order_seen.
type ImportRepo interface {
	// GetMapping returns a SKU mapping or nil when none exists.
	GetMapping(ctx context.Context, tenantID uuid.UUID, platform, platformSKU string) (*SKUMapping, error)
	// UpsertMapping creates or updates a SKU mapping.
	UpsertMapping(ctx context.Context, m *SKUMapping) error
	// ListMappings returns all mappings for the tenant, optionally filtered by platform.
	ListMappings(ctx context.Context, tenantID uuid.UUID, platform string) ([]SKUMapping, error)
	// IsOrderSeen returns true + the bill_id when the order has already been imported.
	IsOrderSeen(ctx context.Context, tenantID uuid.UUID, platform, orderNo string) (bool, uuid.UUID, error)
	// MarkOrderSeen records that an order has been imported as bill billID.
	MarkOrderSeen(ctx context.Context, tenantID uuid.UUID, platform, orderNo string, billID uuid.UUID) error
}

// SaleCreatorInput is the minimal input needed to create a sale bill draft.
// Mirrors bill.CreateSaleRequest so the supervisor can wire the real use case.
type SaleCreatorInput struct {
	TenantID    uuid.UUID
	CreatorID   uuid.UUID
	WarehouseID *uuid.UUID
	BillDate    time.Time
	Remark      string
	Items       []SaleLineItem
}

// SaleLineItem is a single line in a sale bill draft.
type SaleLineItem struct {
	ProductID uuid.UUID
	LineNo    int
	Qty       decimal.Decimal
	UnitPrice decimal.Decimal
}

// SaleCreatorOutput is returned by SaleCreator.Create on success.
type SaleCreatorOutput struct {
	BillID uuid.UUID
	BillNo string
}

// SaleCreator is the minimal interface for creating a sale bill draft.
// Implemented in production by bill.CreateSaleUseCase (via an adapter shim).
type SaleCreator interface {
	Create(ctx context.Context, req SaleCreatorInput) (*SaleCreatorOutput, error)
}

// SaleApproverInput is the minimal input for approving a sale bill.
type SaleApproverInput struct {
	TenantID  uuid.UUID
	BillID    uuid.UUID
	CreatorID uuid.UUID
}

// SaleApprover is the minimal interface for approving a sale bill.
// Implemented in production by bill.ApproveSaleUseCase (via an adapter shim).
type SaleApprover interface {
	Approve(ctx context.Context, req SaleApproverInput) error
}

// StockChecker returns the current available quantity for a product/warehouse.
// Used in preview mode to flag potential oversell without touching any bill.
type StockChecker interface {
	AvailableQty(ctx context.Context, tenantID, productID, warehouseID uuid.UUID) (decimal.Decimal, error)
}

// WarehouseChecker validates that warehouseID belongs to tenantID. It is the
// gate that prevents a caller from passing another tenant's warehouse UUID and
// silently writing stock into that tenant. Implemented in lifecycle over the
// warehouse repo's tenant-scoped GetByID.
type WarehouseChecker interface {
	BelongsToTenant(ctx context.Context, tenantID, warehouseID uuid.UUID) error
}

// CurrencyRater converts an amount from one currency to another on a given date.
// Returns rate=1 and a warning when no rate data is available (graceful degradation).
type CurrencyRater interface {
	GetRate(ctx context.Context, tenantID uuid.UUID, from, to string, date time.Time) (decimal.Decimal, error)
}

// ----- use case -------------------------------------------------------------

// ImportOrdersUseCase orchestrates multi-platform order CSV ingestion.
type ImportOrdersUseCase struct {
	repo         ImportRepo
	saleCreator  SaleCreator
	saleApprover SaleApprover
	stockChecker StockChecker
	whChecker    WarehouseChecker
	currencyRate CurrencyRater
	// targetCurrency is the currency bills are denominated in (default "CNY").
	targetCurrency string
}

// NewImportOrdersUseCase constructs the use case.
// targetCurrency defaults to "CNY" when empty.
// whChecker may be nil only in tests; production callers must provide one so the
// tenant-warehouse binding is enforced before any sale bill is created.
func NewImportOrdersUseCase(
	repo ImportRepo,
	saleCreator SaleCreator,
	saleApprover SaleApprover,
	stockChecker StockChecker,
	whChecker WarehouseChecker,
	currencyRate CurrencyRater,
	targetCurrency string,
) *ImportOrdersUseCase {
	if targetCurrency == "" {
		targetCurrency = "CNY"
	}
	return &ImportOrdersUseCase{
		repo:           repo,
		saleCreator:    saleCreator,
		saleApprover:   saleApprover,
		stockChecker:   stockChecker,
		whChecker:      whChecker,
		currencyRate:   currencyRate,
		targetCurrency: targetCurrency,
	}
}

// Execute parses the CSV, resolves SKUs, deduplicates, optionally converts FX,
// and creates + approves one sale bill per unique order.
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

	// Guard against cross-tenant warehouse_id: a malicious caller could otherwise
	// pass another tenant's warehouse UUID and silently deduct stock there.
	if uc.whChecker != nil {
		if err := uc.whChecker.BelongsToTenant(ctx, req.TenantID, req.WarehouseID); err != nil {
			return nil, fmt.Errorf("importing: warehouse not in tenant: %w", err)
		}
	}

	// 1. Parse CSV into rows.
	rows, err := parseCSV(req.Platform, req.CSVData)
	if err != nil {
		return nil, fmt.Errorf("importing: parse csv: %w", err)
	}

	// Build a lookup from hints so we don't call the DB for each hint SKU.
	hintMap := make(map[string]uuid.UUID, len(req.SKUHints))
	for _, h := range req.SKUHints {
		hintMap[h.PlatformSKU] = h.ProductID
	}

	// Group rows by order number so each order becomes one bill.
	type orderGroup struct {
		orderDate time.Time
		currency  string
		lines     []OrderRow
	}
	orderMap := make(map[string]*orderGroup)
	var orderKeys []string // preserve encounter order
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

	result := &ImportResult{}

	for _, orderNo := range orderKeys {
		grp := orderMap[orderNo]

		// 2. Dedup check.
		seen, existingBillID, err := uc.repo.IsOrderSeen(ctx, req.TenantID, string(req.Platform), orderNo)
		if err != nil {
			return nil, fmt.Errorf("importing: dedup check for order %s: %w", orderNo, err)
		}
		if seen {
			result.Skipped = append(result.Skipped, SkippedOrder{
				PlatformOrderNo: orderNo,
				Reason:          fmt.Sprintf("duplicate:bill_id=%s", existingBillID),
			})
			continue
		}

		// 3. Resolve SKUs → product_ids for every line in this order.
		type resolvedLine struct {
			row       OrderRow
			productID uuid.UUID
		}
		var resolved []resolvedLine
		hasUnknown := false

		for _, row := range grp.lines {
			productID, unknown, err := uc.resolveSKU(ctx, req.TenantID, string(req.Platform), row.PlatformSKU, hintMap)
			if err != nil {
				return nil, fmt.Errorf("importing: resolve sku %s: %w", row.PlatformSKU, err)
			}
			if unknown {
				result.UnknownSKUs = appendUniqueSKU(result.UnknownSKUs, UnknownSKU{
					Platform:    string(req.Platform),
					PlatformSKU: row.PlatformSKU,
				})
				hasUnknown = true
			}
			resolved = append(resolved, resolvedLine{row: row, productID: productID})
		}
		if hasUnknown {
			result.Skipped = append(result.Skipped, SkippedOrder{
				PlatformOrderNo: orderNo,
				Reason:          "unknown_sku",
			})
			continue
		}

		// 4. FX conversion: convert unit_price to targetCurrency.
		type lineWithPrice struct {
			productID uuid.UUID
			qty       decimal.Decimal
			unitPrice decimal.Decimal // in targetCurrency
		}
		var convertedLines []lineWithPrice

		for _, rl := range resolved {
			price := rl.row.UnitPrice
			srcCur := strings.ToUpper(rl.row.Currency)
			if srcCur != "" && srcCur != uc.targetCurrency {
				rate, err := uc.currencyRate.GetRate(ctx, req.TenantID, srcCur, uc.targetCurrency, rl.row.OrderDate)
				if err != nil {
					return nil, fmt.Errorf("importing: fx for order %s: %w", orderNo, err)
				}
				price = price.Mul(rate).Round(4)
			}
			convertedLines = append(convertedLines, lineWithPrice{
				productID: rl.productID,
				qty:       rl.row.Qty,
				unitPrice: price,
			})
		}

		// 5a. Preview (DryRun) mode: check oversell without writing.
		if req.DryRun {
			for _, cl := range convertedLines {
				avail, err := uc.stockChecker.AvailableQty(ctx, req.TenantID, cl.productID, req.WarehouseID)
				if err != nil {
					return nil, fmt.Errorf("importing: stock check for order %s product %s: %w", orderNo, cl.productID, err)
				}
				if cl.qty.GreaterThan(avail) {
					result.Oversells = append(result.Oversells, OversellRow{
						PlatformOrderNo: orderNo,
						PlatformSKU:     "", // resolved away; caller cross-refs by productID
						ProductID:       cl.productID,
						Requested:       cl.qty,
						Available:       avail,
					})
				}
			}
			// In dry-run mode we record the order as "processed" for display but
			// do not create a bill.
			result.Imported = append(result.Imported, ImportedOrder{
				PlatformOrderNo: orderNo,
				BillID:          uuid.Nil,
				BillNo:          "(preview)",
			})
			continue
		}

		// 5b. Create and approve the sale bill.
		warehouseID := req.WarehouseID
		items := make([]SaleLineItem, len(convertedLines))
		for i, cl := range convertedLines {
			items[i] = SaleLineItem{
				ProductID: cl.productID,
				LineNo:    i + 1,
				Qty:       cl.qty,
				UnitPrice: cl.unitPrice,
			}
		}

		out, err := uc.saleCreator.Create(ctx, SaleCreatorInput{
			TenantID:    req.TenantID,
			CreatorID:   req.CreatorID,
			WarehouseID: &warehouseID,
			BillDate:    grp.orderDate,
			Remark:      fmt.Sprintf("import:%s:%s", req.Platform, orderNo),
			Items:       items,
		})
		if err != nil {
			return nil, fmt.Errorf("importing: create sale bill for order %s: %w", orderNo, err)
		}

		if err := uc.saleApprover.Approve(ctx, SaleApproverInput{
			TenantID:  req.TenantID,
			BillID:    out.BillID,
			CreatorID: req.CreatorID,
		}); err != nil {
			// Approval failure (e.g. oversell): report as skipped so caller can surface the error.
			result.Skipped = append(result.Skipped, SkippedOrder{
				PlatformOrderNo: orderNo,
				Reason:          fmt.Sprintf("approve_failed:%s", err.Error()),
			})
			continue
		}

		// 6. Mark order seen — idempotent on concurrent re-import.
		if err := uc.repo.MarkOrderSeen(ctx, req.TenantID, string(req.Platform), orderNo, out.BillID); err != nil {
			// Non-fatal: bill was created. Log the anomaly via error return is safest;
			// supervisor should surface this but not roll back the bill.
			return nil, fmt.Errorf("importing: mark order seen for %s (bill %s): %w", orderNo, out.BillID, err)
		}

		// Persist any new mappings learned from hints that were used for this order.
		for _, rl := range resolved {
			if hintProductID, ok := hintMap[rl.row.PlatformSKU]; ok {
				if err := uc.repo.UpsertMapping(ctx, &SKUMapping{
					TenantID:    req.TenantID,
					Platform:    string(req.Platform),
					PlatformSKU: rl.row.PlatformSKU,
					ProductID:   hintProductID,
				}); err != nil {
					return nil, fmt.Errorf("importing: persist mapping for sku %s: %w", rl.row.PlatformSKU, err)
				}
			}
		}

		result.Imported = append(result.Imported, ImportedOrder{
			PlatformOrderNo: orderNo,
			BillID:          out.BillID,
			BillNo:          out.BillNo,
		})
	}

	return result, nil
}

// ListMappings delegates to the repo for the GET /mappings endpoint.
func (uc *ImportOrdersUseCase) ListMappings(ctx context.Context, tenantID uuid.UUID, platform string) ([]SKUMapping, error) {
	return uc.repo.ListMappings(ctx, tenantID, platform)
}

// ----- helpers --------------------------------------------------------------

// resolveSKU resolves a platformSKU to a product_id using (in order):
//  1. Caller-supplied hints (hintMap) — highest priority.
//  2. Persisted mapping in import_sku_map.
//
// Returns (productID, false, nil) when resolved, (uuid.Nil, true, nil) when unknown.
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

// appendUniqueSKU appends s to list only when it is not already present.
func appendUniqueSKU(list []UnknownSKU, s UnknownSKU) []UnknownSKU {
	for _, existing := range list {
		if existing.PlatformSKU == s.PlatformSKU {
			return list
		}
	}
	return append(list, s)
}

// ----- CSV parsers ----------------------------------------------------------

// parseCSV dispatches to the platform-specific parser.
func parseCSV(p Platform, data []byte) ([]OrderRow, error) {
	switch p {
	case PlatformAmazon:
		return parseAmazonCSV(data)
	case PlatformShopify:
		return parseShopifyCSV(data)
	default:
		return nil, fmt.Errorf("importing: no parser for platform %q", string(p))
	}
}

// Amazon CSV expected columns (case-insensitive header):
// order-id, asin/sku, quantity-purchased, item-price, currency, purchase-date
func parseAmazonCSV(data []byte) ([]OrderRow, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.TrimLeadingSpace = true

	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("amazon csv: read header: %w", err)
	}
	idx, err := columnIndex(header, map[string][]string{
		"order_id":   {"order-id", "order_id", "orderid"},
		"sku":        {"sku", "asin", "listing-sku", "merchant-sku"},
		"qty":        {"quantity-purchased", "quantity", "qty"},
		"unit_price": {"item-price", "unit-price", "price"},
		"currency":   {"currency"},
		"order_date": {"purchase-date", "order-date", "date"},
	})
	if err != nil {
		return nil, fmt.Errorf("amazon csv: %w", err)
	}

	var rows []OrderRow
	lineNo := 1
	for {
		lineNo++
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("amazon csv: line %d: %w", lineNo, err)
		}
		row, err := buildRow(rec, idx)
		if err != nil {
			return nil, fmt.Errorf("amazon csv: line %d: %w", lineNo, err)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// Shopify CSV expected columns:
// Name, Lineitem sku, Lineitem quantity, Lineitem price, Currency, Created at
func parseShopifyCSV(data []byte) ([]OrderRow, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.TrimLeadingSpace = true

	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("shopify csv: read header: %w", err)
	}
	idx, err := columnIndex(header, map[string][]string{
		"order_id":   {"name", "order_name", "order-name", "order_id", "order-id"},
		"sku":        {"lineitem sku", "lineitem_sku", "sku", "variant sku"},
		"qty":        {"lineitem quantity", "lineitem_quantity", "quantity", "qty"},
		"unit_price": {"lineitem price", "lineitem_price", "price"},
		"currency":   {"currency"},
		"order_date": {"created at", "created_at", "order_date"},
	})
	if err != nil {
		return nil, fmt.Errorf("shopify csv: %w", err)
	}

	var rows []OrderRow
	lineNo := 1
	for {
		lineNo++
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("shopify csv: line %d: %w", lineNo, err)
		}
		// Shopify may have blank SKU lines for shipping/fees — skip them.
		if idx["sku"] < len(rec) && strings.TrimSpace(rec[idx["sku"]]) == "" {
			continue
		}
		row, err := buildRow(rec, idx)
		if err != nil {
			return nil, fmt.Errorf("shopify csv: line %d: %w", lineNo, err)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// columnIndex matches header columns (case-insensitive, trimmed) against each
// fieldName's list of aliases, returning a map of fieldName → column index.
// Returns an error when a required field has no matching column.
func columnIndex(header []string, fields map[string][]string) (map[string]int, error) {
	normalised := make([]string, len(header))
	for i, h := range header {
		normalised[i] = strings.ToLower(strings.TrimSpace(h))
	}

	result := make(map[string]int, len(fields))
	for field, aliases := range fields {
		found := false
		for _, alias := range aliases {
			for i, h := range normalised {
				if h == alias {
					result[field] = i
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("missing required column %q (tried: %s)", field, strings.Join(aliases, ", "))
		}
	}
	return result, nil
}

// buildRow converts a CSV record into an OrderRow using the resolved column index.
func buildRow(rec []string, idx map[string]int) (OrderRow, error) {
	get := func(field string) string {
		i, ok := idx[field]
		if !ok || i >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[i])
	}

	qty, err := decimal.NewFromString(get("qty"))
	if err != nil || qty.IsNegative() || qty.IsZero() {
		return OrderRow{}, fmt.Errorf("invalid qty %q", get("qty"))
	}

	price, err := decimal.NewFromString(get("unit_price"))
	if err != nil || price.IsNegative() {
		return OrderRow{}, fmt.Errorf("invalid unit_price %q", get("unit_price"))
	}

	var orderDate time.Time
	rawDate := get("order_date")
	for _, layout := range []string{
		time.RFC3339, "2006-01-02T15:04:05-07:00",
		"2006-01-02 15:04:05", "2006-01-02",
		"01/02/2006", "2006/01/02",
	} {
		if t, err := time.Parse(layout, rawDate); err == nil {
			orderDate = t.UTC()
			break
		}
	}
	if orderDate.IsZero() {
		return OrderRow{}, fmt.Errorf("cannot parse order_date %q", rawDate)
	}

	return OrderRow{
		PlatformOrderNo: get("order_id"),
		PlatformSKU:     get("sku"),
		Qty:             qty,
		UnitPrice:       price,
		Currency:        strings.ToUpper(get("currency")),
		OrderDate:       orderDate,
	}, nil
}
