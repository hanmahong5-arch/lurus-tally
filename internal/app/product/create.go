// Package product implements use cases for the product catalogue.
package product

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/app/entitlement"
	appsku "github.com/hanmahong5-arch/lurus-tally/internal/app/sku"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/product"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/codegen"
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

// SaveError is a create/update validation failure with a plain Chinese message.
type SaveError struct{ Msg string }

func (e *SaveError) Error() string { return e.Msg }

// TxRunner runs fn atomically: repositories called with the context it passes
// share one database transaction (commit on nil, rollback on error). nil means
// "no transaction" and fn runs directly.
type TxRunner interface {
	InTx(ctx context.Context, fn func(ctx context.Context) error) error
}

func runAtomic(tx TxRunner, ctx context.Context, fn func(ctx context.Context) error) error {
	if tx == nil {
		return fn(ctx)
	}
	return tx.InTx(ctx, fn)
}

// CreateUseCase creates a new product in the catalogue.
type CreateUseCase struct {
	repo   Repository
	codes  codegen.Source
	prices appsku.DefaultSKUWriter
	tx     TxRunner
	guard  PlanGuard
}

// PlanGuard checks the plan's SKU limit before products are created.
// *entitlement.Guard satisfies it; nil means no limit is applied.
type PlanGuard interface {
	Check(ctx context.Context, tenantID uuid.UUID, key string, delta int) error
}

// WithPlanGuard enables the max_skus plan check on single creates.
func (uc *CreateUseCase) WithPlanGuard(g PlanGuard) *CreateUseCase {
	uc.guard = g
	return uc
}

// NewCreateUseCase constructs the use case with its required repository.
func NewCreateUseCase(repo Repository) *CreateUseCase {
	return &CreateUseCase{repo: repo, codes: codegen.Default()}
}

// WithCodeSource replaces the clock/randomness used for generated codes (tests).
func (uc *CreateUseCase) WithCodeSource(s codegen.Source) *CreateUseCase {
	uc.codes = s
	return uc
}

// WithPriceWriter attaches the default-SKU price writer used by ExecuteWithPrices.
func (uc *CreateUseCase) WithPriceWriter(w appsku.DefaultSKUWriter) *CreateUseCase {
	uc.prices = w
	return uc
}

// WithTx makes the product row and its default SKU commit (or roll back) together.
func (uc *CreateUseCase) WithTx(tx TxRunner) *CreateUseCase {
	uc.tx = tx
	return uc
}

// ExecuteWithPrices creates the product and, when any price is given, upserts its
// default SKU (retail / wholesale / purchase) in the same transaction. Prices are
// validated before anything is written (failure is an *appsku.PriceError with a plain
// Chinese message); blank prices leave the SKU absent so the POS falls back to manual entry.
func (uc *CreateUseCase) ExecuteWithPrices(ctx context.Context, in domain.CreateInput, pi appsku.PriceInput) (*domain.Product, error) {
	prices, err := appsku.ParsePrices(pi)
	if err != nil {
		return nil, err
	}
	return uc.create(ctx, in, prices)
}

// Execute validates the input and persists the new product (no prices).
// An empty Code is server-assigned (P + yymmdd + 4 symbols, see codegen) and
// regenerated on a unique violation; an explicit Code is used verbatim.
// Returns the created product on success.
func (uc *CreateUseCase) Execute(ctx context.Context, in domain.CreateInput) (*domain.Product, error) {
	return uc.create(ctx, in, appsku.DefaultPrices{})
}

func (uc *CreateUseCase) create(ctx context.Context, in domain.CreateInput, prices appsku.DefaultPrices) (*domain.Product, error) {
	if in.TenantID == uuid.Nil {
		return nil, fmt.Errorf("create product: tenant_id is required")
	}
	if in.Name == "" {
		return nil, fmt.Errorf("create product: name is required")
	}

	if uc.guard != nil {
		if err := uc.guard.Check(ctx, in.TenantID, entitlement.KeyMaxSKUs, 1); err != nil {
			return nil, err
		}
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

	// Each attempt is its own transaction: a unique violation aborts the tx, so the
	// regenerated-code retry must start a fresh one.
	err := uc.codes.Create(codegen.PrefixProduct, in.Code, codegen.IsUniqueViolation, func(code string) error {
		p.Code = code
		return runAtomic(uc.tx, ctx, func(ctx context.Context) error {
			if err := uc.repo.Create(ctx, p); err != nil {
				return err
			}
			if uc.prices != nil && !prices.IsEmpty() {
				if err := uc.prices.UpsertDefaultSKU(ctx, p.TenantID, p.ID, prices); err != nil {
					return fmt.Errorf("save prices: %w", err)
				}
			}
			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("create product: %w", err)
	}
	return p, nil
}
