package product

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/app/entitlement"
	appsku "github.com/hanmahong5-arch/lurus-tally/internal/app/sku"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/product"
)

// UpdateUseCase modifies an existing product.
type UpdateUseCase struct {
	repo   Repository
	prices appsku.DefaultSKUWriter
	tx     TxRunner
	guard  PlanGuard
}

// WithPlanGuard 启用重新启用商品时的 max_skus 套餐检查。
func (uc *UpdateUseCase) WithPlanGuard(g PlanGuard) *UpdateUseCase {
	uc.guard = g
	return uc
}

// NewUpdateUseCase constructs the use case.
func NewUpdateUseCase(repo Repository) *UpdateUseCase {
	return &UpdateUseCase{repo: repo}
}

// WithPriceWriter attaches the default-SKU price writer used by ExecuteWithPrices.
func (uc *UpdateUseCase) WithPriceWriter(w appsku.DefaultSKUWriter) *UpdateUseCase {
	uc.prices = w
	return uc
}

// WithTx makes the product row and its default SKU commit (or roll back) together.
func (uc *UpdateUseCase) WithTx(tx TxRunner) *UpdateUseCase {
	uc.tx = tx
	return uc
}

// ExecuteWithPrices updates the product and upserts the provided prices on its default
// SKU in the same transaction. A nil price is left untouched; a blank price clears it
// (resets to 0). Prices are validated first (*appsku.PriceError, plain Chinese message).
func (uc *UpdateUseCase) ExecuteWithPrices(ctx context.Context, tenantID, id uuid.UUID, in domain.UpdateInput, pi appsku.PriceInput) (*domain.Product, error) {
	prices, err := appsku.ParsePricesForUpdate(pi)
	if err != nil {
		return nil, err
	}
	var p *domain.Product
	err = runAtomic(uc.tx, ctx, func(ctx context.Context) error {
		var err error
		if p, err = uc.Execute(ctx, tenantID, id, in); err != nil {
			return err
		}
		if uc.prices != nil && !prices.IsEmpty() {
			if err := uc.prices.UpsertDefaultSKU(ctx, tenantID, p.ID, prices); err != nil {
				return fmt.Errorf("update product: save prices: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return p, nil
}

// Execute applies the non-zero fields in UpdateInput to the product and persists the change.
// Attributes are merged key by key into the stored object (null removes a key), never replaced.
func (uc *UpdateUseCase) Execute(ctx context.Context, tenantID, id uuid.UUID, in domain.UpdateInput) (*domain.Product, error) {
	if tenantID == uuid.Nil {
		return nil, fmt.Errorf("update product: tenant_id is required")
	}

	p, err := uc.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, fmt.Errorf("update product: %w", err)
	}

	if in.CategoryID != nil {
		p.CategoryID = in.CategoryID
	}
	if in.Name != "" {
		p.Name = in.Name
	}
	if in.Manufacturer != "" {
		p.Manufacturer = in.Manufacturer
	}
	if in.Model != "" {
		p.Model = in.Model
	}
	if in.Spec != "" {
		p.Spec = in.Spec
	}
	if in.Brand != "" {
		p.Brand = in.Brand
	}
	if in.Mnemonic != "" {
		p.Mnemonic = in.Mnemonic
	}
	if in.Color != "" {
		p.Color = in.Color
	}
	if in.ExpiryDays != nil {
		p.ExpiryDays = in.ExpiryDays
	}
	if in.WeightKg != nil {
		p.WeightKg = in.WeightKg
	}
	if in.Enabled != nil {
		// 停用 -> 启用会重新占用 max_skus 额度（用量只统计在用商品），需先过套餐检查。
		if *in.Enabled && !p.Enabled && uc.guard != nil {
			if err := uc.guard.Check(ctx, tenantID, entitlement.KeyMaxSKUs, 1); err != nil {
				return nil, err
			}
		}
		p.Enabled = *in.Enabled
	}
	if in.EnableSerialNo != nil {
		p.EnableSerialNo = *in.EnableSerialNo
	}
	if in.EnableLotNo != nil {
		p.EnableLotNo = *in.EnableLotNo
	}
	if in.ShelfPosition != "" {
		p.ShelfPosition = in.ShelfPosition
	}
	if len(in.ImgURLs) > 0 {
		p.ImgURLs = in.ImgURLs
	}
	if in.Remark != "" {
		p.Remark = in.Remark
	}
	if in.MeasurementStrategy != "" {
		if err := in.MeasurementStrategy.Validate(); err != nil {
			return nil, fmt.Errorf("update product: %w", err)
		}
		p.MeasurementStrategy = in.MeasurementStrategy
	}
	if in.DefaultUnitID != nil {
		p.DefaultUnitID = in.DefaultUnitID
	}
	if len(in.Attributes) > 0 {
		merged, err := mergeAttributes(p.Attributes, in.Attributes)
		if err != nil {
			return nil, fmt.Errorf("update product: %w", err)
		}
		p.Attributes = merged
	}
	p.UpdatedAt = time.Now().UTC()

	if err := uc.repo.Update(ctx, p); err != nil {
		return nil, fmt.Errorf("update product: %w", err)
	}
	return p, nil
}

// mergeAttributes overlays patch onto base key by key. A null value removes the key;
// so does a blank barcode (the POS looks products up by attributes.barcode, and an
// empty string would only pollute duplicate checks).
func mergeAttributes(base, patch json.RawMessage) (json.RawMessage, error) {
	merged := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(base)) > 0 {
		if err := json.Unmarshal(base, &merged); err != nil || merged == nil {
			merged = map[string]json.RawMessage{}
		}
	}
	var over map[string]json.RawMessage
	if err := json.Unmarshal(patch, &over); err != nil {
		return nil, &SaveError{Msg: "商品的附加信息格式不正确，请刷新页面后重试"}
	}
	for k, v := range over {
		t := bytes.TrimSpace(v)
		if string(t) == "null" || (k == "barcode" && string(t) == `""`) {
			delete(merged, k)
			continue
		}
		merged[k] = v
	}
	out, err := json.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("attributes: %w", err)
	}
	return out, nil
}
