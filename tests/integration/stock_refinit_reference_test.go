//go:build integration

// Package integration — RefInit reference_id self-reference regression test.
//
// WHY THIS TEST EXISTS
// --------------------
// Onboarding seed-demo (internal/adapter/handler/onboarding/handler.go,
// stockAdapter) records the opening stock for each demo SKU as an init movement
// (ReferenceType = "init") but supplies NO ReferenceID — an init balance has no
// source business document. Migration 000034 made stock_movement.reference_id
// NOT NULL, so that INSERT failed with SQLSTATE 23502 (not_null_violation),
// surfacing as a 500 on POST /api/v1/onboarding/seed-demo for EVERY new tenant
// (UAT finding #1 — onboarding fully broken).
//
// The fix (internal/app/stock/usecase.go): when ReferenceType == RefInit and
// ReferenceID is nil, the movement self-references its own id. This test drives
// the REAL RecordMovementUseCase + real repo against a REAL Postgres container
// (so the NOT NULL constraint is actually enforced) and asserts the init
// movement persists with a non-null, self-referencing reference_id.
//
// Run:
//
//	go test -tags integration -run TestRecordMovement_RefInitNilReference ./tests/integration/ -timeout 360s -v
package integration

import (
	"context"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"

	repostock "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/stock"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// refInitWACProfile forces the WAC strategy (what onboarding seed-demo uses).
// Prefixed to avoid symbol collisions with helpers in other files of this package.
type refInitWACProfile struct{}

func (refInitWACProfile) InventoryMethod() string { return domainstock.CostStrategyWAC }

// TestRecordMovement_RefInitNilReference_SelfReferences proves that an init
// movement created WITHOUT an explicit reference_id persists successfully (and
// self-references its own id) instead of failing the migration-000034 NOT NULL
// constraint. Before the usecase guard this INSERT raised SQLSTATE 23502 and the
// seed-demo endpoint 500'd for every new tenant.
func TestRecordMovement_RefInitNilReference_SelfReferences(t *testing.T) {
	db, cleanup := sqlRealDB(t)
	defer cleanup()

	ctx := context.Background()

	tenantID := insertTenant(t, db, ctx)
	warehouseID := insertWarehouse(t, db, ctx, tenantID)
	productID := insertProduct(t, db, ctx, tenantID, "Seed Init Widget", "SEED-INIT-1")

	repo := repostock.New(db)
	calc := appstock.NewCalculator(refInitWACProfile{}, repo)
	uc := appstock.NewRecordMovementUseCase(repo, calc, nil, nil)

	// Init movement with NO ReferenceID — exactly what the onboarding stock
	// adapter sends. Must NOT error (pre-fix: 23502 not_null_violation -> 500).
	snap, err := uc.Execute(ctx, appstock.RecordMovementRequest{
		TenantID:      tenantID,
		ProductID:     productID,
		WarehouseID:   warehouseID,
		Direction:     domainstock.DirectionIn,
		Qty:           decimal.NewFromInt(50),
		ConvFactor:    "1",
		UnitCost:      decimal.NewFromInt(7),
		CostStrategy:  domainstock.CostStrategyWAC,
		ReferenceType: domainstock.RefInit,
		// ReferenceID deliberately omitted (nil) — the bug trigger.
	})
	if err != nil {
		t.Fatalf("RefInit movement with nil reference_id must succeed (seed-demo P0); got: %v", err)
	}
	if snap == nil {
		t.Fatal("snapshot return is nil after init movement")
	}
	if !snap.OnHandQty.Equal(decimal.NewFromInt(50)) {
		t.Fatalf("on_hand after init = %s, want 50", snap.OnHandQty)
	}

	// The persisted movement must carry a non-null reference_id that
	// self-references the movement's own id (the guard's contract).
	var movID, refID uuid.UUID
	err = db.QueryRowContext(ctx, `
		SELECT id, reference_id
		FROM tally.stock_movement
		WHERE tenant_id = $1 AND product_id = $2 AND reference_type = 'init'
	`, tenantID, productID).Scan(&movID, &refID)
	if err != nil {
		t.Fatalf("query persisted init movement: %v", err)
	}
	if refID == uuid.Nil {
		t.Fatal("persisted reference_id is NULL/zero — guard did not populate it")
	}
	if refID != movID {
		t.Errorf("reference_id = %s, want self-reference to movement id %s", refID, movID)
	}

	t.Logf("PASS: RefInit movement persisted with self-referencing reference_id=%s", refID)
}
