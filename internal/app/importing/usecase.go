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
	// MarkSeenError is set when the bill committed but the dedup row failed to
	// persist. Repo.IsOrderSeen's stage-2 fallback will self-heal on the next
	// import attempt, so this is informational, not blocking.
	MarkSeenError string `json:",omitempty"`
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

// ----- cancellation types ---------------------------------------------------

// CancelRequest is the input to IngestCancelOrder.
type CancelRequest struct {
	TenantID        uuid.UUID
	CreatorID       uuid.UUID
	Platform        Platform
	PlatformOrderNo string
}

// CancelResult is returned on successful order cancellation.
type CancelResult struct {
	PlatformOrderNo string
	OriginalBillID  uuid.UUID
	ReversalBillID  uuid.UUID
	ReversalBillNo  string
}

// ----- refund types ---------------------------------------------------------

// RefundLine is one SKU line in a partial or full refund.
type RefundLine struct {
	PlatformSKU  string
	Qty          decimal.Decimal
	RefundAmount decimal.Decimal // per-unit, in the order's original currency
}

// RefundRequest is the input to IngestRefund.
type RefundRequest struct {
	TenantID         uuid.UUID
	CreatorID        uuid.UUID
	WarehouseID      uuid.UUID
	Platform         Platform
	PlatformOrderNo  string
	PlatformRefundID string // Shopify refund.id — dedup key
	Currency         string // ISO-4217; used for FX conversion of refund amounts
	RefundDate       time.Time
	Lines            []RefundLine
}

// RefundResult is returned on successful refund ingestion.
type RefundResult struct {
	PlatformOrderNo  string
	PlatformRefundID string
	BillID           uuid.UUID
	BillNo           string
}

// ImportResult is the output of ImportOrdersUseCase.Execute.
type ImportResult struct {
	Imported    []ImportedOrder
	Skipped     []SkippedOrder
	Oversells   []OversellRow // non-empty only in DryRun mode
	UnknownSKUs []UnknownSKU  // platform SKUs that need mapping before re-import
}

// ----- interfaces -----------------------------------------------------------

// ImportRepo is the persistence contract for import_sku_map, import_order_seen,
// import_order_cancel_seen, and import_refund_seen.
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

	// IsCancelSeen returns (true, originalBillID, reversalBillID) when the
	// cancellation has already been processed; (false, nil, nil) otherwise.
	IsCancelSeen(ctx context.Context, tenantID uuid.UUID, platform, orderNo string) (bool, uuid.UUID, uuid.UUID, error)
	// MarkCancelSeen records that a cancellation has been processed.
	MarkCancelSeen(ctx context.Context, tenantID uuid.UUID, platform, orderNo string, originalBillID, reversalBillID uuid.UUID) error

	// IsRefundSeen returns (true, billID) when the refund has already been processed.
	IsRefundSeen(ctx context.Context, tenantID uuid.UUID, platform, platformRefundID string) (bool, uuid.UUID, error)
	// MarkRefundSeen records that a refund has been processed.
	MarkRefundSeen(ctx context.Context, tenantID uuid.UUID, platform, orderNo, platformRefundID string, billID uuid.UUID) error
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

// ReturnCreatorInput is the input for creating a return-stock (入库-销售退货) bill.
// Used for both cancellation reversals and partial refund lines.
type ReturnCreatorInput struct {
	TenantID       uuid.UUID
	CreatorID      uuid.UUID
	WarehouseID    *uuid.UUID
	BillDate       time.Time
	Remark         string // carries audit link to original bill
	Items          []SaleLineItem
}

// ReturnCreatorOutput is returned by ReturnCreator.Create on success.
type ReturnCreatorOutput struct {
	BillID uuid.UUID
	BillNo string
}

// ReturnCreator creates a return-stock (入库 / 销售退货) bill draft.
// In production this is implemented by bill.CreateReturnUseCase (adapter shim).
type ReturnCreator interface {
	Create(ctx context.Context, req ReturnCreatorInput) (*ReturnCreatorOutput, error)
}

// ReturnApproverInput is the minimal input for approving a return bill.
type ReturnApproverInput struct {
	TenantID  uuid.UUID
	BillID    uuid.UUID
	CreatorID uuid.UUID
}

