package product

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/product"
)

// UpdateUseCase modifies an existing product.
type UpdateUseCase struct {
	repo Repository
}

// NewUpdateUseCase constructs the use case.
func NewUpdateUseCase(repo Repository) *UpdateUseCase {
	return &UpdateUseCase{repo: repo}
}

// Execute applies the non-zero fields in UpdateInput to the product and persists the change.
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
		p.Attributes = in.Attributes
	}
	p.UpdatedAt = time.Now().UTC()

	if err := uc.repo.Update(ctx, p); err != nil {
		return nil, fmt.Errorf("update product: %w", err)
	}
	return p, nil
}
