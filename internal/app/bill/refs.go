package bill

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// validateRefs verifies that every referenced product — and the optional
// bill-level warehouse — exists and belongs to tenantID. PostgreSQL foreign
// keys on bill_item.product_id and bill_head.warehouse_id guarantee the row
// EXISTS, but FK validation bypasses row-level security, so a caller scoped to
// tenant A could otherwise reference tenant B's product/warehouse id. This
// tenant-scoped pre-check closes that cross-tenant hole and surfaces a clean
// ErrValidation (HTTP 400) instead of a raw FK violation surfacing as a 409.
//
// Duplicate product ids are checked once. Nil ids are skipped here — the
// per-item loop in each use case already rejects them with a clearer message.
func validateRefs(ctx context.Context, repo BillRepo, tenantID uuid.UUID, productIDs []uuid.UUID, warehouseID *uuid.UUID) error {
	seen := make(map[uuid.UUID]struct{}, len(productIDs))
	for _, pid := range productIDs {
		if pid == uuid.Nil {
			continue
		}
		if _, dup := seen[pid]; dup {
			continue
		}
		seen[pid] = struct{}{}

		ok, err := repo.ProductExists(ctx, tenantID, pid)
		if err != nil {
			return fmt.Errorf("validate product reference %s: %w", pid, err)
		}
		if !ok {
			return fmt.Errorf("%w: product %s does not exist in this tenant", ErrValidation, pid)
		}
	}

	if warehouseID != nil && *warehouseID != uuid.Nil {
		ok, err := repo.WarehouseExists(ctx, tenantID, *warehouseID)
		if err != nil {
			return fmt.Errorf("validate warehouse reference %s: %w", *warehouseID, err)
		}
		if !ok {
			return fmt.Errorf("%w: warehouse %s does not exist in this tenant", ErrValidation, *warehouseID)
		}
	}

	return nil
}