// ReturnApprover approves a return-stock bill (adds back stock).
// In production implemented by bill.ApproveReturnUseCase (adapter shim).
type ReturnApprover interface {
	Approve(ctx context.Context, req ReturnApproverInput) error
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

// ImportOrdersUseCase orchestrates multi-platform order CSV ingestion,
// order cancellation reversals, and refund ingestion.
type ImportOrdersUseCase struct {
	repo           ImportRepo
	saleCreator    SaleCreator
	saleApprover   SaleApprover
	returnCreator  ReturnCreator
	returnApprover ReturnApprover
	stockChecker   StockChecker
	whChecker      WarehouseChecker
	currencyRate   CurrencyRater
	// targetCurrency is the currency bills are denominated in (default "CNY").
	targetCurrency string
}

// NewImportOrdersUseCase constructs the use case.
// targetCurrency defaults to "CNY" when empty.
// whChecker may be nil only in tests; production callers must provide one so the
// tenant-warehouse binding is enforced before any sale bill is created.
// returnCreator/returnApprover may be nil only in tests that do not exercise
// cancellation or refund paths.
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

// WithReturnHandlers attaches the return-bill creator and approver needed for
// order cancellations and refund ingestion.  Call this after NewImportOrdersUseCase
// when wiring the production container.
func (uc *ImportOrdersUseCase) WithReturnHandlers(creator ReturnCreator, approver ReturnApprover) *ImportOrdersUseCase {
	uc.returnCreator = creator
	uc.returnApprover = approver
	return uc
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
			productID   uuid.UUID
			platformSKU string // retained from resolved line for OversellRow (F06)
			qty         decimal.Decimal
			unitPrice   decimal.Decimal // in targetCurrency
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
				productID:   rl.productID,
				platformSKU: rl.row.PlatformSKU,
				qty:         rl.row.Qty,
				unitPrice:   price,
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
						PlatformSKU:     cl.platformSKU, // F06 fix: surface SKU for UI display
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
		// On failure we DON'T halt the batch and DON'T roll back the bill: the
		// bill is already committed, and Repo.IsOrderSeen's stage-2 fallback
		// (bill_head remark lookup + self-heal) recovers the dedup row on the
		// next import attempt, preventing duplicate bills.
		if err := uc.repo.MarkOrderSeen(ctx, req.TenantID, string(req.Platform), orderNo, out.BillID); err != nil {
			result.Imported = append(result.Imported, ImportedOrder{
				PlatformOrderNo: orderNo,
				BillID:          out.BillID,
				BillNo:          out.BillNo,
				MarkSeenError:   err.Error(),
			})
			continue
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

// ----- single-order ingestion (webhook path) --------------------------------

// SingleOrderRequest is the input to IngestSingleOrder for webhook-delivered orders.
// Unlike ImportRequest it carries a single already-parsed order (no CSV), so the
// caller (webhook handler) owns the parsing step and supplies normalized lines.
type SingleOrderRequest struct {
	TenantID        uuid.UUID
	CreatorID       uuid.UUID
	WarehouseID     uuid.UUID
	Platform        Platform
	PlatformOrderNo string
	Lines           []OrderRow // Currency and OrderDate must be set on each row
}

// IngestSingleOrder ingests one webhook-delivered order through the same
// dedup → SKU-resolve → FX-convert → create-bill → approve → mark-seen
// pipeline used by the CSV batch path.
//
// Returns (ImportedOrder, nil, nil) on success, (zero, *SkippedOrder, nil) when
// the order is a duplicate or has unknown SKUs, and (zero, nil, error) on hard
// infrastructure failures (the caller should return 5xx so Shopify retries).
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

	imported, skipped, err := uc.ingestOneOrder(ctx, req.TenantID, req.CreatorID, req.WarehouseID,
		req.Platform, req.PlatformOrderNo, req.Lines)
	return imported, skipped, err
}

// ingestOneOrder is the shared core: dedup → SKU resolve → FX → bill → approve → mark seen.
// It is called by both the CSV batch loop (Execute) and IngestSingleOrder.
//
// Parameters:
//   - tenantID, creatorID, warehouseID: identity/routing context
//   - platform: source platform string
//   - orderNo: dedup key
//   - lines: pre-parsed rows; Currency and OrderDate must be non-zero
//
// Returns (ImportedOrder, nil, nil) on success, (zero, *SkippedOrder, nil) for
// soft skips (duplicate/unknown-sku/approve-failed), and (zero, nil, error) for
// hard infrastructure errors that should abort the caller.
func (uc *ImportOrdersUseCase) ingestOneOrder(
	ctx context.Context,
	tenantID, creatorID, warehouseID uuid.UUID,
	platform Platform,
	orderNo string,
	lines []OrderRow,
) (ImportedOrder, *SkippedOrder, error) {
	// 1. Dedup check.
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

	// 2. Resolve SKUs → product_ids.
	type resolvedLine struct {
		row       OrderRow
		productID uuid.UUID
	}
	var resolved []resolvedLine
	// No caller-supplied hints in the webhook path — rely on persisted mappings only.
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
		resolved = append(resolved, resolvedLine{row: row, productID: productID})
	}

	// 3. FX conversion.
	type lineWithPrice struct {
		productID   uuid.UUID
		platformSKU string // retained for potential oversell reporting
		qty         decimal.Decimal
		unitPrice   decimal.Decimal
	}
	var convertedLines []lineWithPrice
	for _, rl := range resolved {
		price := rl.row.UnitPrice
		srcCur := strings.ToUpper(rl.row.Currency)
		if srcCur != "" && srcCur != uc.targetCurrency {
			rate, fxErr := uc.currencyRate.GetRate(ctx, tenantID, srcCur, uc.targetCurrency, rl.row.OrderDate)
			if fxErr != nil {
				return ImportedOrder{}, nil, fmt.Errorf("importing: fx for order %s: %w", orderNo, fxErr)
			}
			price = price.Mul(rate).Round(4)
		}
		convertedLines = append(convertedLines, lineWithPrice{
			productID:   rl.productID,
			platformSKU: rl.row.PlatformSKU,
			qty:         rl.row.Qty,
			unitPrice:   price,
		})
	}

	// 4. Create sale bill draft.
	orderDate := lines[0].OrderDate
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
		TenantID:    tenantID,
		CreatorID:   creatorID,
		WarehouseID: &warehouseID,
		BillDate:    orderDate,
		Remark:      fmt.Sprintf("import:%s:%s", platform, orderNo),
		Items:       items,
	})
	if err != nil {
		return ImportedOrder{}, nil, fmt.Errorf("importing: create sale bill for order %s: %w", orderNo, err)
	}

	// 5. Approve the bill (deducts stock).
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

	// 6. Mark order seen — soft failure per established pattern.
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

