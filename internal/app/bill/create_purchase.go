package bill

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// CreatePurchaseItemInput describes one line item in a purchase draft.
type CreatePurchaseItemInput struct {
	ProductID uuid.UUID
	UnitID    *uuid.UUID
	UnitName  string
	LineNo    int
	Qty       decimal.Decimal
	UnitPrice decimal.Decimal
}

// CreatePurchaseDraftRequest is the input to CreatePurchaseDraftUseCase.
type CreatePurchaseDraftRequest struct {
	TenantID    uuid.UUID
	CreatorID   uuid.UUID
	PartnerID   *uuid.UUID
	WarehouseID *uuid.UUID
	BillDate    time.Time
	ShippingFee decimal.Decimal
	TaxAmount   decimal.Decimal
	Remark      string
	Items       []CreatePurchaseItemInput

	// Multi-currency fields (Story 9.1). Optional; zero values = CNY domestic.
	// Currency is the original invoice currency code (e.g. "USD").
	// ExchangeRate is 1 orig-currency = ExchangeRate CNY.
	// When Currency is empty or "CNY", ExchangeRate is ignored and set to 1.
	Currency     string
	ExchangeRate decimal.Decimal
}

// CreatePurchaseDraftOutput is returned on success.
type CreatePurchaseDraftOutput struct {
	BillID uuid.UUID
	BillNo string
}

// CreatePurchaseDraftUseCase creates a new purchase bill in draft status.
type CreatePurchaseDraftUseCase struct {
	repo BillRepo
}

// NewCreatePurchaseDraftUseCase constructs the use case.
func NewCreatePurchaseDraftUseCase(repo BillRepo) *CreatePurchaseDraftUseCase {
	return &CreatePurchaseDraftUseCase{repo: repo}
}

// Execute validates and persists a new purchase draft.
func (uc *CreatePurchaseDraftUseCase) Execute(ctx context.Context, req CreatePurchaseDraftRequest) (*CreatePurchaseDraftOutput, error) {
	if req.TenantID == uuid.Nil {
		return nil, fmt.Errorf("%w: tenant_id is required", ErrValidation)
	}
	if req.CreatorID == uuid.Nil {
		return nil, fmt.Errorf("%w: creator_id is required", ErrValidation)
	}
	if len(req.Items) == 0 {
		return nil, fmt.Errorf("%w: at least one item is required", ErrValidation)
	}
	if req.BillDate.IsZero() {
		req.BillDate = time.Now().UTC()
	}

	// Build items and calculate totals.
	items := make([]*domain.BillItem, 0, len(req.Items))
	var subtotal decimal.Decimal
	for _, it := range req.Items {
		if it.ProductID == uuid.Nil {
			return nil, fmt.Errorf("%w: item product_id is required", ErrValidation)
		}
		if it.Qty.IsZero() || it.Qty.IsNegative() {
			return nil, fmt.Errorf("%w: item qty must be positive", ErrValidation)
		}
		if it.UnitPrice.IsNegative() {
			return nil, fmt.Errorf("%w: item unit_price must be non-negative", ErrValidation)
		}
		lineAmt := it.Qty.Mul(it.UnitPrice).Round(4)
		subtotal = subtotal.Add(lineAmt)

		bi := &domain.BillItem{
			ID:         uuid.New(),
			TenantID:   req.TenantID,
			ProductID:  it.ProductID,
			UnitID:     it.UnitID,
			UnitName:   it.UnitName,
			LineNo:     it.LineNo,
			Qty:        it.Qty,
			UnitPrice:  it.UnitPrice,
			LineAmount: lineAmt,
		}
		items = append(items, bi)
	}

	shippingFee := req.ShippingFee
	taxAmount := req.TaxAmount
	if shippingFee.IsNegative() {
		shippingFee = decimal.Zero
	}
	if taxAmount.IsNegative() {
		taxAmount = decimal.Zero
	}
	// totalInOrigCurrency is the sum in the original currency (before conversion).
	totalInOrigCurrency := subtotal.Add(shippingFee).Add(taxAmount).Round(4)

	// Resolve multi-currency fields.
	currency := req.Currency
	if currency == "" {
		currency = "CNY"
	}
	exchangeRate := req.ExchangeRate
	var amountLocal decimal.Decimal
	var totalAmountCNY decimal.Decimal

	if currency == "CNY" {
		// Domestic: no conversion needed.
		exchangeRate = decimal.NewFromInt(1)
		amountLocal = totalInOrigCurrency
		totalAmountCNY = totalInOrigCurrency
	} else {
		// Foreign currency: validate exchange rate.
		if exchangeRate.IsZero() || exchangeRate.IsNegative() {
			return nil, fmt.Errorf("%w: exchange_rate must be positive for non-CNY currency", ErrValidation)
		}
		// amountLocal = original-currency total (snapshot)
		amountLocal = totalInOrigCurrency
		// totalAmount = CNY equivalent (report basis)
		totalAmountCNY = amountLocal.Mul(exchangeRate).Round(4)
	}

	billID := uuid.New()
	now := time.Now().UTC()

	var out CreatePurchaseDraftOutput

	if err := uc.repo.WithTx(ctx, func(tx *sql.Tx) error {
		billNo, err := uc.repo.NextBillNo(ctx, tx, req.TenantID, "PO")
		if err != nil {
			return fmt.Errorf("create purchase draft: generate bill_no: %w", err)
		}

		head := &domain.BillHead{
			ID:              billID,
			TenantID:        req.TenantID,
			BillNo:          billNo,
			BillType:        domain.BillTypePurchase,
			SubType:         domain.BillSubTypePurchase,
			Status:          domain.StatusDraft,
			PartnerID:       req.PartnerID,
			WarehouseID:     req.WarehouseID,
			CreatorID:       req.CreatorID,
			BillDate:        req.BillDate,
			Subtotal:        subtotal,
			ShippingFee:     shippingFee,
			TaxAmount:       taxAmount,
			TotalAmount:     totalAmountCNY,
			Currency:        currency,
			ExchangeRateVal: exchangeRate,
			AmountLocal:     amountLocal,
			Remark:          req.Remark,
			CreatedAt:       now,
			UpdatedAt:       now,
		}

		for _, item := range items {
			item.HeadID = billID
		}

		if err := uc.repo.CreateBill(ctx, tx, head, items); err != nil {
			return fmt.Errorf("create purchase draft: persist bill: %w", err)
		}
		out.BillID = billID
		out.BillNo = billNo
		return nil
	}); err != nil {
		return nil, err
	}

	return &out, nil
}
