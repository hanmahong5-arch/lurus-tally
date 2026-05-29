package shopify

import (
	"context"
	"errors"

	"github.com/google/uuid"
	appwarehouse "github.com/hanmahong5-arch/lurus-tally/internal/app/warehouse"
	domainwarehouse "github.com/hanmahong5-arch/lurus-tally/internal/domain/warehouse"
)

// WarehouseGetter is the narrow interface this adapter uses to verify warehouse
// ownership.  *appwarehouse.GetByIDUseCase satisfies this interface.
type WarehouseGetter interface {
	Execute(ctx context.Context, tenantID, id uuid.UUID) (*domainwarehouse.Warehouse, error)
}

// WarehouseCheckerAdapter wraps a WarehouseGetter and implements
// WarehouseChecker.  appwarehouse.ErrNotFound → (false, nil); other error
// propagates.
type WarehouseCheckerAdapter struct {
	get WarehouseGetter
}

// NewWarehouseCheckerAdapter constructs the adapter.
// Pass *appwarehouse.GetByIDUseCase directly.
func NewWarehouseCheckerAdapter(get WarehouseGetter) *WarehouseCheckerAdapter {
	return &WarehouseCheckerAdapter{get: get}
}

// BelongsToTenant returns true when the warehouse is visible to tenantID.
func (a *WarehouseCheckerAdapter) BelongsToTenant(ctx context.Context, tenantID, warehouseID uuid.UUID) (bool, error) {
	_, err := a.get.Execute(ctx, tenantID, warehouseID)
	if err != nil {
		if errors.Is(err, appwarehouse.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
