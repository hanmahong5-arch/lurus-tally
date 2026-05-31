//go:build integration

// Package integration — FIFO lot-ordering invariant test.
//
// WHY THIS TEST EXISTS
// --------------------
// The FIFO consumed-lot cost is the trickiest money calculation in the system:
// it feeds margin, valuation, and tax. Until now it was only verified against a
// mock repo (internal/app/stock/calc_fifo_test.go) whose ListActiveLots returns
// lots in INSERT order — which, in those fixtures, happens to coincide with
// received_at order. That coincidence hides a whole class of bug: if the real
// SQL ever stopped honouring `ORDER BY received_at ASC`, the mock-backed tests
// would still pass while goods got mis-costed in production.
//
// This test drives the REAL RecordMovementUseCase (internal/app/stock,
// NewRecordMovementUseCase) backed by the REAL stock repo
// (internal/adapter/repo/stock.New) against a REAL Postgres container, and
// deliberately arranges fixtures so INSERT order and received_at order DISAGREE.
// The assertion is on the real computed money values (the outbound movement's
// unit_cost / total_cost), proving lots are consumed in received_at order, not
// insert order.
//
// Run:
//
//	go test -tags integration -run TestFIFOLotOrdering ./tests/integration/ -timeout 360s -v
package integration

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"

	repostock "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/stock"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// fifoOrderProfile is a minimal appstock.Profile that forces the FIFO strategy.
// Prefixed with "fifoOrder" to avoid symbol collisions with helpers added by
// other integration-test files compiled into the same package.
type fifoOrderProfile struct{}

func (fifoOrderProfile) InventoryMethod() string { return domainstock.CostStrategyFIFO }

// fifoOrderInboundLot describes one inbound receipt used to build a FIFO lot.
// receivedOffset is the number of days added to a fixed base time to set the
// lot's received_at (the column ListActiveLots orders by). Insert order is the
// slice order; received_at order is determined by receivedOffset — the test
// makes these two orders deliberately disagree.
type fifoOrderInboundLot struct {
	qty            decimal.Decimal
	unitCost       decimal.Decimal
	receivedOffset time.Duration
}

// recordFIFOInbound applies a single inbound movement through the REAL use case,
// setting OccurredAt so that the created lot's received_at is deterministic.
// The FIFO calculator copies Movement.OccurredAt → Lot.ReceivedAt (see
// calc_fifo.go: ReceivedAt: m.OccurredAt), so OccurredAt is how we control the
// FIFO ordering key from the outside.
//
// A reference_id is required: migration 000034 enforces stock_movement.reference_id
// NOT NULL (every legitimate movement has a source purchase bill). We synthesise a
// distinct reference per inbound to model three separate purchase receipts.
func recordFIFOInbound(
	ctx context.Context,
	uc *appstock.RecordMovementUseCase,
	tenantID, productID, warehouseID uuid.UUID,
	lot fifoOrderInboundLot,
	baseTime time.Time,
) error {
	refID := uuid.New()
	_, err := uc.Execute(ctx, appstock.RecordMovementRequest{
		TenantID:      tenantID,
		ProductID:     productID,
		WarehouseID:   warehouseID,
		Direction:     domainstock.DirectionIn,
		Qty:           lot.qty,
		ConvFactor:    "1",
		UnitCost:      lot.unitCost,
		CostStrategy:  domainstock.CostStrategyFIFO,
		ReferenceType: domainstock.RefPurchase,
		ReferenceID:   &refID,
		OccurredAt:    baseTime.Add(lot.receivedOffset),
	})
	if err != nil {
		return fmt.Errorf("record inbound (cost=%s, offset=%s): %w", lot.unitCost, lot.receivedOffset, err)
	}
	return nil
}

