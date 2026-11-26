// Package importing implements multi-platform order import, webhook ingestion,
// order cancellation reversals, and refund stock restoration.
package importing

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

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

// OrderRow is a single normalised record parsed from a platform CSV or webhook.
type OrderRow struct {
	PlatformOrderNo string
	PlatformSKU     string
	Qty             decimal.Decimal
	UnitPrice       decimal.Decimal
	Currency        string
	OrderDate       time.Time
}

// SKUHint is a caller-supplied mapping override from platform_sku to product_id.
type SKUHint struct {
	PlatformSKU string
	ProductID   uuid.UUID
}

// ImportRequest is the input to ImportOrdersUseCase.Execute.
type ImportRequest struct {
	TenantID    uuid.UUID
	CreatorID   uuid.UUID
	WarehouseID uuid.UUID
	Platform    Platform
	CSVData     []byte
	SKUHints    []SKUHint
	DryRun      bool
}

// ImportedOrder summarises one successfully imported order.
type ImportedOrder struct {
	PlatformOrderNo string
	BillID          uuid.UUID
	BillNo          string
	MarkSeenError   string `json:",omitempty"`
}

// SkippedOrder records an order that was not imported and the reason.
type SkippedOrder struct {
	PlatformOrderNo string
	Reason          string
}

// OversellRow is populated in preview mode for orders exceeding available stock.
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
	Oversells   []OversellRow
	UnknownSKUs []UnknownSKU
}

// ImportRepo is the persistence contract for SKU mappings and order/cancel/refund dedup.
type ImportRepo interface {
	GetMapping(ctx context.Context, tenantID uuid.UUID, platform, platformSKU string) (*SKUMapping, error)
	UpsertMapping(ctx context.Context, m *SKUMapping) error
	ListMappings(ctx context.Context, tenantID uuid.UUID, platform string) ([]SKUMapping, error)
	IsOrderSeen(ctx context.Context, tenantID uuid.UUID, platform, orderNo string) (bool, uuid.UUID, error)
	MarkOrderSeen(ctx context.Context, tenantID uuid.UUID, platform, orderNo string, billID uuid.UUID) error
	IsCancelSeen(ctx context.Context, tenantID uuid.UUID, platform, orderNo string) (bool, uuid.UUID, uuid.UUID, error)
	MarkCancelSeen(ctx context.Context, tenantID uuid.UUID, platform, orderNo string, originalBillID, reversalBillID uuid.UUID) error
	IsRefundSeen(ctx context.Context, tenantID uuid.UUID, platform, platformRefundID string) (bool, uuid.UUID, error)
	MarkRefundSeen(ctx context.Context, tenantID uuid.UUID, platform, orderNo, platformRefundID string, billID uuid.UUID) error
}

// reversalBillNoLookup is an optional capability of ImportRepo for resolving reversal bill numbers.
type reversalBillNoLookup interface {
	ReversalBillNo(ctx context.Context, tenantID, billID uuid.UUID) (string, error)
}

// SaleCreatorInput is the minimal input needed to create a sale bill draft.
type SaleCreatorInput struct {
	TenantID    uuid.UUID
	CreatorID   uuid.UUID
	WarehouseID *uuid.UUID
	BillDate    time.Time
	Remark      string
	Items       []SaleLineItem
}

// SaleLineItem is a single line in a sale or return bill draft.
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
type SaleApprover interface {
	Approve(ctx context.Context, req SaleApproverInput) error
}

// ReturnCreatorInput is the input for creating a return-stock bill.
type ReturnCreatorInput struct {
	TenantID    uuid.UUID
	CreatorID   uuid.UUID
	WarehouseID *uuid.UUID
	BillDate    time.Time
	Remark      string
	Items       []SaleLineItem
}

// ReturnCreatorOutput is returned by ReturnCreator.Create on success.
type ReturnCreatorOutput struct {
	BillID uuid.UUID
	BillNo string
}

// ReturnCreator creates a return-stock bill draft.
type ReturnCreator interface {
	Create(ctx context.Context, req ReturnCreatorInput) (*ReturnCreatorOutput, error)
}

// ReturnApproverInput is the minimal input for approving a return bill.
type ReturnApproverInput struct {
	TenantID  uuid.UUID
	BillID    uuid.UUID
	CreatorID uuid.UUID
}

// ReturnApprover approves a return-stock bill.
type ReturnApprover interface {
	Approve(ctx context.Context, req ReturnApproverInput) error
}

// StockChecker returns the current available quantity for a product/warehouse.
type StockChecker interface {
	AvailableQty(ctx context.Context, tenantID, productID, warehouseID uuid.UUID) (decimal.Decimal, error)
}

// WarehouseChecker validates that warehouseID belongs to tenantID.
type WarehouseChecker interface {
	BelongsToTenant(ctx context.Context, tenantID, warehouseID uuid.UUID) error
}

// CurrencyRater converts an amount from one currency to another on a given date.
type CurrencyRater interface {
	GetRate(ctx context.Context, tenantID uuid.UUID, from, to string, date time.Time) (decimal.Decimal, error)
}

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
	targetCurrency string
}

// NewImportOrdersUseCase constructs the use case with targetCurrency defaulting to CNY.
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

// WithReturnHandlers attaches the return-bill creator and approver for cancellations and refunds.
func (uc *ImportOrdersUseCase) WithReturnHandlers(creator ReturnCreator, approver ReturnApprover) *ImportOrdersUseCase {
	uc.returnCreator = creator
	uc.returnApprover = approver
	return uc
}

// ListMappings delegates to the repo for the GET /mappings endpoint.
func (uc *ImportOrdersUseCase) ListMappings(ctx context.Context, tenantID uuid.UUID, platform string) ([]SKUMapping, error) {
	return uc.repo.ListMappings(ctx, tenantID, platform)
}
