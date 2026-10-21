package warehouse

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/app/entitlement"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/warehouse"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/codegen"
)

// CreateUseCase creates a new warehouse.
type CreateUseCase struct {
	repo  Repository
	codes codegen.Source
	guard PlanGuard
}

// PlanGuard checks the plan's warehouse limit before a warehouse is created.
// *entitlement.Guard satisfies it; nil means no limit is applied.
type PlanGuard interface {
	Check(ctx context.Context, tenantID uuid.UUID, key string, delta int) error
}

// WithPlanGuard enables the max_warehouses plan check.
func (uc *CreateUseCase) WithPlanGuard(g PlanGuard) *CreateUseCase {
	uc.guard = g
	return uc
}

// NewCreateUseCase constructs a CreateUseCase.
func NewCreateUseCase(repo Repository) *CreateUseCase {
	return &CreateUseCase{repo: repo, codes: codegen.Default()}
}

// WithCodeSource replaces the clock/randomness used for generated codes (tests).
func (uc *CreateUseCase) WithCodeSource(s codegen.Source) *CreateUseCase {
	uc.codes = s
	return uc
}

// Execute validates the input and creates a new Warehouse.
// An empty Code is server-assigned (W + yymmdd + 4 symbols, see codegen).
// tally.warehouse has no unique index on code (only ux_warehouse_tenant_name),
// so the only unique violation a create can hit is the name; redrawing the code
// cannot cure that, hence no conflict is treated as retryable here.
func (uc *CreateUseCase) Execute(ctx context.Context, in domain.CreateInput) (*domain.Warehouse, error) {
	now := time.Now().UTC()
	w := &domain.Warehouse{
		ID:        uuid.New(),
		TenantID:  in.TenantID,
		Code:      in.Code,
		Name:      in.Name,
		Address:   in.Address,
		Manager:   in.Manager,
		IsDefault: in.IsDefault,
		Remark:    in.Remark,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := w.Validate(); err != nil {
		return nil, fmt.Errorf("warehouse create validate: %w", err)
	}
	if uc.guard != nil {
		if err := uc.guard.Check(ctx, in.TenantID, entitlement.KeyMaxWarehouses, 1); err != nil {
			return nil, err
		}
	}
	noCodeConflict := func(error) bool { return false }
	err := uc.codes.Create(codegen.PrefixWarehouse, in.Code, noCodeConflict, func(code string) error {
		w.Code = code
		return uc.repo.Create(ctx, w)
	})
	if err != nil {
		if errors.Is(err, ErrDuplicateName) {
			return nil, ErrDuplicateName
		}
		return nil, fmt.Errorf("warehouse create: %w", err)
	}
	return w, nil
}
