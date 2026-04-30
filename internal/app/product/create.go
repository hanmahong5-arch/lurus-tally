// Package product implements use cases for the product catalogue.
package product

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/product"
)

// Repository defines the persistence interface required by product use cases.
// Implementations live in internal/adapter/repo/product/.
type Repository interface {
	Create(ctx context.Context, p *domain.Product) error
	GetByID(ctx context.Context, tenantID, id uuid.UUID) (*domain.Product, error)
	List(ctx context.Context, filter domain.ListFilter) ([]*domain.Product, int, error)
	Update(ctx context.Context, p *domain.Product) error
	Delete(ctx context.Context, tenantID, id uuid.UUID) error
	// Restore un-deletes a soft-deleted product. Returns ErrNotFound if the product does
	// not exist, is not owned by the tenant, or has not been soft-deleted.
	Restore(ctx context.Context, tenantID, id uuid.UUID) (*domain.Product, error)
}

// CreateUseCase creates a new product in the catalogue.
type CreateUseCase struct {
	repo Repository
}

// NewCreateUseCase constructs the use case with its required repository.
func NewCreateUseCase(repo Repository) *CreateUseCase {
	return &CreateUseCase{repo: repo}
}

// Execute validates the input and persists the new product.
// Returns the created product on success.
func (uc *CreateUseCase) Execute(ctx context.Context, in domain.CreateInput) (*domain.Product, error) {
	if in.TenantID == uuid.Nil {
		return nil, fmt.Errorf("create product: tenant_id is required")
	}
	if in.Code == "" {
		return nil, fmt.Errorf("create product: code is required")
	}
	if in.Name == "" {
		return nil, fmt.Errorf("create product: name is required")
	}

	strategy := in.MeasurementStrategy
	if strategy == "" {
		strategy = domain.StrategyIndividual
	}
	if err := strategy.Validate(); err != nil {
		return nil, fmt.Errorf("create product: %w", err)
	}

	attrs := in.Attributes
	if len(attrs) == 0 {
		attrs = json.RawMessage("{}")
	}

	now := time.Now().UTC()
	p := &domain.Product{
		ID:                  uuid.New(),
		TenantID:            in.TenantID,
		CategoryID:          in.CategoryID,
		Code:                in.Code,
		Name:                in.Name,
		Manufacturer:        in.Manufacturer,
		Model:               in.Model,
		Spec:                in.Spec,
		Brand:               in.Brand,
		Mnemonic:            in.Mnemonic,
		Color:               in.Color,
		ExpiryDays:          in.ExpiryDays,
		WeightKg:            in.WeightKg,
		Enabled:             true,
		EnableSerialNo:      in.EnableSerialNo,
		EnableLotNo:         in.EnableLotNo,
		ShelfPosition:       in.ShelfPosition,
		ImgURLs:             in.ImgURLs,
		Remark:              in.Remark,
		MeasurementStrategy: strategy,
		DefaultUnitID:       in.DefaultUnitID,
		Attributes:          attrs,
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	if err := uc.repo.Create(ctx, p); err != nil {
		return nil, fmt.Errorf("create product: %w", err)
	}
	return p, nil
}