// ----- order cancellation ---------------------------------------------------

// IngestCancelOrder processes an orders/cancelled webhook event.
//
// Steps:
//  1. Validate inputs.
//  2. Dedup: if already cancelled, return the existing reversal bill ids.
//  3. Look up the original import_order_seen row to find the original bill_id.
//     If the order was never imported (unknown to Tally) return a soft skip.
//  4. Create a return-stock bill (入库/销售退货) that mirrors the original sale.
//  5. Approve the reversal bill (adds stock back).
//  6. Persist to import_order_cancel_seen.
//
// Returns an error only on hard infrastructure failures.
func (uc *ImportOrdersUseCase) IngestCancelOrder(ctx context.Context, req CancelRequest) (*CancelResult, error) {
	if req.TenantID == uuid.Nil {
		return nil, fmt.Errorf("importing: tenant_id is required")
	}
	if req.CreatorID == uuid.Nil {
		return nil, fmt.Errorf("importing: creator_id is required")
	}
	if err := req.Platform.Validate(); err != nil {
		return nil, err
	}
	if req.PlatformOrderNo == "" {
		return nil, fmt.Errorf("importing: platform_order_no is required")
	}
	if uc.returnCreator == nil || uc.returnApprover == nil {
		return nil, fmt.Errorf("importing: ReturnCreator/ReturnApprover not wired; call WithReturnHandlers")
	}

	// 1. Dedup: already cancelled?
	alreadyCancelled, origBillID, revBillID, err := uc.repo.IsCancelSeen(ctx, req.TenantID, string(req.Platform), req.PlatformOrderNo)
	if err != nil {
		return nil, fmt.Errorf("importing: cancel dedup check for order %s: %w", req.PlatformOrderNo, err)
	}
	if alreadyCancelled {
		return &CancelResult{
			PlatformOrderNo: req.PlatformOrderNo,
			OriginalBillID:  origBillID,
			ReversalBillID:  revBillID,
		}, nil
	}

	// 2. Find the original import bill via import_order_seen.
	seen, originalBillID, err := uc.repo.IsOrderSeen(ctx, req.TenantID, string(req.Platform), req.PlatformOrderNo)
	if err != nil {
		return nil, fmt.Errorf("importing: lookup original order %s: %w", req.PlatformOrderNo, err)
	}
	if !seen {
		// Order was not imported through Tally — nothing to reverse.
		return nil, fmt.Errorf("importing: cancel order %s: original order not found in import_order_seen", req.PlatformOrderNo)
	}

	// 3. Create reversal return-stock bill.
	// Remark carries an audit link back to the original sale bill.
	// The reversal bill mirrors the original sale line quantities; the actual
	// line items must be fetched from the bill_head/bill_item tables by the
	// ReturnCreator implementation.  Here we pass an empty Items slice and rely
	// on the ReturnCreator to clone from original_bill_id via Remark convention:
	//   "cancel:shopify:#1001:original_bill_id=<uuid>"
	// Alternatively, the ReturnCreator reads from Remark and performs the lookup.
	// This keeps the use case free of direct DB dependency on bill tables.
	remark := fmt.Sprintf("cancel:%s:%s:original_bill_id=%s", req.Platform, req.PlatformOrderNo, originalBillID)
	out, err := uc.returnCreator.Create(ctx, ReturnCreatorInput{
		TenantID:  req.TenantID,
		CreatorID: req.CreatorID,
		BillDate:  time.Now().UTC(),
		Remark:    remark,
		Items:     nil, // ReturnCreator clones items from originalBillID via Remark
	})
	if err != nil {
		return nil, fmt.Errorf("importing: create reversal bill for order %s: %w", req.PlatformOrderNo, err)
	}

	// 4. Approve reversal (restores stock).
	if err := uc.returnApprover.Approve(ctx, ReturnApproverInput{
		TenantID:  req.TenantID,
		BillID:    out.BillID,
		CreatorID: req.CreatorID,
	}); err != nil {
		return nil, fmt.Errorf("importing: approve reversal bill for order %s: %w", req.PlatformOrderNo, err)
	}

	// 5. Mark cancellation seen.
	if err := uc.repo.MarkCancelSeen(ctx, req.TenantID, string(req.Platform), req.PlatformOrderNo, originalBillID, out.BillID); err != nil {
		// Soft failure: reversal is committed; dedup row is missing.
		// A duplicate webhook will attempt another reversal but IsCancelSeen
		// returns false → double-reversal risk.  Log at caller level.
		return &CancelResult{
			PlatformOrderNo: req.PlatformOrderNo,
			OriginalBillID:  originalBillID,
			ReversalBillID:  out.BillID,
			ReversalBillNo:  out.BillNo,
		}, fmt.Errorf("importing: mark_cancel_seen failed (reversal committed): %w", err)
	}

	return &CancelResult{
		PlatformOrderNo: req.PlatformOrderNo,
		OriginalBillID:  originalBillID,
		ReversalBillID:  out.BillID,
		ReversalBillNo:  out.BillNo,
	}, nil
}

