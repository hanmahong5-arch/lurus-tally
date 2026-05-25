package importing_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	appimporting "github.com/hanmahong5-arch/lurus-tally/internal/app/importing"
)

// stubWarehouseChecker records the (tenant, warehouse) pair it was asked to validate
// and returns errNotMine when the pair does not match the seeded tenant.
type stubWarehouseChecker struct {
	allowedTenant    uuid.UUID
	allowedWarehouse uuid.UUID
	called           bool
}

var errNotMine = errors.New("warehouse not found in tenant")

func (s *stubWarehouseChecker) BelongsToTenant(_ context.Context, tenantID, warehouseID uuid.UUID) error {
	s.called = true
	if tenantID == s.allowedTenant && warehouseID == s.allowedWarehouse {
		return nil
	}
	return errNotMine
}

// TestImport_RejectsCrossTenantWarehouse covers UAT-1 P0-2: a caller passing
// another tenant's warehouse_id must be rejected before any sale bill is created.
func TestImport_RejectsCrossTenantWarehouse(t *testing.T) {
	tenantA := uuid.New()
	warehouseB := uuid.New() // belongs to a different tenant
	check := &stubWarehouseChecker{
		allowedTenant:    tenantA,
		allowedWarehouse: uuid.New(), // tenant A's real warehouse — not the one we pass in
	}

	uc := appimporting.NewImportOrdersUseCase(
		newMockRepo(),
		&mockCreator{},
		&mockApprover{},
		newMockStockChecker(),
		check,
		newMockRater(),
		"CNY",
	)

	csv := "order-id,sku,quantity-purchased,item-price,currency,purchase-date\n" +
		"ORD-001,SKU-A,1,9.99,USD,2026-01-15\n"

	_, err := uc.Execute(context.Background(), appimporting.ImportRequest{
		TenantID:    tenantA,
		CreatorID:   uuid.New(),
		WarehouseID: warehouseB, // <-- the cross-tenant warehouse
		Platform:    appimporting.PlatformAmazon,
		CSVData:     []byte(csv),
	})
	if err == nil {
		t.Fatal("expected cross-tenant warehouse to be rejected, got nil error")
	}
	if !errors.Is(err, errNotMine) {
		t.Fatalf("expected wrapped errNotMine, got: %v", err)
	}
	if !strings.Contains(err.Error(), "warehouse not in tenant") {
		t.Errorf("expected import error to surface the rejection reason, got: %v", err)
	}
	if !check.called {
		t.Error("WarehouseChecker.BelongsToTenant was never called")
	}
}

// TestImport_AcceptsOwnWarehouse confirms the gate does not over-reject
// legitimate same-tenant warehouse_id.
func TestImport_AcceptsOwnWarehouse(t *testing.T) {
	tenantA := uuid.New()
	warehouseA := uuid.New()
	check := &stubWarehouseChecker{allowedTenant: tenantA, allowedWarehouse: warehouseA}

	repo := newMockRepo()
	sku := uuid.New()
	repo.addMapping("amazon", "SKU-A", sku)

	uc := appimporting.NewImportOrdersUseCase(
		repo,
		&mockCreator{},
		&mockApprover{},
		newMockStockChecker(),
		check,
		newMockRater(),
		"CNY",
	)

	csv := "order-id,sku,quantity-purchased,item-price,currency,purchase-date\n" +
		"ORD-001,SKU-A,1,9.99,USD,2026-01-15\n"

	_, err := uc.Execute(context.Background(), appimporting.ImportRequest{
		TenantID:    tenantA,
		CreatorID:   uuid.New(),
		WarehouseID: warehouseA,
		Platform:    appimporting.PlatformAmazon,
		CSVData:     []byte(csv),
	})
	if err != nil {
		t.Fatalf("expected own-tenant warehouse to be accepted, got: %v", err)
	}
	if !check.called {
		t.Error("WarehouseChecker.BelongsToTenant was never called")
	}
}