// TestFIFOLotOrdering_ConsumesByReceivedAtNotInsertOrder proves, end-to-end
// against real Postgres, that an outbound movement drains lots in received_at
// order — not the order rows were inserted.
//
// FIXTURE (insert order vs received_at order deliberately disagree):
//
//	insert #1: qty 10, cost 30, received_at = T+3d   (newest)
//	insert #2: qty 10, cost 10, received_at = T+1d   (OLDEST)
//	insert #3: qty 10, cost 20, received_at = T+2d   (middle)
//
// Outbound of 15 units. Two competing predictions:
//
//	received_at order (CORRECT, what FIFO means):
//	    consume T+1d/cost-10 (10 units) then T+2d/cost-20 (5 units)
//	    total_cost = 10*10 + 5*20 = 200
//	    unit_cost  = 200 / 15      = 13.333333  (Round(6))
//
//	insert order (the BUG this test guards against):
//	    consume T+3d/cost-30 (10 units) then T+1d/cost-10 (5 units)
//	    total_cost = 10*30 + 5*10 = 350
//	    unit_cost  = 350 / 15      = 23.333333
//
// Asserting on 13.333333 / 200 fails loudly if the real SQL ever stops ordering
// by received_at. The numbers were computed by hand from calc_fifo.go's
// `m.UnitCost = totalCost.Div(m.QtyBase).Round(6)` and `m.TotalCost = totalCost`.
func TestFIFOLotOrdering_ConsumesByReceivedAtNotInsertOrder(t *testing.T) {
	db, cleanup := sqlRealDB(t)
	defer cleanup()

	ctx := context.Background()

	tenantID := insertTenant(t, db, ctx)
	warehouseID := insertWarehouse(t, db, ctx, tenantID)
	productID := insertProduct(t, db, ctx, tenantID, "FIFO Ordering Widget", "FIFO-ORD-1")

	// Real repo + real FIFO calculator + real use case (no mocks).
	repo := repostock.New(db)
	calc := appstock.NewCalculator(fifoOrderProfile{}, repo)
	if calc.Name() != domainstock.CostStrategyFIFO {
		t.Fatalf("calculator strategy = %q, want %q (test must exercise FIFO)", calc.Name(), domainstock.CostStrategyFIFO)
	}
	uc := appstock.NewRecordMovementUseCase(repo, calc, nil, nil)

	// Fixed base time well in the past so all received_at values are stable and
	// strictly ordered regardless of when the test runs.
	baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Insert order: cost 30 (T+3d) → cost 10 (T+1d) → cost 20 (T+2d).
	// received_at order is therefore: cost 10 → cost 20 → cost 30.
	inbounds := []fifoOrderInboundLot{
		{qty: decimal.NewFromInt(10), unitCost: decimal.NewFromInt(30), receivedOffset: 3 * 24 * time.Hour},
		{qty: decimal.NewFromInt(10), unitCost: decimal.NewFromInt(10), receivedOffset: 1 * 24 * time.Hour},
		{qty: decimal.NewFromInt(10), unitCost: decimal.NewFromInt(20), receivedOffset: 2 * 24 * time.Hour},
	}
	for i, in := range inbounds {
		if err := recordFIFOInbound(ctx, uc, tenantID, productID, warehouseID, in, baseTime); err != nil {
			t.Fatalf("inbound #%d: %v", i+1, err)
		}
	}

	// Sanity: 30 units on hand across three lots before the drain.
	snap, err := repo.GetSnapshot(ctx, tenantID, productID, warehouseID)
	if err != nil {
		t.Fatalf("GetSnapshot after inbounds: %v", err)
	}
	if snap == nil {
		t.Fatal("snapshot missing after three inbounds")
	}
	if !snap.OnHandQty.Equal(decimal.NewFromInt(30)) {
		t.Fatalf("on_hand after inbounds = %s, want 30", snap.OnHandQty)
	}

	// Drive a stock-OUT of 15 that must span more than one lot.
	outRef := uuid.New()
	outSnap, err := uc.Execute(ctx, appstock.RecordMovementRequest{
		TenantID:      tenantID,
		ProductID:     productID,
		WarehouseID:   warehouseID,
		Direction:     domainstock.DirectionOut,
		Qty:           decimal.NewFromInt(15),
		ConvFactor:    "1",
		CostStrategy:  domainstock.CostStrategyFIFO,
		ReferenceType: domainstock.RefSale,
		ReferenceID:   &outRef,
		OccurredAt:    baseTime.Add(10 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("outbound Execute: %v", err)
	}

	// ---- Assert the real computed money values ----
	//
	// We read the persisted outbound movement back from the DB (not just the
	// in-memory return) so we are asserting on what was actually written by the
	// real SQL path.
	var unitCostStr, totalCostStr, qtyStr string
	err = db.QueryRowContext(ctx, `
		SELECT unit_cost::text, total_cost::text, qty_base::text
		FROM tally.stock_movement
		WHERE tenant_id = $1 AND product_id = $2 AND direction = 'out' AND reference_id = $3
	`, tenantID, productID, outRef).Scan(&unitCostStr, &totalCostStr, &qtyStr)
	if err != nil {
		t.Fatalf("query outbound movement: %v", err)
	}

	gotUnitCost, err := decimal.NewFromString(unitCostStr)
	if err != nil {
		t.Fatalf("parse persisted unit_cost %q: %v", unitCostStr, err)
	}
	gotTotalCost, err := decimal.NewFromString(totalCostStr)
	if err != nil {
		t.Fatalf("parse persisted total_cost %q: %v", totalCostStr, err)
	}

	// received_at-order expectation (correct FIFO).
	wantTotalCost := decimal.NewFromInt(200)               // 10*10 + 5*20
	wantUnitCost := decimal.RequireFromString("13.333333") // 200 / 15, Round(6)

	// insert-order expectation (the bug we are guarding against) — used only to
	// make a divergence unmistakable in the failure message.
	insertOrderTotalCost := decimal.NewFromInt(350) // 10*30 + 5*10
	insertOrderUnitCost := decimal.RequireFromString("23.333333")

	if !gotTotalCost.Equal(wantTotalCost) {
		t.Errorf(
			"outbound total_cost = %s, want %s (received_at order). "+
				"If this equals %s the SQL is draining lots in INSERT order — FIFO is broken.",
			gotTotalCost, wantTotalCost, insertOrderTotalCost,
		)
	}
	if !gotUnitCost.Equal(wantUnitCost) {
		t.Errorf(
			"outbound unit_cost = %s, want %s (received_at order). "+
				"If this equals %s the SQL is draining lots in INSERT order — FIFO is broken.",
			gotUnitCost, wantUnitCost, insertOrderUnitCost,
		)
	}

	// The use-case return value must agree with what was persisted.
	if outSnap == nil {
		t.Fatal("outbound snapshot return is nil")
	}
	if !outSnap.OnHandQty.Equal(decimal.NewFromInt(15)) {
		t.Errorf("on_hand after outbound = %s, want 15 (30 - 15)", outSnap.OnHandQty)
	}

	// ---- Assert lot-level draining matches received_at order ----
	//
	// Independently confirm which physical lots were drained by inspecting
	// qty_remaining per cost bucket. received_at order predicts:
	//   cost-10 lot: 10 → 0   (fully consumed first)
	//   cost-20 lot: 10 → 5   (partially consumed second)
	//   cost-30 lot: 10 → 10  (untouched, it is the newest)
	type lotRem struct {
		unitCost     decimal.Decimal
		qtyRemaining decimal.Decimal
	}
	rows, err := db.QueryContext(ctx, `
		SELECT unit_cost::text, qty_remaining::text
		FROM tally.stock_lot
		WHERE tenant_id = $1 AND product_id = $2 AND warehouse_id = $3
		ORDER BY received_at ASC
	`, tenantID, productID, warehouseID)
	if err != nil {
		t.Fatalf("query lots: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var lots []lotRem
	for rows.Next() {
		var ucStr, remStr string
		if err := rows.Scan(&ucStr, &remStr); err != nil {
			t.Fatalf("scan lot: %v", err)
		}
		uc, err := decimal.NewFromString(ucStr)
		if err != nil {
			t.Fatalf("parse lot unit_cost %q: %v", ucStr, err)
		}
		rem, err := decimal.NewFromString(remStr)
		if err != nil {
			t.Fatalf("parse lot qty_remaining %q: %v", remStr, err)
		}
		lots = append(lots, lotRem{unitCost: uc, qtyRemaining: rem})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate lots: %v", err)
	}
	if len(lots) != 3 {
		t.Fatalf("expected 3 lots, got %d", len(lots))
	}

	// Lots are ordered by received_at ASC, so: [cost10, cost20, cost30].
	wantRemaining := []struct {
		unitCost     int64
		qtyRemaining int64
	}{
		{unitCost: 10, qtyRemaining: 0},  // oldest, fully drained
		{unitCost: 20, qtyRemaining: 5},  // middle, partially drained
		{unitCost: 30, qtyRemaining: 10}, // newest, untouched
	}
	for i, want := range wantRemaining {
		if !lots[i].unitCost.Equal(decimal.NewFromInt(want.unitCost)) {
			t.Errorf("lot[%d] (received_at order) unit_cost = %s, want %d",
				i, lots[i].unitCost, want.unitCost)
		}
		if !lots[i].qtyRemaining.Equal(decimal.NewFromInt(want.qtyRemaining)) {
			t.Errorf("lot[%d] (cost=%s) qty_remaining = %s, want %d",
				i, lots[i].unitCost, lots[i].qtyRemaining, want.qtyRemaining)
		}
	}

	t.Logf("PASS: outbound 15 consumed received_at order [cost10 x10, cost20 x5] → "+
		"unit_cost=%s total_cost=%s (insert order would have been %s / %s)",
		gotUnitCost, gotTotalCost, insertOrderUnitCost, insertOrderTotalCost)

	// Keep sql import referenced even if the file evolves.
	var _ *sql.DB = db
}