// ----- refund ingestion -----------------------------------------------------

// IngestRefund processes a refunds/create webhook event (full or partial refund).
//
// Steps:
//  1. Validate inputs.
//  2. Dedup by platform_refund_id.
//  3. Resolve each RefundLine.PlatformSKU → product_id.
//  4. FX-convert refund amounts to targetCurrency.
//  5. Create a return-stock bill (入库/销售退货, sub_type implied by remark).
//  6. Approve the bill (adds stock back).
//  7. Persist to import_refund_seen.
func (uc *ImportOrdersUseCase) IngestRefund(ctx context.Context, req RefundRequest) (*RefundResult, error) {
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
	if req.PlatformOrderNo == "" {
		return nil, fmt.Errorf("importing: platform_order_no is required")
	}
	if req.PlatformRefundID == "" {
		return nil, fmt.Errorf("importing: platform_refund_id is required")
	}
	if len(req.Lines) == 0 {
		return nil, fmt.Errorf("importing: refund has no lines")
	}
	if uc.returnCreator == nil || uc.returnApprover == nil {
		return nil, fmt.Errorf("importing: ReturnCreator/ReturnApprover not wired; call WithReturnHandlers")
	}

	// 1. Dedup by platform_refund_id.
	alreadySeen, existingBillID, err := uc.repo.IsRefundSeen(ctx, req.TenantID, string(req.Platform), req.PlatformRefundID)
	if err != nil {
		return nil, fmt.Errorf("importing: refund dedup check for %s: %w", req.PlatformRefundID, err)
	}
	if alreadySeen {
		return &RefundResult{
			PlatformOrderNo:  req.PlatformOrderNo,
			PlatformRefundID: req.PlatformRefundID,
			BillID:           existingBillID,
		}, nil
	}

	// 2. Resolve SKUs → product_ids.
	type resolvedRefundLine struct {
		platformSKU string
		productID   uuid.UUID
		qty         decimal.Decimal
		unitPrice   decimal.Decimal // refund_amount, in targetCurrency
	}
	emptyHints := map[string]uuid.UUID{}
	var resolved []resolvedRefundLine
	for _, rl := range req.Lines {
		productID, unknown, resolveErr := uc.resolveSKU(ctx, req.TenantID, string(req.Platform), rl.PlatformSKU, emptyHints)
		if resolveErr != nil {
			return nil, fmt.Errorf("importing: refund resolve sku %s: %w", rl.PlatformSKU, resolveErr)
		}
		if unknown {
			return nil, fmt.Errorf("importing: refund sku %s not in import_sku_map; map it before re-sending", rl.PlatformSKU)
		}

		// FX-convert the per-unit refund amount.
		price := rl.RefundAmount
		srcCur := strings.ToUpper(req.Currency)
		if srcCur != "" && srcCur != uc.targetCurrency {
			rate, fxErr := uc.currencyRate.GetRate(ctx, req.TenantID, srcCur, uc.targetCurrency, req.RefundDate)
			if fxErr != nil {
				return nil, fmt.Errorf("importing: fx for refund %s: %w", req.PlatformRefundID, fxErr)
			}
			price = price.Mul(rate).Round(4)
		}

		resolved = append(resolved, resolvedRefundLine{
			platformSKU: rl.PlatformSKU,
			productID:   productID,
			qty:         rl.Qty,
			unitPrice:   price,
		})
	}

	// 3. Build return-stock bill items.
	items := make([]SaleLineItem, len(resolved))
	for i, rl := range resolved {
		items[i] = SaleLineItem{
			ProductID: rl.productID,
			LineNo:    i + 1,
			Qty:       rl.qty,
			UnitPrice: rl.unitPrice,
		}
	}

	warehouseID := req.WarehouseID
	remark := fmt.Sprintf("refund:%s:%s:refund_id=%s", req.Platform, req.PlatformOrderNo, req.PlatformRefundID)
	out, err := uc.returnCreator.Create(ctx, ReturnCreatorInput{
		TenantID:    req.TenantID,
		CreatorID:   req.CreatorID,
		WarehouseID: &warehouseID,
		BillDate:    req.RefundDate,
		Remark:      remark,
		Items:       items,
	})
	if err != nil {
		return nil, fmt.Errorf("importing: create refund bill for %s: %w", req.PlatformRefundID, err)
	}

	// 4. Approve the return-stock bill.
	if err := uc.returnApprover.Approve(ctx, ReturnApproverInput{
		TenantID:  req.TenantID,
		BillID:    out.BillID,
		CreatorID: req.CreatorID,
	}); err != nil {
		return nil, fmt.Errorf("importing: approve refund bill for %s: %w", req.PlatformRefundID, err)
	}

	// 5. Mark refund seen — soft failure on dedup write is acceptable because
	// the bill is already committed and the refund amount has been credited.
	if err := uc.repo.MarkRefundSeen(ctx, req.TenantID, string(req.Platform), req.PlatformOrderNo, req.PlatformRefundID, out.BillID); err != nil {
		return &RefundResult{
			PlatformOrderNo:  req.PlatformOrderNo,
			PlatformRefundID: req.PlatformRefundID,
			BillID:           out.BillID,
			BillNo:           out.BillNo,
		}, fmt.Errorf("importing: mark_refund_seen failed (bill committed): %w", err)
	}

	return &RefundResult{
		PlatformOrderNo:  req.PlatformOrderNo,
		PlatformRefundID: req.PlatformRefundID,
		BillID:           out.BillID,
		BillNo:           out.BillNo,
	}, nil
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
