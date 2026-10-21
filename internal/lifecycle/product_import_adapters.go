package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
	repounit "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/unit"
	repowarehouse "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/warehouse"
	appproduct "github.com/hanmahong5-arch/lurus-tally/internal/app/product"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	appwarehouse "github.com/hanmahong5-arch/lurus-tally/internal/app/warehouse"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
	domainunit "github.com/hanmahong5-arch/lurus-tally/internal/domain/unit"
	domainwh "github.com/hanmahong5-arch/lurus-tally/internal/domain/warehouse"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/codegen"
)

// product_import_adapters.go bridges the product bulk-import ports (units, warehouses,
// opening stock) to the existing repositories and the stock movement use case. Kept in
// the wiring layer so app/product stays free of adapter and stock imports.

// importUnits resolves unit names for the import: system and tenant units alike.
type importUnits struct{ repo *repounit.Repo }

func (a importUnits) UnitIDsByName(ctx context.Context, tenantID uuid.UUID) (map[string]uuid.UUID, error) {
	units, err := a.repo.List(ctx, domainunit.ListFilter{TenantID: tenantID})
	if err != nil {
		return nil, err
	}
	out := make(map[string]uuid.UUID, len(units)*2)
	for _, u := range units {
		out[strings.ToLower(u.Name)] = u.ID
		if _, ok := out[strings.ToLower(u.Code)]; !ok {
			out[strings.ToLower(u.Code)] = u.ID
		}
	}
	return out, nil
}

// CreateUnit adds a tenant unit named name (type count). The code defaults to the name
// itself; a code clash with a different unit falls back to a short suffixed code.
func (a importUnits) CreateUnit(ctx context.Context, tenantID uuid.UUID, name string) (uuid.UUID, error) {
	now := time.Now().UTC()
	u := &domainunit.UnitDef{
		ID: uuid.New(), TenantID: tenantID, Code: truncRunes(name, 20), Name: name,
		UnitType: domainunit.UnitTypeCount, CreatedAt: now, UpdatedAt: now,
	}
	err := a.repo.Create(ctx, u)
	if codegen.IsUniqueViolation(err) {
		u.ID = uuid.New()
		u.Code = truncRunes(name, 14) + "-" + u.ID.String()[:5]
		err = a.repo.Create(ctx, u)
	}
	if err != nil {
		return uuid.Nil, err
	}
	return u.ID, nil
}

func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// importWarehouses resolves warehouse names and the tenant default.
type importWarehouses struct{ repo *repowarehouse.Repo }

func (a importWarehouses) WarehousesByName(ctx context.Context, tenantID uuid.UUID) (map[string]uuid.UUID, uuid.UUID, error) {
	items, _, err := a.repo.List(ctx, domainwh.ListFilter{TenantID: tenantID, Limit: 1000})
	if err != nil {
		return nil, uuid.Nil, err
	}
	byName := make(map[string]uuid.UUID, len(items))
	for _, w := range items {
		byName[strings.ToLower(w.Name)] = w.ID
	}
	def, err := a.repo.DefaultWarehouseID(ctx, tenantID)
	if errors.Is(err, appwarehouse.ErrNotFound) {
		return byName, uuid.Nil, nil
	}
	if err != nil {
		return nil, uuid.Nil, err
	}
	return byName, def, nil
}

// importOpeningStock books opening balances as stock-in movements with reference_type=init,
// inside the transaction the import opened for the row (see dbscope.InTx).
type importOpeningStock struct {
	uc *appstock.RecordMovementUseCase
}

func (a importOpeningStock) RecordOpening(ctx context.Context, in appproduct.OpeningStockInput) error {
	tx := dbscope.TxFrom(ctx)
	if tx == nil {
		return fmt.Errorf("opening stock: no transaction in context")
	}
	_, err := a.uc.ExecuteInTx(ctx, tx, appstock.RecordMovementRequest{
		TenantID:      in.TenantID,
		ProductID:     in.ProductID,
		WarehouseID:   in.WarehouseID,
		Direction:     domainstock.DirectionIn,
		Qty:           in.Qty,
		UnitCost:      in.UnitCost,
		ReferenceType: domainstock.RefInit,
		Note:          "期初建账",
	})
	return err
}
